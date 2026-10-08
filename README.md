# Portunus

A lightweight, self-hosted control panel for [sing-box](https://sing-box.sagernet.org).
One container, tens of megabytes of RAM, loopback-only by default.

> **Status: early development.** Built: inbounds (VLESS+REALITY, Hysteria2,
> TUIC, Trojan, Shadowsocks-2022), clients with limits and traffic
> accounting, subscriptions and QR codes, Let's Encrypt certificates, exit
> and backup nodes joined with one command, routing by lists. The hidden
> panel and SSO follow. See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Goals

- Manage sing-box inbounds, clients and subscriptions (VLESS+REALITY,
  Hysteria2, Trojan, Shadowsocks, TUIC).
- Add a hop or backup node with **one command** pasted into its terminal.
- Route by domain lists (URL sources or your own): direct, through another
  node, or block.
- Client-side failover through `urltest`/`selector` groups in subscriptions,
  plus server-side health checks.
- Resist active probing: REALITY, Hysteria2 masquerade, decoy fallback, and a
  hidden panel.
- OIDC single sign-on (tested with pocket-id).
- Fit on a 1 vCPU / 1 GB VPS.

## Quick start

```sh
curl -fsSLO https://raw.githubusercontent.com/PaulichP/Portunus/main/docker-compose.yml
docker compose up -d
docker compose logs portunus                     # address, login and password
ssh -N -L 2053:127.0.0.1:2053 user@your-server   # then open http://localhost:2053
```

Full instructions: [docs/DEPLOY.md](docs/DEPLOY.md).

## Documentation

The user guide is built into the panel (**Documentation** in the sidebar):
protocols, certificates, clients and subscriptions, apps, firewall,
troubleshooting, backups. The files below cover deployment and design.

- [Architecture](docs/ARCHITECTURE.md)
- [Decisions](docs/DECISIONS.md)
- [Deployment](docs/DEPLOY.md)
- [Security](docs/SECURITY.md)
- [Design system](docs/DESIGN.md)

## License

[AGPL-3.0](LICENSE)
