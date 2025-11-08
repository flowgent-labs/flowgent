.PHONY: build build-bot build-mcp clean test

# Build all
build: build-bot build-mcp

# Build Bot main program
build-bot:
	GOPROXY=https://goproxy.cn,direct GONOSUMCHECK='*' go build -o bin/cyberbot \
		-ldflags "-X main.Version=dev -X main.GitCommit=$(shell git rev-parse HEAD) -X main.BuildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)" \
		./src/cmd/cyberbot

# Build all MCP Servers
build-mcp: build-mcp-github build-mcp-test build-mcp-sonarqube build-mcp-sonatypeiq build-mcp-agent

build-mcp-github:
	GOPROXY=https://goproxy.cn,direct GONOSUMCHECK='*' go build -o bin/mcp-server-github ./src/cmd/mcp-server-github

build-mcp-test:
	GOPROXY=https://goproxy.cn,direct GONOSUMCHECK='*' go build -o bin/mcp-server-test ./src/cmd/mcp-server-test

build-mcp-sonarqube:
	GOPROXY=https://goproxy.cn,direct GONOSUMCHECK='*' go build -o bin/mcp-server-sonarqube ./src/cmd/mcp-server-sonarqube

build-mcp-sonatypeiq:
	GOPROXY=https://goproxy.cn,direct GONOSUMCHECK='*' go build -o bin/mcp-server-sonatypeiq ./src/cmd/mcp-server-sonatypeiq

build-mcp-agent:
	GOPROXY=https://goproxy.cn,direct GONOSUMCHECK='*' go build -o bin/cyberbot-agent ./src/cmd/agent

# Clean
clean:
	rm -rf bin/

# Run tests
test:
	GOPROXY=https://goproxy.cn,direct GONOSUMCHECK='*' go test -v ./...

# Format code
fmt:
	go fmt ./...

# Docker build
docker-build:
	docker build -t cyberbot/cve-auto-fix:latest .

# Dev mode
dev: build-bot
	./bin/cyberbot --config src/configs/config.yaml.example
