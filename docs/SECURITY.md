# Security

## Reporting

Please report vulnerabilities privately, as described in
[the security policy](../.github/SECURITY.md), rather than in public issues.

## Threat model (summary)

Portunus administers censorship-circumvention infrastructure. The assumed
adversaries are:

1. **Internet scanners and active probers** that try to identify the server
   as a proxy or reach the admin panel.
2. **Credential attackers** targeting the panel login.
3. **A compromised browser tab or site** trying to act on the operator's
   behalf (CSRF, clickjacking, script injection).
4. **An adversary who obtains a copy of the data volume** (backup leak).

Out of scope: an attacker with root on the host, and traffic confirmation by
a global passive observer.

## Defaults and controls

| Area | Control |
|---|---|
| Exposure | Panel binds to `127.0.0.1` by default; access through SSH tunnel. Non-loopback binding produces a warning in logs and UI. |
| Exposure | Panel behind a probe-resistant decoy on a secret path; unknown paths show the decoy site. The path is one random word of about 6,000 (13 bits), chosen for being easy to remember, so guessing is throttled: more than 10 wrong word-like paths from one address in 10 minutes shut the entry to that address for the window. It hides the panel; the password protects it. |
| Container | Non-root user (uid 10001), read-only root filesystem, all capabilities dropped except `NET_BIND_SERVICE` (granted to the sing-box binary and to the panel binary, which listens on 443 itself when opened by its domain, via a file capability), base images pinned by digest, sing-box built from a verified commit. |
| Passwords | argon2id (m=19 MiB, t=2, p=1); min. length 12; constant-time compare; same-duration failure for unknown users. |
| Bootstrap | Random initial admin password, logged once, must be changed before the API allows anything else. |
| Brute force | Per-IP token bucket and per-username exponential lockout; `429` with `Retry-After`. |
| Sessions | Opaque 256-bit tokens, stored hashed (SHA-256); `HttpOnly`, `SameSite=Strict`, `Secure` under TLS and behind a TLS-terminating proxy (`X-Forwarded-Proto: https`); 24 h idle, 7 d absolute; password change revokes other sessions. |
| CSRF | Per-session token header on all state-changing requests; cross-origin `Origin` rejected; JSON-only bodies. |
| Input | Every SQL statement uses placeholders; nothing typed is ever part of a statement (a table name, where one is needed, comes from a fixed list). Typed text reaches the sing-box configuration and client profiles through a JSON encoder, links through a URL encoder, pages through React, and no shell at all: the only program the panel runs is sing-box, with arguments of its own. Numbers are checked against their ranges, addresses and URLs against their shape (`http`/`https` only, no login in them, lists never fetched from private addresses), names against control and direction-reversing characters, identifiers against being numbers of things that exist. Unknown fields and wrong types are refused. A form is saved whole or not at all. Two tests keep this: every hostile value in every field of every changing request (about 5,400 requests) must never produce a server error or damage the database, and about 230 kinds of nonsense must each be refused. |
| Single sign-on | OpenID Connect, authorization code with PKCE (S256), `state` and `nonce`; the attempt is kept in a sealed, `HttpOnly`, `SameSite=Lax` cookie scoped to the two sign-in paths and is good once, for 10 minutes. The ID token's signature, issuer, audience, expiry and nonce are verified (go-oidc). The provider must be HTTPS (plain HTTP on loopback only); the panel follows no redirects when talking to it. A person is known by issuer and subject, never by name or email: an identity is never joined to an existing account, a taken name gets a number. |
| Roles | `admin` everything; `operator` clients and connections, including their keys and links; `viewer` reads, without keys, links, logs or the generated configuration. Read at every request, so a change applies to sessions already open. From a provider the role comes from the person's groups (exact names) at every sign-in; someone in no mapped group is not let in unless a default role is set. |
| Break-glass | One enabled administrator with a password is always kept: it cannot be demoted, disabled or deleted, whoever asks. Administrators from a provider do not count, as the provider can take the role back or be unreachable. |
| Browser hardening | Per-page CSP with hashed inline scripts (no `unsafe-inline` for scripts), `frame-ancestors 'none'`, `X-Frame-Options: DENY`, `nosniff`, `Referrer-Policy: no-referrer`, COOP/CORP; self-hosted fonts. |
| Audit | Login, failed login, logout, password change and reset are written to `audit_log`; the panel has no page for it yet. |
| Secrets at rest | Inbound private keys and the Cloudflare token are encrypted with AES-256-GCM under a master key kept outside the database, each bound to its row. |
| Data plane isolation | Clients cannot reach the host's loopback or private networks through the proxy (the panel and stats API live there); sing-box runs unprivileged with only `NET_BIND_SERVICE`. |
| Private CA | Per-installation CA for TLS inbounds without a public certificate; key encrypted at rest, never served; server certificates 397 days, renewed automatically; the CA certificate is published with its SHA-256 fingerprint to compare on the device. |
| Nodes | The panel reaches each node's agent over TLS 1.3 with certificates from the installation's CA on both sides: the node proves its address, the panel presents a client-only certificate that a node cannot reuse. The join command carries the node's key and is shown to admins only. |
| Routing | Traffic meant for an exit that is gone or down is refused, never sent directly; list downloads refuse loopback and private addresses; the private-address guard applies to everything leaving directly, whatever the rules. |
| Third-party lookups | The panel asks external services for its own public address (first run) and for the location of each server (`PORTUNUS_GEO_LOOKUP`) and fetches map tiles (`PORTUNUS_MAP_TILES`), both for the map of nodes and both can be turned off; route lists are downloaded from the URLs the operator chose. The browser itself contacts nothing but the panel. |
| Subscriptions | Public only through the optional dedicated listener, which serves nothing else; keyed by 256-bit tokens; unknown or disabled tokens get a plain 404; rotation revokes the old URL. |
| Access control | Viewer role is read-only; client links and credentials are operator-only; settings, logs and config are admin-only; every change is audited. |
| Probe resistance | Proxies: REALITY; Hysteria2 masquerade and Salamander obfuscation; Trojan falls back to the cover site (main server). Panel: on its public HTTPS port everybody who has not opened the secret entry address is answered by the cover site (a small site the panel generates, or another site passed on), with none of the panel's headers; a handshake without the domain's name fails; VLESS can share the port, presenting the panel's own domain and certificate. The private CA's name still says "Portunus" in certificates of TLS inbounds without a public certificate. |
| Supply chain | trivy (image, repository) and semgrep must report no HIGH or CRITICAL findings; lagging sing-box dependencies are bumped in the Dockerfile at pinned versions; a finding accepted on purpose is listed in `.trivyignore` with its reason. |

## Operational advice

- Keep the default loopback binding unless you have a reason not to. SSH is
  already hardened and monitored on most servers; a web login page is not.
- Treat the data volume as secret: it contains password hashes and
  private keys.
- Install the Portunus CA only on your own devices, and check the
  fingerprint the device shows against the one in Settings before trusting
  it: a CA downloaded over plain HTTP could have been swapped on the way.
- `docker compose logs` contains the initial admin password until you
  rotate logs. Change the password on first login. It is required anyway.
