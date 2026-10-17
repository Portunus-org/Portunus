# Contributing

Thank you for wanting to help. This page says how changes get in and what
they have to pass on the way.

## Branches

| Branch | What it is |
|---|---|
| `dev` | Where work comes together. Every pull request goes here. |
| `main` | What is released. It takes changes from `dev` only. |

A release is made by merging `dev` into `main` through a pull request whose
title names the version, as in `v0.4.4: SSO and phone layout`: the merge
commit is tagged with it and the images are published (see
[Versions and releases](docs/DEPLOY.md#versions-and-releases)). After every
merge `dev` is moved forward to `main`, so the two stay level. Only
maintainers merge into `main` and create release tags.

## Sending a change

1. Fork the repository and branch off `dev`.
2. Make the change, with tests where it changes behaviour.
3. Run the checks below locally.
4. Open a pull request against `dev`, saying what the change does and why.

Small, focused pull requests are reviewed fastest. For anything large,
open an issue first so the approach can be agreed before the work is done.

## Checks

Merging `dev` into `main` is what makes a release possible, so that pull
request is scanned first ([`checks.yml`](.github/workflows/checks.yml)), and
it can be merged only when every check passes:

| Check | What it does |
|---|---|
| `sast` | static analysis of the code with Semgrep |
| `sca` | known vulnerabilities in the Go modules and web packages (govulncheck, pnpm audit, Trivy) and secrets in the whole history (gitleaks) |
| `images` | both images built and scanned with Trivy, as a release would publish them |
| `dast` | the panel started from its image and probed from outside with OWASP ZAP |
| `source` | the pull request into `main` comes from `dev` |
| `version` | the version in the title, if any, is new and higher than the last release |

A finding of HIGH or CRITICAL severity fails its check, and so does any
ZAP warning that is not accepted. A finding accepted on purpose is listed
with its reason in `.trivyignore` or `.zap/rules.tsv`; such a change needs
a maintainer's agreement.

Before sending a change, run what applies to it:

```sh
cd server && gofmt -l . && go vet -tags noui ./... && go test -tags noui ./...
cd web && pnpm install && pnpm exec tsc --noEmit && pnpm exec eslint && pnpm build
docker build -f deploy/Dockerfile -t portunus:local . && docker build -f deploy/Dockerfile --target node -t portunus-node:local .
scripts/scan.sh
```

## Style

The design rules of the interface are in [docs/DESIGN.md](docs/DESIGN.md),
the reasons behind larger choices in [docs/DECISIONS.md](docs/DECISIONS.md).
Interface texts live in `web/locales/<code>.json`, never in components.

## Security issues

Do not open a public issue for a vulnerability. Report it privately, as
described in [SECURITY.md](.github/SECURITY.md).

## License

By contributing you agree that your contribution is licensed under the
[GNU AGPL v3.0](LICENSE), like the rest of the project.
