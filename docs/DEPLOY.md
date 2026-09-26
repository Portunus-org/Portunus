# Deploying Portunus

## Requirements

- Linux host (a 1 vCPU / 1 GB VPS is the target), Docker Engine with the
  Compose plugin.
- Host networking (`network_mode: host`) works on Linux. Docker Desktop on
  macOS/Windows is fine for trying out the UI, but not as a proxy server.

## Quick start

```sh
git clone https://github.com/PaulichP/Portunus.git && cd Portunus
docker compose up -d
docker compose logs portunus | grep "initial admin"   # one-time admin password
```

The panel listens on `127.0.0.1:2053` of the server only. From your machine:

```sh
ssh -N -L 2053:127.0.0.1:2053 user@your-server
```

Open <http://localhost:2053>, sign in as `admin` with the password from the
log, and set your own password (required before anything else is accessible).

The initial password is printed exactly once, on the first start with an
empty database. If you lose it:

```sh
docker compose exec portunus portunus reset-password          # user "admin"
docker compose exec portunus portunus reset-password alice    # any user
```

## Configuration

All configuration is environment variables. With Docker Compose, put them in
a `.env` file next to `docker-compose.yml` (start from `.env.example`). Without
a `.env`, the defaults below apply.

| Variable | Default (image) | Description |
|---|---|---|
| `PORTUNUS_LISTEN` | `127.0.0.1:2053` | Panel HTTP address. Anything other than loopback logs a warning and shows a banner in the UI. |
| `PORTUNUS_DATA_DIR` | `/var/lib/portunus` | Database and (later) keys, rule-set cache, certificates. Mounted as a volume. |
| `PORTUNUS_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `PORTUNUS_LOG_FORMAT` | `json` | `json` or `text` (the binary outside Docker defaults to `text`) |
| `PORTUNUS_COOKIE_SECURE` | `auto` | Session cookie `Secure` flag: `auto` (when served over TLS), `always`, `never` |
| `PORTUNUS_SUB_LISTEN` | empty | Public listener that serves only `/sub/<token>`, e.g. `0.0.0.0:2096`. Empty: subscriptions are not published. |
| `PORTUNUS_SECRET_KEY_FILE` | `$DATA_DIR/master.key` | Master key for encrypting keys in the database; generated on first start. |
| `PORTUNUS_SINGBOX_BIN` | `sing-box` | sing-box executable (the image ships a patched build, see DECISIONS D-17/D-18). |
| `PORTUNUS_SINGBOX_API` | `127.0.0.1:2054` | Loopback address of sing-box's stats API (per-client traffic). |
| `PORTUNUS_ACME_DIRECTORY` | Let's Encrypt | Other ACME directory URL (another CA, or Pebble for testing). |
| `GOMEMLIMIT` | `96MiB` | Go runtime soft memory limit |

## First steps in the panel

After the first sign-in the panel greets you, asks for a new password and
for the basics: it shows the detected public address and asks whether the
server has a domain (checked against DNS as you type).

Connections are then created in **Connections → Add connection**, a
step-by-step wizard for the chosen protocol. It picks a free port, measures
REALITY camouflage sites from the server, and for Hysteria2, TUIC and Trojan
asks where the certificate comes from:

- **Let's Encrypt** for a domain pointing to the server. The wizard shows
  how it will be verified: directly if port 80 is free, through your web
  server on port 80 (with the snippet to add and a live check), or through
  Cloudflare DNS;
- **Portunus certificate**: issued by the panel's own CA for a domain or the
  IP address, works immediately. The last step offers the CA for download
  (`.crt`, `.cer`, a QR code when subscriptions are on) with install steps
  for iOS, Android, Windows and macOS. It is also in **Settings**;
- **existing files** from certbot, Nginx Proxy Manager or Caddy.

## Ports and firewall

sing-box listens on the host directly (`network_mode: host`). Open the inbound
ports in your firewall, for TCP and/or UDP as the Inbounds table shows. The
panel (`2053`) and the stats API (`2054`) stay on loopback; clients cannot
reach them through the proxy either.

For QUIC throughput (Hysteria2, TUIC) raise the UDP buffer limits on the host:

```sh
sudo sysctl -w net.core.rmem_max=16777216 net.core.wmem_max=16777216
```

(persist in `/etc/sysctl.d/`).

## Certificates (Hysteria2, TUIC, Trojan)

Many client apps refuse servers with self-signed certificates, so give these
inbounds a real one: set **Certificate** to *Let's Encrypt* and enter a
domain whose A/AAAA record points to this server. Choose how the domain is
verified in **Settings → Certificates**:

- **HTTP-01 on port 80:** nothing else on the host uses TCP 80. sing-box
  answers the check itself.
- **HTTP-01 behind a reverse proxy:** Nginx, Nginx Proxy Manager, Caddy or
  Traefik already owns port 80. Forward the challenge path to the local port
  shown in the panel (default 8088):

  ```nginx
  location /.well-known/acme-challenge/ {
      proxy_pass http://127.0.0.1:8088;
      proxy_set_header Host $host;
  }
  ```

  Nginx Proxy Manager runs in Docker, so use the host address as seen from its
  container (usually `http://172.17.0.1:8088`) for a proxy host of that
  domain, without requesting an SSL certificate in NPM. The port only
  listens while a certificate is being issued or renewed.
