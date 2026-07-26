.PHONY: help build build-core build-wallet build-image build-image-core build-image-wallet clean test test-x402 fmt

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
	@echo "    make deploy:secret:llm:<p>    Create flowgent-llm-<p> (p=deepseek|openai|bailian) KEY=<apikey>"
	@echo "    make deploy:secret:wallet     Create flowgent-wallet KEY=<master-key>"
	@echo "    make deploy:secret:mcp:<s>    Create flowgent-mcp-<s> (s=github|sonarqube|..) KEY=<token>"
	@echo "    make deploy:secret:notifier:<c> Create flowgent-notifier-<c> (c=telegram|slack|dingtalk|webhook|email)"
	@echo "    make deploy:secret:custom:<n> Create flowgent-custom-<n> VALUE=<secret> [KEY=<name>]"
	@echo ""
	@echo "  Test:"
	@echo "    make test test-x402 test-it fmt clean"

.DEFAULT_GOAL := help

# ── Docker builds ─────────────────────────────────────────────────

build-image: build-image-core build-image-wallet

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

# ── Binary builds ─────────────────────────────────────────────────

build: build-core build-wallet

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
# All targets accept KEY (plain text) or KEY_B64 (base64-encoded).
# Override SECRET_NS to target a namespace other than default.
SECRET_NS ?= default

# ── deploy:secret:llm:<provider> ──────────────────────────────────
# LLM provider API keys. Creates secret flowgent-llm-<provider>
# with key "apikey".
#   make deploy:secret:llm:deepseek KEY=sk-xxx
#   make deploy:secret:llm:openai   KEY=sk-xxx
#   make deploy:secret:llm:bailian  KEY=sk-xxx
deploy-secret-llm-%:
	@[ -n "$(KEY)" ] || [ -n "$(KEY_B64)" ] || \
	  { echo "ERROR: KEY or KEY_B64 is required. Usage: make deploy:secret:llm:$* KEY=<api-key>"; exit 1; }
	@_val="$$(if [ -n "$(KEY_B64)" ]; then echo "$(KEY_B64)" | base64 -d 2>/dev/null; else echo '$(KEY)'; fi)"; \
	kubectl delete secret flowgent-llm-$* -n $(SECRET_NS) --ignore-not-found=true; \
	kubectl create secret generic flowgent-llm-$* -n $(SECRET_NS) --from-literal=apikey="$$_val"; \
	echo "✓ Secret flowgent-llm-$* (key: apikey, len: $${#_val})"

# ── deploy:secret:wallet ──────────────────────────────────────────
# Wallet master encryption key. Creates secret flowgent-wallet
# with key "master.key".
#   make deploy:secret:wallet KEY=<master-key>
deploy-secret-wallet:
	@[ -n "$(KEY)" ] || [ -n "$(KEY_B64)" ] || \
	  { echo "ERROR: KEY or KEY_B64 is required. Usage: make deploy:secret:wallet KEY=<master-key>"; exit 1; }
	@_val="$$(if [ -n "$(KEY_B64)" ]; then echo "$(KEY_B64)" | base64 -d 2>/dev/null; else echo '$(KEY)'; fi)"; \
	kubectl delete secret flowgent-wallet -n $(SECRET_NS) --ignore-not-found=true; \
	kubectl create secret generic flowgent-wallet -n $(SECRET_NS) --from-literal=master.key="$$_val"; \
	echo "✓ Secret flowgent-wallet (key: master.key, len: $${#_val})"

