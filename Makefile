.PHONY: build build-flowgent examples example-mcps example-flows clean test fmt

# ── Default: build flowgent only ─────────────────────────
build: build-flowgent

# ── Flowgent core binary (daemon, apiserver, a2a, wallet, etc.) ─
build-flowgent:
	GONOSUMCHECK='*' GOFLAGS=-mod=mod go build -o bin/flowgent \
		-ldflags "-X main.Version=dev -X main.GitCommit=$(shell git rev-parse HEAD) -X main.BuildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)" \
		./src/cmd/flowgent

# ── Example MCP servers (for e2e testing only) ────────────
example-mcps: example-mcp-github example-mcp-sonarqube example-mcp-sonatypeiq example-mcp-nexus3 example-mcp-test

example-mcp-github:
	GONOSUMCHECK='*' GOFLAGS=-mod=mod go build -o bin/mcp-server-github ./examples/mcp-github

example-mcp-sonarqube:
	GONOSUMCHECK='*' GOFLAGS=-mod=mod go build -o bin/mcp-server-sonarqube ./examples/mcp-sonarqube

example-mcp-sonatypeiq:
	GONOSUMCHECK='*' GOFLAGS=-mod=mod go build -o bin/mcp-server-sonatypeiq ./examples/mcp-sonatypeiq

example-mcp-nexus3:
	GONOSUMCHECK='*' GOFLAGS=-mod=mod go build -o bin/mcp-server-nexus3 ./examples/mcp-nexus3

example-mcp-test:
	GONOSUMCHECK='*' GOFLAGS=-mod=mod go build -o bin/mcp-server-test ./examples/mcp-test

# ── All examples ──────────────────────────────────────────
examples: example-mcps

# ── Utilities ─────────────────────────────────────────────
clean:
	rm -rf bin/

test:
	GONOSUMCHECK='*' GOFLAGS=-mod=mod go test -count=1 -timeout 120s ./src/... ./tests/...

fmt:
	go fmt ./src/... ./tests/... ./examples/...

# ── Docker ────────────────────────────────────────────────
docker-build:
	docker build -t flowgent/flowgent:latest .
