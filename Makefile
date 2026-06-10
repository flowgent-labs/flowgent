.PHONY: help build build-all build-host build-host-all build-image build-image-all clean test fmt

BIN_DIR  ?= bin
GO       ?= $(GOROOT)/bin/go
LDFLAGS  := -s -w -X main.Version=dev -X main.GitCommit=$(shell git rev-parse --short HEAD 2>/dev/null || echo unknown) -X main.BuildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)

help:
	@echo "Flowgent -- Makefile"
	@echo ""
	@echo "  Docker builds (no host Go):"
	@echo "    make build             flowgent binary via Docker"
	@echo "    make build-all         flowgent + MCPs via Docker"
	@echo "    make build-image       production image (flowgent:latest)"
	@echo "    make build-image-all   all-in-one image (flowgent:all-in-one)"
	@echo ""
	@echo "  Host builds (dev, requires Go):"
	@echo "    make build-host        flowgent on host"
	@echo "    make build-host-all    flowgent + MCPs on host"
	@echo ""
	@echo "  Utils: make test fmt clean"

.DEFAULT_GOAL := help

build:
	@mkdir -p $(BIN_DIR)
	DOCKER_BUILDKIT=1 docker build -f deploy/docker/Dockerfile --target export --output type=local,dest=$(BIN_DIR) .

build-all:
	@mkdir -p $(BIN_DIR)
	DOCKER_BUILDKIT=1 docker build -f deploy/docker/Dockerfile.all-in-one --target export --output type=local,dest=$(BIN_DIR) .

build-image:
	DOCKER_BUILDKIT=1 docker build -t flowgent:latest -f deploy/docker/Dockerfile .

build-image-all:
	DOCKER_BUILDKIT=1 docker build -t flowgent:all-in-one -f deploy/docker/Dockerfile.all-in-one .

build-host:
	@mkdir -p $(BIN_DIR)
	cd pkg/cmd && CGO_ENABLED=0 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o ../../$(BIN_DIR)/flowgent ./pkg/

build-host-all: build-host
	@echo "build-host-all: done (MCP examples removed — see git history)"

clean:
	rm -rf $(BIN_DIR)/

test:
	cd pkg/common    && CGO_ENABLED=0 $(GO) test -count=1 -timeout 120s ./...
	cd pkg/model     && CGO_ENABLED=0 $(GO) test -count=1 -timeout 120s ./...
	cd pkg/messager && CGO_ENABLED=0 $(GO) test -count=1 -timeout 120s ./...
	cd pkg/cache     && CGO_ENABLED=0 $(GO) test -count=1 -timeout 120s ./...
	cd pkg/config    && CGO_ENABLED=0 $(GO) test -count=1 -timeout 120s ./...
	cd pkg/wallet    && CGO_ENABLED=0 $(GO) test -count=1 -timeout 120s ./...
	cd pkg/notifier  && CGO_ENABLED=0 $(GO) test -count=1 -timeout 120s ./...
	cd pkg/api       && CGO_ENABLED=0 $(GO) test -count=1 -timeout 120s ./...
	cd pkg/store     && CGO_ENABLED=0 $(GO) test -count=1 -timeout 120s ./...
	cd pkg/sandbox   && CGO_ENABLED=0 $(GO) test -count=1 -timeout 120s ./...
	cd pkg/core      && CGO_ENABLED=0 $(GO) test -count=1 -timeout 120s ./...
	cd pkg/cmd       && CGO_ENABLED=0 $(GO) test -count=1 -timeout 120s ./...

fmt:
	cd pkg/common    && $(GO) fmt ./...
	cd pkg/model     && $(GO) fmt ./...
	cd pkg/messager && $(GO) fmt ./...
	cd pkg/cache     && $(GO) fmt ./...
	cd pkg/config    && $(GO) fmt ./...
	cd pkg/wallet    && $(GO) fmt ./...
	cd pkg/notifier  && $(GO) fmt ./...
	cd pkg/api       && $(GO) fmt ./...
	cd pkg/store     && $(GO) fmt ./...
	cd pkg/sandbox   && $(GO) fmt ./...
	cd pkg/core      && $(GO) fmt ./...
	cd pkg/cmd       && $(GO) fmt ./...

# ── Secrets ───────────────────────────────────────────────────
deploy-secret-create:
	@[ -n "$(PROVIDER)" ] || { echo "Usage: make deploy-secret-create PROVIDER=<openai|deepseek|bailian> KEY_B64=<b64-string>"; exit 1; }
	@[ -n "$(KEY_B64)" ] || { echo "ERROR: KEY_B64 is required"; exit 1; }
	@case "$(PROVIDER)" in \
	  deepseek)   SECRET_NAME=flowgent-llm-deepseek ;; \
	  openai)     SECRET_NAME=flowgent-llm-openai ;; \
	  bailian)    SECRET_NAME=flowgent-llm-bailian ;; \
	  *)          echo "Unknown provider: $(PROVIDER)"; exit 1 ;; \
	esac; \
	APIKEY=$$(echo "$(KEY_B64)" | base64 -d 2>/dev/null || echo "$(KEY_B64)"); \
	kubectl delete secret $$SECRET_NAME -n default --ignore-not-found=true; \
	kubectl create secret generic $$SECRET_NAME -n default --from-literal=apikey="$$APIKEY"; \
	echo "Secret $$SECRET_NAME created (key length: $${#APIKEY})"
