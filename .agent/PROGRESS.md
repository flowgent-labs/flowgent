# Flowgent Layer 1 Engine — Implementation Progress

**Date:** 2026-05-11
**Status:** Engine refactored to Flink-aligned architecture, 7/7 unit tests passing, build clean.

## Latest Changes (2026-05-11)

### Engine Architecture Refactoring (Flink-Aligned)

**Problem:** `DAGScheduler` was a passive state container (just maps + locks), `runtime.go` was too fat (did DAG loop + scope + supervisor handling), and `worker.go` wrapped a full runtime to execute a single node (code smell).

**Solution:** Replaced the three-way split with a clean JobManager / Scheduler / TaskManager hierarchy inspired by Apache Flink.

**New file structure:**

```
src/engine/
├── jobmanager.go          — JobManager (DAG orchestrator, replaces DAGScheduler + runtime)
├── taskmanager.go         — TaskManager (node executor, renamed from Executor)
├── scheduler.go           — Scheduler interface + TaskSubmit/TaskResult types
├── standalone_scheduler.go — Goroutine-pool scheduler (dev/test/all-in-one)
├── k8s_scheduler.go       — Kubernetes scheduler stub (production distributed)
├── map_runner.go          — MapRunner (updated refs)
├── testing.go             — Test helpers (updated refs)
├── state_machine.go       — (unchanged)
└── retry.go               — (unchanged)
```

**Deleted files:**
- `src/engine/dag_scheduler.go` + `dag_scheduler_test.go` — merged into jobmanager.go
- `src/engine/runtime.go` — merged into jobmanager.go
- `src/engine/executor.go` — renamed to taskmanager.go
- `src/worker/` — whole package deleted (absorbed by StandaloneScheduler)

**Updated files:**
- `src/engine/map_runner.go` — references TaskManager instead of Executor
- `src/engine/testing.go` — NewTestJobManager replaces NewTestRuntime
- `src/cmd/core/run.go` — uses JobManager + StandaloneScheduler, removed worker.Pool
- `tests/e2e/engine_e2e_test.go` — uses NewTestJobManager
- `tests/e2e/local_e2e_test.go` — uses JobManager directly
- `.agent/` — 01-ENGINE-IMPL-DESIGN.md added, existing files renumbered

### JobManager Responsibilities
- Build DAG execution graph from AgentFlowSpec
- Drive topological execution loop (Ready → SubmitTask → Collect → Done)
- Dispatch tasks via Scheduler (not directly to TaskManager)
- Handle condition routing, supervisor actions (retry/redirect/inject/abort)
- Generate ClusterID per job for resource tracking

### Scheduler Implementations
- **StandaloneScheduler**: goroutine pool with semaphore, calls TaskManager.ExecuteNode
- **K8sScheduler**: stub for production distributed mode (launches K8s pod per task)

### TaskManager Responsibilities
- Stateless node executor, no DAG awareness
- Execute any node type (agent/tool/condition/tribunal/human/supervisor/noop)
- Single public method: `ExecuteNode(ctx, task, node, scope) error`

---

## Implemented Packages

### `src/engine/`
- `jobmanager.go` — JobManager DAG orchestrator (state + execution loop + scheduling)
- `taskmanager.go` — TaskManager node executor
- `scheduler.go` — Scheduler interface (Standalone/K8s)
- `standalone_scheduler.go` — goroutine pool implementation
- `k8s_scheduler.go` — Kubernetes stub
- `map_runner.go` — Map node with goroutine pool, OTEL tracing
- `retry.go` — Exponential backoff per-node retry
- `state_machine.go` — Run/task lifecycle state transitions
- `cron_scheduler.go` — Register flows with cron expressions
- `testing.go` — MockStore, test helpers (NewTestTaskManager, NewTestJobManager)

### `src/model/`
- `agentflow.go` — Node, Edge, AgentFlowSpec, SupervisorConfig
- `run.go` — AgentFlowRun, TaskRun, HumanApproval
- `node.go` — NodeType constants
- `config.go` — ServiceConfig, LLMConfig, OrchestrationConfig
- `memory.go` — RAG memory types

