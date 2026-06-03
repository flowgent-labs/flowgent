.PHONY: help build build-all build-host build-host-all build-image build-image-all clean test fmt

BIN_DIR  ?= bin
GO       ?= /usr/local/go1.26.1.linux-amd64/bin/go
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
	DOCKER_BUILDKIT=1 sudo docker build -f deploy/docker/Dockerfile --target export --output type=local,dest=$(BIN_DIR) .

build-all:
	@mkdir -p $(BIN_DIR)
	DOCKER_BUILDKIT=1 sudo docker build -f deploy/docker/Dockerfile.all-in-one --target export --output type=local,dest=$(BIN_DIR) .

build-image:
	DOCKER_BUILDKIT=1 sudo docker build -t flowgent:latest -f deploy/docker/Dockerfile .

build-image-all:
	DOCKER_BUILDKIT=1 sudo docker build -t flowgent:all-in-one -f deploy/docker/Dockerfile.all-in-one .

build-host:
	@mkdir -p $(BIN_DIR)
	cd src/cmd && CGO_ENABLED=0 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o ../../$(BIN_DIR)/flowgent ./src/flowgent

build-host-all: build-host
	@echo "build-host-all: done (MCP examples removed — see git history)"

clean:
	rm -rf $(BIN_DIR)/

test:
	cd src/common    && CGO_ENABLED=0 $(GO) test -count=1 -timeout 120s ./...
	cd src/model     && CGO_ENABLED=0 $(GO) test -count=1 -timeout 120s ./...
	cd src/messaging && CGO_ENABLED=0 $(GO) test -count=1 -timeout 120s ./...
	cd src/cache     && CGO_ENABLED=0 $(GO) test -count=1 -timeout 120s ./...
	cd src/config    && CGO_ENABLED=0 $(GO) test -count=1 -timeout 120s ./...
	cd src/wallet    && CGO_ENABLED=0 $(GO) test -count=1 -timeout 120s ./...
	cd src/notifier  && CGO_ENABLED=0 $(GO) test -count=1 -timeout 120s ./...
	cd src/api       && CGO_ENABLED=0 $(GO) test -count=1 -timeout 120s ./...
	cd src/store     && CGO_ENABLED=0 $(GO) test -count=1 -timeout 120s ./...
	cd src/sandbox   && CGO_ENABLED=0 $(GO) test -count=1 -timeout 120s ./...
	cd src/core      && CGO_ENABLED=0 $(GO) test -count=1 -timeout 120s ./...
	cd src/cmd       && CGO_ENABLED=0 $(GO) test -count=1 -timeout 120s ./...

fmt:
	cd src/common    && $(GO) fmt ./...
	cd src/model     && $(GO) fmt ./...
	cd src/messaging && $(GO) fmt ./...
	cd src/cache     && $(GO) fmt ./...
	cd src/config    && $(GO) fmt ./...
	cd src/wallet    && $(GO) fmt ./...
	cd src/notifier  && $(GO) fmt ./...
	cd src/api       && $(GO) fmt ./...
	cd src/store     && $(GO) fmt ./...
	cd src/sandbox   && $(GO) fmt ./...
	cd src/core      && $(GO) fmt ./...
	cd src/cmd       && $(GO) fmt ./...

# ── Secrets ───────────────────────────────────────────────────
deploy-secret-create:
	@[ -n "$(PROVIDER)" ] || { echo "Usage: make deploy-secret-create PROVIDER=<openai|deepseek|bailian> KEY_B64=<b64-string>"; exit 1; }
	@[ -n "$(KEY_B64)" ] || { echo "ERROR: KEY_B64 is required"; exit 1; }
	@case "$(PROVIDER)" in \
	  deepseek)   SECRET_NAME=flowgent-llm-deepseek; ENV_KEY=FLOWGENT_LLM_PROVIDERS_DEEPSEEK_CREDENTIALS_APIKEY ;; \
	  openai)     SECRET_NAME=flowgent-llm-openai;   ENV_KEY=FLOWGENT_LLM_PROVIDERS_OPENAI_CREDENTIALS_APIKEY ;; \
	  bailian)    SECRET_NAME=flowgent-llm-bailian;  ENV_KEY=FLOWGENT_LLM_PROVIDERS_BAILIAN_CREDENTIALS_APIKEY ;; \
	  *)          echo "Unknown provider: $(PROVIDER)"; exit 1 ;; \
	esac; \
	APIKEY=$$(echo "$(KEY_B64)" | base64 -d 2>/dev/null || echo "$(KEY_B64)"); \
	kubectl delete secret $$SECRET_NAME -n default --ignore-not-found=true; \
	kubectl create secret generic $$SECRET_NAME -n default --from-literal=apikey="$$APIKEY"; \
	echo "Secret $$SECRET_NAME created (key length: $${#APIKEY})"
