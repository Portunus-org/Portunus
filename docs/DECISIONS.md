# Decisions

Architecture decision log. Every **[default]** from the build spec is listed
here as accepted, changed, or pending the owner's confirmation. New decisions
get the next number; superseded ones are marked, not deleted.

Status legend: **Accepted** · **Proposed** (needs owner sign-off before the
stage that depends on it) · **Superseded**.

---

## D-01 Toolchain and dependency versions — Accepted (2026-09-24)

Latest stable versions checked at initialization; all pinned (`go.mod`/`go.sum`,
exact versions in `web/package.json` + `pnpm-lock.yaml`, base images by digest
in `deploy/Dockerfile`).

| Component | Version | Notes |
|---|---|---|
| Go | 1.27.1 | |
| sing-box | 1.14.1 | built from source, see D-18 |
| Node.js (build only) | 26.9.0 | not present at runtime |
| pnpm | 12.6.0 | |
| Next.js | 16.3.6 | Turbopack, static export |
| React | 19.3.0 | |
| Tailwind CSS | 4.3.3 | |
| shadcn (CLI) | 4.21.0 | style `radix-nova`, base color neutral. Not installed: run with `npx shadcn@4.21.0 add …`; its `tailwind.css` is copied to `web/app/shadcn.css`. The package and `eslint-config-next` both brought `braces` 3.0.3 (CVE-2026-93687, no fixed release) |
| TypeScript | **6.0.3** | 7.0.2 is out, but `typescript-eslint` 8.x supports `<6.1` only |
| ESLint | **9.39.5** | 10.x is out, but `eslint-plugin-react`, `-import`, `-jsx-a11y` declare `^9` at most |
| modernc.org/sqlite | 1.59.0 | pure-Go SQLite |
| golang.org/x/crypto | 0.57.0 | argon2id |
| Base images | `golang:1.27.1-alpine3.24`, `node:26.9.0-alpine3.24`, `alpine:3.24.2` | pinned by digest |

TypeScript 7 and ESLint 10 will be adopted once the Next.js lint toolchain
supports them.

## D-02 Frontend as a static export embedded in the Go binary — Accepted

The spec default holds. `next build` with `output: "export"` and
`trailingSlash: true` produces plain files in `web/out`. The build copies them to
`server/internal/webui/dist`, where `go:embed` picks them up (embedding only works
inside the Go module), and `internal/webui` serves them. No Node.js runtime in
production: one process and one container, and the UI costs essentially no
RAM beyond the pages kept in memory.

Consequences: no SSR, no Next.js middleware or route handlers. All dynamic
behaviour lives in the Go API. Dynamic routes must be client-side (query
parameters or client state), not `[param]` segments.

`next.config.ts` switches to proxy mode (rewrites `/api/*` → Go) only under
`next dev`.

## D-03 Storage: SQLite (WAL) via a pure-Go driver — Accepted

The spec default holds. `modernc.org/sqlite` instead of `mattn/go-sqlite3` keeps
`CGO_ENABLED=0`: a static binary and a trivial cross-compile. Pragmas:
WAL, `synchronous=NORMAL`, `foreign_keys=ON`, `busy_timeout=5000`,
`BEGIN IMMEDIATE`. Pool of 4 connections.

Migrations are embedded, numbered SQL files (`internal/db/migrations/NNNN_*.sql`),
forward-only, each applied in its own transaction. A binary refuses to start
against a newer schema than it knows.

All SQL lives in `internal/store`; the rest of the code depends on its methods,
not on `database/sql`, so the backend can be replaced.

## D-04 sing-box runs as a supervised child process — Accepted

Portunus runs sing-box as a child process with a generated config file,
rather than linking sing-box as a Go library.

- Linking would couple Portunus releases to sing-box's internal APIs, which
  change between minor versions.
- A separate process isolates crashes, and sing-box can be upgraded
  independently.

Every change is rendered, checked with `sing-box check`, written, and loaded
with SIGHUP (sing-box re-creates its instance in place, same PID). An invalid
config is rejected and the running one kept. If sing-box exits unexpectedly it
is restarted with exponential backoff (1 s → 30 s). With no enabled inbounds
sing-box is stopped. `Pdeathsig` stops it if the panel dies.

A reload closes existing connections. TCP clients reconnect at once; QUIC
clients needed the fix in D-17.

## D-05 One container, host networking — Accepted

`docker-compose.yml` has exactly one service. It uses `network_mode: host`:

- inbounds are created at runtime on arbitrary TCP/UDP ports, and publishing
  them through compose would mean editing compose on every change;
- docker-proxy/NAT on the data path costs CPU at 1 Gbit/s, especially for
  QUIC (Hysteria2, TUIC).

Host networking is also why the panel's loopback-only default (D-06) is
meaningful: `127.0.0.1` inside the container is the host's loopback.

## D-06 Panel exposure: loopback + SSH tunnel by default — Accepted

Default `PORTUNUS_LISTEN=127.0.0.1:2053`. The operator reaches the panel with
`ssh -L 2053:127.0.0.1:2053 user@server`. The panel logs a warning and the UI
shows a banner whenever it listens on a non-loopback address.

Stage 6 adds the second mode from the spec: panel on a secret path behind
the probe-resistant decoy site on :443, with TLS via sing-box ACME.

## D-07 Runtime image: Alpine, non-root — Accepted

