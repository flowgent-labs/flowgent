# tests/it — Portable Local Integration Tests

**集成测试定义：除外部 API 走本地 Mock，其余全部使用真实组件（Docker PostgreSQL、真实 Sandbox 等）。MCP 协议走 in-process bridge，无需 Docker 容器。**

与 `use-cases/` 真实 E2E 的核心区别：此处外部 API 全部本地模拟，可零依赖 `go test` 运行（仅需 Docker PostgreSQL）；真实案例测试依赖外部真实 API（GitHub、SonarQube 等）。

```bash
# 1. Start real middleware
docker compose -f deploy/docker/pgvector/docker-compose.yml up -d

# 2. Run all IT tests (-p 1 required: fixed-port mocks + shared PG)
go test github.com/flowgent-labs/flowgent/tests/it/... -count=1 -timeout 300s -p 1
```

## Structure

```
tests/it/
  runner.go               ← 总入口：ITRunner harness（对应 use-cases/.../runner.py）
  externalmock/            ← 外部 API Mock（LLM、GitHub、SonarQube、Telegram）
    base_api_mocksvc.go    ← Mock LLM server (random port)
    github_api_mocksvc.go  ← Mock GitHub REST API (fixed :19002)
    sonarqube_api_mocksvc.go ← Mock SonarQube REST API (fixed :19001)
    telegram_api_mocksvc.go  ← Mock Telegram Bot API (fixed :19003)
    mcp_bridge.go          ← In-process MCP↔REST bridge (:13080/:13081)
  engine/                  ← 各模块集成测试（对应 use-cases/.../scenarios/）
  apiserver/
  controller/
  notifier/
  knowledge/
  sandbox/
  deploy/docker/           ← 中间件 Docker compose（PostgreSQL 必选，MCP 容器可选）
    pgvector/
    github-mcp/
    sonarqube-mcp/
```

## Design

- `it.New(flow)` 启动真实 apiserver（httptest）+ standalone engine + Docker PostgreSQL，LLM/github/sonarqube 走本地 mock（随机端口）
- `it.NewWithExternalMocks(flow)` 额外启动 in-process MCP bridge（:13080/:13081），将 MCP JSON-RPC 调用翻译到固定端口 REST mock（:19001/:19002），真实 ToolNode 执行全链路
- 测试通过 HTTP API 触发 flow 和查询状态，通过 `ITRunner.Pool()` 直接 SQL 校验 PG
- MCP 容器（docker/github-mcp/, docker/sonarqube-mcp/）为可选项，仅在需要测试真实 MCP 服务端行为时使用
