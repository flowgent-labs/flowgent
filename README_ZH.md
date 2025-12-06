# Flowgent


> **构建由自主化 AI Agents 驱动的确定性工作流。**

Flowgent 是一款通用原生 AI 编排引擎，它将 LLM 代理的自适应智能与确定性 DAG 执行的可预测性、可追溯性和可靠性相结合。

---

## 核心亮点

| 特性 | 为什么重要 |
|---------|----------------|
| **动态 + 确定性兼顾** | `agent` 和 `supervisor` 节点提供 LLM 智能；`tool`、`map`、`condition`、`tribunal`、`human`、`noop` 节点保证确定性执行。智能放哪里，由你决定 |
| **DAG 拓扑调度器** | 可预测的执行顺序 + 并发扇出（`map` 节点）——有界 goroutine pool 并行处理 100+ 仓库 |
| **受控自主性** | Supervisor 被严格限制为 5 种操作（`continue` / `redirect` / `retry` / `inject` / `abort`），可配置配额上限——LLM 驱动但绝不失控 |
| **A2A 协议服务** | 外部 AI 系统可通过 Google A2A 协议在独立端口上**动态调用 Flowgent**——发现 agentflow、触发运行并以编程方式查询结果 |
| **状态机持久化** | 每次运行和任务均可持久化；可在任意 `human` 审批节点暂停，通过 API 恢复，幂等重放 |
| **双模式部署** | **一体化：** SQLite + 内存队列（单二进制、零依赖）。**分布式：** PostgreSQL + MQTT（EMQX）+ Kubernetes |
| **OTEL 逐节点追踪** | 每个节点 Span 记录输入、输出和内部状态——在 Jaeger 中可调试任意执行路径 |
| **9 种节点类型** | `agent`、`tool`、`map`、`agentflow`、`condition`、`tribunal`、`human`、`supervisor`、`noop`——组合出任意的编排拓扑 |
| **Cron + Webhook 触发器** | 每个 agentflow 支持定时调度和事件驱动（GitHub/GitLab webhook）|
| **多提供商 LLM** | OpenAI 兼容适配器，支持按提供商限流、SOCKS/HTTP 代理、modalities、extended thinking |
| **MCP 生态** | 5 个 stdio MCP 服务：GitHub、SonarQube、Sonatype IQ、Nexus3、Test（Maven/Cucumber）|
| **OAS 3.1 + Swagger** | 开箱即用的完整 REST API 文档 + Swagger UI |

---

## 快速安装

### 环境要求

- **Go 1.25+**
- （可选）PostgreSQL 15+（分布式模式）
- （可选）EMQX 5.x（MQTT 分布式队列）

### 构建

```bash
git clone git@github.com:flowgent-labs/flowgent.git && cd flowgent
make build
```

生成 1 个主二进制 + 5 个 MCP 服务，位于 `bin/`：

```
bin/
├── flowgent
├── mcp-server-github
├── mcp-server-sonarqube
├── mcp-server-sonatypeiq
├── mcp-server-nexus3
└── mcp-server-test
```

### 运行

```bash
# 一体化模式（SQLite——零外部依赖）
./bin/flowgent daemon start

# 自定义配置 + 调试日志
export FLOWGENT_CONFIG_FILE=/etc/flowgent/production.yaml
./bin/flowgent -v daemon start
```

REST API `:9999` · A2A `:9992` · pprof `:9991`

### 验证

```bash
curl http://localhost:9999/_/healthz                    # {"status":"ok"}
curl http://localhost:9999/_/openapi.yaml               # OpenAPI 3.1 规范
curl http://localhost:9992/.well-known/agent.json       # A2A agent card
```

### 交互式控制台

```bash
./bin/flowgent console

flowgent> list agentflows
flowgent> list runs
flowgent> show run <id>
flowgent> tasks <run-id>
flowgent> exit
```

---

## 开发者快速入门

```bash
make build        # 编译全部二进制文件
make test         # 运行全部测试
make fmt          # 格式化源码
```

### 单独运行组件

```bash
# 主服务
go build -o bin/flowgent ./src/cmd/server && ./bin/flowgent daemon start

# 特定 MCP 服务
go build -o bin/mcp-server-github ./src/cmd/mcp-server-github
GITHUB_TOKEN=xxx ./bin/mcp-server-github
```

### 新增节点类型

1. 在 `src/model/node.go` 中添加常量
2. 在 `src/engine/executor.go` → `executeNode` 中注册
3. LLM 节点 → YAML 中提供 `soul` + `instruction`；确定性节点 → 纯 Go 逻辑

### 新增 MCP 工具

1. 创建 `src/cmd/mcp-server-<name>/main.go`（stdio MCP 模式）
2. 在 `etc/flowgent.yaml` → `orchestration.mcps` 中注册
3. 在任意 L2 agentflow 中通过 `type: tool` + `tool: <name>` 引用

### 配置优先级

```
-c/--config 参数  >  $FLOWGENT_CONFIG_FILE  >  etc/flowgent.yaml
```

---

## 核心架构

```
触发器层（定时任务 / Webhook）
        ↓
监督者（控制平面 — LLM，受约束）
        ↓
DAG 执行器（拓扑调度 + 数据流）
        ↓
节点（agent / tool / map / agentflow / condition / tribunal / human / supervisor / noop）
```

| 平面 | 节点 | 行为 |
|-------|-------|-----------|
| **数据平面** | tool, map, agentflow, condition, tribunal, human, noop | 确定性 |
| **控制平面** | agent, supervisor | LLM 驱动，受约束 |

---

## 企业级 Agentflow 示例（L2）

### 安全自动修复——11 阶段漏洞修复闭环

```
4 路并行扫描 → LLM 分类 → 嵌套 map 扇出修复 → 3 Agent 同行评审 →
多数投票表决 → 监督者安全检查 → 人工审批（24h 超时）→
提交 & PR → 多渠道通知
```

→ `etc/sample-security-autonomy-fixer.yaml`

### 自动测试代码生成——Confluence 到 Cucumber 流水线

```
Confluence 获取 → 需求提取 → 按项目类型生成测试计划 →
Cucumber .feature + step 定义（Spring Boot / Flask / React）→
审查 → 投票 → 提交 & PR → 通知
```

→ `etc/sample-autotest-generation.yaml`

---

## 部署模式

| 模式 | 存储 | 队列 | 适用场景 |
|------|---------|-------|--------|
| **一体化** | SQLite | 内存队列 | 本地开发、单节点 |
| **分布式** | PostgreSQL | MQTT (EMQX) | Kubernetes 集群 |

---

## API

| 接口 | 端口 | 规范 |
|-----------|------|------|
| REST API | `:9999` | OAS 3.1（`/_/openapi.yaml`）+ Swagger UI |
| A2A 协议 | `:9992` | Google Agent-to-Agent（`/.well-known/agent.json`）|
| 管理端口 | `:9991` | pprof（`/debug/pprof/`）|

---

## License

详见 [LICENSE](LICENSE)。
