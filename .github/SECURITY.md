# Security policy

## Reporting a vulnerability

Please report vulnerabilities privately through
[GitHub Security Advisories](https://github.com/Portunus-org/Portunus/security/advisories/new),
not in public issues or pull requests.

Include what is affected, how to reproduce it, and what an attacker could
achieve. You will get an answer within a few days; a fix is released as a
new version, and the advisory is published once servers can update.

## Supported versions

Only the latest release receives security fixes. The panel can install a
new release by itself (Settings → Updates).

## How the project guards itself

Nothing reaches a release unchecked: before dev can be merged into main,
the change is scanned with static analysis (Semgrep), for known
vulnerabilities in the dependencies (govulncheck, pnpm audit, Trivy) and
secrets in the history (gitleaks), the built images are scanned (Trivy),
and the running panel is probed from outside (OWASP ZAP). A HIGH or
CRITICAL finding blocks the merge. The published images are scanned again
every week.

The threat model and the controls in place are described in
[docs/SECURITY.md](../docs/SECURITY.md).