Final stage is `alpine` (pinned by digest), not distroless. Stage 2 has to
`setcap cap_net_bind_service=+ep` the sing-box binary so a non-root process can
bind :443, and that is easiest in an image that has a package manager at build
time. The process runs as `portunus` (uid/gid 10001), the root filesystem is
read-only, and all capabilities are dropped.

The sing-box binary carries the file capability `cap_net_bind_service=ep`
(set in the build stage; `COPY` preserves it), and compose adds
`NET_BIND_SERVICE` to the otherwise empty capability set. `no-new-privileges`
is deliberately not set: it disables file capabilities.

Image size: ~93 MB (sing-box ~41 MB, panel ~15 MB, Alpine ~10 MB).

## D-08 Authentication and sessions — Accepted

- Passwords: argon2id, m=19 MiB, t=2, p=1 (OWASP minimum), PHC string format so
  parameters can change later. Concurrent hashes are limited to GOMAXPROCS.
  After hashing, memory is returned to the OS (see D-11).
- Sessions: server-side. The cookie holds 32 random bytes; the database stores
  only their SHA-256. Idle TTL 24 h (sliding, written at most once a minute),
  absolute lifetime 7 days.
- Cookie: `HttpOnly`, `SameSite=Strict`, `Secure` when served over TLS
  (`PORTUNUS_COOKIE_SECURE=auto|always|never`).
- CSRF: per-session token in the `X-Portunus-CSRF` header on every
  state-changing request, plus rejection of cross-origin `Origin` headers and
  a JSON-only request body (a cross-site HTML form cannot reach the API).
- First start creates `admin` with a random password, printed once to the log,
  and flags the account `must_change_password`. Until the password is changed,
  the API refuses everything except `me`, `password` and `logout`, and the UI
  redirects to the account page. Recovery: `portunus reset-password [user]`.
- Failed logins take the same time whether or not the user exists (a dummy hash
  is verified).

## D-09 Login rate limiting — Accepted

In-memory, two layers: a per-IP token bucket (burst 10, refill 5/min) and a
per-username exponential lockout (after 5 failures: 30 s doubling to 15 min).
The client IP is the TCP peer. `X-Forwarded-For` is ignored until an explicit
trusted-proxy setting exists.

## D-10 Configuration via environment only — Accepted

All settings come from `PORTUNUS_*` variables with safe defaults, so the stock
compose file runs unmodified. Reference: `docs/DEPLOY.md`.

## D-11 Memory budget — Accepted

`GOMEMLIMIT=96MiB` (soft) in the image. argon2 allocations are the only large
ones; a debounced `debug.FreeOSMemory()` runs 2 s after the last hash.
Measured in the container: **~15 MB RSS idle**, ~55 MB peak during a login,
back to ~16 MB within 2 s.

## D-12 Design system implementation — Superseded by D-22

- shadcn/ui `radix-nova` style. The only runtime UI dependencies are the ones
  shadcn components are built on: `radix-ui`, `lucide-react`, `sonner`,
  `next-themes`, `class-variance-authority`, `cn`, `tw-animate-css`. No
  third-party component kits or dashboard themes. dashboard-01's demo-only
  dependencies (`@dnd-kit/*`, `@tanstack/react-table`, `recharts`, `vaul`,
  `zod`) were removed together with the demo components. They will be re-added
  through `shadcn add` if a later screen needs them.
- Tokens are shadcn CSS variables in `web/app/globals.css`. The one accent is
  `--signal` (vermilion, `oklch(0.62 0.2 36)` light / `oklch(0.7 0.18 40)` dark).
  `--destructive` aliases it, so critical states use the same single accent.
- Fonts: Fraunces (display), Geist, Geist Mono, all self-hosted at build time
  through `next/font`. Fraunces has no Cyrillic, so Source Serif 4 (Cyrillic
  subset only, not preloaded) follows it in the serif stack.
- Details: `docs/DESIGN.md`.

## D-13 Secrets at rest — Accepted (stage 2)

Inbound key material (REALITY private keys, TLS keys, Shadowsocks server keys)
and the Cloudflare API token are encrypted with AES-256-GCM under a 32-byte
master key. Each ciphertext is bound to its row (`inbound:<id>`) as additional
data, so it cannot be copied to another row. The key comes from
`PORTUNUS_SECRET_KEY_FILE`, or is generated on first start as
`$PORTUNUS_DATA_DIR/master.key` (0600). Other keys (the QUIC stateless-reset
key, D-17) are derived from it with HKDF.

Not encrypted: client UUIDs and passwords. sing-box needs them in its config
file anyway, and they can be rotated per client.

## D-14 Panel ↔ agent control channel — Superseded by D-29

The agent dials the panel (outbound only, no open ports on agents) over HTTPS
with a long-lived WebSocket, authenticated by mTLS. The panel runs a private CA.
At join time the one-time token is exchanged for a client certificate bound to
the node ID. Chosen over gRPC to avoid the protobuf/gRPC dependency weight
(several MB of binary, more RSS) for what is a low-volume message channel.

## D-15 Inter-node data transport — Accepted (VLESS + REALITY hop)

The spec default is WireGuard. The proposal is to **use sing-box itself
(VLESS + REALITY over TCP, `detour` chaining) for entry → exit hops instead**:

