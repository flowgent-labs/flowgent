.PHONY: help build build-core build-wallet build-image build-image-core build-image-wallet clean test-ut test-x402 test-it fmt

BIN_DIR  ?= bin
GO       ?= go
LDFLAGS  := -s -w -X main.Version=dev -X main.GitCommit=$(shell git rev-parse --short HEAD 2>/dev/null || echo unknown) -X main.BuildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)
# IN_CN_GFW controls whether to route Go module downloads through a proxy/CDN.
#   IN_CN_GFW=true  → prefer HTTPS_PROXY if set, otherwise use goproxy.cn
#   IN_CN_GFW=false → no proxy (GitHub Actions CI / non-GFW environment)
ifneq ($(IN_CN_GFW),true)
GOENV    := GONOSUMDB=* GONOSUMCHECK=*
else ifdef HTTPS_PROXY
GOENV    := GONOSUMDB=* GONOSUMCHECK=*
else
GOPROXY  ?= https://goproxy.cn,direct
GOENV    := GOPROXY=$(GOPROXY) GONOSUMDB=* GONOSUMCHECK=*
endif
TAGS_X402 := x402

help:
	@echo "Flowgent -- Makefile"
	@echo ""
	@echo "  Docker builds:"
	@echo "    make build:image              Build production images (flowgent-core + flowgent-wallet)."
	@echo "    make build:image:core         Build the production flowgent-core image."
	@echo "    make build:image:wallet       Build the production flowgent-wallet image."
	@echo ""
	@echo "  Binary builds:"
	@echo "    make build                    Build all binaries (flowgent-core + flowgent-wallet)."
	@echo "    make build:core               Build the flowgent-core binary."
	@echo "    make build:wallet             Build the flowgent-wallet binary."
	@echo ""
	@echo "  Secrets (Kubernetes):"
	@echo "    make deploy:secret:<ns>:llm:<p>    Create flowgent-<ns>-llm-<p> (p=deepseek|openai|bailian) KEY=<apikey>"
	@echo "    make deploy:secret:<ns>:wallet     Create flowgent-<ns>-wallet KEY=<master-key>"
	@echo "    make deploy:secret:<ns>:mcp:<s>    Create flowgent-<ns>-mcp-<s> (s=github|sonarqube|..) KEY=<token>"
	@echo "    make deploy:secret:<ns>:notifier:<c> Create flowgent-<ns>-notifier-<c> (c=telegram|slack|dingtalk|webhook|email)"
	@echo "    make deploy:secret:<ns>:custom:<n> Create flowgent-<ns>-custom-<n> VALUE=<secret> [KEY=<name>]"
	@echo "    Namespace defaults to 'default' if omitted: make deploy:secret:default:llm:deepseek KEY=sk-xxx"
	@echo ""
	@echo "  Test:"
	@echo "    make test-ut test-x402 test-it fmt clean"

.DEFAULT_GOAL := help

# ── Bootstrap: regenerates go.work on clean clone ────
# go.work with `use` directives is Go's equivalent of Maven's reactor —
# it tells Go all sibling modules live locally. No `go work sync`
# (that would ping the proxy to validate pseudo-versions and trigger
# "downloading myself" network calls). All go.sum / go.work.sum are
# committed in git — a clean clone builds deterministically with
# zero network resolution of local modules.
_GO_MODULES = migration \
	pkg/a2a pkg/api pkg/cache pkg/cmd pkg/common pkg/config \
	pkg/console pkg/controller pkg/core pkg/messager pkg/model \
	pkg/notifier pkg/sandbox pkg/store pkg/wallet tests

bootstrap-go:
	@if [ ! -f go.work ]; then \
		echo "INFO: go.work missing, regenerating..."; \
		$(GO) work init; \
		for m in $(_GO_MODULES); do $(GO) work use ./$$m; done; \
	fi

# ── Docker builds ─────────────────────────────────────────────────

build-image: build-image-core build-image-wallet

build-image-core:
ifeq ($(IN_CN_GFW),true)
ifdef HTTPS_PROXY
	DOCKER_BUILDKIT=1 docker build --network=host --build-arg="HTTPS_PROXY=$(HTTPS_PROXY)" --build-arg="HTTP_PROXY=$(HTTPS_PROXY)" --build-arg GOPROXY="" --build-arg BUILD_TAGS="$(TAGS_X402)" -t flowgent-core:latest -f deploy/docker/Dockerfile.core .
else
	DOCKER_BUILDKIT=1 docker build --build-arg GOPROXY="https://goproxy.cn,direct" --build-arg BUILD_TAGS="$(TAGS_X402)" -t flowgent-core:latest -f deploy/docker/Dockerfile.core .
