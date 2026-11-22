ARG GO_IMAGE
ARG RUNTIME_IMAGE
FROM ${GO_IMAGE} AS builder
ARG GOFLAGS=-p=1
ARG GOMAXPROCS=1
RUN export https_proxy="${HTTPS_PROXY:-}"; apk add --no-cache make
WORKDIR /build
COPY Makefile ./Makefile
COPY use-cases/autotest-generator/deploy/sonarqube-mcp/source/ ./component/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    make build:e2e-component E2E_COMPONENT_SOURCE=/build/component E2E_COMPONENT_OUTPUT=/build/e2e-service
FROM ${RUNTIME_IMAGE}
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /build/e2e-service /usr/local/bin/e2e-service
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/e2e-service"]
