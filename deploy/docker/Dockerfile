ARG GO_IMAGE=registry.cn-shenzhen.aliyuncs.com/wl4g/golang:1.26-alpine
ARG RUNTIME_IMAGE=registry.cn-shenzhen.aliyuncs.com/wl4g/alpine:3.21
# ─── Stage 1: Build flowgent-core ────────────────────────────────
FROM ${GO_IMAGE} AS builder

ARG GOPROXY
ARG GOFLAGS
ARG GOMAXPROCS
ARG BUILD_TS=0
ARG BUILD_TAGS=
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=bind,source=.,target=/build \
    set -e; \
    if [ -n "${GOFLAGS}" ]; then export GOFLAGS; fi; \
    if [ -n "${GOMAXPROCS}" ]; then export GOMAXPROCS; fi; \
    cd /build/pkg/cmd; \
    CGO_ENABLED=0 go build -v -trimpath -tags "${BUILD_TAGS}" -ldflags="-s -w -X main.BuildTime=$BUILD_TS" -o /out/flowgent ./pkg/

# ─── Stage 2: Export ────────────────────────────────────────────
FROM scratch AS export
COPY --from=builder /out/ /

# ─── Stage 3: Runtime ───────────────────────────────────────────
FROM ${RUNTIME_IMAGE} AS runtime-base

RUN export http_proxy="${HTTP_PROXY:-}" https_proxy="${HTTPS_PROXY:-}"; \
    if command -v apk >/dev/null; then \
      apk add --no-cache ca-certificates tzdata curl jq bash git python3; \
    else \
      apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ca-certificates tzdata curl jq bash git && rm -rf /var/lib/apt/lists/*; \
    fi
ENV TZ=Asia/Shanghai \
    FLOWGENT_MIGRATION_DIR=/app/migration

COPY etc/ /app/etc/
COPY migration/ /app/migration/

ENTRYPOINT ["/app/flowgent"]
CMD ["--help"]

# E2E can reuse the approved host cross-build and its verified module cache.
FROM runtime-base AS runtime-prebuilt
COPY bin/linux-amd64/flowgent-core /app/flowgent

# Preserve the normal self-contained image build as the default final target.
FROM runtime-base AS runtime
COPY --from=builder /out/flowgent /app/flowgent
