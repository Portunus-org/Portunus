#!/bin/sh
# Scans the finished images for vulnerabilities, the same way a release
# does: HIGH or CRITICAL findings fail. Build the images first.
#
#   scripts/scan.sh                                  portunus:local and portunus-node:local
#   scripts/scan.sh ghcr.io/paulichp/portunus:latest any other image
set -eu
cd "$(dirname "$0")/.."
trivy=aquasec/trivy@sha256:62b1e65e8869bc4b4c6aa4fa2b21595256c7c2f6018a9d9ad61caf87187c1969
[ $# -gt 0 ] || set -- portunus:local portunus-node:local
status=0
for image in "$@"; do
	docker run --rm \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v "$PWD/.trivyignore:/.trivyignore:ro" \
		-v portunus-trivy-cache:/root/.cache \
		"$trivy" image --quiet --exit-code 1 --severity HIGH,CRITICAL \
		--ignorefile /.trivyignore "$image" || status=1
done
exit $status