1. **Blocking.** WireGuard's handshake is trivially fingerprinted, and
   deployments of this kind (the example rule-list source, itdoginfo/allow-domains,
   targets Russian censorship) sit behind DPI that actively blocks WireGuard.
   A WireGuard link between an entry node inside such a network and an exit node
   outside it gets cut. A REALITY hop looks like TLS to a real site.
2. **Container constraints.** Kernel WireGuard needs `NET_ADMIN`, the host
   module and routing on both ends, which conflicts with the non-root,
   capability-free, one-container model. sing-box's userspace WireGuard runs on a
   gVisor netstack, which costs far more CPU per Gbit than kernel TCP.
3. **Simplicity.** A hop is just another outbound in the generated config.
   Chaining is a `detour`, and health checks and `urltest` apply unchanged.

Cost: TLS on the node-to-node leg (AES-GCM with AES-NI, cheap). Shadowsocks-2022
(`2022-blake3-aes-128-gcm`) is the fallback for node pairs where REALITY
camouflage is unnecessary (both nodes outside the censored network).

## D-16 Repository layout — Accepted

```
server/     Go module (go.mod, go.sum, cmd/, internal/)
web/        Next.js app
deploy/     Dockerfile (+ Dockerfile.dockerignore)
scripts/    build.sh; later the node join script
docs/       documentation
```

The root keeps only what tools and readers expect there: README, LICENSE,
`docker-compose.yml` (so `docker compose up` works from the checkout),
`.env.example`, `.gitignore`. The frontend export is copied into
`server/internal/webui/dist` at build time (`scripts/build.sh`, Dockerfile);
that copy is git-ignored.

## D-17 Stable QUIC stateless-reset key (patched sing-quic) — Accepted

**Problem, measured:** after any config change (even adding a client),
Hysteria2 and TUIC clients stayed broken for 30 s. sing-quic gives every
listener a random stateless-reset key, so after a reload the new instance
cannot produce resets that clients accept, and clients wait for their QUIC
idle timeout.

**Fix:** a small patch (`deploy/patches/sing-quic-stable-stateless-reset.patch`)
lets sing-quic take the key from `SING_QUIC_STATELESS_RESET_KEY`. Portunus
derives it from the master key and passes it to sing-box. The patch also
enables resets for TUIC. Hysteria2 with Salamander keeps them off on purpose,
so an obfuscated server stays silent to unknown packets.

Result (`TestQUICClientsSurviveReload`): clients work again right after the
reload, versus no recovery within 15 s unpatched. The Dockerfile refuses to
build if a sing-box upgrade moves sing-quic away from the patched version.
The change is a candidate to upstream.

## D-18 sing-box built from source — Accepted

The image builds sing-box from the pinned tag (commit verified) instead of
using the release archive:

- per-client traffic needs `with_v2ray_api`, which release builds omit;
- D-17 needs the patched sing-quic;
- unneeded features (tailscale, cloudflared, naive, wireguard…) are left out.

Tags: `with_quic,with_utls,with_acme,with_clash_api,with_v2ray_api`.
Traffic is read over the v2ray stats gRPC API by a minimal hand-written client
(HTTP/2 cleartext and two protobuf messages), which avoids adding grpc-go to
the panel. Counters are collected every 30 s and before every reload.

## D-19 Certificates for TLS inbounds — Accepted

Hysteria2, TUIC and Trojan get their certificate from one of three sources:

1. **Let's Encrypt** through sing-box's ACME provider (certmagic). The domain
   is checked in one of three ways:
   - HTTP-01 on TCP 80, answered by sing-box directly;
   - HTTP-01 behind a reverse proxy: sing-box answers on a local port and the
     proxy that owns port 80 (Nginx, Nginx Proxy Manager, Caddy, Traefik)
     forwards `/.well-known/acme-challenge/` to it. Verified end to end
     against Pebble with real validation through Nginx;
   - DNS-01 through Cloudflare, with no open ports.
   TLS-ALPN is disabled because TCP 443 usually belongs to REALITY.
2. **Certificate files** already managed elsewhere (certbot, NPM, Caddy),
   mounted into the container. sing-box reloads them when they change.
3. **Self-signed** (the default until a domain exists). sing-box profiles pin
   it, and share links carry `insecure=1` + `pinSHA256`.

Many client apps now refuse insecure links, so (1) or (2) is the recommended
setup; links and profiles for them verify certificates normally.

Hysteria2 is not proxied through the reverse proxy itself: it is QUIC/UDP,
which HTTP reverse proxies do not forward.

## D-20 Hysteria2 hardening — Accepted

- **Masquerade:** without it, a plain HTTP/3 request gets a bare
  `404 page not found`, a known Hysteria2 fingerprint. An inbound can proxy
  such requests to a real site instead.
- **Salamander obfuscation** (optional): packets stop looking like QUIC, so
  DPI cannot read SNI from the QUIC Initial. The server then answers nothing
  to non-clients.
- Measured throughput, one connection over loopback with the server pinned
  to one core (Ryzen 7 255): Hysteria2 1.47 Gbit/s, VLESS+REALITY (Vision)
  4.25 Gbit/s, Shadowsocks-2022 10.4 Gbit/s. Reproduce with
  `PORTUNUS_TEST_THROUGHPUT=1` (see `internal/singbox/throughput_test.go`).

## D-21 Clients may not reach the host's private networks — Accepted

The server config resolves destinations first and rejects private and
loopback addresses. With host networking, loopback is where the panel and the
stats API listen. Verified: requests to `127.0.0.1:2053` and
`localhost:2053` through a proxy fail.

