.PHONY: build build-server build-wallet build-mcp clean test fmt

# Build all
build: build-server build-wallet build-mcp

# Build main server
build-server:
	GONOSUMCHECK='*' GOFLAGS=-mod=mod go build -o bin/flowgent \
		-ldflags "-X main.Version=dev -X main.GitCommit=$(shell git rev-parse HEAD) -X main.BuildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)" \
		./src/cmd/server

# Build wallet daemon
build-wallet:
	GONOSUMCHECK='*' GOFLAGS=-mod=mod go build -o bin/flowgent-wallet \
		-ldflags "-X main.Version=dev -X main.GitCommit=$(shell git rev-parse HEAD) -X main.BuildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)" \
		./src/cmd/flowgent-wallet

# Build all MCP servers
build-mcp: build-mcp-github build-mcp-test build-mcp-sonarqube build-mcp-sonatypeiq build-mcp-nexus3

build-mcp-github:
	GONOSUMCHECK='*' GOFLAGS=-mod=mod go build -o bin/mcp-server-github ./src/cmd/mcp-server-github

build-mcp-test:
	GONOSUMCHECK='*' GOFLAGS=-mod=mod go build -o bin/mcp-server-test ./src/cmd/mcp-server-test

build-mcp-sonarqube:
	GONOSUMCHECK='*' GOFLAGS=-mod=mod go build -o bin/mcp-server-sonarqube ./src/cmd/mcp-server-sonarqube

build-mcp-sonatypeiq:
	GONOSUMCHECK='*' GOFLAGS=-mod=mod go build -o bin/mcp-server-sonatypeiq ./src/cmd/mcp-server-sonatypeiq

build-mcp-nexus3:
	GONOSUMCHECK='*' GOFLAGS=-mod=mod go build -o bin/mcp-server-nexus3 ./src/cmd/mcp-server-nexus3

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
