.PHONY: build build-server build-mcp clean test fmt

# Build all
build: build-server build-mcp

# Build main server
build-server:
	GONOSUMCHECK='*' GOFLAGS=-mod=mod go build -o bin/flowgent-server \
		-ldflags "-X main.Version=dev -X main.GitCommit=$(shell git rev-parse HEAD) -X main.BuildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)" \
		./src/cmd/server

# Build A2A agent server
build-agent:
	GONOSUMCHECK='*' GOFLAGS=-mod=mod go build -o bin/flowgent-agent \
		-ldflags "-X main.Version=dev -X main.GitCommit=$(shell git rev-parse HEAD) -X main.BuildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)" \
		./src/cmd/agent

# Build all MCP servers
build-mcp: build-mcp-github build-mcp-test build-mcp-sonarqube build-mcp-sonatypeiq

build-mcp-github:
	GONOSUMCHECK='*' GOFLAGS=-mod=mod go build -o bin/mcp-server-github ./src/cmd/mcp-server-github

build-mcp-test:
	GONOSUMCHECK='*' GOFLAGS=-mod=mod go build -o bin/mcp-server-test ./src/cmd/mcp-server-test

build-mcp-sonarqube:
	GONOSUMCHECK='*' GOFLAGS=-mod=mod go build -o bin/mcp-server-sonarqube ./src/cmd/mcp-server-sonarqube

build-mcp-sonatypeiq:
	GONOSUMCHECK='*' GOFLAGS=-mod=mod go build -o bin/mcp-server-sonatypeiq ./src/cmd/mcp-server-sonatypeiq

# Clean
clean:
	rm -rf bin/

# Run tests
test:
	GONOSUMCHECK='*' GOFLAGS=-mod=mod go test -count=1 -timeout 120s ./src/... ./tests/...

# Format
fmt:
	go fmt ./src/... ./tests/...

# Docker build
docker-build:
	docker build -t flowgent/flowgent:latest .