## D-22 UI follows dashboard-01 as shipped — Accepted (replaces D-12)

Owner feedback: the custom editorial styling (serif titles, accent markers,
flattened components) drifted from the reference. The UI now uses shadcn/ui
components exactly as the registry ships them (`radix-nova`, neutral) and the
composition of `dashboard-01`:

- inset sidebar: logo and name, primary "New client" button with a secondary
  icon button, main nav, secondary nav at the bottom, user menu with avatar;
- header with the sidebar trigger and the page title;
- section cards with the template's gradient, outline badges and two-line
  footers; tables with view tabs, column picker and pagination; drawers for
  row details;
- status badges like the template's "Done" / "In Process".

Components are not edited locally; `shadcn add … --overwrite` must keep
producing the same result. The only additions are the font wiring (Geist with
Cyrillic) and the handwritten welcome (Great Vibes).

## D-23 Welcome for the basics, a wizard per inbound — Accepted (revised)

After the first login the panel shows a welcome screen (the greeting writes
itself in a script font) that covers only the basics: a password of one's
own and, for admins, where the server is reachable. The public address is
detected (interfaces, or three external lookup services by majority when
behind NAT); the admin says whether there is a domain, and it is checked
against DNS as they type. With a domain, subscriptions are turned on.

Inbounds are then created by a step-by-step wizard (`/inbounds/new/`)
instead of a side drawer; the drawer remains for editing. The server
inspects the host for the chosen protocol (`POST /api/inbounds/suggest`)
and the wizard only asks what that protocol needs:

- **VLESS + REALITY**: camouflage site. Ten well-known sites are handshaked
  from the server; those without TLS 1.3, X25519 and HTTP/2 are marked
  unsuitable, the lowest RTT is preselected;
- **Hysteria2, TUIC, Trojan**: a certificate step with the three sources.
  Let's Encrypt asks for the domain (live DNS check) and shows how it will
  be verified: HTTP-01 direct if port 80 is free, otherwise the web server
  on port 80 is identified from its `Server` header and the wizard shows the
  forwarding snippet with a live check, or Cloudflare DNS with a token.
  The Portunus certificate (D-26) needs nothing. Existing files take paths;
- **Hysteria2**: protection: masquerade site (the fastest probed site) and
  optional Salamander obfuscation;
- **ports**: read from `/proc/net` (host networking) and from our own
  inbounds, so a busy port is skipped (443 → 8443 → …).

The last step creates the inbound and follows through: sing-box applied,
the ACME certificate issued, or, for the Portunus certificate, the CA with
downloads, a QR code and per-platform install steps.

## D-24 Subscription listener managed by the panel — Accepted

The subscription port is a setting (default off, the wizard picks 2096), and
the panel binds it at runtime; `PORTUNUS_SUB_LISTEN` still overrides. The
listener serves HTTP and HTTPS on the same port, choosing by the first byte
of the connection. HTTPS uses the domain's ACME certificate from sing-box's
storage once it exists, so URLs imported before issuance keep working.

## D-25 Interface languages — Accepted

English and Russian, chosen from the browser and switchable in the user
menu. The dictionaries (`web/lib/messages.ts`) are typed from the English
keys, so a missing translation is a type error. Russian plural forms are
handled.

---

## D-26 A private CA for certificates without a domain — Accepted

A TLS inbound without a public certificate used to get a standalone
self-signed certificate, which clients could only pin (and which changed on
every rotation). Each installation now has its own CA (ECDSA P-256, 10
years, created on first use, key encrypted like other secrets). Server
certificates are issued by it for a domain or an IP address, valid 397 days
(Apple rejects longer), renewed 30 days before expiry and whenever the CA
changes. sing-box profiles embed the CA; share links pin the certificate
unless the inbound is marked "devices have the CA installed" (`strictTls`).
The CA certificate (never the key) is downloadable as PEM or DER from the
panel and, when subscriptions are on, from `/ca.crt` on the subscription
listener, with the SHA-256 fingerprint shown for comparison on the device.
Android apps built on sing-box ignore user CAs: they get the profile or the
pinned link.

## D-27 Security scanning of the image — Accepted

Releases must have no HIGH or CRITICAL findings from trivy (image and
repository) and semgrep (`p/default`, `p/golang`, `p/typescript`,
`p/dockerfile`, `p/secrets`). Findings are fixed where real (an open
redirect on directory URLs; the Secure cookie flag now also follows
`X-Forwarded-Proto: https`; the sing-box binary is resolved
to an absolute path once). Reviewed false positives carry a `nosemgrep`
comment with the reason. sing-box's own dependencies are bumped in the
Dockerfile (`SINGBOX_DEP_BUMPS`, pinned) when its release lags behind a
fix; each entry is dropped once sing-box requires that version. The same is
done for the frontend's transitive packages (`overrides` in
`web/pnpm-workspace.yaml`). A finding with no fixed release that affects
build-time tooling only goes into `.trivyignore` with the reason and the
condition for removing it; the image scan takes no exceptions.

## D-28 Documentation inside the panel — Accepted

The user guide ships in the panel (`/docs/`, ten topics) instead of linking
to the repository: it is available on a server without internet access to
GitHub, matches the installed version, follows the interface language and
links straight to the pages it describes. Content is typed data in
`web/lib/docs.ts` (English and Russian side by side) rendered by one
component; topics are prerendered by the static export. The files in
`docs/` stay for deployment and design notes.

