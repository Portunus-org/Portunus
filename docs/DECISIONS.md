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
| shadcn (CLI) | 4.21.0 | style `radix-nova`, base color neutral |
| TypeScript | **6.0.3** | 7.0.2 is out, but `typescript-eslint` 8.x (used by `eslint-config-next`) supports `<6.1` only |
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

## D-14 Panel ↔ agent control channel — Proposed for stage 3

The agent dials the panel (outbound only, no open ports on agents) over HTTPS
with a long-lived WebSocket, authenticated by mTLS. The panel runs a private CA.
At join time the one-time token is exchanged for a client certificate bound to
the node ID. Chosen over gRPC to avoid the protobuf/gRPC dependency weight
(several MB of binary, more RSS) for what is a low-volume message channel.

## D-15 Inter-node data transport — Proposed, needs owner decision

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
`X-Forwarded-Proto: https` and localhost; the sing-box binary is resolved
to an absolute path once). Reviewed false positives carry a `nosemgrep`
comment with the reason. sing-box's own dependencies are bumped in the
Dockerfile (`SINGBOX_DEP_BUMPS`, pinned) when its release lags behind a
fix; each entry is dropped once sing-box requires that version.

## Open questions for the owner

1. **D-15:** replace WireGuard with a sing-box VLESS+REALITY hop between nodes?
2. **Subscription formats:** the plan is native sing-box JSON plus a base64
   list of share links (`vless://`, `hysteria2://`, …) for v2rayN, Hiddify and
   NekoBox. Is a Clash/mihomo YAML format needed too?
3. **Tenancy:** the plan is a single admin circuit with admin / operator / viewer
   roles, without multi-organization tenancy. Is that sufficient?
