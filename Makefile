.PHONY: build build-bot build-mcp clean test

# 构建所有
build: build-bot build-mcp

# 构建 Bot 主程序
build-bot:
	go build -o bin/cyberbot \
		-ldflags "-X main.Version=dev -X main.GitCommit=$(shell git rev-parse HEAD) -X main.BuildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)" \
		./src/cmd/cyberbot

# 构建 MCP Server
build-mcp:
	go build -o bin/mcp-server-cyberflows ./src/cmd/mcp-server-cyberflows

# 清理
clean:
	rm -rf bin/

# 运行测试
test:
	go test ./...

# 格式化代码
fmt:
	go fmt ./...

# 安装到系统
install: build
	cp bin/cyberbot /usr/local/bin/
	cp bin/mcp-server-cyberflows /usr/local/bin/

# Docker 构建
docker-build:
	docker build -t cyberbot/cve-auto-fix:latest .

# 开发模式运行
dev: build-bot
	./bin/cyberbot --config src/configs/config.yaml.example
