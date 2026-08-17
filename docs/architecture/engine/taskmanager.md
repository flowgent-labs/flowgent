# TaskManager

[System overview](../overview.md) · [AgentFlow application layer](../agent-flow.md) · [Sandbox](sandbox.md)

TaskManager is the execution worker. MQTT shared subscriptions distribute plans
across pods and slots; each slot routes one plan to the matching executor,
persists task state through the API Server, and returns routing/status data to
JobManager. TaskManager has no direct database access.

```text
JobManager ──ExecutionPlan/MQTT──► SlotWorker ──► TaskExecutorRouter
     ▲                                  │                 │
     └──────exec/result/MQTT────────────┘                 ├── LLM / MCP
                                                         ├── subflow / skill
                                                         └── Sandbox via MQTT
```

## Worker Execution

Kubernetes uses namespace/pool-scoped shared Deployments; all-in-one uses
in-process workers. A Pool worker subscribes only to its own scoped plan topic
and rejects mismatched payload metadata.
Each TM pod runs N `SlotWorker` goroutines (default 4). Each slot independently dequeues one
`ExecutionPlan` from the MQTT queue (or channel), executes it via the
`TaskExecutorRouter`, persists task status via the apiserver REST API
(`TaskStateStore.SaveTask`), and reports the result back to JM via MQTT.

The execution relationship is **1 Slot = 1 ExecutionPlan attempt**. JobManager
retains one logical plan per DAG node but may dispatch it repeatedly under an
explicit node retry policy; every dispatch has a distinct TaskRun identity. A TM
pod with 4 slots executes up to 4 attempts concurrently.

Before publishing through MQTT, JobManager injects W3C TraceContext and Baggage
into `ExecutionPlan.trace_context`. SlotWorker extracts it and creates a
`taskmanager.execute` child span with run/node/task/attempt correlation IDs.
Input/output content remains in TaskRun; trace attributes contain only bounded
content type, byte count, SHA-256 and capture metadata.

### Executor Router Boundary

TaskManager owns the `TaskExecutorRouter`, but node semantics and composition rules are an L2 concern. See the [AgentFlow node catalog](../agent-flow.md#supported-node-types) for the canonical 12-node matrix.

### MCP Transport — HTTP-Only (Streamable HTTP)

TM pods are pure backend agents running in K8s — no human interaction, no desktop
environment, no subprocess launcher. All MCP (Model Context Protocol) server
communication uses **Streamable HTTP** transport via `mark3labs/mcp-go`'s
`client.NewStreamableHttpClient`. The `McpManager` manages HTTP client lifecycle:
each MCP definition stores a URL and optional headers. Secret-bearing headers
and environment entries are stored as injected environment references and
expanded inside TaskManager immediately before client construction. The browser
and API read model never contain the resolved credential. TaskManager does not
start a command/args/env subprocess.

```
TM Pod (SlotWorker)
  → ToolExecutor.Execute(plan)
    → McpManager.CallTool(serverName, toolName, args)
      → GetClient(name) → NewStreamableHttpClient(url, WithHTTPHeaders(headers))
        → c.Initialize(ctx, initReq)     // MCP protocol handshake over HTTP
        → c.CallTool(ctx, callToolReq)   // JSON-RPC over HTTP POST
          → upstream MCP server (sonarqube-mcp, github-mcp, etc.)
```

The `McpInfo` entity stored in PG (`llm_mcp` table) carries:
- `url` — MCP server endpoint (e.g. `http://172.29.235.101:18080/mcp`)
- `headers` — optional auth/forwarding headers; sensitive values use templates
  such as `Bearer ${GITHUB_TOKEN}` and resolve from the pod environment

LLM provider API keys follow the same boundary: the definition stores an env
reference, and the LLM manager resolves it inside the worker process before
constructing the provider client. A missing referenced variable fails the task
explicitly; it never falls back to sending the reference string upstream.

This is consistent with how Claude Code, Codex, Cursor, and OpenCode all support
remote MCP servers — the TM is just another MCP client, connecting over HTTP.

### Heartbeat & Failover

TMs send periodic heartbeats. JM's KubernetesResourceManager detects dead TMs
and re-dispatches orphaned plans.

---

## Expected Behavior

1. **Given** `N` pods with `M` slots, **when** plans arrive through the shared
   subscription, **then** no pod may execute more than `M` plans concurrently
   and each accepted plan MUST be owned by one slot.
2. **Given** a plan kind, **when** routing begins, **then** exactly the matching
   executor MUST run; unsupported kinds MUST fail explicitly.
3. **Given** a tool node, **when** its MCP is invoked, **then** TaskManager MUST
   use configured Streamable HTTP URL/headers and MUST NOT spawn a stdio MCP
   server process.
4. **Given** any task completion, **when** the result is reported, **then** full
   task state/output MUST be persisted through API Server before the state-only
   MQTT callback is considered delivered.
5. **Given** a sandbox node, **when** Sandbox execution is enabled, **then**
   TaskManager MUST delegate through the sandbox trigger/result contract and
   MUST NOT silently run an embedded sandbox worker.
6. **Given** heartbeat loss during an active plan, **when** JobManager declares
   the TM dead, **then** orphan recovery MUST not produce two successful owners
   for the same task.
7. **Given** a plan received over MQTT, **when** execution starts, **then** the
   worker MUST extract its W3C context, preserve the parent attempt ID when
   saving TaskRun, and MUST NOT copy payload content into span attributes.
8. **Given** an LLM/MCP credential reference, **when** a worker constructs the
   client, **then** it MUST resolve the value from its injected environment and
   MUST fail explicitly when missing without logging the value.
