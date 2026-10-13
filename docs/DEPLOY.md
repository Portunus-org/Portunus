# Deploying Portunus

## Requirements

- Linux host (a 1 vCPU / 1 GB VPS is the target), Docker Engine with the
  Compose plugin.
- Host networking (`network_mode: host`) works on Linux. Docker Desktop on
  macOS/Windows is fine for trying out the UI, but not as a proxy server.

## Quick start

Only `docker-compose.yml` is needed: it starts the published image
(`ghcr.io/paulichp/portunus`) and, next to it, the updater that installs
new releases when the panel asks. There is nothing to fill in.

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

If the server has a domain, the first setup then offers to open the panel
by it: the panel takes TCP port 443, gets a certificate from Let's Encrypt
and shows a secret address, `https://your-domain/…`, to use from then on.
To anyone without that address the server answers as if there were nothing
there. A certificate of your own, or another port, is set in **Settings →
Opening the panel**. The tunnel keeps working either way.

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
| `PORTUNUS_UPDATE_CHECK` | `ghcr.io/paulichp/portunus` | Image repository whose tags tell the panel that a newer release is out (asked shortly after the start and every 6 hours, without an account); `off` turns the check off. |
| `PORTUNUS_GEO_LOOKUP` | `https://ipwho.is/` | Service asked where servers are, for the map of nodes (`<URL><IP>`); `off` disables it. |
| `PORTUNUS_MAP_DETAIL` | OpenFreeMap, then VersaTiles, then OpenStreetMap's own vector tiles | Where the drawn map of nodes gets what it shows when zoomed in past the scale of countries (coasts, borders of regions, names of towns): TileJSON addresses of vector tile sets, separated by commas, in the OpenMapTiles or the Shortbread layout. The panel uses the first one this server can actually get tiles from, and looks again every few hours: a provider that works in one country may be out of reach in another (OpenFreeMap is behind Cloudflare, which Russian hosting cannot reach). The panel fetches and caches the tiles itself; the browser talks to the panel only. `off` keeps the built-in map, which needs nothing from outside and goes down to the scale of countries. |
| `PORTUNUS_MAP_TILES` | `off` | By default the panel draws the map of nodes itself (country shapes it carries; nothing is fetched). Set a tile server to use map images instead: a template with `{z}`, `{x}`, `{y}`, optional `{style}` = `light`/`dark`, or `esri` for Esri's gray canvas. Images are cached in the data volume and served to the browser by the panel. |
| `PORTUNUS_MAP_LABELS` | none | Optional second layer drawn over the tiles (place names), same format. `esri` brings its own. |
| `PORTUNUS_MAP_CREDIT` | none | Attribution shown on the map when tiles are used. `esri` brings its own. |
| `PORTUNUS_SINGBOX_BIN` | `sing-box` | sing-box executable (the image ships a patched build, see DECISIONS D-17/D-18). |
| `PORTUNUS_SINGBOX_API` | `127.0.0.1:2054` | Loopback address of sing-box's stats API (per-client traffic). |
| `PORTUNUS_ACME_DIRECTORY` | Let's Encrypt | Other ACME directory URL (another CA, or Pebble for testing). |
| `GOMEMLIMIT` | `96MiB` | Go runtime soft memory limit of the panel itself |
| `PORTUNUS_SINGBOX_MEMLIMIT` | `256MiB` | The same limit for sing-box, which needs room for its connections. Raise it on servers with many simultaneous clients and memory to spare. |

## First steps in the panel

After the first sign-in the panel greets you, asks for a new password and
for the basics: it shows the detected public address and asks whether the
server has a domain (checked against DNS as you type).

With a domain the panel then sets itself up to be opened by it (a
certificate, a check that the domain leads here) and hands over its address.
The setup goes on from there, also after you have signed in anew at that
address: a first connection, made by the same wizard as any later one, and
a first client. Every step can be skipped; the overview then shows what is
left to do.

The panel has its own user guide under **Documentation** in the sidebar.

Connections are created in **Connections → Add connection**, a
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

The control port is safe to leave open to everyone: only the panel of this
installation gets an answer, and the port does not say what runs behind it.
Opening it to the main server's address alone hides even that it is there.

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

The panel says when a newer release is out: Settings come marked in the
menu, and the page opens with the version and three ways to install it:

- **Install now.** The panel is replaced by the new release; the nodes
  follow by themselves.
- **Pick a time.** A day and an hour for this release, a quiet one by your
  traffic for example. It can be taken back until then.
- **Install new versions automatically.** A switch: releases are installed
  by themselves at the chosen hour, on any day or on one day of the week.

By hand it always works as well:

```sh
docker compose pull && docker compose up -d
```

on the main server, and the join command again on each node.

### The updater

