.PHONY: help build build-core build-image build-image-core build-image-web clean test test-ut test-web test-x402 test-it test-it-deps test-authguard-adapter test-sql-scope test-runtime-isolation test-e2e-runner e2e-security-autonomy-fixer-with-helm e2e-security-autonomy-fixer-with-docker fmt

BIN_DIR  ?= bin
GO       ?= go
CORE_IMAGE ?= flowgent:latest
CORE_LOCAL_IMAGE ?= localhost/flowgent:latest
WEB_IMAGE ?= flowgent-web:latest
WEB_LOCAL_IMAGE ?= localhost/flowgent-web:latest
WEB_BUILD_TARGET ?= runtime-dist
WEB_NODE_IMAGE ?= docker.io/library/node:22-alpine
WEB_NGINX_IMAGE ?= registry.cn-shenzhen.aliyuncs.com/wl4g/nginx:1.27-alpine
E2E_VENV_PYTHON := use-cases/security-autonomy-fixer/e2e/.venv/bin/python
E2E_PYTHON ?= $(if $(wildcard $(E2E_VENV_PYTHON)),$(E2E_VENV_PYTHON),python3)
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
	@echo "    make build:image              Build the production flowgent and flowgent-web images."
	@echo "    make build:image:core         Build the production flowgent image."
	@echo "    make build:image:web          Build the production flowgent-web image."
	@echo ""
	@echo "  Binary builds:"
	@echo "    make build                    Build the flowgent-core binary."
	@echo "    make build:core               Build the flowgent-core binary."
	@echo ""
	@echo "  Secrets (Kubernetes):"
	@echo "    make deploy:secret:<ns>:llm:<p>    Create flowgent-<ns>-llm-<p> (p=deepseek|openai|bailian) KEY=<apikey>"
	@echo "    make deploy:secret:<ns>:mcp:<s>    Create flowgent-<ns>-mcp-<s> (s=github|sonarqube|..) KEY=<token>"
	@echo "    make deploy:secret:<ns>:notifier:<c> Create flowgent-<ns>-notifier-<c> (c=telegram|slack|dingtalk|webhook|email)"
	@echo "    make deploy:secret:<ns>:custom:<n> Create flowgent-<ns>-custom-<n> VALUE=<secret> [KEY=<name>]"
	@echo "    Namespace defaults to 'default' if omitted: make deploy:secret:default:llm:deepseek KEY=sk-xxx"
	@echo ""
	@echo "  Test:"
	@echo "    make test-ut test-x402 test-it fmt clean"
	@echo "    make e2e-security-autonomy-fixer-with-helm   Build, deploy, and verify the real use case with Helm."
	@echo "    make e2e-security-autonomy-fixer-with-docker Run the equivalent isolated Docker Compose E2E."

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
	pkg/notifier pkg/sandbox pkg/storage tests

bootstrap-go:
	@if [ ! -f go.work ]; then \
		echo "INFO: go.work missing, regenerating..."; \
		$(GO) work init; \
		for m in $(_GO_MODULES); do $(GO) work use ./$$m; done; \
	fi

# ── Docker builds ─────────────────────────────────────────────────

build-image: build-image-core build-image-web

build-image-core:
ifeq ($(IN_CN_GFW),true)
ifdef HTTPS_PROXY
	DOCKER_BUILDKIT=1 docker build --network=host --build-arg="HTTPS_PROXY=$(HTTPS_PROXY)" --build-arg="HTTP_PROXY=$(HTTPS_PROXY)" --build-arg GOPROXY="" --build-arg GOFLAGS="$(GOFLAGS)" --build-arg GOMAXPROCS="$(GOMAXPROCS)" --build-arg BUILD_TAGS="$(TAGS_X402)" --build-arg BUILD_TS="$$(date -u +%Y%m%d%H%M%S)" -t $(CORE_IMAGE) -t $(CORE_LOCAL_IMAGE) -f deploy/docker/Dockerfile.core .
else
	DOCKER_BUILDKIT=1 docker build --build-arg GOPROXY="https://goproxy.cn,direct" --build-arg GOFLAGS="$(GOFLAGS)" --build-arg GOMAXPROCS="$(GOMAXPROCS)" --build-arg BUILD_TAGS="$(TAGS_X402)" --build-arg BUILD_TS="$$(date -u +%Y%m%d%H%M%S)" -t $(CORE_IMAGE) -t $(CORE_LOCAL_IMAGE) -f deploy/docker/Dockerfile.core .
