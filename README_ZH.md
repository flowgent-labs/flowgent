# Flowgent

构建下一代可预测、可审计、分布式、有约束的企业级超级智能体——原生内置 AI 经济层（x402/MPP）。

> Flowgent 是一款 AI-Native 通用编排引擎，架构参考 Apache Flink 的 Session/Application 模式。它将 LLM Agent 的自适应智能与确定性 DAG 执行深度结合——既保留传统工作流的可预测性、可靠性和可审计性，又赋予企业级分布式超级智能体在受控边界内的动态自主能力。原生支持 AI-to-AI 支付协议（x402/MPP），让智能体之间的服务调用可计量、可结算、可治理。

---

## 特性

- **DAG 拓扑调度器** — 可预测的执行顺序 + 并发扇出。11 种节点类型组合出任意编排拓扑。
- **确定性 + 智能兼顾** — `agent` 和 `supervisor` 节点提供 LLM 智能；其余节点保证确定性执行。
- **受控自主性** — Supervisor 限制为 `continue|retry|inject|abort`，可配置配额上限。
- **状态机持久化** — 每次运行均可持久化。可在 `human` 审批节点暂停，通过 API 恢复，幂等重放。
- **多租户 API** — 租户范围路径 `/api/v1/{tenant}/...`，支持 JWT/OIDC/GitHub OAuth，A2A 协议服务。
- **Session & Application 模式** — 优先级 `grade` → 独立 K8s 集群；`low|medium|high` → 共享池。
- **双模式部署** — 一体化（SQLite + 内存队列）或生产（PostgreSQL + MQTT/EMQX + Redis + K8s）。
- **OTEL 逐节点追踪** — 每个节点 Span 记录输入、输出和内部状态，可在 Jaeger 中调试。
- **Cron + Webhook 触发器** — 每个 agentflow 支持定时调度和事件驱动（GitHub/GitLab webhook）。
- **多提供商 LLM** — OpenAI 兼容适配器，支持按提供商限流、SOCKS/HTTP 代理、modalities、extended thinking。
- **JSON Schema 校验** — agent 输出可配置 `output_schema`，校验失败自动重试。
- **通知服务** — Telegram、DingTalk、Slack、Email、Webhook；WebSocket SSE 推送人工审批。
- **可选经济层** — x402 支付协议客户端，含消费策略、钱包抽象、人工审批治理。

---

## 快速开始

**环境要求:** Go 1.26+ · （可选）PostgreSQL 15+, EMQX 5.x, Redis 7.x

```bash
git clone git@github.com:flowgent-labs/flowgent.git && cd flowgent
make build-flowgent    # 核心引擎
make example-mcps      # 示例 MCP 服务（e2e 测试用）
```

运行时必须指定配置文件：

```bash
# 一体化模式（SQLite + 内存队列）
./bin/flowgent daemon start -c examples/scenario-e2e-allinone.yaml

# 生产模式（PostgreSQL + MQTT + Redis）
./bin/flowgent daemon start -c examples/scenario-e2e-production.yaml

# 使用完整示例配置作为模板
cp etc/flowgent.yaml.fully.sample my-config.yaml
./bin/flowgent daemon start -c my-config.yaml
```

验证：

```bash
curl http://localhost:9999/_/healthz             # {"status":"ok"}
curl http://localhost:9999/_/openapi.yaml        # OpenAPI 3.1 规范
curl http://localhost:9992/.well-known/agent.json # A2A agent card
```

REST API `:9999` · A2A `:9992` · Wallet `:9901` · pprof `:9991`

---

## 核心架构

```
API Server ──→ JobManager ──→ Scheduler ──→ TaskManager(s) ──→ MQTT ──→ Executors
  (网关)        (控制)        (可插拔)      (弹性 Pod)        (总线)    (11 类型)
```

| 组件 | 职责 | 文档 |
|------|------|------|
| API Server | 多租户 REST + A2A 网关 | [01-DESIGN](docs/01-DESIGN-engine-architecture.md) |
| JobManager | DAG 编排，模式路由 | [01-DESIGN](docs/01-DESIGN-engine-architecture.md) |
| ResourceManager | 可插拔调度（local/K8s） | [01-DESIGN](docs/01-DESIGN-engine-architecture.md) |
| TaskManager | 常驻 SlotWorker 池 | [01-DESIGN](docs/01-DESIGN-engine-architecture.md) |
| MQTT Event Bus | 分布式 JM↔TM 通信 | [01-DESIGN](docs/01-DESIGN-engine-architecture.md) |

**节点类型（11 种）：** `agent` `tool` `map` `join` `agentflow` `condition` `tribunal` `human` `supervisor` `sandbox` `noop`

---

## 示例

内置示例在 `examples/` 目录下——包含 agent 定义、agentflow 编排、MCP 服务器和场景配置。

### AgentFlow 示例（L2）

| 流程 | 说明 | 文件 |
|------|------|------|
| 安全自动修复 v1 | 21 节点完整管线（需 SonarQube + Sonatype MCP） | `examples/flows/01-security-autonomy-fix-v1.yaml` |
| 安全自动修复 v2 | 11 节点简化版（纯 agent + cyberbot 元数据） | `examples/flows/01-security-autonomy-fix-v2.yaml` |
| 自动测试生成 | Confluence → Cucumber 流水线 | `examples/flows/20-autotest-generation-v1.yaml` |

### 场景配置

| 配置 | 存储 | 缓存 | 队列 | 用途 |
|------|------|------|------|------|
| `examples/scenario-e2e-allinone.yaml` | SQLite | Memory | Memory | 开发 / CI |
| `examples/scenario-e2e-production.yaml` | PostgreSQL | Redis | MQTT | K8s / 生产 |

E2E 完整指南 → [docs/20-TEST-e2e-guide.md](docs/20-TEST-e2e-guide.md)

---

## 开发者快速入门

```bash
make build-flowgent    # 核心二进制
make example-mcps      # 示例 MCP 服务
make test              # 运行全部测试
make fmt               # 格式化源码
```

### 项目结构

```
src/cmd/flowgent/   — 核心 CLI（daemon, apiserver, a2a, wallet, console）
src/api/            — REST API 处理器（租户范围 CRUD）
src/engine/         — JM, RM（调度器）, TM, 执行器（11 种节点类型）
src/model/          — 领域类型（AgentFlowSpec, ExecutionPlan, NodeSpec 等）
src/store/          — 持久化（SQLite, PostgreSQL）
src/llm/            — LLM 客户端（OpenAI 兼容）+ MCP 工厂
src/notification/   — 通知服务（Telegram, DingTalk, Slack, Email, Webhook）
examples/           — agent, flow, MCP 服务器, 场景配置
docs/               — 设计文档, e2e 指南
```

### 配置

```
-c/--config 参数  >  $FLOWGENT_CONFIG_FILE  （两者必须指定其一，无默认值）
```

完整配置参考 → `etc/flowgent.yaml.fully.sample`

---

## License

详见 [LICENSE](LICENSE)。
