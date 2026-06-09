# syntax=docker/dockerfile:1
# ─── Stage 1: Build flowgent ──────────────────────────────────
FROM registry.cn-shenzhen.aliyuncs.com/wl4g/golang:1.26-alpine AS builder

ENV GOPROXY=https://goproxy.cn,direct

ARG BUILD_TS=0
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=bind,source=.,target=/build \
    cd /build/pkg/cmd && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.BuildTime=$BUILD_TS" -o /out/flowgent ./pkg/

# ─── Stage 2: Build GitHub MCP ────────────────────────────────
FROM registry.cn-shenzhen.aliyuncs.com/wl4g/golang:1.26-alpine AS builder-github-mcp

ENV GOPROXY=https://goproxy.cn,direct

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=bind,source=.,target=/build \
    cd /build/examples/security-autonomy-fixer/mcps/github && GOWORK=off CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/github-mcp .

# ─── Stage 3: Build SonarQube MCP ─────────────────────────────
FROM registry.cn-shenzhen.aliyuncs.com/wl4g/golang:1.26-alpine AS builder-sonarqube-mcp

ENV GOPROXY=https://goproxy.cn,direct

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=bind,source=.,target=/build \
    cd /build/examples/security-autonomy-fixer/mcps/sonarqube && GOWORK=off CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sonarqube-mcp .

# ─── Stage 4: Export (used by `make build`) ───────────────────
FROM scratch AS export
COPY --from=builder /out/ /
COPY --from=builder-github-mcp /out/ /
COPY --from=builder-sonarqube-mcp /out/ /

# ─── Stage 5: Runtime ─────────────────────────────────────────
FROM registry.cn-shenzhen.aliyuncs.com/wl4g/alpine:3.21

RUN apk add --no-cache ca-certificates tzdata curl jq bash python3
ENV TZ=Asia/Shanghai

COPY --from=builder /out/flowgent /app/flowgent
COPY --from=builder-github-mcp /out/github-mcp /usr/local/bin/github-mcp
COPY --from=builder-sonarqube-mcp /out/sonarqube-mcp /usr/local/bin/sonarqube-mcp
COPY examples/ /app/examples/
COPY etc/ /app/etc/

ENTRYPOINT ["/app/flowgent"]
CMD ["--help"]
