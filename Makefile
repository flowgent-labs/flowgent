.PHONY: help build-all build-core build-wallet build-image-all build-image-core build-image-wallet build-image-all-in-one clean test test-x402 fmt deploy-secret-create

BIN_DIR  ?= bin
GO       ?= go
LDFLAGS  := -s -w -X main.Version=dev -X main.GitCommit=$(shell git rev-parse --short HEAD 2>/dev/null || echo unknown) -X main.BuildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)
TAGS_X402 := x402

help:
	@echo "Flowgent -- Makefile"
	@echo ""
	@echo "  Docker builds (Recommended):"
	@echo "    make build:image:all          Build all images (flowgent-core + flowgent-wallet + all-in-one)."
	@echo "    make build:image:core         Build the production flowgent-core image."
	@echo "    make build:image:wallet       Build the production flowgent-wallet image (payment wallet service only)."
	@echo "    make build:image:all-in-one   Build the all-in-one development image (single binary, all services)."
	@echo ""
	@echo "  Binary builds (Development):"
	@echo "    make build:all                Build all binaries (flowgent-core + MCPs + flowgent-wallet) on the host."
	@echo "    make build:core               Build the flowgent-core binary on the host."
	@echo "    make build:wallet             Build the flowgent-wallet binary on the host."
	@echo ""
	@echo "  Utils: make test test-x402 fmt clean"

.DEFAULT_GOAL := help

# ── Docker builds ─────────────────────────────────────────────────

build-image-all: build-image-core build-image-wallet build-image-all-in-one

build-image-core:
	DOCKER_BUILDKIT=1 docker build --progress=plain --build-arg BUILD_TAGS="$(TAGS_X402)" -t flowgent-core:latest -f deploy/docker/Dockerfile.core .

build-image-wallet:
	DOCKER_BUILDKIT=1 docker build --progress=plain --build-arg BUILD_TAGS="$(TAGS_X402)" -t flowgent-wallet:latest -f deploy/docker/Dockerfile.wallet .

build-image-all-in-one:
	DOCKER_BUILDKIT=1 docker build --progress=plain --build-arg BUILD_TAGS="$(TAGS_X402)" -t flowgent:all-in-one -f deploy/docker/Dockerfile.all-in-one .

# ── Binary builds ─────────────────────────────────────────────────

build-all: build-core build-wallet
	@mkdir -p $(BIN_DIR)
	cd examples/security-autonomy-fixer/mcps/github && GOWORK=off CGO_ENABLED=0 $(GO) build -v -trimpath -ldflags="-s -w" -o ../../../../$(BIN_DIR)/github-mcp .
	cd examples/security-autonomy-fixer/mcps/sonarqube && GOWORK=off CGO_ENABLED=0 $(GO) build -v -trimpath -ldflags="-s -w" -o ../../../../$(BIN_DIR)/sonarqube-mcp .

build-core:
	@mkdir -p $(BIN_DIR)
	cd pkg/cmd && CGO_ENABLED=0 $(GO) build -v -trimpath -tags "$(TAGS_X402)" -ldflags="$(LDFLAGS)" -o ../../$(BIN_DIR)/flowgent-core ./pkg/

build-wallet:
	@mkdir -p $(BIN_DIR)
	cd pkg/wallet && CGO_ENABLED=0 $(GO) build -v -trimpath -ldflags="$(LDFLAGS)" -o ../../$(BIN_DIR)/flowgent-wallet ./pkg/cmd/

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

test-x402:
	cd pkg/wallet && CGO_ENABLED=0 $(GO) test -count=1 -tags "$(TAGS_X402)" -timeout 120s ./...
	cd pkg/core   && CGO_ENABLED=0 $(GO) test -count=1 -tags "$(TAGS_X402)" -timeout 120s ./pkg/client/...

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

# ── Colon-name translation (make build:image:core → make build-image-core) ──
# GNU Make treats ':' as literal in goal names, but not in rule definitions.
# This catch-all pattern matches colon-containing goals, translates ':' → '-',
# and re-invokes make with the hyphenated target.
%:
	@case "$@" in \
	  *:*) $(MAKE) $(subst :,-,$@) ;; \
	  *)   echo "Unknown target: $@. Run 'make help' for usage."; exit 1 ;; \
	esac
