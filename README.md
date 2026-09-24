# Portunus

A lightweight, self-hosted control panel for [sing-box](https://sing-box.sagernet.org).
One container, tens of megabytes of RAM, loopback-only by default.

> **Status: early development (stage 1 of 8).** The panel shell, local
> accounts and deployment are in place. sing-box management, multihop,
> list-based routing, failover, probe resistance and SSO follow. See
> [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

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
docker compose up -d
docker compose logs portunus | grep "initial admin"
ssh -N -L 2053:127.0.0.1:2053 user@your-server   # then open http://localhost:2053
```

Full instructions: [docs/DEPLOY.md](docs/DEPLOY.md).

## Documentation

- [Architecture](docs/ARCHITECTURE.md)
- [Decisions](docs/DECISIONS.md)
- [Deployment](docs/DEPLOY.md)
- [Security](docs/SECURITY.md)
- [Design system](docs/DESIGN.md)

## License

[AGPL-3.0](LICENSE)
