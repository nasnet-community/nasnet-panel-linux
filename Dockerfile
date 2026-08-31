# Stage: Build frontend
FROM --platform=$BUILDPLATFORM node:22-alpine AS web-builder
RUN corepack enable && corepack prepare pnpm@10.28.2 --activate
WORKDIR /web
COPY web-panel/package.json web-panel/pnpm-lock.yaml web-panel/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY web-panel/ ./
RUN pnpm build

# Build stage
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

WORKDIR /app

# Install dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy source code
COPY . .

# Download Iran geofiles for embedding
RUN mkdir -p pkg/geofiles/embedded && \
    wget -q -O pkg/geofiles/embedded/geoip.dat \
      "https://github.com/chocolate4u/Iran-v2ray-rules/releases/latest/download/geoip.dat" && \
    wget -q -O pkg/geofiles/embedded/geosite.dat \
      "https://github.com/chocolate4u/Iran-v2ray-rules/releases/latest/download/geosite.dat"

# Copy built frontend from web-builder stage
COPY --from=web-builder /web/dist ./web-panel/dist

# Build main binary (panel + embedded xray node)
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    set -eux; \
    export CGO_ENABLED=0 GOOS=linux; \
    if [ -n "${TARGETARCH:-}" ]; then export GOARCH="${TARGETARCH}"; fi; \
    go build -ldflags="-w -s \
      -X github.com/nasnet-community/nasnet-panel-linux/cmd.Version=${VERSION} \
      -X github.com/nasnet-community/nasnet-panel-linux/cmd.Commit=${COMMIT} \
      -X github.com/nasnet-community/nasnet-panel-linux/cmd.BuildTime=${BUILD_TIME}" \
      -o main .

# Final stage
FROM alpine:3.21

WORKDIR /app

# Install runtime dependencies. iproute2 + iptables are needed for xray bandwidth shaping
RUN apk --no-cache add ca-certificates tzdata postgresql16-client wget su-exec sqlite unzip iproute2 iptables openssh-client

# Install xray-core. The single binary runs xray as a local child.
# Pinned to the version the panel expects and verified against the
# release's published .dgst — never "latest", never unchecked.
ARG TARGETARCH
ARG XRAY_VERSION=26.7.28
RUN set -eux; \
    case "${TARGETARCH:-amd64}" in \
      amd64) asset="Xray-linux-64.zip" ;; \
      arm64) asset="Xray-linux-arm64-v8a.zip" ;; \
      *) echo "unsupported TARGETARCH: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    url="https://github.com/XTLS/Xray-core/releases/download/v${XRAY_VERSION}/${asset}"; \
    wget -q -O /tmp/xray.zip "$url"; \
    wget -q -O /tmp/xray.zip.dgst "${url}.dgst"; \
    want=$(grep -iE 'sha2?-?256' /tmp/xray.zip.dgst | grep -oE '[0-9a-f]{64}' | head -1); \
    test -n "$want"; \
    echo "${want}  /tmp/xray.zip" | sha256sum -c -; \
    mkdir -p /usr/local/bin /usr/local/etc/xray; \
    unzip -o /tmp/xray.zip xray -d /usr/local/bin/; \
    chmod +x /usr/local/bin/xray; \
    rm /tmp/xray.zip /tmp/xray.zip.dgst

# Create non-root user with a stable UID/GID so host bind mounts
# (e.g. ./data/backups) can be chowned to match.
RUN addgroup -g 1000 appgroup && adduser -D -u 1000 -G appgroup appuser

# Copy binary from builder
COPY --from=builder /app/main .

# Ship geoip/geosite next to the xray config so routing rules resolve
COPY --from=builder /app/pkg/geofiles/embedded/geoip.dat /usr/local/etc/xray/geoip.dat
COPY --from=builder /app/pkg/geofiles/embedded/geosite.dat /usr/local/etc/xray/geosite.dat

# Create data directories with proper ownership. xray writes its config,
# certs, logs, and traffic buffer under these paths, so appuser must own them
RUN mkdir -p /app/data/backups /app/data/acme \
        /usr/local/etc/xray /var/log/xray /var/lib/nasnet-agent && \
    chown -R appuser:appgroup /app /usr/local/etc/xray /var/log/xray /var/lib/nasnet-agent

# Copy entrypoint that fixes bind-mount ownership at runtime, then drops
# to appuser via su-exec. Keeping USER root here is intentional — the
# entrypoint demotes privileges before exec'ing the binary.
COPY --chmod=0755 scripts/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

# Expose port (default 9761, configurable via docker-compose)
EXPOSE 9761

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["./main", "serve"]
