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
| sing-box | 1.14.1 | integrated in stage 2 |
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

Portunus ships the official sing-box release binary and runs it as a child
process with a generated config file, rather than linking sing-box as a Go
library.

- Linking would require sing-box's build tags (`with_quic`,
  `with_utls`, `with_reality_server`, `with_acme`, …) and would couple
  Portunus releases to sing-box releases and its internal APIs, which change
  between minor versions.
- A separate process isolates crashes: a panel panic or restart does not
  have to tear down the data plane.
- sing-box can be upgraded independently (image rebuild only).

Config changes go through `sing-box check` first, then a reload. Where a
change cannot be applied without restarting listeners, existing connections
drop; Stage 2 documents exactly which changes are disruptive.

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

Compose currently sets `no-new-privileges`, which blocks file capabilities.
Stage 2 will replace it with `cap_add: [NET_BIND_SERVICE]` plus the file
capability, and document why.

Image size at stage 1: ~34 MB.

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

## D-12 Design system implementation — Accepted

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

## D-13 Secrets at rest — Proposed for stage 2

Key material (REALITY private keys, agent CA key, OIDC client secret) is
encrypted with AES-256-GCM using a 32-byte master key. The key comes from
`PORTUNUS_SECRET_KEY_FILE` (Docker/compose secret), or is generated on first
start as `$PORTUNUS_DATA_DIR/master.key` (0600). Password hashes and session
hashes are not encrypted (they are one-way already).

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

---

## Open questions for the owner

1. **D-15:** replace WireGuard with a sing-box VLESS+REALITY hop between nodes?
2. **Subscription formats:** the plan is native sing-box JSON plus a base64
   list of share links (`vless://`, `hysteria2://`, …) for v2rayN, Hiddify and
   NekoBox. Is a Clash/mihomo YAML format needed too?
3. **Tenancy:** the plan is a single admin circuit with admin / operator / viewer
   roles, without multi-organization tenancy. Is that sufficient?