### `src/store/`
- `common.go` — Store interface
- `sqlite.go` — SQLite implementation
- `postgres.go` — PostgreSQL implementation (pgx/v5)
- `schema.sql` — PG DDL with agentflow_ prefix tables

### `src/api/`
- `agentflow.go` — AgentFlow CRUD + trigger endpoints
- `human.go` — Human approval (approve/reject by token)
- `health.go` — Healthz endpoint
- `trigger.go` — Webhook dispatcher (GitHub/GitLab)
- `openapi.go` — OpenAPI 3.1 spec + Swagger UI

### `src/queue/`
- `queue.go` — Queue interface
- `memory.go` — In-memory queue
- `mqtt.go` — MQTT queue (Eclipse Paho / EMQX)

### `src/memory/`
- `store.go` — Store interface for RAG memories + vector search
- `sqlite_store.go` — SQLite with JSON embedding + cosine similarity
- `pg_store.go` — PG with JSONB embedding + cosine similarity

### `src/config/`
- `config.go` — Load/ReloadAgentFlows, BuildAppConfig, hot-reload

### `src/llm/`
- `llm.go` — OpenAI-compatible HTTP adapter, rate limiting, proxy, thinking

### `src/mcp/`
- `mcp.go` — MCP stdio client factory

### `src/payments/` (isolated economic layer)
- x402 payload parsing, spending policy engine, wallet abstraction
- PayableWebFetch runtime, facilitator client, payment receipts
- Encrypted secret store (AES-256-GCM), vault provider contract

### `src/cmd/`
- `cmd/core/` — Main server binary (API + cron + webhook + OTEL)
- `cmd/mcp-server-github/` — GitHub MCP stdio server
- `cmd/mcp-server-sonarqube/` — SonarQube MCP stdio server
- `cmd/mcp-server-sonatypeiq/` — Sonatype IQ MCP stdio server
- `cmd/mcp-server-nexus3/` — Sonatype Nexus3 MCP stdio server
- `cmd/mcp-server-test/` — Test MCP (Maven/Cucumber integration tests)

---

## Test Results

### Engine Unit Tests (7/7 PASS)
```
TestJobManager_BasicTopology      — linear A→B→C
TestJobManager_ParallelReady      — fan-out A→B, A→C
TestJobManager_Skip               — skip node cascade
TestJobManager_Fail               — failure detection
TestJobManager_Inject             — dynamic node injection
TestJobManager_EdgeCondition      — edge condition storage
TestJobManager_ConditionResult    — condition set/get
TestRetryWithBackoff_Success      — retry 4 tests
TestStateMachine_ValidTransition  — state machine 3 tests
```

### E2E Tests (9 total)
```
TestE2E_BasicAgentFlow            — 6 node complete chain
TestE2E_SupervisorAllowedActions  — invalid action rejected
TestE2E_MapNodeExecution          — fan-out with concurrency
TestE2E_NodeRetry                 — retry after transient failure
TestE2E_DAGExecutor               — topological ordering
TestE2E_DAGInject                 — dynamic node injection
TestE2E_ConditionSkip             — condition-based path skipping
TestE2E_HumanApprovalOutput       — on_approve/on_reject routing tokens
TestE2E_WebhookTriggerMatch       — provider/event filtering
TestE2E_SecurityFixPipeline_Local — 13 node full pipeline
TestE2E_MQTTQueue_Local           — MQTT broker integration
```

## Build Status
- `go build ./src/engine/...` ✅
- `go build ./src/cmd/core` ✅
- All core engine packages compile clean

## Remaining Gaps
1. **K8sScheduler** — implement Kubernetes client to launch TaskManager pods
2. **PWF integration** — pay-per-call tool node type (x402)
3. **PG queue** — pgqueue.go exists but not wired; memory/MQTT cover local/distributed
4. **Native pgvector** — JSONB + Go cosine similarity works; pgvector `<->` operator for scale
5. **Vault SDK** — VaultSecretStoreProvider contract defined, SDK wiring deferred
6. **Payments unit tests** — x402, policy, pwf, wallet, facilitator need dedicated tests
