# Contributing

Thank you for wanting to help. This page says how changes get in and what
they have to pass on the way.

## Branches

| Branch | What it is |
|---|---|
| `dev` | Where work comes together. Every pull request goes here. |
| `main` | What is released. It takes changes from `dev` only. |

A release is a tag on `main`: `dev` is merged into `main` through a pull
request, and the merge commit is tagged `vX.Y.Z` (see
[Versions and releases](docs/DEPLOY.md#versions-and-releases)). Only
maintainers merge into `main` and create release tags.

## Sending a change

1. Fork the repository and branch off `dev`.
2. Make the change, with tests where it changes behaviour.
3. Run the checks below locally.
4. Open a pull request against `dev`, saying what the change does and why.

Small, focused pull requests are reviewed fastest. For anything large,
open an issue first so the approach can be agreed before the work is done.

## Checks

Every pull request, and every push to `dev` and `main`, runs
[`checks.yml`](.github/workflows/checks.yml). A pull request can be merged
only when all of them pass:

| Check | What it does |
|---|---|
| `go` | `gofmt`, `go vet` and the server's tests with the race detector |
| `web` | type check, ESLint, Prettier and the production build of the web app |
| `sast` | static analysis of the code with Semgrep |
| `codeql (…)` | static analysis with CodeQL, for Go, TypeScript and the workflows |
| `sca` | known vulnerabilities in the Go modules and web packages (govulncheck, pnpm audit, Trivy) and secrets in the whole history (gitleaks) |
| `images` | both images built and scanned with Trivy, as a release would publish them |
| `dast` | the panel started from its image and probed from outside with OWASP ZAP |
| `workflows` | the workflows checked with actionlint |
| `source` | a pull request into `main` comes from `dev` |

A finding of HIGH or CRITICAL severity fails its check. A finding that is
accepted on purpose is listed with its reason in `.trivyignore` or
`.zap/rules.tsv`; such a change needs a maintainer's agreement.

The same, on your machine:

```sh
cd server && gofmt -l . && go vet -tags noui ./... && go test -tags noui -race ./...
cd web && pnpm install && pnpm exec tsc --noEmit && pnpm exec eslint && npx prettier@3.9.10 --check . && pnpm build
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
