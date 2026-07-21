.PHONY: help build-all build-core build-wallet build-image-all build-image-core build-image-wallet build-image-all-in-one clean test test-x402 fmt deploy-secret-create

BIN_DIR  ?= bin
GO       ?= go
LDFLAGS  := -s -w -X main.Version=dev -X main.GitCommit=$(shell git rev-parse --short HEAD 2>/dev/null || echo unknown) -X main.BuildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)
# When HTTPS_PROXY is set, Go uses it to route module downloads — don't override
# GOPROXY. Otherwise, use goproxy.cn for speed in mainland China.
ifdef HTTPS_PROXY
GOENV    := GONOSUMDB=* GONOSUMCHECK=*
else
GOPROXY  ?= https://goproxy.cn,direct
GOENV    := GOPROXY=$(GOPROXY) GONOSUMDB=* GONOSUMCHECK=*
endif
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
ifdef HTTPS_PROXY
	DOCKER_BUILDKIT=1 docker build --network=host --build-arg="HTTPS_PROXY=$(HTTPS_PROXY)" --build-arg="HTTP_PROXY=$(HTTPS_PROXY)" --build-arg GOPROXY="" --build-arg BUILD_TAGS="$(TAGS_X402)" -t flowgent-core:latest -f deploy/docker/Dockerfile.core .
else
	DOCKER_BUILDKIT=1 docker build --build-arg GOPROXY="https://goproxy.cn,direct" --build-arg BUILD_TAGS="$(TAGS_X402)" -t flowgent-core:latest -f deploy/docker/Dockerfile.core .
endif

build-image-wallet:
ifdef HTTPS_PROXY
	DOCKER_BUILDKIT=1 docker build --network=host --build-arg="HTTPS_PROXY=$(HTTPS_PROXY)" --build-arg="HTTP_PROXY=$(HTTPS_PROXY)" --build-arg GOPROXY="" --build-arg BUILD_TAGS="$(TAGS_X402)" -t flowgent-wallet:latest -f deploy/docker/Dockerfile.wallet .
else
	DOCKER_BUILDKIT=1 docker build --build-arg GOPROXY="https://goproxy.cn,direct" --build-arg BUILD_TAGS="$(TAGS_X402)" -t flowgent-wallet:latest -f deploy/docker/Dockerfile.wallet .
endif

build-image-all-in-one:
ifdef HTTPS_PROXY
	DOCKER_BUILDKIT=1 docker build --network=host --build-arg="HTTPS_PROXY=$(HTTPS_PROXY)" --build-arg="HTTP_PROXY=$(HTTPS_PROXY)" --build-arg GOPROXY="" --build-arg BUILD_TAGS="$(TAGS_X402)" -t flowgent:all-in-one -f deploy/docker/Dockerfile.all-in-one .
else
	DOCKER_BUILDKIT=1 docker build --build-arg GOPROXY="https://goproxy.cn,direct" --build-arg BUILD_TAGS="$(TAGS_X402)" -t flowgent:all-in-one -f deploy/docker/Dockerfile.all-in-one .
endif

# ── Binary builds ─────────────────────────────────────────────────

build-all: build-core build-wallet
	@mkdir -p $(BIN_DIR)
	cd examples/security-autonomy-fixer/config/mcps/github && GOWORK=off CGO_ENABLED=0 $(GOENV) $(GO) build -v -trimpath -ldflags="-s -w" -o ../../../../$(BIN_DIR)/github-mcp .
	cd examples/security-autonomy-fixer/config/mcps/sonarqube && GOWORK=off CGO_ENABLED=0 $(GOENV) $(GO) build -v -trimpath -ldflags="-s -w" -o ../../../../$(BIN_DIR)/sonarqube-mcp .

build-core:
	@mkdir -p $(BIN_DIR)
	cd pkg/cmd && CGO_ENABLED=0 $(GOENV) $(GO) build -v -trimpath -tags "$(TAGS_X402)" -ldflags="$(LDFLAGS)" -o ../../$(BIN_DIR)/flowgent-core ./pkg/

build-wallet:
	@mkdir -p $(BIN_DIR)
	cd pkg/wallet && CGO_ENABLED=0 $(GOENV) $(GO) build -v -trimpath -ldflags="$(LDFLAGS)" -o ../../$(BIN_DIR)/flowgent-wallet ./pkg/cmd/

# ── Utils ─────────────────────────────────────────────────────────

clean:
	rm -rf $(BIN_DIR)/

test:
	cd pkg/common    && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./...
	cd pkg/model     && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./...
	cd pkg/messager && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./...
	cd pkg/cache     && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./...
	cd pkg/config    && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./...
	cd pkg/wallet    && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./...
	cd pkg/notifier  && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./...
	cd pkg/api       && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./...
	cd pkg/store     && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./...
	cd pkg/sandbox   && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./...
	cd pkg/core      && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./...
	cd pkg/cmd       && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./...

test-x402:
	cd pkg/wallet && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -tags "$(TAGS_X402)" -timeout 120s ./...
	cd pkg/core   && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -tags "$(TAGS_X402)" -timeout 120s ./pkg/client/...

# Portable local integration tests: a real end-to-end run of the Flowgent
# engine (apiserver + JobManager + TaskManager + executors) with every external
# service (GitHub, SonarQube, LLM) replaced by an in-process mock. Requires no
# docker / k8s / broker / database — runs on a clean Ubuntu CI runner.
test-it:
	cd tests/it && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 300s ./...

# Auth integration tests need a live LDAP (GLAuth) / OIDC (Dex) container:
#   cd deploy/docker/glauth && docker compose up -d   (then: make test-it-ldap)
#   cd deploy/docker/dex    && docker compose up -d   (then: make test-it-oidc)
test-it-ldap:
	cd tests/it && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -tags=ldap -timeout 120s -run TestE2E_LDAP ./...
test-it-oidc:
	cd tests/it && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -tags=oidc -timeout 120s -run TestE2E_OIDC ./...

fmt:
	cd pkg/common    && $(GOENV) $(GO) fmt ./...
	cd pkg/model     && $(GOENV) $(GO) fmt ./...
	cd pkg/messager && $(GOENV) $(GO) fmt ./...
	cd pkg/cache     && $(GOENV) $(GO) fmt ./...
	cd pkg/config    && $(GOENV) $(GO) fmt ./...
	cd pkg/wallet    && $(GOENV) $(GO) fmt ./...
	cd pkg/notifier  && $(GOENV) $(GO) fmt ./...
	cd pkg/api       && $(GOENV) $(GO) fmt ./...
	cd pkg/store     && $(GOENV) $(GO) fmt ./...
	cd pkg/sandbox   && $(GOENV) $(GO) fmt ./...
	cd pkg/core      && $(GOENV) $(GO) fmt ./...
	cd pkg/cmd       && $(GOENV) $(GO) fmt ./...

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