endif
else
	DOCKER_BUILDKIT=1 docker build --build-arg BUILD_TAGS="$(TAGS_X402)" -t flowgent-core:latest -f deploy/docker/Dockerfile.core .
endif

build-image-wallet:
ifeq ($(IN_CN_GFW),true)
ifdef HTTPS_PROXY
	DOCKER_BUILDKIT=1 docker build --network=host --build-arg="HTTPS_PROXY=$(HTTPS_PROXY)" --build-arg="HTTP_PROXY=$(HTTPS_PROXY)" --build-arg GOPROXY="" --build-arg BUILD_TAGS="$(TAGS_X402)" -t flowgent-wallet:latest -f deploy/docker/Dockerfile.wallet .
else
	DOCKER_BUILDKIT=1 docker build --build-arg GOPROXY="https://goproxy.cn,direct" --build-arg BUILD_TAGS="$(TAGS_X402)" -t flowgent-wallet:latest -f deploy/docker/Dockerfile.wallet .
endif
else
	DOCKER_BUILDKIT=1 docker build --build-arg BUILD_TAGS="$(TAGS_X402)" -t flowgent-wallet:latest -f deploy/docker/Dockerfile.wallet .
endif

# ── Binary builds ─────────────────────────────────────────────────

build: build-core build-wallet

build-core: bootstrap-go
	@mkdir -p $(BIN_DIR)
	cd pkg/cmd && CGO_ENABLED=0 $(GOENV) $(GO) build -v -trimpath -tags "$(TAGS_X402)" -ldflags="$(LDFLAGS)" -o ../../$(BIN_DIR)/flowgent-core ./pkg/

build-wallet: bootstrap-go
	@mkdir -p $(BIN_DIR)
	cd pkg/wallet && CGO_ENABLED=0 $(GOENV) $(GO) build -v -trimpath -ldflags="$(LDFLAGS)" -o ../../$(BIN_DIR)/flowgent-wallet ./pkg/cmd/

# ── Utils ─────────────────────────────────────────────────────────

clean:
	rm -rf $(BIN_DIR)/

test-ut:
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
	cd tests/it && CGO_ENABLED=0 $(GOENV) $(GO) test -v -count=1 -timeout 300s ./probe ./apiserver ./controller ./engine ./externalmock ./knowledge ./notifier ./sandbox .

# Auth integration tests need a live LDAP (GLAuth) / OIDC (Keycloak) container:
#   cd deploy/docker/glauth   && docker compose up -d   (then: make test-it-ldap)
#   cd deploy/docker/keycloak && docker compose up -d   (then: make test-it-oidc)
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
# K8s Secret management for Flowgent microservices.
# Target format: deploy:secret:<namespace>:<type>:<name>
# Namespace defaults to 'default'.
# All targets accept KEY (plain text) or KEY_B64 (base64-encoded).
#
#   make deploy:secret:default:llm:deepseek    KEY=sk-xxx
#   make deploy:secret:default:wallet          KEY=<master-key>
#   make deploy:secret:default:mcp:github      KEY=<gh-pat>
#   make deploy:secret:default:notifier:slack  KEY=<webhook-url>
#   make deploy:secret:default:notifier:email  SMTP_HOST=.. SMTP_PASS=..
#   make deploy:secret:default:custom:nexus3   VALUE=<secret> [KEY=name]

