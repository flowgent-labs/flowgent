.PHONY: build build-flowgent build-mcps build-all clean test fmt

# ── Default: build everything ──────────────────────────────
build: build-flowgent build-mcps

# ── Flowgent core binary (daemon, apiserver, a2a, wallet, etc.) ─
build-flowgent:
	GONOSUMCHECK='*' GOFLAGS=-mod=mod go build -o bin/flowgent \
		-ldflags "-X main.Version=dev -X main.GitCommit=$(shell git rev-parse HEAD) -X main.BuildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)" \
		./cmd/flowgent

# ── All MCP servers ────────────────────────────────────────
build-mcps: build-mcp-github build-mcp-sonarqube build-mcp-sonatypeiq build-mcp-nexus3 build-mcp-test

build-mcp-github:
	GONOSUMCHECK='*' GOFLAGS=-mod=mod go build -o bin/mcp-server-github ./cmd/mcp-github

build-mcp-sonarqube:
	GONOSUMCHECK='*' GOFLAGS=-mod=mod go build -o bin/mcp-server-sonarqube ./cmd/mcp-sonarqube

build-mcp-sonatypeiq:
	GONOSUMCHECK='*' GOFLAGS=-mod=mod go build -o bin/mcp-server-sonatypeiq ./cmd/mcp-sonatypeiq

build-mcp-nexus3:
	GONOSUMCHECK='*' GOFLAGS=-mod=mod go build -o bin/mcp-server-nexus3 ./cmd/mcp-nexus3

build-mcp-test:
	GONOSUMCHECK='*' GOFLAGS=-mod=mod go build -o bin/mcp-server-test ./cmd/mcp-test

# ── Utilities ──────────────────────────────────────────────
clean:
	rm -rf bin/

test:
	GONOSUMCHECK='*' GOFLAGS=-mod=mod go test -count=1 -timeout 120s ./src/... ./tests/...

fmt:
	go fmt ./src/... ./tests/... ./cmd/...

# ── Docker ─────────────────────────────────────────────────
docker-build:
	docker build -t flowgent/flowgent:latest .
