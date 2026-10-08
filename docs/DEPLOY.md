# Deploying Portunus

## Requirements

- Linux host (a 1 vCPU / 1 GB VPS is the target), Docker Engine with the
  Compose plugin.
- Host networking (`network_mode: host`) works on Linux. Docker Desktop on
  macOS/Windows is fine for trying out the UI, but not as a proxy server.

## Quick start

Only `docker-compose.yml` is needed: eleven lines that start the published
image (`ghcr.io/paulichp/portunus`), with nothing to fill in.

```sh
mkdir portunus && cd portunus
curl -fsSLO https://raw.githubusercontent.com/PaulichP/Portunus/main/docker-compose.yml
docker compose up -d
docker compose logs portunus      # how to sign in
```

The log shows the address, the login (`admin`) and a password made up for
this installation, and how to reach the panel: it listens on
`127.0.0.1:2053` of the server only, so from your machine

```sh
ssh -N -L 2053:127.0.0.1:2053 user@your-server
```

and open <http://localhost:2053>. At the first sign-in you set your own
password (required before anything else is accessible).

Until then the made-up password is kept in the data volume
(`initial-password`, readable by the service only) and shown again on every
start, so it cannot be lost with the logs. Changing the password removes it.
If you forget your own password later:

```sh
docker compose exec portunus portunus reset-password          # user "admin"
docker compose exec portunus portunus reset-password alice    # any user
```

## Configuration

All configuration is environment variables. With Docker Compose, put them in
an `environment:` block of the service in `docker-compose.yml`; none is
required. For example:

```yaml
    environment:
      PORTUNUS_SUB_LISTEN: 0.0.0.0:2096
```

The compose file is kept to the minimum on purpose. To harden the container
further, add `read_only: true` with `tmpfs: [/tmp]`, `cap_drop: [ALL]` with
`cap_add: [NET_BIND_SERVICE]`, and a limit for the log
(`logging: {options: {max-size: 10m}}`); the service already runs as an
unprivileged user.

| Variable | Default (image) | Description |
|---|---|---|
| `PORTUNUS_LISTEN` | `127.0.0.1:2053` | Panel HTTP address. Anything other than loopback logs a warning and shows a banner in the UI. |
| `PORTUNUS_DATA_DIR` | `/var/lib/portunus` | Database and (later) keys, rule-set cache, certificates. Mounted as a volume. |
| `PORTUNUS_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `PORTUNUS_LOG_FORMAT` | `json` | `json` or `text` (the binary outside Docker defaults to `text`) |
| `PORTUNUS_COOKIE_SECURE` | `auto` | Session cookie `Secure` flag: `auto` (when served over TLS), `always`, `never` |
| `PORTUNUS_SUB_LISTEN` | empty | Public listener that serves only `/sub/<token>`, e.g. `0.0.0.0:2096`. Empty: subscriptions are not published. |
| `PORTUNUS_SECRET_KEY_FILE` | `$DATA_DIR/master.key` | Master key for encrypting keys in the database; generated on first start. |
| `PORTUNUS_SINGBOX_CTL` | `127.0.0.1:2055` | Loopback address of sing-box's control API; the panel measures exits through it. |
| `PORTUNUS_NODE_IMAGE` | `ghcr.io/paulichp/portunus-node:<version>` | Image the join command for new nodes uses: by default the published node image of the panel's version (`latest` for a build that is not a release). Without a registry in the name it is a local build, and the panel also shows how to copy it to the server. |
| `PORTUNUS_GEO_LOOKUP` | `https://ipwho.is/` | Service asked where servers are, for the map of nodes (`<URL><IP>`); `off` disables it. |
| `PORTUNUS_MAP_TILES` | OpenStreetMap | Tile server the panel fetches map images from (`{z}`, `{x}`, `{y}`, optional `{style}`); cached in the data volume and served to the browser by the panel. `off`: plain outlines. |
| `PORTUNUS_SINGBOX_BIN` | `sing-box` | sing-box executable (the image ships a patched build, see DECISIONS D-17/D-18). |
| `PORTUNUS_SINGBOX_API` | `127.0.0.1:2054` | Loopback address of sing-box's stats API (per-client traffic). |
| `PORTUNUS_ACME_DIRECTORY` | Let's Encrypt | Other ACME directory URL (another CA, or Pebble for testing). |
| `GOMEMLIMIT` | `96MiB` | Go runtime soft memory limit of the panel itself |
| `PORTUNUS_SINGBOX_MEMLIMIT` | `256MiB` | The same limit for sing-box, which needs room for its connections. Raise it on servers with many simultaneous clients and memory to spare. |

## First steps in the panel

After the first sign-in the panel greets you, asks for a new password and
for the basics: it shows the detected public address and asks whether the
server has a domain (checked against DNS as you type).

The panel has its own user guide under **Documentation** in the sidebar.

Connections are then created in **Connections → Add connection**, a
step-by-step wizard for the chosen protocol (Hysteria2 is preselected). It picks a free port, measures
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

## Nodes: exits and backup entries

Other servers are added in **Nodes → Add node**. The panel creates the keys
and shows one command; run it on the new server as a user who may use sudo
(Docker required; `curl -fsSL https://get.docker.com | sudo sh` installs it):