endif
else
	DOCKER_BUILDKIT=1 docker build --build-arg GOFLAGS="$(GOFLAGS)" --build-arg GOMAXPROCS="$(GOMAXPROCS)" --build-arg BUILD_TAGS="$(TAGS_X402)" --build-arg BUILD_TS="$$(date -u +%Y%m%d%H%M%S)" -t $(CORE_IMAGE) -t $(CORE_LOCAL_IMAGE) -f deploy/docker/Dockerfile.core .
endif

build-image-web:
ifeq ($(WEB_BUILD_TARGET),runtime-dist)
	@if [ ! -x web/node_modules/.bin/tsc ]; then \
		cd web && npm ci --prefer-offline --no-audit --no-fund; \
	fi
	cd web && npm run build
endif
ifeq ($(IN_CN_GFW),true)
ifdef HTTPS_PROXY
	DOCKER_BUILDKIT=1 docker build --network=host --target="$(WEB_BUILD_TARGET)" --build-arg="NODE_IMAGE=$(WEB_NODE_IMAGE)" --build-arg="NGINX_IMAGE=$(WEB_NGINX_IMAGE)" --build-arg="HTTPS_PROXY=$(HTTPS_PROXY)" --build-arg="HTTP_PROXY=$(HTTPS_PROXY)" --build-arg="NO_PROXY=$(NO_PROXY)" -t $(WEB_IMAGE) -t $(WEB_LOCAL_IMAGE) -f deploy/docker/Dockerfile.web web
else
	DOCKER_BUILDKIT=1 docker build --target="$(WEB_BUILD_TARGET)" --build-arg="NODE_IMAGE=$(WEB_NODE_IMAGE)" --build-arg="NGINX_IMAGE=$(WEB_NGINX_IMAGE)" -t $(WEB_IMAGE) -t $(WEB_LOCAL_IMAGE) -f deploy/docker/Dockerfile.web web
endif
else
	DOCKER_BUILDKIT=1 docker build --target="$(WEB_BUILD_TARGET)" --build-arg="NODE_IMAGE=$(WEB_NODE_IMAGE)" --build-arg="NGINX_IMAGE=$(WEB_NGINX_IMAGE)" -t $(WEB_IMAGE) -t $(WEB_LOCAL_IMAGE) -f deploy/docker/Dockerfile.web web
endif

# ── Binary builds ─────────────────────────────────────────────────

build: build-core

build-core: bootstrap-go
	@mkdir -p $(BIN_DIR)
	cd pkg/cmd && CGO_ENABLED=0 $(GOENV) $(GO) build -v -trimpath -tags "$(TAGS_X402)" -ldflags="$(LDFLAGS)" -o ../../$(BIN_DIR)/flowgent-core ./pkg/

# ── Utils ─────────────────────────────────────────────────────────

test: test-ut test-x402 test-web test-e2e-runner

clean:
	rm -rf $(BIN_DIR)/

test-ut:
	cd pkg/a2a      && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./...
	cd pkg/common    && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./...
	cd pkg/model     && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./...
	cd pkg/messager && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./...
	cd pkg/cache     && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./...
	cd pkg/config    && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./...
	cd pkg/console   && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./...
	cd pkg/controller && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./...
	cd pkg/notifier  && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./...
	cd pkg/api       && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./...
	cd pkg/storage   && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./...
	cd pkg/sandbox   && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./...
	cd pkg/core      && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./...
	cd pkg/cmd       && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./...

test-web:
	cd web && npm ci --prefer-offline --no-audit --no-fund
	cd web && npm run format:check
	cd web && npm run lint
	cd web && npm run typecheck
	cd web && npm run test
	cd web && npm run build

test-x402:
	cd pkg/core   && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -tags "$(TAGS_X402)" -timeout 120s ./pkg/client/...

# Local integration tests: a real end-to-end run of the Flowgent engine
# (apiserver + JobManager + TaskManager + executors) backed by PostgreSQL, with
# external SaaS services (GitHub, SonarQube, LLM) replaced by in-process mocks.
# Keep packages serial because the harness shares PostgreSQL and fixed ports.
test-it-deps:
	@if [ -S /run/podman/podman.sock ]; then \
		DOCKER_HOST=unix:///run/podman/podman.sock docker-compose -f deploy/docker/pgvector/docker-compose.yml up -d; \
	else \
		docker compose -f deploy/docker/pgvector/docker-compose.yml up -d; \
	fi
	@for _attempt in $$(seq 1 60); do \
		_status=$$(docker inspect --format='{{.State.Health.Status}}' flowgent-test-pgvector 2>/dev/null); \
		if [ "$$_status" = "healthy" ]; then exit 0; fi; \
		sleep 1; \
	done; \
	docker logs --tail 80 flowgent-test-pgvector; \
	echo "ERROR: flowgent-test-pgvector did not become healthy"; \
	exit 1