deploy-secret-%:
	@_ns=$$(echo "$*" | cut -d- -f1); \
	_type=$$(echo "$*" | cut -d- -f2); \
	_name=$$(echo "$*" | cut -d- -f3-); \
	case "$$_type" in \
	  llm) \
	    [ -n "$(KEY)" ] || [ -n "$(KEY_B64)" ] || { echo "ERROR: KEY or KEY_B64 is required."; exit 1; }; \
	    _val="$$(if [ -n "$(KEY_B64)" ]; then echo "$(KEY_B64)" | base64 -d 2>/dev/null; else echo '$(KEY)'; fi)"; \
	    kubectl delete secret flowgent-$$_ns-llm-$$_name -n $$_ns --ignore-not-found=true; \
	    kubectl create secret generic flowgent-$$_ns-llm-$$_name -n $$_ns --from-literal=apikey="$$_val"; \
	    echo "✓ Secret flowgent-$$_ns-llm-$$_name (key: apikey, len: $${#_val})"; \
	    ;; \
	  wallet) \
	    [ -n "$(KEY)" ] || [ -n "$(KEY_B64)" ] || { echo "ERROR: KEY or KEY_B64 is required."; exit 1; }; \
	    _val="$$(if [ -n "$(KEY_B64)" ]; then echo "$(KEY_B64)" | base64 -d 2>/dev/null; else echo '$(KEY)'; fi)"; \
	    kubectl delete secret flowgent-$$_ns-wallet -n $$_ns --ignore-not-found=true; \
	    kubectl create secret generic flowgent-$$_ns-wallet -n $$_ns --from-literal=master.key="$$_val"; \
	    echo "✓ Secret flowgent-$$_ns-wallet (key: master.key, len: $${#_val})"; \
	    ;; \
	  notifier) \
	    case "$$_name" in \
	      telegram)  _key=bot_token      ;; \
	      slack|dingtalk)  _key=webhook_url   ;; \
	      webhook)   _key=signing_secret  ;; \
	      email)     ;; \
	      *) echo "ERROR: Unknown notifier '$$_name'. Supported: telegram slack dingtalk webhook email"; exit 1 ;; \
	    esac; \
	    if [ "$$_name" = "email" ]; then \
	      [ -n "$(SMTP_HOST)" ] || { echo "ERROR: SMTP_HOST is required for email"; exit 1; }; \
	      [ -n "$(SMTP_PASS)" ] || { echo "ERROR: SMTP_PASS is required for email"; exit 1; }; \
	      kubectl delete secret flowgent-$$_ns-notifier-email -n $$_ns --ignore-not-found=true; \
	      kubectl create secret generic flowgent-$$_ns-notifier-email -n $$_ns \
	        --from-literal=smtp_host="$(SMTP_HOST)" \
	        --from-literal=smtp_port="$(or $(SMTP_PORT),587)" \
	        --from-literal=username="$(SMTP_USER)" \
	        --from-literal=password="$(SMTP_PASS)" \
	        --from-literal=from="$(SMTP_FROM)"; \
	      echo "✓ Secret flowgent-$$_ns-notifier-email (keys: smtp_host, smtp_port, username, password, from)"; \
	    else \
	      [ -n "$(KEY)" ] || [ -n "$(KEY_B64)" ] || { echo "ERROR: KEY or KEY_B64 is required."; exit 1; }; \
	      _val="$$(if [ -n "$(KEY_B64)" ]; then echo "$(KEY_B64)" | base64 -d 2>/dev/null; else echo '$(KEY)'; fi)"; \
	      kubectl delete secret flowgent-$$_ns-notifier-$$_name -n $$_ns --ignore-not-found=true; \
	      kubectl create secret generic flowgent-$$_ns-notifier-$$_name -n $$_ns --from-literal="$$_key=$$_val"; \
	      echo "✓ Secret flowgent-$$_ns-notifier-$$_name (key: $$_key, len: $${#_val})"; \
	    fi; \
	    ;; \
	  mcp) \
	    [ -n "$(KEY)" ] || [ -n "$(KEY_B64)" ] || { echo "ERROR: KEY or KEY_B64 is required."; exit 1; }; \
	    _val="$$(if [ -n "$(KEY_B64)" ]; then echo "$(KEY_B64)" | base64 -d 2>/dev/null; else echo '$(KEY)'; fi)"; \
	    kubectl delete secret flowgent-$$_ns-mcp-$$_name -n $$_ns --ignore-not-found=true; \
	    kubectl create secret generic flowgent-$$_ns-mcp-$$_name -n $$_ns --from-literal=token="$$_val"; \
	    echo "✓ Secret flowgent-$$_ns-mcp-$$_name (key: token, len: $${#_val})"; \
	    ;; \
	  custom) \
	    [ -n "$(VALUE)" ] || [ -n "$(VALUE_B64)" ] || { echo "ERROR: VALUE or VALUE_B64 is required."; exit 1; }; \
	    _key="$(or $(KEY),value)"; \
	    _val="$$(if [ -n "$(VALUE_B64)" ]; then echo "$(VALUE_B64)" | base64 -d 2>/dev/null; else echo '$(VALUE)'; fi)"; \
	    kubectl delete secret flowgent-$$_ns-custom-$$_name -n $$_ns --ignore-not-found=true; \
	    kubectl create secret generic flowgent-$$_ns-custom-$$_name -n $$_ns --from-literal="$$_key=$$_val"; \
	    echo "✓ Secret flowgent-$$_ns-custom-$$_name (key: $$_key, len: $${#_val})"; \
	    ;; \
	  *) \
	    echo "ERROR: Unknown deploy-secret type '$$_type'. Expected: llm|wallet|notifier|mcp|custom"; exit 1; \
	    ;; \
	esac

# ── Colon-name translation (make build:image:core → make build-image-core) ──
# GNU Make treats ':' as literal in goal names, but not in rule definitions.
# This catch-all pattern matches colon-containing goals, translates ':' → '-',
# and re-invokes make with the hyphenated target.
%:
	@case "$@" in \
	  *:*) $(MAKE) $(subst :,-,$@) ;; \
	  *)   echo "Unknown target: $@. Run 'make help' for usage."; exit 1 ;; \
	esac
