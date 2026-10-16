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

Every change is checked before it is merged: static analysis (Semgrep,
CodeQL), known vulnerabilities in the dependencies (govulncheck, pnpm audit,
Trivy), secrets in the history (gitleaks), the built images (Trivy) and the
running panel probed from outside (OWASP ZAP). A HIGH or CRITICAL finding
blocks the merge. The published images are scanned again every week.

The threat model and the controls in place are described in
[docs/SECURITY.md](../docs/SECURITY.md).
