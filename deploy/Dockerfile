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

# --- data plane: sing-box built from source ---------------------------------
# Built here rather than taken from the release archive: Portunus needs the
# v2ray_api stats service (per-client traffic), which release builds omit,
# and does not need tailscale, cloudflared & co. (smaller binary, less RAM).
FROM ${GO_IMAGE} AS singbox
ARG SINGBOX_VERSION=v1.14.1
ARG SINGBOX_COMMIT=1ac1a339cb1223e9c70eae14c44411c75033c02d
ARG SINGBOX_TAGS=with_quic,with_utls,with_acme,with_clash_api,with_v2ray_api,badlinkname,tfogo_checklinkname0
RUN apk add --no-cache git patch
RUN git clone --depth 1 --branch "${SINGBOX_VERSION}" https://github.com/SagerNet/sing-box.git /src/sing-box \
 && test "$(git -C /src/sing-box rev-parse HEAD)" = "${SINGBOX_COMMIT}"
WORKDIR /src/sing-box
# Security bumps of sing-box's dependencies ahead of its own release: fixes
# for CVEs in grpc (v2ray_api), x/crypto and x/mod that trivy reports in the
# release's module graph. Pinned, so builds stay reproducible; drop an entry
# once sing-box itself requires that version or newer.
ARG SINGBOX_DEP_BUMPS="golang.org/x/crypto@v0.55.0 golang.org/x/mod@v0.40.0 google.golang.org/grpc@v1.83.2"
RUN --mount=type=cache,target=/go/pkg/mod \
    go get ${SINGBOX_DEP_BUMPS} && go mod tidy
# Patched sing-quic: a stable stateless-reset key so Hysteria2/TUIC clients
# reconnect immediately after a reload instead of after a 30 s idle timeout
# (docs/DECISIONS.md, D-17). The patch targets one sing-quic version; the
# build fails loudly if a sing-box upgrade moves it.
ARG SINGQUIC_VERSION=v0.7.0
COPY deploy/patches/sing-quic-stable-stateless-reset.patch /patches/
RUN --mount=type=cache,target=/go/pkg/mod \
    test "$(go list -m -f '{{.Version}}' github.com/sagernet/sing-quic)" = "${SINGQUIC_VERSION}" \
 && cp -r "$(go mod download -json github.com/sagernet/sing-quic@${SINGQUIC_VERSION} | sed -n 's/.*"Dir": "\(.*\)",/\1/p')" /src/sing-quic \
 && chmod -R u+w /src/sing-quic \
 && patch -d /src/sing-quic -p1 < /patches/sing-quic-stable-stateless-reset.patch \
 && go mod edit -replace github.com/sagernet/sing-quic=/src/sing-quic
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -tags "${SINGBOX_TAGS}" \
      -ldflags "-s -w -X github.com/sagernet/sing-box/constant.Version=${SINGBOX_VERSION#v} -X runtime.godebugDefault=multipathtcp=0,tlssha1=1 -checklinkname=0" \
      -o /out/sing-box ./cmd/sing-box
# Let the unprivileged process bind ports below 1024 (443), nothing else.
# Set here so the runtime image does not store the binary twice; COPY keeps
# the security.capability attribute.
RUN apk add --no-cache libcap-setcap && setcap cap_net_bind_service=+ep /out/sing-box

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

# --- node: the agent alone --------------------------------------------------
# A node runs sing-box with what the panel sends it and nothing else: no web
# interface, no database, no accounts. Its binary is built without the
# frontend, so `docker build --target node` never touches Node.js.
FROM ${GO_IMAGE} AS build-node
ARG VERSION=dev
ARG COMMIT=unknown
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY server/ ./
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X github.com/PaulichP/Portunus/server/internal/version.Version=${VERSION} -X github.com/PaulichP/Portunus/server/internal/version.Commit=${COMMIT}" \
      -o /out/portunus-node ./cmd/portunus-node \
 && install -d -m 0700 /out/data

# The image is the two binaries and the CA roots: no shell, no package
# manager, nothing to log in to. (sing-box got its file capability in the
# stage that built it; COPY keeps it.)
FROM scratch AS node
COPY --from=build-node /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=singbox /out/sing-box /usr/local/bin/sing-box
COPY --from=build-node /out/portunus-node /usr/local/bin/portunus-node
COPY --from=build-node --chown=10001:10001 /out/data /var/lib/portunus
USER 10001:10001
ENV PATH=/usr/local/bin \
    PORTUNUS_DATA_DIR=/var/lib/portunus \
    PORTUNUS_LOG_FORMAT=json \
    GOMEMLIMIT=32MiB
VOLUME ["/var/lib/portunus"]
ENTRYPOINT ["portunus-node"]

# --- panel ----------------------------------------------------------------
# The default target. Alpine rather than distroless: setcap on the sing-box
# binary needs a package manager at build time (see docs/DECISIONS.md, D-07).
FROM ${RUNTIME_IMAGE} AS panel
COPY --from=singbox /out/sing-box /usr/local/bin/sing-box
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