- **DNS-01 via Cloudflare:** no open ports needed. Create an API token with
  *Zone · DNS · Edit* for the zone and paste it into the panel (stored
  encrypted).

Certificates are renewed automatically. The Inbounds table shows the expiry
date, and "Pending" until the first one is issued (see Settings → sing-box →
Logs if it stays pending).

**Existing certificates:** if certbot, NPM or Caddy already manages a
certificate for the domain, choose *Certificate files* and mount the
directory read-only, e.g. for certbot:

```yaml
    volumes:
      - portunus-data:/var/lib/portunus
      - /etc/letsencrypt:/certs:ro
```

then use `/certs/live/<domain>/fullchain.pem` and `.../privkey.pem`. The
files must be readable by uid 10001 inside the container. Renewed files are
picked up automatically.

## Subscriptions

Subscription URLs let client apps pick up changes (new inbounds, rotated keys)
without re-importing. Set the port in **Settings → Subscriptions** (the
welcome does it when you have a domain); the panel opens it itself and serves
HTTPS there once the domain has a certificate. Behind your own reverse proxy,
enter its public URL instead. `PORTUNUS_SUB_LISTEN` in `.env` fixes the
listener address if you prefer configuration files. Only `/sub/<token>` and the CA certificate
(`/ca.crt`) are served on that listener, and the 256-bit token in the URL is the only credential. The format
is chosen automatically: sing-box apps get a sing-box profile with an
`urltest` group, all others get a base64 list of share links.

## Data and backups

Everything lives in the `portunus-data` volume (`/var/lib/portunus`):

- `portunus.db`, `portunus.db-wal`, `portunus.db-shm`: SQLite in WAL mode;
- `master.key`: encrypts private keys in the database. **Back it up with the
  database.** Without it, inbounds cannot be decrypted and must be
  re-created (clients re-import);
- `sing-box/`: the generated config and ACME accounts and certificates.

For a consistent backup, stop the container or use SQLite's online backup:

```sh
docker compose stop portunus
docker run --rm -v portunus_portunus-data:/data -v "$PWD":/backup alpine \
  tar czf /backup/portunus-$(date +%F).tgz -C /data .
docker compose start portunus
```

## Upgrading

```sh
git pull
docker compose up -d --build
```

Database migrations run automatically on start. Migrations are forward-only.
A binary refuses to start against a database from a newer version, so take a
backup before upgrading if you might want to roll back.

## Health and logs

- `docker compose ps` shows the health status (`portunus healthcheck` probes
  `/healthz`, which also pings the database).
- Logs are structured JSON on stderr. Compose rotates them (3 × 10 MB).

## Resource usage (measured)

| | |
|---|---|
| Image size | ~93 MB (sing-box ~41 MB) |
| Panel RSS, idle | ~15 MB |
| Panel RSS, during a login (argon2id) | ~55 MB, back to idle within ~2 s |
| sing-box RSS, 3 inbounds, idle | ~50 MB |

Throughput, one connection, server pinned to a single core (loopback,
Ryzen 7 255): VLESS+REALITY 4.2 Gbit/s, Hysteria2 1.5 Gbit/s,
Shadowsocks-2022 10 Gbit/s. See DECISIONS D-20 to reproduce.

## Development

```sh
# API on 127.0.0.1:2053 with ./data, no UI embedded
cd server && PORTUNUS_DATA_DIR=../data PORTUNUS_LOG_LEVEL=debug go run -tags noui ./cmd/portunus

# Next.js dev server on :3000, proxies /api to :2053
cd web && pnpm dev

# Tests and checks
cd server && go test -tags noui ./... && go vet -tags noui ./...
# With a (patched) sing-box binary, also run the data-plane tests:
PORTUNUS_TEST_SINGBOX=/path/to/sing-box go test -tags noui ./...
cd web && pnpm lint && pnpm typecheck

# Release binary with the UI embedded → bin/portunus
scripts/build.sh
```

pnpm is required for the web part (`npm i -g pnpm@12.6.0`).