```sh
sudo docker rm -f portunus-node 2>/dev/null; sudo docker run -d --pull always \
  --name portunus-node --restart unless-stopped --network host \
  --read-only --cap-drop ALL --cap-add NET_BIND_SERVICE \
  -v portunus-node:/var/lib/portunus \
  -e PORTUNUS_JOIN=<token from the panel> ghcr.io/paulichp/portunus-node:<version>
```

The server downloads the image itself. It is a separate, minimal image
(`--target node` in `deploy/Dockerfile`): the agent and sing-box on an empty
base, without the web interface, the database or a shell. Releases publish
it next to the panel's image (`.github/workflows/release.yml`), and a panel
uses the node image of its own version. Running the command again replaces
the container, which is also how a node is updated.

For a registry of your own, or a local build, set `PORTUNUS_NODE_IMAGE`.
A name without a registry (`portunus-node:local`) is taken to be a local
build; the panel then also shows how to copy it over
(`docker save portunus-node:local | ssh <server> sudo docker load`).

On the node, open the control port (TCP 2056) to the main server, the port
for traffic between servers (TCP 443, or 8443 on a backup entry) and, for a
backup entry, the ports of your connections. The node keeps its last
configuration in the `portunus-node` volume and goes on working when the
main server or the panel is down.

A node can also be registered from the shell of the main server, which
prints the same command:

```sh
docker compose exec portunus portunus node add "Amsterdam" 203.0.113.20 exit entry
docker compose exec portunus portunus node join "Amsterdam"   # the command again, later
```

To remove a node: delete it in the panel, then `docker rm -f portunus-node`
and `docker volume rm portunus-node` on the server.

Which traffic uses an exit is set in **Routing**; the panel's own
documentation explains both pages.

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

Take a backup while the panel runs:

```sh
docker compose exec -T portunus portunus backup > portunus-$(date +%F).tgz
```

The archive is a consistent snapshot of the database with the master key
and the issued certificates: the whole panel. **It holds every secret of the
installation**, so store it like one, and store it somewhere other than the
main server (a nightly cron job that copies it to another machine is
enough).

To restore, into an empty installation on any server:

```sh
docker compose run --rm -T portunus restore - < portunus-2026-10-08.tgz
docker compose up -d
```

## If the main server is lost

The panel exists on the main server only. While that server is down
(measured on a stand with two exits and two backup entries):

- **Clients stay online** through the backup entries, with the same rules
  and exits. Apps on the sing-box core move over by themselves (8 s in the
  test, up to about half a minute); in other apps the client picks a backup
  link. Nodes keep working with their last configuration, also after a
  reboot.
- **The web interface is unavailable**, so nothing can be changed.
  Subscriptions do not answer either: apps go on with the profile they
  have. Traffic limits and expiry dates are not enforced, and clients of an
  exit that fails are not moved to another one.
- Traffic is still counted on the nodes and is added up when a panel
  reaches them again.

### Standby nodes

So that the panel can be brought back without a backup of your own, mark
nodes with **Keeps a copy of the panel** (the node's card, or `portunus node
add NAME ADDRESS standby`; a server may have this role alone). The panel
keeps a copy of itself there, refreshed within half a minute after a change
of clients, connections, routes or nodes and at least every five minutes,
encrypted with the installation's **recovery key**. The key is shown in the card of such a
node: save it outside the main server. A node cannot read its copy.

When the main server is gone, on a server that keeps a copy:

```sh
curl -fsSLO https://raw.githubusercontent.com/PaulichP/Portunus/main/docker-compose.yml
sudo docker compose run --rm -v portunus-node:/node:ro portunus restore /node/panel-backup
sudo docker rm -f portunus-node && sudo docker compose up -d
```

`restore` asks for the recovery key (or takes `PORTUNUS_RECOVERY_KEY`),
unpacks the copy and makes this server the main one:

- its connections take over the keys and certificates it served as a backup
  entry, so links and profiles clients already hold for it keep working;
- its address becomes the address given to clients;
- routes through it become "direct" (that traffic then leaves from the
  server a client is connected to; review Routing afterwards), and clients
  that had it as their exit are bound to another;
- it is removed from the list of nodes and manages the others, which accept
  it at once: nodes are tied to the installation's keys, not to an address.

Sign in with your usual login and password. Traffic counters of the last
minutes before the loss may be missing from the copy.

Nothing takes over by itself, and two panels must never run from the same
data: each would push its own view to the nodes. If the old server returns
after you have moved, do not start its panel again.

## Upgrading

```sh
docker compose pull && docker compose up -d
```

`latest` follows releases; put a version instead (`image:
ghcr.io/paulichp/portunus:v1.2.3`) to stay on one. Nodes are updated by
running their join command again (the panel shows it in the node's menu).

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

# Images from this checkout instead of the published ones, and a scan of the
# finished images (what a release does before it publishes anything)
docker build -f deploy/Dockerfile --target node -t portunus-node:local .
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d --build
scripts/scan.sh
```

Releases: pushing a tag `v1.2.3` builds both images, scans them with Trivy
and, if nothing HIGH or CRITICAL is found, publishes them to
`ghcr.io/paulichp/portunus` and `ghcr.io/paulichp/portunus-node`
(`.github/workflows/release.yml`). The published images are scanned again
every week (`scan.yml`).

pnpm is required for the web part (`npm i -g pnpm@12.6.0`).
