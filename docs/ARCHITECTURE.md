# Architecture

Portunus is a lightweight control panel for [sing-box](https://sing-box.sagernet.org).
It generates sing-box configuration, manages nodes and clients, and serves
subscriptions. It never carries proxy traffic itself.

Status markers: **[built]** exists in the code, **[planned: stage N]** is
designed but not implemented yet. Stages refer to the build plan in the
project spec.

## 1. Planes

```
                  operator (browser, via SSH tunnel)
                               │ HTTP :2053 (loopback)
┌──────────────────────────────▼──────────────────────────────┐
│ main node · container "portunus" · network_mode: host        │
│                                                              │
│  portunus (Go, one static binary)          CONTROL PLANE     │
│   ├─ HTTP API + embedded web UI  [built]                     │
│   ├─ auth: local accounts, sessions  [built] · OIDC [st. 7]  │
│   ├─ SQLite (WAL) on volume  [built]                         │
│   ├─ config generator → sing-box JSON  [built]               │
│   ├─ rule-set compiler / updater  [st. 4]                    │
│   └─ agent hub (mTLS WebSocket)  [st. 3]                     │
│          │ spawns, reloads, supervises                       │
│  sing-box (child process)               DATA PLANE [built]   │
│   inbounds: VLESS+REALITY, Hysteria2, Trojan, SS, TUIC       │
└──────┬───────────────────────────────▲───────────────────────┘
       │ hop (sing-box outbound, D-15) │ agent control channel
┌──────▼───────────────────────────────┴───────┐
│ hop / backup node · `portunus agent` + sing-box │  [stage 3, 5]
└───────────────────────────────────────────────┘
```

- **Control plane**: the `portunus` binary. It owns state, renders sing-box
  configs, and pushes them to local and remote sing-box processes.
- **Data plane**: sing-box processes. They only proxy. They keep running if the
  panel restarts; a panel outage freezes configuration but does not stop
  traffic.

## 2. Process model (main node) [built / st. 2]

One container, one entrypoint (`portunus serve`), which:

1. loads `PORTUNUS_*` configuration from the environment;
2. opens SQLite and applies pending migrations;
3. creates the initial admin on an empty database;
4. [st. 2] renders the sing-box config, validates it with `sing-box check`,
   starts sing-box as a child process, and restarts it with backoff if it exits;
5. serves HTTP on `PORTUNUS_LISTEN` until SIGTERM, then shuts down gracefully.

The same binary runs `portunus agent` on other nodes [st. 3], plus the
utility commands `healthcheck`, `reset-password` and `version`.

## 3. Code layout

```
server/                     Go module (go.mod, go.sum)
  cmd/portunus              entrypoint and subcommands
  internal/config           env-based configuration
  internal/db               SQLite open + embedded forward-only migrations
  internal/store            all SQL; the only package that touches database/sql
  internal/auth             argon2id, sessions, login rate limiter, bootstrap
  internal/api              HTTP router, middleware (security headers, CSRF, logging)
  internal/webui            static file server for the exported UI, per-page CSP;
                            embeds internal/webui/dist (copy of web/out, build artifact)
  internal/sysinfo          self-reported resource usage
  internal/version          build metadata (-ldflags)
web/                        Next.js app (static export → web/out)
deploy/                     Dockerfile
scripts/                    build.sh
docs/                       this document, DECISIONS, DEPLOY, SECURITY, DESIGN
```

Planned packages (under `server/`): `internal/singbox` (config model, generator, process
supervisor), `internal/nodes` (agent hub, join tokens, health),
`internal/routing` (list sources, rule-set compiler), `internal/subscription`,
`internal/secrets`, `internal/oidc`.

## 4. HTTP surface [built]

| Path | Auth | Purpose |
|---|---|---|
| `GET /healthz` | none | liveness + DB ping; used by the container health check |
| `POST /api/auth/login` | none (rate-limited) | username/password → session cookie + CSRF token |
| `GET /api/auth/me` | session | current user + CSRF token |
| `POST /api/auth/password` | session + CSRF | change own password; revokes other sessions |
| `POST /api/auth/logout` | session + CSRF | end session |
| `GET /api/system` | session | version, uptime, memory, DB size, listen address |
| everything else | none | static UI (embedded export), 404 page for unknown paths |

API responses carry `Content-Security-Policy: default-src 'none'` and
`Cache-Control: no-store`. HTML pages get a per-page CSP whose `script-src`
lists the SHA-256 hashes of that page's inline Next.js bootstrap scripts,
computed at startup. There is no `'unsafe-inline'` for scripts.

Stage 2 endpoints: `/api/inbounds`, `/api/clients` (+ `/access` with links
and QR codes), `/api/settings`, `/api/singbox` (status, restart, logs, masked
config), and `GET /sub/<token>` (also on the optional public listener).
Planned: the join script (`/join/<token>`) [st. 3]. When the panel is published behind the decoy
[st. 6], only these paths and the secret panel prefix are routed to Portunus;
everything else returns the decoy site.

## 5. Data model [built: users, sessions, audit_log, settings]

- `users`: local and (later) OIDC-linked accounts, role
  `admin|operator|viewer`.
- `sessions`: SHA-256 of the cookie token, CSRF token, sliding expiry.
- `audit_log`: security-relevant actions (login, failed login, password
  change or reset); later all config mutations.
- `settings`: key/value for panel-wide settings.

Stage 2: `inbounds` (public settings JSON + encrypted secret) and `clients`
(credentials, subscription token, limit, expiry, traffic counters).
Planned: `nodes`, `join_tokens`, `chains`, `list_sources`, `route_rules`,
`oidc_providers`.

## 6. Multihop, routing and failover (design) [planned: stages 3–5]

- **Join**: the panel issues a one-time token with a TTL →
  `curl -fsSL https://<panel>/join/<token> | sh` on the new node installs
  the agent and sing-box. The agent exchanges the token for an mTLS client
  certificate (D-14) and receives its config. The token is burnt on first use.
- **Chains**: declared as ordered node lists (entry → hop1 → … → exit). The
  generator emits one outbound per hop, linked with sing-box `detour`. The
  node-to-node transport is a VLESS+REALITY hop (proposed, D-15).
- **Routing**: domain lists (URL sources such as itdoginfo/allow-domains, or
  local lists) are compiled to sing-box binary rule-sets (`.srs`), cached in the
  data dir and refreshed on a schedule. Ordered rules map a list to
  `direct | via chain X | block`.
- **Failover**:
  - *client side*: subscriptions contain a `urltest` group (auto-select by
    latency) wrapped in a `selector`, over all entry points of the same
    service, so a dead entry is skipped without operator action;
  - *server side*: agents send heartbeats, the panel actively probes. A node
    is marked down after N missed probes, and chains that use it are re-rendered
    to use a backup exit.
- **Degradation**:
  - *exit/hop node down*: only chains through it are affected. They switch to
    a backup exit, and `direct` traffic is untouched.
  - *panel down*: every sing-box keeps its last config. Clients keep working,
    but nothing can be changed and no failover re-render happens. Client-side
    `urltest` still covers entry-node failure.
  - *main node down*: its entry disappears. Clients fail over to other entries
    in their subscription (backup nodes deployed with the entry role). The panel
    itself is unavailable until the main node returns. Panel HA is out of scope.

## 7. Frontend [built]

Next.js App Router, static export, shadcn/ui only. Route groups:

- `app/login`: public sign-in page.
- `app/(panel)/…`: authenticated shell (sidebar + header, based on
  dashboard-01). `SessionGate` loads `/api/auth/me` and redirects to `/login/`
  on 401. This is a UX guard only; the API enforces authorization itself.

All data is fetched client-side from the JSON API. `lib/api.ts` attaches the
CSRF header on state-changing requests. Fonts are self-hosted, so the browser
never contacts third parties.
