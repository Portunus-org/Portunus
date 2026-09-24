# Security

## Reporting

Please report vulnerabilities privately through GitHub Security Advisories
on this repository rather than in public issues.

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

| Area | Control | Status |
|---|---|---|
| Exposure | Panel binds to `127.0.0.1` by default; access through SSH tunnel. Non-loopback binding produces a warning in logs and UI. | built |
| Exposure | Panel behind a probe-resistant decoy on a secret path; unknown paths show the decoy site. | stage 6 |
| Container | Non-root user (uid 10001), read-only root filesystem, all capabilities dropped, `no-new-privileges`, base images pinned by digest. | built |
| Passwords | argon2id (m=19 MiB, t=2, p=1); min. length 12; constant-time compare; same-duration failure for unknown users. | built |
| Bootstrap | Random initial admin password, logged once, must be changed before the API allows anything else. | built |
| Brute force | Per-IP token bucket and per-username exponential lockout; `429` with `Retry-After`. | built |
| Sessions | Opaque 256-bit tokens, stored hashed (SHA-256); `HttpOnly`, `SameSite=Strict`, `Secure` under TLS; 24 h idle, 7 d absolute; password change revokes other sessions. | built |
| CSRF | Per-session token header on all state-changing requests; cross-origin `Origin` rejected; JSON-only bodies. | built |
| Browser hardening | Per-page CSP with hashed inline scripts (no `unsafe-inline` for scripts), `frame-ancestors 'none'`, `X-Frame-Options: DENY`, `nosniff`, `Referrer-Policy: no-referrer`, COOP/CORP; self-hosted fonts. | built |
| Audit | Login, failed login, logout, password change and reset are written to `audit_log`. | built; UI in stage 8 |
| Secrets at rest | Key material encrypted with AES-256-GCM under a master key kept outside the database (D-13). | stage 2 |
| Probe resistance | REALITY, Hysteria2 masquerade, TLS fallback to decoy. | stage 6 |
| SSO | OIDC Authorization Code + PKCE. | stage 7 |

## Operational advice

- Keep the default loopback binding unless you have a reason not to. SSH is
  already hardened and monitored on most servers; a web login page is not.
- Treat the data volume as secret: it contains password hashes and, from
  stage 2, private keys.
- `docker compose logs` contains the initial admin password until you
  rotate logs. Change the password on first login. It is required anyway.
