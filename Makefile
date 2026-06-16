.PHONY: help build build-dev build-bin build-bin-dev clean test fmt

BIN_DIR  ?= bin
GO       ?= go
LDFLAGS  := -s -w -X main.Version=dev -X main.GitCommit=$(shell git rev-parse --short HEAD 2>/dev/null || echo unknown) -X main.BuildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)

help:
	@echo "Flowgent -- Makefile"
	@echo ""
	@echo "  Docker builds (Recommended):"
	@echo "    make build              Build the production image."
	@echo "    make build-dev          Build the all-in-one image (bundles example MCPs)."
	@echo ""
	@echo "  Binary builds (Development only):"
	@echo "    make build-bin          Build the flowgent binary on the host."
	@echo "    make build-bin-dev      Build flowgent + example MCP binaries on the host."
	@echo ""
	@echo "  Utils: make test fmt clean"

.DEFAULT_GOAL := help

# ── Docker builds ─────────────────────────────────────────────────

build:
	DOCKER_BUILDKIT=1 docker build --progress=plain -t flowgent:latest -f deploy/docker/Dockerfile .

build-dev:
	DOCKER_BUILDKIT=1 docker build --progress=plain -t flowgent:all-in-one -f deploy/docker/Dockerfile.all-in-one .

# ── Binary builds ─────────────────────────────────────────────────

build-bin:
	@mkdir -p $(BIN_DIR)
	cd pkg/cmd && CGO_ENABLED=0 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o ../../$(BIN_DIR)/flowgent ./pkg/

build-bin-dev: build-bin
	@echo "build-bin-dev: MCP example binaries — see examples/security-autonomy-fixer/mcps/"

# ── Utils ─────────────────────────────────────────────────────────

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
