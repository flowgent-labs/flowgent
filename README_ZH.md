# Flowgent

构建下一代可预测、可审计、分布式、有约束的企业级超级智能体——原生内置 AI 经济层（x402/MPP）。

> Flowgent 是一款 AI-Native 通用编排引擎。它将 LLM Agent 的自适应智能与确定性 DAG 执行深度结合——既保留传统工作流的可预测性、可靠性和可审计性，又赋予企业级分布式超级智能体在受控边界内的动态自主能力。原生支持 AI-to-AI 支付协议（x402/MPP），让智能体之间的服务调用可计量、可结算、可治理。

---

## 特性

**DAG 拓扑调度器** — 可预测的执行顺序 + 并发扇出（`map` 节点）。有界 goroutine pool 并行处理 100+ 仓库。

**9 种节点类型** — `agent`、`tool`、`map`、`agentflow`、`condition`、`tribunal`、`human`、`supervisor`、`noop`。组合出任意的编排拓扑。

**确定性 + 智能兼顾** — `agent` 和 `supervisor` 节点提供 LLM 智能。其余节点保证确定性执行。智能放哪里，由你决定。

**受控自主性** — Supervisor 被严格限制为 5 种操作（`continue`、`redirect`、`retry`、`inject`、`abort`），可配置配额上限。LLM 驱动但绝不失控。

**状态机持久化** — 每次运行和任务均可持久化。可在任意 `human` 审批节点暂停，通过 API 恢复，幂等重放。

**A2A 协议服务** — 外部 AI 系统可通过 Google A2A 协议动态调用 Flowgent。发现 agentflow、触发运行、查询结果。

**可选经济层** — x402 支付协议客户端支持，包含消费策略、钱包抽象、人工审批治理。Coinbase facilitator 集成。

**双模式部署** — **一体化：** SQLite + 内存队列（单二进制、零依赖）。**分布式：** PostgreSQL + MQTT（EMQX）+ Kubernetes。

**OTEL 逐节点追踪** — 每个节点 Span 记录输入、输出和内部状态。在 Jaeger 中调试任意执行路径。

**Cron + Webhook 触发器** — 每个 agentflow 支持定时调度和事件驱动（GitHub/GitLab webhook）。

**多提供商 LLM** — OpenAI 兼容适配器，支持按提供商限流、SOCKS/HTTP 代理、modalities、extended thinking。

**MCP 生态** — 5 个 stdio MCP 服务：GitHub、SonarQube、Sonatype IQ、Nexus3、Test（Maven/Cucumber）。

**OAS 3.1 + Swagger** — 开箱即用的完整 REST API 文档 + Swagger UI。

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

生成 1 个统一二进制 + 5 个 MCP 服务，位于 `bin/`：

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

# 单独启动组件
./bin/flowgent apiserver     # 仅 REST API
./bin/flowgent a2a           # 仅 A2A 协议
./bin/flowgent wallet        # 钱包密钥管理守护进程
./bin/flowgent console       # 交互式管理控制台

# 自定义配置 + 调试日志
./bin/flowgent -v --config /etc/flowgent/production.yaml daemon start
```

REST API `:9999` · A2A `:9992` · Wallet `:9901` · pprof `:9991`

### 验证

```bash
curl http://localhost:9999/_/healthz                    # {"status":"ok"}
curl http://localhost:9999/_/openapi.yaml               # OpenAPI 3.1 规范
curl http://localhost:9992/.well-known/agent.json       # A2A agent card
curl http://localhost:9901/health                       # Wallet 健康检查
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

### CLI 帮助

```bash
./bin/flowgent --help              # 顶级命令列表
./bin/flowgent daemon --help       # daemon 子命令（start/stop/restart）
./bin/flowgent wallet --help       # wallet 选项（--listen, --master-key, --generate-key）
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
go build -o bin/flowgent ./src/cmd/core && ./bin/flowgent daemon start

# 特定组件
./bin/flowgent apiserver
./bin/flowgent wallet

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
