# syntax=docker/dockerfile:1
# ─── Stage 1: Build ──────────────────────────────────────────
FROM registry.cn-shenzhen.aliyuncs.com/wl4g/golang:1.26-alpine AS builder

ENV GOPROXY=https://goproxy.cn,direct

ARG BUILD_TS=0
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=bind,source=.,target=/build \
    cd /build/src/cmd && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.BuildTime=$BUILD_TS" -o /out/flowgent ./src/flowgent/

# ─── Stage 2: Export (used by `make build`) ─────────────────
FROM scratch AS export
COPY --from=builder /out/ /

# ─── Stage 3: Runtime ────────────────────────────────────────
FROM registry.cn-shenzhen.aliyuncs.com/wl4g/alpine:3.21

RUN apk add --no-cache ca-certificates tzdata
ENV TZ=Asia/Shanghai

COPY --from=builder /out/flowgent /app/flowgent

ENTRYPOINT ["/app/flowgent"]
CMD ["--help"]
