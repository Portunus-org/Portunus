# syntax=docker/dockerfile:1

# Base images are pinned by digest; bump tag and digest together.
ARG NODE_IMAGE=node:26.9.0-alpine3.24@sha256:dbaa92e5758cbbcf85d65d5403fdb530fe3442cbe8c6dbfb7ef23365450d5070
ARG GO_IMAGE=golang:1.27.1-alpine3.24@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414
ARG RUNTIME_IMAGE=alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

# --- frontend: Next.js static export --------------------------------------
FROM ${NODE_IMAGE} AS web
ARG PNPM_VERSION=12.6.0
ENV NEXT_TELEMETRY_DISABLED=1 PNPM_HOME=/pnpm PATH=/pnpm:$PATH
RUN npm install -g pnpm@${PNPM_VERSION}
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN --mount=type=cache,id=pnpm,target=/pnpm/store pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

# --- backend: static Go binary with the frontend embedded ------------------
FROM ${GO_IMAGE} AS build
ARG VERSION=dev
ARG COMMIT=unknown
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY server/ ./
COPY --from=web /src/web/out internal/webui/dist
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X github.com/PaulichP/Portunus/server/internal/version.Version=${VERSION} -X github.com/PaulichP/Portunus/server/internal/version.Commit=${COMMIT}" \
      -o /out/portunus ./cmd/portunus

# --- runtime --------------------------------------------------------------
# Alpine rather than distroless: the sing-box stage needs setcap on the
# binary to bind :443 without root (see docs/DECISIONS.md, D-07).
FROM ${RUNTIME_IMAGE}
RUN addgroup -S -g 10001 portunus \
 && adduser -S -D -H -u 10001 -G portunus -s /sbin/nologin portunus \
 && install -d -o portunus -g portunus -m 0700 /var/lib/portunus
COPY --from=build /out/portunus /usr/local/bin/portunus

USER portunus:portunus
ENV PORTUNUS_DATA_DIR=/var/lib/portunus \
    PORTUNUS_LOG_FORMAT=json \
    GOMEMLIMIT=96MiB
VOLUME ["/var/lib/portunus"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["portunus", "healthcheck"]
ENTRYPOINT ["portunus"]
CMD ["serve"]