## D-29 Nodes: one agent, pushed configuration — Accepted

A node is another server of the installation running the same image as
`portunus agent`, or a third-party proxy imported from a share link. A node
of ours has one or both roles: **exit** (traffic leaves through it) and
**backup entry** (it accepts the same clients on the same ports).

- **Join without a callback.** The panel listens on loopback, so a new
  server cannot fetch anything from it. The join command therefore carries
  everything (`PORTUNUS_JOIN`: the CA certificate and the node's own
  certificate and key, issued by the panel for the node's address) and the
  panel dials the node, not the other way round.
- **Channel.** HTTPS with TLS 1.3 and certificates from the installation's
  CA on both sides. The panel's certificate is for client authentication
  only and the agent checks its subject, so a node, which holds server keys
  from the same CA, cannot command another node. This replaces the
  WebSocket hub proposed in D-14: the panel pushes when something changed
  and polls every 30 s, which needs no long-lived connection.
- **Configuration.** The panel renders the node's whole sing-box config and
  sends it when its hash differs from what the node reports. The agent
  validates it, runs it and saves it, so a restarted node resumes by itself
  and a node keeps serving while the panel is unreachable.
- **Exit.** A VLESS + REALITY inbound with a single user on the node (D-15)
  and a matching outbound on every server that routes through it.
- **Backup entry.** The node gets the main server's inbounds (same ports and
  keys; TLS inbounds with a private certificate for the node's address), the
  active clients and the routing rules. Links and subscriptions list it
  after the main server; sing-box profiles put all entries into one
  `urltest`, so clients move over by themselves. The agent accumulates
  per-client traffic across reloads and the panel collects it into the
  client's counter. Nodes are offered to clients only after they have
  answered once.
- **Scope.** The panel itself is not replicated. A backup entry keeps
  clients connected while the main server is down; changes wait for it to
  return. Restoring the panel is a matter of the data volume backup.
- **Image.** `PORTUNUS_NODE_IMAGE`; with the default local build the panel
  also shows the `docker save | ssh docker load` step.

## D-30 Routing by lists — Accepted

Ordered rules map a list of sites and addresses to a target: direct, a
particular exit, the client's own exit (D-34; until then the fastest working
exit, a sing-box `urltest` over all exits), or block. Traffic no rule matches follows the default target.

- **Lists** are plain text (domain and subdomains, `full:`, `keyword:`,
  addresses and networks) or sing-box rule-sets (`.srs`, `.json`), typed in
  the panel or downloaded once a day by the panel itself and inlined into
  the config. Rule-sets are converted and validated by the sing-box binary
  (`rule-set decompile` / `compile`) at download time, so a broken file is
  refused then and can never fail a later config check. Downloading in the
  panel rather than by sing-box means a failed fetch never stops the proxy
  (the previous copy stays) and backup entries get the same content.
- **One thing to the operator.** In the panel a rule is "what" and "where":
  its list is created, edited and deleted with it. "What" is picked from a
  catalog (addresses by country from sing-geoip, services from sing-geosite,
  the itdoginfo collections), typed, or given as a link. A list's name is
  never "taken": a leftover list gives way and a second rule over the same
  set gets a number.
- **Fail closed.** A rule or default that points at an exit which no longer
  exists or is disabled refuses the traffic instead of sending it directly.
  A node that rules still use cannot be deleted, disabled or stripped of the
  exit role.
- **Two passes around resolve.** sing-box replaces the destination with the
  resolved address, so traffic resolved here reaches an exit as an address.
  Rules that send traffic away are therefore evaluated before resolving
  (matching by name; earlier "direct" rules are excluded with a logical
  rule), then everything that may leave directly is resolved and checked
  against private addresses (D-21), then all rules run again so address
  lists apply. When everything else goes to an exit and no list contains
  addresses, nothing but the "direct" lists is resolved on this server.
- **Exit health** is read from sing-box's control API on loopback
  (`PORTUNUS_SINGBOX_CTL`), enabled only while exits exist.

## D-31 A map of servers — Accepted (revised)

The Nodes page draws the main server, the nodes and their links on a map,
because where the exits are is what the operator reasons about. It is a
real map (Leaflet): it can be dragged and zoomed down to cities and regions.

- **Tiles through the panel.** The browser may talk to nobody but the panel
  (CSP), so the panel fetches tiles (`PORTUNUS_MAP_TILES`, OpenStreetMap by
  default: it needs no key), keeps them in `cache/tiles` in the data volume
  and serves them at `/api/map/tiles/…` to signed-in users. The provider
  sees the server, not the operator. Requests are capped at zoom 12 and
  identify the application, as the tile usage policy asks. With `off`, the
  map shows country outlines bundled with the panel.
- **Look.** Tiles are rendered grey, and inverted in the dark theme, so the
  map sits in the panel's neutral palette. A server is one plain dot whose
  fill is the state; the legend, the label on hover and the table below say
  the same in words. Names are placed on the free side of their dots.
- **Places** come from a geolocation service queried by the panel
  (`PORTUNUS_GEO_LOOKUP`, ipwho.is by default; `off` disables it), once per
  server, never for private addresses. A place set by clicking the map is
  never overwritten.

A first version drew a fixed SVG from bundled outlines; it could not be
moved and showed no cities, which is what a map is for.

## D-32 A change must not take the proxy down — Accepted