test-it: test-it-deps
	cd tests/it && CGO_ENABLED=0 $(GOENV) $(GO) test -v -count=1 -timeout 300s -p 1 ./probe ./apiserver ./controller ./engine ./externalmock ./knowledge ./notifier ./sandbox .

test-authguard-adapter:
	cd pkg/api && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./pkg/authz

test-sql-scope:
	cd pkg/storage && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./...

test-runtime-isolation:
	cd pkg/controller && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./pkg
	cd pkg/core && CGO_ENABLED=0 $(GOENV) $(GO) test -count=1 -timeout 120s ./pkg/engine/resourcemanager

test-e2e-runner:
	$(E2E_PYTHON) -m compileall -q use-cases/security-autonomy-fixer/e2e
	$(E2E_PYTHON) use-cases/security-autonomy-fixer/e2e/runner.py --help >/dev/null
	$(E2E_PYTHON) use-cases/security-autonomy-fixer/e2e/runner.py --list
	helm lint deploy/helm/flowgent
	@_rendered="$$(mktemp)"; \
	trap 'rm -f "$$_rendered"' EXIT; \
	helm template flowgent deploy/helm/flowgent > "$$_rendered"; \
	if grep -q '^# Source: flowgent/charts/authguard-middleware/' "$$_rendered" || \
	   grep -q '^[[:space:]]*- name: AUTHGUARD__AUTHZ__SCOPE_DELIVERY__DIRECT_CONTEXT_HMAC_KEY' "$$_rendered"; then \
		echo "ERROR: authguard-middleware.enabled=false rendered AuthGuard runtime resources"; exit 1; \
	fi
	@_rendered="$$(mktemp)"; \
	trap 'rm -f "$$_rendered"' EXIT; \
	helm template e2e-flowgent deploy/helm/flowgent \
	  --set a2a.enabled=true \
	  --set authguard-middleware.enabled=true \
	  --set authguard-middleware.envoy_gateway.ext_authz.authguardRoute.backend.name=e2e-flowgent-apiserver \
	  --set authguard-middleware.adapter.existingSecret=e2e-flowgent-authguard-context > "$$_rendered"; \
	grep -q '^# Source: flowgent/charts/authguard-middleware/' "$$_rendered"; \
	[ "$$(grep -c '^[[:space:]]*- name: AUTHGUARD__AUTHZ__SCOPE_DELIVERY__DIRECT_CONTEXT_HMAC_KEY' "$$_rendered")" -eq 2 ]; \
	grep -q 'api_server_url: "http://e2e-flowgent-apiserver:9990"' "$$_rendered"; \
	grep -A1 '^[[:space:]]*- name: e2e-flowgent-apiserver$$' "$$_rendered" | grep -q 'port: 9999'

e2e-security-autonomy-fixer-with-helm:
	HTTPS_PROXY="$${HTTPS_PROXY:-http://127.0.0.1:8800}" $(E2E_PYTHON) -u use-cases/security-autonomy-fixer/e2e/runner.py --deployer k8s $(E2E_ARGS)

e2e-security-autonomy-fixer-with-docker:
	HTTPS_PROXY="$${HTTPS_PROXY:-http://127.0.0.1:8800}" $(E2E_PYTHON) -u use-cases/security-autonomy-fixer/e2e/runner.py --deployer docker $(E2E_ARGS)

fmt:
	gofmt -w $$(rg --files pkg tests -g '*.go')

# ── Secrets ───────────────────────────────────────────────────
# K8s Secret management for Flowgent microservices.
# Target format: deploy:secret:<namespace>:<type>:<name>
# Namespace defaults to 'default'.
# All targets accept KEY (plain text) or KEY_B64 (base64-encoded).
#
#   make deploy:secret:default:llm:deepseek    KEY=sk-xxx
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
	    echo "ERROR: Unknown deploy-secret type '$$_type'. Expected: llm|notifier|mcp|custom"; exit 1; \
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
