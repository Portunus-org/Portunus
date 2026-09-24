#!/bin/sh
# Builds bin/portunus with the web UI embedded.
#
#   scripts/build.sh          full build (needs pnpm and Go)
#   scripts/build.sh noui     API-only binary (Go only)
set -eu

cd "$(dirname "$0")/.."
root=$(pwd)

version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
commit=$(git rev-parse --short HEAD 2>/dev/null || echo unknown)
pkg=github.com/PaulichP/Portunus/server
ldflags="-s -w -X $pkg/internal/version.Version=$version -X $pkg/internal/version.Commit=$commit"

tags=""
if [ "${1:-}" = "noui" ]; then
	tags="noui"
else
	(cd web && pnpm install --frozen-lockfile && NEXT_TELEMETRY_DISABLED=1 pnpm build)
	# go:embed only sees files inside the Go module, so the export is copied in.
	rm -rf server/internal/webui/dist
	cp -r web/out server/internal/webui/dist
fi

cd server
CGO_ENABLED=0 go build -tags "$tags" -trimpath -ldflags "$ldflags" -o "$root/bin/portunus" ./cmd/portunus
echo "built $root/bin/portunus ($version)"
