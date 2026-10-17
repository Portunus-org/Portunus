# Architecture

Portunus is a lightweight control panel for [sing-box](https://sing-box.sagernet.org).
It generates sing-box configuration, manages nodes and clients, and serves
subscriptions. It never carries proxy traffic itself.

## 1. Planes

```
                  operator (browser, via SSH tunnel)
                               │ HTTP :2053 (loopback)
┌──────────────────────────────▼──────────────────────────────┐
│ main node · container "portunus" · network_mode: host       │
│                                                             │
│  portunus (Go, one static binary)          CONTROL PLANE    │
│   ├─ HTTP API + embedded web UI                             │
│   ├─ auth: local accounts, sessions, OIDC                   │
│   ├─ SQLite (WAL) on volume                                 │
│   ├─ config generator → sing-box JSON                       │
│   ├─ route list updater                                     │
│   └─ node client (mTLS HTTPS)                               │
│          │ spawns, reloads, supervises                      │
│  sing-box (child process)               DATA PLANE          │
│   inbounds: VLESS+REALITY, Hysteria2, Trojan, SS, TUIC      │
└──────┬───────────────────────────────▲──────────────────────┘
       │ hop (sing-box outbound)       │ agent control channel
┌──────▼───────────────────────────────┴──────────────────────┐
│ exit / backup node · `portunus agent` + sing-box            │
└─────────────────────────────────────────────────────────────┘
```

- **Control plane**: the `portunus` binary. It owns state, renders sing-box
  configs, and pushes them to local and remote sing-box processes.
- **Data plane**: sing-box processes. They only proxy. They keep running if the
  panel restarts; a panel outage freezes configuration but does not stop
  traffic.

## 2. Process model (main node)

One container, one entrypoint (`portunus serve`), which:

1. loads `PORTUNUS_*` configuration from the environment;
2. opens SQLite and applies pending migrations;
3. creates the initial admin on an empty database;
4. renders the sing-box config, validates it with `sing-box check`,
   starts sing-box as a child process, and restarts it with backoff if it exits;
5. serves HTTP on `PORTUNUS_LISTEN` until SIGTERM, then shuts down gracefully.

The same binary runs `portunus agent` on other nodes, plus the
utility commands `healthcheck`, `reset-password` and `version`.

## 3. Code layout

```
server/                     Go module (go.mod, go.sum)
  cmd/portunus              entrypoint and subcommands
  cmd/portunus-node         what a node runs: the agent alone (its own image)
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
docs/                       this document, DEPLOY, SECURITY, DESIGN
```

Nodes and routing live in `internal/agent` (both ends of the control
channel), `internal/proxy` (routing rules, hops, link import) and
`internal/core` (nodes, lists, rules, sync). Sign-in through a provider is
`internal/oidc` (the flow) and `internal/core` (its settings, groups to
roles).

## 4. HTTP surface

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

Proxies and clients: `/api/inbounds`, `/api/clients` (+ `/access` with links
and QR codes), `/api/settings`, `/api/singbox` (status, restart, logs, masked
config), and `GET /sub/<token>` (also on the optional public listener).
Nodes and routing: `/api/nodes` (with `/join` and `/check`) and
`/api/routing` (default, lists, rules). When the panel is published behind the decoy,
only these paths and the secret panel prefix are routed to Portunus;
everything else returns the decoy site.

## 5. Data model

- `users`: accounts with a password of their own and accounts of an OIDC
  provider (known by issuer and subject), role `admin|operator|viewer`.
- `sessions`: SHA-256 of the cookie token, CSRF token, sliding expiry.
- `audit_log`: security-relevant actions (login, failed login, password
  change or reset); later all config mutations.
- `settings`: key/value for panel-wide settings.

Proxies and clients: `inbounds` (public settings JSON + encrypted secret) and `clients`
(credentials, subscription token, limit, expiry, traffic counters).
Nodes and routing: `nodes` (roles, public settings, encrypted keys),
`route_lists`, `route_rules`. The one OIDC provider is a setting, its client
secret encrypted like the other secrets.

## 6. Nodes, routing and failover

- **Join**: the panel issues the node's keys and shows one `docker run …
  agent` command with a `PORTUNUS_JOIN` token. The agent listens for the
  panel over mutually authenticated TLS; the panel sends it a complete
  sing-box configuration and polls it every 30 s.
- **Exits**: a node of ours (VLESS + REALITY hop) or a proxy imported
  from a share link. Each becomes an outbound. Every client is bound to one
  exit; the route target `auto` sends traffic through it.
- **Routing**: text lists of sites and addresses, typed or downloaded daily,
  inlined as sing-box rule-sets. Ordered rules map a list to `direct`, an
  exit, `auto` or `block`; a default covers the rest.
- **Failover**:
  - *client side*: subscriptions contain every entry point (the main server
    and backup entries) in one `urltest` group, so a dead entry is skipped
    without operator action;
  - *exits*: the panel moves the clients of an exit that has not answered
    for two minutes to the other exits; until then, and for a rule
    bound to one exit, traffic fails closed.
- **Degradation**:
  - *exit down*: only traffic routed to it is affected; `direct` traffic is
    untouched.
  - *panel or main server down*: every node keeps its last configuration,
    also across restarts. Clients keep working through backup entries, but
    nothing can be changed and limits are not enforced until the panel is
    back. Panel HA is out of scope.

## 7. Frontend

Next.js App Router, static export, shadcn/ui only. Route groups:

- `app/login`: public sign-in page.
- `app/welcome`: first-run basics (password, public address, domain).
- `app/(panel)/inbounds/new`: the inbound wizard.
- `app/(panel)/docs`: the built-in user guide; content in `lib/docs.ts`,
  one prerendered page per topic.
- `app/(panel)/…`: authenticated shell (sidebar + header, based on
  dashboard-01). `SessionGate` loads `/api/auth/me` and redirects to `/login/`
  on 401. This is a UX guard only; the API enforces authorization itself.

All data is fetched client-side from the JSON API. `lib/api.ts` attaches the
CSRF header on state-changing requests. Fonts are self-hosted, so the browser
never contacts third parties.