# ── deploy:secret:notifier:<channel> ───────────────────────────────
# Notifier channel credentials. Creates secret flowgent-notifier-<channel>.
#   make deploy:secret:notifier:telegram  KEY=<bot-token>
#   make deploy:secret:notifier:slack     KEY=<webhook-url>
#   make deploy:secret:notifier:dingtalk  KEY=<webhook-url>
#   make deploy:secret:notifier:webhook   KEY=<signing-secret>
#   make deploy:secret:notifier:email     SMTP_HOST=.. SMTP_PASS=.. \
#                                         [SMTP_PORT=587] [SMTP_USER=..] [SMTP_FROM=..]
deploy-secret-notifier-%:
	@case "$*" in \
	  telegram) _key=bot_token ;; \
	  slack|dingtalk) _key=webhook_url ;; \
	  webhook) _key=signing_secret ;; \
	  email) ;; \
	  *) echo "ERROR: Unknown channel '$*'. Supported: telegram slack dingtalk webhook email"; exit 1 ;; \
	esac; \
	if [ "$*" = "email" ]; then \
	  [ -n "$(SMTP_HOST)" ] || { echo "ERROR: SMTP_HOST is required for email"; exit 1; }; \
	  [ -n "$(SMTP_PASS)" ] || { echo "ERROR: SMTP_PASS is required for email"; exit 1; }; \
	  kubectl delete secret flowgent-notifier-email -n $(SECRET_NS) --ignore-not-found=true; \
	  kubectl create secret generic flowgent-notifier-email -n $(SECRET_NS) \
	    --from-literal=smtp_host="$(SMTP_HOST)" \
	    --from-literal=smtp_port="$(or $(SMTP_PORT),587)" \
	    --from-literal=username="$(SMTP_USER)" \
	    --from-literal=password="$(SMTP_PASS)" \
	    --from-literal=from="$(SMTP_FROM)"; \
	  echo "✓ Secret flowgent-notifier-email (keys: smtp_host, smtp_port, username, password, from)"; \
	else \
	  [ -n "$(KEY)" ] || [ -n "$(KEY_B64)" ] || \
	    { echo "ERROR: KEY or KEY_B64 is required for $*"; exit 1; }; \
	  _val="$$(if [ -n "$(KEY_B64)" ]; then echo "$(KEY_B64)" | base64 -d 2>/dev/null; else echo '$(KEY)'; fi)"; \
	  kubectl delete secret flowgent-notifier-$* -n $(SECRET_NS) --ignore-not-found=true; \
	  kubectl create secret generic flowgent-notifier-$* -n $(SECRET_NS) --from-literal="$$_key=$$_val"; \
	  echo "✓ Secret flowgent-notifier-$* (key: $$_key, len: $${#_val})"; \
	fi

# ── deploy:secret:mcp:<server> ────────────────────────────────────
# MCP server backend auth tokens (static web_token). Creates secret
# flowgent-mcp-<server> with key "token".
#   make deploy:secret:mcp:github     KEY=<gh-pat>
#   make deploy:secret:mcp:sonarqube  KEY=<sq-api-token>
deploy-secret-mcp-%:
	@[ -n "$(KEY)" ] || [ -n "$(KEY_B64)" ] || \
	  { echo "ERROR: KEY or KEY_B64 is required. Usage: make deploy:secret:mcp:$* KEY=<api-token>"; exit 1; }
	@_val="$$(if [ -n "$(KEY_B64)" ]; then echo "$(KEY_B64)" | base64 -d 2>/dev/null; else echo '$(KEY)'; fi)"; \
	kubectl delete secret flowgent-mcp-$* -n $(SECRET_NS) --ignore-not-found=true; \
	kubectl create secret generic flowgent-mcp-$* -n $(SECRET_NS) --from-literal=token="$$_val"; \
	echo "✓ Secret flowgent-mcp-$* (key: token, len: $${#_val})"

# ── deploy:secret:custom:<name> ────────────────────────────────────
# Generic custom secrets for L2 skills, external integrations, etc.
# Creates secret flowgent-custom-<name>.
#   make deploy:secret:custom:nexus3   VALUE=<secret>
#   make deploy:secret:custom:my-skill KEY=api_token VALUE=<token>
deploy-secret-custom-%:
	@[ -n "$(VALUE)" ] || [ -n "$(VALUE_B64)" ] || \
	  { echo "ERROR: VALUE or VALUE_B64 is required. Usage: make deploy:secret:custom:$* VALUE=<secret> [KEY=secret_key]"; exit 1; }
	@_key="$(or $(KEY),value)"; \
	_val="$$(if [ -n "$(VALUE_B64)" ]; then echo "$(VALUE_B64)" | base64 -d 2>/dev/null; else echo '$(VALUE)'; fi)"; \
	kubectl delete secret flowgent-custom-$* -n $(SECRET_NS) --ignore-not-found=true; \
	kubectl create secret generic flowgent-custom-$* -n $(SECRET_NS) --from-literal="$$_key=$$_val"; \
	echo "✓ Secret flowgent-custom-$* (key: $$_key, len: $${#_val})"

# ── Colon-name translation (make build:image:core → make build-image-core) ──
# GNU Make treats ':' as literal in goal names, but not in rule definitions.
# This catch-all pattern matches colon-containing goals, translates ':' → '-',
# and re-invokes make with the hyphenated target.
%:
	@case "$@" in \
	  *:*) $(MAKE) $(subst :,-,$@) ;; \
	  *)   echo "Unknown target: $@. Run 'make help' for usage."; exit 1 ;; \
	esac