`sing-box check` validates syntax, not whether the process can start: a
port held by another program passes the check and then kills sing-box, and
with it every connection. Two layers guard against that.

- **Before saving.** A port is refused when another inbound on the same
  transport has it (TCP and UDP are separate spaces, so VLESS on TCP 443
  next to Hysteria2 on UDP 443 is fine), when the panel itself listens
  there (panel, statistics and control APIs, subscriptions, the port for
  certificate checks, in both directions), when a backup entry needs it for
  its own services, or when anything else on the host listens on it (read
  from `/proc/net`, the inbound's own current port excepted).
- **After applying.** The supervisor keeps the last configuration that ran
  for ten seconds. If sing-box exits within ten seconds of a new one, the
  previous configuration is restored at once, the new one is remembered as
  rejected with sing-box's own reason, and applying it again reports that
  reason instead of repeating the outage. Nodes do the same and report the
  hash of what they really run, so the panel shows the node as having
  rejected the configuration.

Related rules of the same kind: the last exit cannot be deleted or
disabled while something is routed "through the client's exit" (that traffic
would be refused).

Known limit: traffic limits are enforced by polling every 30 s, so a client
can exceed its limit by what passes in that time.

## D-33 Failover timing and what "fastest exit" means — Accepted

Measured on a stand with a main server, two exits, an exit that is also a
backup entry, and a separate backup entry (every rule checked from every
entry over VLESS and Hysteria2):

- **Entries.** Client profiles test their entries every 30 s (was 3 min,
  which left a client offline for three minutes when its entry died; now
  about 30 s), keep an entry unless another is 100 ms faster, and drop
  connections stuck on a dead entry.
- **Exits.** "The fastest exit" is one exit at a time for all traffic routed
  that way, not load balancing: sing-box has no balancer, and spreading
  connections of one session over several addresses breaks sites. It is
  tested every 20 s with the same 100 ms tolerance; a dead exit is left in
  about 20 s. A rule bound to one exit fails closed.
- **Same rules everywhere.** Every entry routes a listed destination to the
  same exit; only direct traffic differs, leaving from the entry itself.
- **Subscriptions** of disabled and expired clients answer 404.

The exit part and the 100 ms tolerance are superseded by D-34.

## D-34 A client keeps one exit — Accepted

"The fastest exit" could change the address sites see for a client whenever
latencies shifted or a single check was lost, and it differed between entry
points (each ran its own `urltest`; an entry that was an exit itself sent
that traffic out directly). Services treat a client whose address keeps
changing as suspicious. So the choice moved from sing-box to the panel:

- **Binding.** Every client has an exit (`clients.exit_node_id`). By
  default it is the fastest exit that answers when the client is bound
  (latency of a small request through the exit, measured from the main
  server); exits within 50 ms of the fastest count as equal and the one
  with the fewest clients is taken, so similar exits share the load. The
  operator can choose any exit instead. The
  route target `auto` now means "through the client's exit": it is rendered
  as rules matching `auth_user` per exit, identical on the main server and
  on every backup entry. On the entry that is that exit, the traffic leaves
  directly (after the private-address guard). A client without an exit is
  refused there.
- **When it changes.** Only when the exit has failed every check for
  2 minutes (at least 3 checks, 30 s apart) and another exit is not given
  up on; when the exit is removed or switched off; when the client's keys
  are reset; or when the operator picks another exit. If every exit is
  down, nobody is moved. Clients do not move back when the old exit
  returns.
- **Cost.** For 2 to 3 minutes (measured: 163 s) the traffic of a dead exit's clients is
  refused (fail closed) instead of moving at once. Moving is done by the
  panel, so exits are not changed while the main server is down, and it is
  judged from the main server's view of the exits. Applying the move
  reloads sing-box, which reconnects everybody once.
- **Entries.** Client profiles keep the entry in use for as long as it
  answers (tolerance raised from 100 ms to 5 s, i.e. never for speed) and
  start with the main server, because direct traffic carries the entry's
  address.
- **Limits.** A client over its traffic limit is cut off everywhere,
  subscription included (404), the same as a disabled or expired one.

## D-35 Nodes run a minimal image from a registry — Accepted

A node used to run the panel's image with `agent` as its command, copied
over by hand (`docker save | ssh docker load`). Now:

- **Its own image** (`--target node`): the `portunus-node` binary (agent
  only, 8 MB, no web interface, database or accounts) and sing-box on
  `scratch`, with the CA roots. No shell or package manager; about 50 MB
  against 67 MB of content for the panel. sing-box is the same binary as in
  the panel's image: a second build without ACME and the control API would
  save a few megabytes at the price of two variants to keep in step.
- **From a registry.** A release publishes `ghcr.io/paulichp/portunus` and
  `ghcr.io/paulichp/portunus-node` with the version as tag; the panel's
  join command names the node image of its own version, so panel and node
  never drift apart. `PORTUNUS_NODE_IMAGE` overrides it.
- **One command.** `sudo docker rm -f portunus-node; sudo docker run -d
  --pull always …`: works for an ordinary user with sudo, can be run again
  (which updates the node), and needs nothing on the server but Docker.
  Installing Docker is offered as a separate command, not folded in:
  piping a script from the network into a root shell should be something
  the operator sees and decides.
- `portunus agent` stays in the panel binary for existing nodes.

## D-36 Losing the main server: backup and move, no hot standby — Accepted

Tested by stopping the main server on a stand: clients carry on through
backup entries (an app on the sing-box core was back in 8 s), nodes survive
a reboot with their saved configuration, but the panel, subscriptions,
limits and the moving of clients between exits stop until a panel is back.

The panel is not replicated. Instead:

- `portunus backup` writes the whole panel (SQLite snapshot by `VACUUM
  INTO`, master key, sing-box's certificates) to stdout while it runs.
- Restored on any server, it is the same panel: it is the panel that calls
  the nodes (mTLS with the installation's CA), so nodes are not tied to its
  address. On the stand all four nodes were online for the restored panel
  within seconds, and it could configure them at once.

A hot standby (a second panel taking over by itself) is not built: two
panels over copies of one database would both push configurations, so it
needs replication and a way to decide who is active. For a panel meant for
one small server, a nightly backup and a two-command restore give most of
the benefit at none of that risk. What is lost is whatever changed since
the last backup.

The first sign-in follows the same idea of not depending on one moment:
the made-up admin password is kept in the data volume and printed on every
start until it is changed, instead of exactly once.

## D-37 Standby nodes: a sealed copy and manual promotion — Accepted

Supersedes the "no standby" part of D-36: a burnt data centre must not
mean a lost installation, and a backup the operator has to remember to
make is not a plan.

- **Copies.** A node marked standby (a role of its own; it may be nothing
  else) gets a backup of the panel over the agent channel: at the next
  round after the rendered configuration changes (something was edited),
  and at least every 5 minutes.
  It is sealed with a recovery key (160 random bits, AES-256-GCM) that the
  panel shows to the admin and the admin keeps elsewhere. The node cannot
  read its copy: a standby does not become as sensitive as the main server.
  Without the key the copy is useless; that is the price.
- **Promotion is a command, not an election.** `portunus restore` on the
  node's server opens the copy and rewrites the database so that this
  server is the main one (`core.PromoteNode`): inbounds take over the
  certificates and keys the server had as a backup entry (existing client
  links and profiles for it keep working byte for byte), the public address
  becomes its address, routes through it become direct, the node row is
  removed. The other nodes need nothing: they trust whoever holds the
  installation's keys.
- **No automatic takeover.** A node cannot tell "the main server is gone"
  from "I cannot reach it", and two panels pushing configurations is worse
  than a panel that is down while clients stay online through backup
  entries. A human decides, once.
- Verified on a stand by deleting the main server with its volume and
  promoting a backup entry: the web interface was up and all nodes online
  in 6 s, and the client configurations made before the loss kept working
  untouched. "Through the promoted server" becomes plain "direct", which
  from another entry means leaving from that entry: there is no node left
  to name. The stand also exposed a race that stored the panel's client
  certificate with another certificate's key when several nodes were
  first contacted at once (fixed: serialised and verified on load).

The compose file shrank to the image, host networking and the volume;
hardening options moved to the documentation, settings to `environment:`.

## D-38 The map is drawn by the panel — Accepted

The map of nodes showed OpenStreetMap's standard tiles turned gray by a CSS
filter: far more detail than a map of a handful of servers needs. A quieter
tile set (Esri's gray canvas) fixed the detail but not two things that come
with any map made of pictures: the sea has the provider's colour, not the
panel's, and on a dense screen the pictures are stretched and blurred.

So the default map is drawn in the browser from Natural Earth country
shapes at 1:50m (the `world-atlas` package, loaded only on that page): land
and borders in colours taken from the theme, the sea simply the card
behind, country and city names set in the panel's type and written as they
are where the place is (a built-in list of about 290 cities, shown as one
zooms in). Links between servers have a pane of their own above the land,
and land lying across the 180th meridian is unwrapped before drawing,
or it would be painted as a band across the whole map. It is sharp at any pixel density,
needs no third party and sends nothing anywhere. It stops at the scale of
countries (zoom 6): the shapes are too coarse beyond that.

Closer than that (to where towns a few kilometres apart stand clear, zoom
13) the same map is drawn from vector tiles in the OpenMapTiles layout:
water, borders of countries and regions, and names of cities, towns and
villages as written locally, painted on a canvas by `protomaps-leaflet` in
the theme's colours at the screen's pixel density. The panel fetches them
(`PORTUNUS_MAP_DETAIL`, by default OpenFreeMap, which needs no key and sets
no limits) and caches them; `off` keeps the map at the scale of countries.
Roads, buildings and land use are deliberately not drawn.

Names are written in dozens of scripts, and a computer without a font for
one (Burmese and Khmer are common gaps) drew rows of boxes. The interface
therefore carries Noto Sans for thirteen scripts (Arabic, Hebrew, Armenian,
Georgian, Devanagari, Bengali, Thai, Lao, Khmer, Myanmar, Ethiopic, Sinhala,
Tamil), about 1 MB in the binary, split by range of letters so that a
browser fetches only what a name on screen needs. Chinese, Japanese and
Korean are left to the system (tens of megabytes). For the few scripts
beyond those, the map measures whether the system can draw them and falls
back to the Latin name.

An open tab keeps the scripts it was loaded with, which made a fixed bug
look unfixed after an upgrade. The interface now names its build
(`/build-id`, a hash of its pages) and a tab reloads itself when the name
changes (on return to the tab, and every five minutes).

Raster tiles remain an option: `PORTUNUS_MAP_TILES` (a template, or
`esri`), with `PORTUNUS_MAP_LABELS` and `PORTUNUS_MAP_CREDIT`. They still
pass through the panel and its cache, kept apart per provider. CARTO's
comparable basemaps answer "API key required".

## D-39 Versions are git tags; images build without emulation — Accepted

A version is `vMAJOR.MINOR.PATCH`, and the tag is the only place it is
written: the release workflow passes the tag to the build, the panel reports
it (Settings) and names its node image by it. The workflow accepts only
tags of that shape.

The Dockerfile's build stages run on the builder's own platform and
cross-compile (`GOOS`/`GOARCH` from the target), and the final stages run
nothing: the service's user is a number (10001) and its data directory is
copied in with its owner and mode instead of being made by `adduser` and
`install`. An ARM image therefore builds on an x86 runner in minutes,
without QEMU. (This also lifts the reason D-07 gave for Alpine in the
panel's image; it stays for the shell.)

## D-40 The panel by its domain, behind a gate — Accepted

The panel listened on loopback only, to be reached through an SSH tunnel.
That stays the default and the way back in, but a panel one opens by typing
`https://domain` is what an operator expects, so the panel can now serve
itself over HTTPS:

- **Its own listener**, managed at run time from the settings (domain,
  port, source of the certificate). No reverse proxy to install.
- **Certificate**: Let's Encrypt through `autocert`, answered on the same
  listener (TLS-ALPN-01), obtained on the first connection and renewed in
  time; or a certificate the operator pastes in. TLS-ALPN-01 is only
  checked on port 443, so Let's Encrypt means the panel holds TCP 443 and a
  TCP inbound needs another port (QUIC inbounds are untouched). Port 80 is
  left alone: sing-box's own ACME client may need it for inbounds.
- **The gate.** A panel on a public port must not be recognisable. Every
  request from a browser that has not opened the secret entry path gets the
  standard library's plain 404, with none of the panel's headers: sign-in
  page, static files and API alike. Without the domain's name in the
  handshake there is no handshake. Opening the path sets a cookie derived
  from the path and the master key; a new path voids it. The gate is not a
  credential (signing in still needs the password); it keeps the sign-in
  page off the internet, and with it password guessing and fingerprinting.
- **Two checks before the address is handed over**: the server connects to
  its own listener and reads the certificate (which is also what makes
  `autocert` ask for it), and the operator's browser fetches a one-off
  address on the listener. The second exists because a fresh DNS record is
  seen by Let's Encrypt long before it is seen everywhere. The first setup
  will not continue past the domain step until the domain points at the
  server, and offers the address only once the browser has reached it.
  (The two pages that run the browser check may `connect-src https:`; no
  other page may talk to anything but the panel.)

Settings lost two fields with addresses in them: "domain" beside "public
address" (one field now: a domain is both) and the manual public URL of
subscriptions.

Not verified against the real Let's Encrypt from the development machine
(it has no public address): issuance is exercised on a real server.

## D-41 One port for the panel and VLESS; a cover site instead of refusals — Accepted

D-40 put the panel on TCP 443 and so took the best port away from VLESS,
and answered strangers with a bare 404. Both were wrong for a tool whose
point is not to stand out.

- **A cover site.** Everybody who has not been through the gate is answered
  by an ordinary site. By default the panel makes it itself (package
  `cover`): a small personal site with a few notes, generated from the
  master key and the domain, so it is stable for one installation, differs
  between installations and needs no setting up. Showing a well-known site
  under another domain, the first default, was itself a tell. The operator
  may still name another site (`decoy_url`); that one is
  fetched and passed on as a reverse proxy would: nothing of ours
  goes upstream (no cookies, no visitor address), and headers that would
  bind the visitor's browser to that site's rules for our domain are
  dropped. A domain with a valid certificate that serves a site is what a
  probe expects to find; a 404 on every path is not.
- **Trojan** gets `fallback` to the same cover site (a plain-HTTP listener on
  loopback, port 2059), so a browser on a Trojan port gets a site too and
  not a reset. On the main server only: nodes do not run the panel.
- **Sharing the port.** The panel's HTTPS always listens on loopback as
  well (port 2058). A VLESS + REALITY inbound may be put on the panel's
  public port; it then presents the panel's own domain, and REALITY sends
  everybody who is not its client to the inner listener. To a stranger the
  port is one HTTPS site with a real certificate, which is the best
  camouflage REALITY can have (no borrowed third-party name on an address
  that is not theirs). Let's Encrypt's check (TLS-ALPN-01) arrives the same
  way, so renewal keeps working. The panel lets go of the public port
  before sing-box takes it and takes it back when the inbound goes.
- Costs: behind the shared port the panel sees every visitor as 127.0.0.1
  (sign-in rate limits are then shared, audit shows loopback); on a backup
  entry such a VLESS inbound takes its handshake from the main server, so
  it stops when the main server does (the node's other protocols go on).

## Open questions for the owner

1. ~~D-15~~: decided, nodes are linked by a VLESS + REALITY hop.
2. **Subscription formats:** the plan is native sing-box JSON plus a base64
   list of share links (`vless://`, `hysteria2://`, …) for v2rayN, Hiddify and
   NekoBox. Is a Clash/mihomo YAML format needed too?
3. **Tenancy:** the plan is a single admin circuit with admin / operator / viewer
   roles, without multi-organization tenancy. Is that sufficient?