The panel cannot replace its own container, so a second one does it: the
`updater` service of `docker-compose.yml` on the main server, and
`portunus-updater` on a node (the join command starts it). It is the node
image run with another command, so there is nothing more to publish or to
pull.

What it does: stops the container, starts the same one (same volumes,
ports, environment, restrictions) from the image of the new release, waits
until it is seen to work, and removes the old one. If the new one does not
come up, the old one is put back and the panel shows why. Then it moves
itself to the same release.

A new release may change the database, which the previous one could not
read afterwards. So the panel copies the database (`before-update.db` in
its data directory) before it asks for an update, and a previous version
that is started again takes that copy back; what the failed release wrote
is kept next to it as `portunus.db.newer`. Traffic counted in the minute
between the copy and the restart is the only thing lost.

What it may do is narrow on purpose, because it is the one part that talks
to Docker, which is as good as being root on that server:

- It has no network (`network_mode: none`), no ports, a read-only file
  system and no capabilities. Nothing can connect to it.
- Its only input is a file with a version number, left by the panel (on a
  node: by the agent, which takes it from the panel over the control
  channel). It turns that into "the same image, this release".
- It cannot be told to run another image, another command or another
  container, and it never goes back to an older release, so a panel that
  was broken into cannot plant its own image on your servers through it:
  the most it can do is install a real, newer release early.
- An image that is not there to pull is not installed.

What it does not protect against is someone who already has root on that
server, or a release that is itself bad: updates are taken from the image
registry, and whoever can publish there can publish a bad release, by any
way of updating. To keep Docker out of reach altogether, remove the
`updater` service (and `portunus-updater` on nodes): the panel then only
tells, and shows the commands.

**A server set up before v0.2.5** has no updater. Bring it once:

```sh
curl -fsSLO https://raw.githubusercontent.com/PaulichP/Portunus/main/docker-compose.yml
docker compose pull && docker compose up -d
```

(with a `docker-compose.yml` of your own, add the `updater` service and the
`portunus-update` volume to it instead), and run the join command on each
node again.

`latest` follows releases; put a version instead (`image:
ghcr.io/paulichp/portunus:v1.2.3`) to stay on one. Nodes are updated by
running their join command again (the panel shows it in the node's menu).

Database migrations run automatically on start. Migrations are forward-only.
A binary refuses to start against a database from a newer version, so take a
backup before upgrading if you might want to roll back.

### Starting over, and Let's Encrypt's limits

`docker compose down -v` removes the container with its data, certificates
included, and the next start asks Let's Encrypt for new ones. Let's Encrypt
issues at most **5 certificates for the same domain per 7 days** (one more
becomes available about every 34 hours). A fresh installation with a
domain uses one: the panel's own, which the connections on the same domain
(Hysteria2, TUIC, Trojan) serve as well. They ask for one of their own only
when the panel is not opened by that domain, or when they use another
domain. Five reinstalls in a week are enough to run out. The panel then says so, with
the time the limit ends, and gets its certificate by itself after that.

An upgrade (`pull`, `up -d`) keeps the data and asks for nothing. When
trying things out, either keep the volume (`docker compose down` without
`-v`) or point the panel at Let's Encrypt's staging server, whose limits are
far higher and whose certificates browsers do not trust:

```yaml
    environment:
      PORTUNUS_ACME_DIRECTORY: https://acme-staging-v02.api.letsencrypt.org/directory
```

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

## Versions and releases

A version is three numbers, `vMAJOR.MINOR.PATCH`, for example `v0.3.1`:

- the **last** number grows when something is fixed and nothing else
  changes: `v0.3.1` → `v0.3.2`. Always safe to take;
- the **middle** number grows when something is added: `v0.3.2` → `v0.4.0`
  (the last number starts again from 0);
- the **first** number grows when an upgrade needs something done by hand
  (a setting renamed, a step in the release notes): `v0.9.4` → `v1.0.0`.
  While it is 0 the panel is still taking shape.

A version is a tag in git, and nothing else has to be edited: the build
takes the number from the tag, the panel shows it in **Settings**, and nodes
are started from the node image with the same number.

```sh
git tag v0.1.0
git push origin v0.1.0
```

Pushing the tag starts `.github/workflows/release.yml`: both images are
built for x86 and ARM, scanned with Trivy and, if nothing HIGH or CRITICAL
is found, published as `ghcr.io/paulichp/portunus:v0.1.0` and
`ghcr.io/paulichp/portunus-node:v0.1.0`, and as `latest`. The first time,
make both packages public on GitHub (profile → Packages → the package →
Package settings → Change visibility), or servers would have to sign in to
download them. The published images are scanned again every week
(`scan.yml`).

A build without a tag (the development setup above) calls itself `dev` and
starts nodes from `latest`.

pnpm is required for the web part (`npm i -g pnpm@12.6.0`).
