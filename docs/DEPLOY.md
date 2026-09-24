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
| `GOMEMLIMIT` | `96MiB` | Go runtime soft memory limit |

## Data and backups

Everything lives in the `portunus-data` volume (`/var/lib/portunus`):

- `portunus.db`, `portunus.db-wal`, `portunus.db-shm`: SQLite in WAL mode.

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

## Resource usage (measured, stage 1)

| | |
|---|---|
| Image size | ~34 MB |
| RSS, idle | ~15 MB |
| RSS, during a login (argon2id) | ~55 MB, back to idle within ~2 s |

The 1 Gbit/s throughput measurement is documented once the data plane
exists (stage 2).

## Development

```sh
# API on 127.0.0.1:2053 with ./data, no UI embedded
cd server && PORTUNUS_DATA_DIR=../data PORTUNUS_LOG_LEVEL=debug go run -tags noui ./cmd/portunus

# Next.js dev server on :3000, proxies /api to :2053
cd web && pnpm dev

# Tests and checks
cd server && go test -tags noui ./... && go vet -tags noui ./...
cd web && pnpm lint && pnpm typecheck

# Release binary with the UI embedded → bin/portunus
scripts/build.sh
```

pnpm is required for the web part (`npm i -g pnpm@12.6.0`).
