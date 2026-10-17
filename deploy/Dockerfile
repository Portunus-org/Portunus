# syntax=docker/dockerfile:1

# Base images are pinned by digest; bump tag and digest together.
ARG NODE_IMAGE=node:26.9.0-alpine3.24@sha256:dbaa92e5758cbbcf85d65d5403fdb530fe3442cbe8c6dbfb7ef23365450d5070
ARG GO_IMAGE=golang:1.27.2-alpine3.24@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d
ARG RUNTIME_IMAGE=alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

# --- frontend: Next.js static export --------------------------------------
# The build stages run on the machine that builds (BUILDPLATFORM) and
# cross-compile for the platform the image is for: an image for ARM built on
# an x86 runner needs no emulation, which would take an hour instead of
# minutes.
FROM --platform=$BUILDPLATFORM ${NODE_IMAGE} AS web
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
FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS singbox
ARG TARGETOS
ARG TARGETARCH
ARG SINGBOX_VERSION=v1.14.1
ARG SINGBOX_COMMIT=1ac1a339cb1223e9c70eae14c44411c75033c02d
ARG SINGBOX_TAGS=with_quic,with_utls,with_acme,with_clash_api,with_v2ray_api,badlinkname,tfogo_checklinkname0
RUN apk add --no-cache git patch
RUN git clone --depth 1 --branch "${SINGBOX_VERSION}" https://github.com/SagerNet/sing-box.git /src/sing-box \
 && test "$(git -C /src/sing-box rev-parse HEAD)" = "${SINGBOX_COMMIT}"
WORKDIR /src/sing-box
# Security bumps of sing-box's dependencies ahead of its own release: fixes
# for CVEs in grpc (v2ray_api), x/crypto, x/mod and x/net that trivy reports in the
# release's module graph. Pinned, so builds stay reproducible; drop an entry
# once sing-box itself requires that version or newer.
ARG SINGBOX_DEP_BUMPS="golang.org/x/crypto@v0.57.0 golang.org/x/mod@v0.41.0 golang.org/x/net@v0.60.0 google.golang.org/grpc@v1.83.2"
RUN --mount=type=cache,target=/go/pkg/mod \
    go get ${SINGBOX_DEP_BUMPS} && go mod tidy
# Patched sing-quic: a stable stateless-reset key so Hysteria2/TUIC clients
# reconnect immediately after a reload instead of after a 30 s idle timeout
# (the key is derived from the master key by the panel). The patch targets one sing-quic version; the
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
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -tags "${SINGBOX_TAGS}" \
      -ldflags "-s -w -X github.com/sagernet/sing-box/constant.Version=${SINGBOX_VERSION#v} -X runtime.godebugDefault=multipathtcp=0,tlssha1=1 -checklinkname=0" \
      -o /out/sing-box ./cmd/sing-box
# Let the unprivileged process bind ports below 1024 (443), nothing else.
# Set here so the runtime image does not store the binary twice; COPY keeps
# the security.capability attribute.
RUN apk add --no-cache libcap-setcap && setcap cap_net_bind_service=+ep /out/sing-box

# --- backend: static Go binary with the frontend embedded ------------------
FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY server/ ./
COPY --from=web /src/web/out internal/webui/dist
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
      -ldflags "-s -w -X github.com/Portunus-org/Portunus/server/internal/version.Version=${VERSION} -X github.com/Portunus-org/Portunus/server/internal/version.Commit=${COMMIT}" \
      -o /out/portunus ./cmd/portunus \
 && install -d -m 0700 /out/data && install -d -m 0777 /out/run/portunus-update
# The panel listens on 443 itself when it is opened by its domain, and runs
# as an unprivileged user as well: the same single capability as sing-box.
RUN apk add --no-cache libcap-setcap && setcap cap_net_bind_service=+ep /out/portunus

# --- node: the agent alone --------------------------------------------------
# A node runs sing-box with what the panel sends it and nothing else: no web
# interface, no database, no accounts. Its binary is built without the
# frontend, so `docker build --target node` never touches Node.js.
FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS build-node
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY server/ ./
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
      -ldflags "-s -w -X github.com/Portunus-org/Portunus/server/internal/version.Version=${VERSION} -X github.com/Portunus-org/Portunus/server/internal/version.Commit=${COMMIT}" \
      -o /out/portunus-node ./cmd/portunus-node \
 && install -d -m 0700 /out/data && install -d -m 0777 /out/run/portunus-update

# The image is the two binaries and the CA roots: no shell, no package
# manager, nothing to log in to. (sing-box got its file capability in the
# stage that built it; COPY keeps it.)
FROM scratch AS node
COPY --from=build-node /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=singbox /out/sing-box /usr/local/bin/sing-box
COPY --from=build-node /out/portunus-node /usr/local/bin/portunus-node
COPY --from=build-node --chown=10001:10001 --chmod=700 /out/data /var/lib/portunus
# Shared with the updater, which runs as another user: a version number goes
# one way and a status the other, nothing else is kept here. Under /run and
# open to everyone, so that both can reach it whatever user they run as.
COPY --from=build-node /out/run /run
USER 10001:10001
ENV PATH=/usr/local/bin \
    PORTUNUS_DATA_DIR=/var/lib/portunus \
    PORTUNUS_LOG_FORMAT=json \
    GOMEMLIMIT=32MiB
VOLUME ["/var/lib/portunus"]
# The repository the image comes from: GHCR links the package to it, so the
# package shows on the repository and takes its permissions.
LABEL org.opencontainers.image.source="https://github.com/Portunus-org/Portunus" \
      org.opencontainers.image.licenses="AGPL-3.0-only"
ENTRYPOINT ["portunus-node"]

# --- panel ----------------------------------------------------------------
# The default target. Alpine, for a shell to look around with (`docker
# compose exec portunus sh`). Nothing is run in this stage, so it builds for
# any platform without emulation; the service's user is given by number.
FROM ${RUNTIME_IMAGE} AS panel
COPY --from=singbox /out/sing-box /usr/local/bin/sing-box
COPY --from=build /out/portunus /usr/local/bin/portunus
COPY --from=build --chown=10001:10001 --chmod=700 /out/data /var/lib/portunus
# Shared with the updater (see the node image).
COPY --from=build /out/run /run

USER 10001:10001
ENV PORTUNUS_DATA_DIR=/var/lib/portunus \
    PORTUNUS_LOG_FORMAT=json \
    GOMEMLIMIT=96MiB
VOLUME ["/var/lib/portunus"]
# Checked every second while starting, so "docker compose up --wait" returns
# as soon as the panel answers (and its log already shows how to sign in).
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --start-interval=1s --retries=3 CMD ["portunus", "healthcheck"]
# The repository the image comes from: GHCR links the package to it, so the
# package shows on the repository and takes its permissions.
LABEL org.opencontainers.image.source="https://github.com/Portunus-org/Portunus" \
      org.opencontainers.image.licenses="AGPL-3.0-only"
ENTRYPOINT ["portunus"]
CMD ["serve"]
