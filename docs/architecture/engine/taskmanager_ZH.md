# TaskManager

[系统总览](../overview_ZH.md) · [AgentFlow 应用层](../agent-flow_ZH.md) · [Sandbox](sandbox_ZH.md)

TaskManager 是执行 Worker。MQTT 共享订阅在 Pod 与 Slot 之间分配 Plan；每个
Slot 将一个 Plan 路由到对应 Executor，通过 API Server 持久化 Task 状态，并
把路由/状态结果返回 JobManager。TaskManager 不直接访问数据库。

```text
JobManager ──ExecutionPlan/MQTT──► SlotWorker ──► TaskExecutorRouter
     ▲                                  │                 │
     └──────exec/result/MQTT────────────┘                 ├── LLM / MCP
                                                         ├── Subflow / Skill
                                                         └── Sandbox via MQTT
```

## Worker 执行

Kubernetes 使用 runtime-cluster 范围共享 Deployment，all-in-one 使用进程内
Worker。TaskManager Worker 只订阅自己的 ExecutionPlan topic，并拒绝 namespace
或 `runtime_cluster_id` 不匹配的消息。
每个 TM Pod 运行 N 个
`SlotWorker` Goroutine（默认 4）。每个 Slot 独立从 MQTT Queue（或 Channel）
取一个 `ExecutionPlan`，通过 `TaskExecutorRouter` 执行，经 API Server REST
（`TaskStateStore.SaveTask`）持久化 Task 状态，再通过 MQTT 向 JM 报告结果。

执行关系是：**1 Slot = 1 ExecutionPlan attempt**。JobManager 为每个 DAG Node
保留一个逻辑 Plan，但在显式 Node retry policy 下可重复分发；每次分发都有独立
TaskRun 标识。含 4 个 Slot 的 TM Pod 最多并发执行 4 个 attempt。

JobManager 在经 MQTT 发布前把 W3C TraceContext 与 Baggage 注入
`ExecutionPlan.trace_context`。SlotWorker 提取后创建 `taskmanager.execute`
Child Span，并写入 Run/Node/Task/Attempt 关联 ID。输入输出正文保存在 TaskRun；
Trace attributes 只含有界的 content type、byte count、SHA-256 与 capture 元数据。

### Executor Router 边界

TaskManager 拥有 `TaskExecutorRouter`，但 Node 语义与组合规则属于 L2。
规范的 12 Node 矩阵见 [AgentFlow Node 目录](../agent-flow_ZH.md)。

### MCP 传输——仅 HTTP（Streamable HTTP）

TM Pod 是运行于 K8s 的纯后端 Agent，没有人工交互、桌面环境或子进程启动器。
所有 MCP 通信都通过 `mark3labs/mcp-go` 的
`client.NewStreamableHttpClient` 使用 **Streamable HTTP**。`McpManager`
管理 HTTP Client 生命周期；每个 MCP 定义保存 URL 和可选 Header。含 Secret 的
Header/环境配置只保存为注入环境变量引用，并在构造 Client 前由 TaskManager 内部
解析；浏览器及 API 读取模型不会出现解析后的凭据。TaskManager 不会启动
command/args/env 子进程。

```text
TM Pod (SlotWorker)
  → ToolExecutor.Execute(plan)
    → McpManager.CallTool(serverName, toolName, args)
      → GetClient(name) → NewStreamableHttpClient(url, WithHTTPHeaders(headers))
        → c.Initialize(ctx, initReq)     // 通过 HTTP 完成 MCP 协议握手
        → c.CallTool(ctx, callToolReq)   // 通过 HTTP POST 发送 JSON-RPC
          → 上游 MCP Server（sonarqube-mcp、github-mcp 等）
```

PG `llm_mcp` 表中的 `McpInfo` 包含：

- `url`——MCP Server 端点，例如 `http://172.29.235.101:18080/mcp`。
- `headers`——可选的认证/转发 Header；敏感值使用如
  `Bearer ${GITHUB_TOKEN}` 的模板，并从 Pod 环境解析。

LLM Provider API Key 使用相同边界：定义保存环境变量引用，LLM Manager 在 Worker
进程内构建 Provider Client 前解析。引用缺失时 Task 明确失败，绝不把引用字符串
当作 Key 发送给上游。

这与 Claude Code、Codex、Cursor、OpenCode 对远程 MCP Server 的支持一致；
TM 只是另一个通过 HTTP 连接的 MCP Client。

### 心跳与故障转移

TM 定期发送心跳。JM 的 KubernetesResourceManager 检测死亡 TM 并重新分发
孤儿 Plan。

---

## 预期行为

1. **给定** N 个 Pod、每个 M 个 Slot，**当** Plan 通过共享订阅到达时，**则**任一 Pod **MUST NOT** 同时执行超过 M 个 Plan，且每个已接收 Plan **MUST** 只归一个 Slot 所有。
2. **给定** Plan Kind，**当**开始路由时，**则 MUST** 仅运行对应 Executor；不支持的 Kind **MUST** 显式失败。
3. **给定** Tool Node，**当**调用 MCP 时，**则** TaskManager **MUST** 使用配置的 Streamable HTTP URL/Header，**MUST NOT** 启动 stdio MCP Server 子进程。
4. **给定**任意 Task 完成，**当**报告结果时，**则 MUST** 先通过 API Server 持久化完整 Task 状态/输出，再把仅含状态的 MQTT 回调视为已交付。
5. **给定** Sandbox Node，**当**启用 Sandbox 执行时，**则** TaskManager **MUST** 通过 Sandbox Trigger/Result 契约委托，**MUST NOT** 静默启动内嵌 Sandbox Worker。
6. **给定**活跃 Plan 执行期间心跳丢失，**当** JobManager 判定 TM 死亡时，**则**孤儿恢复 **MUST NOT** 为同一 Task 产生两个成功所有者。
7. **给定**经 MQTT 收到 Plan，**当**开始执行时，**则** Worker **MUST** 提取 W3C Context、保存 TaskRun 时保留 Parent attempt ID，且 **MUST NOT** 把 payload 正文复制到 Span attributes。
8. **给定** LLM/MCP 凭据引用，**当** Worker 构建 Client 时，**则 MUST** 从注入环境解析；缺失时必须明确失败且不得记录凭据值。
