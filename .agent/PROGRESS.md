# Flowgent Layer 1 Engine — Implementation Progress

**Date:** 2026-05-08
**Status:** Core engine complete, cmd structure cleaned up, 9/9 tests passing, build clean.

## Latest Changes (2026-05-08)

### CMD Structure Cleanup
- **Merged `cmd/agent` into `cmd/server`** — A2A endpoints (agent card, task creation, status query) now live on the main server, eliminating ~60% code duplication. The `cmd/agent/` directory has been removed.
- **Added `cmd/mcp-server-nexus3/`** — New MCP stdio server for Sonatype Nexus Repository 3 with tools: `get_foss_solution`, `search_components`, `get_component_details`. Completes the MCP tool chain for the security-autonomy-fixer agentflow (github, sonarqube, sonatype-iq, sonatype-nexus3).
- **Updated `Makefile`** — Removed `build-agent` target, added `build-mcp-nexus3` target.
- **Fixed `etc/flowgent.yaml`** — Corrected `anonymous-paths` indentation, added A2A paths (`/.well-known/**`, `/a2a/**`) to anonymous list, added `test` MCP server config entry.

### Current CMD Layout (6 binaries)
```
cmd/
├── server/                  # Main server (API + A2A + cron + webhook + OTEL)
├── mcp-server-github/       # GitHub MCP (commit/branch/PR operations)
├── mcp-server-sonarqube/    # SonarQube MCP (SAST/DAST scanning)
├── mcp-server-sonatypeiq/   # Sonatype IQ MCP (FOSS dependency scanning)
├── mcp-server-nexus3/       # Sonatype Nexus3 MCP (artifact compliance)
└── mcp-server-test/         # Test MCP (Maven/Cucumber integration tests)
```

## Implemented Packages

### `src/internal/model/`
- `agentflow.go` — Node, Edge, AgentFlowSpec, AgentFlowDefinition, AgentFlowVersion, RetryPolicy, HumanApprovalConfig, SupervisorConfig
- `run.go` — AgentFlowRun (PENDING→RUNNING→COMPLETED/FAILED/PAUSED), TaskRun, HumanApproval
- `node.go` — NodeType constants: agent, tool, map, agentflow, condition, vote, human, supervisor, noop
- `config.go` — ServiceConfig, StorageConfig (was AppDBConfig), LLMConfig, OrchestrationConfig, AppConfig with GetAgent/GetMCP/GetFlow/GetModel
- `memory.go` — Memory, KnowledgeEntry, MemoryType (episodic, procedural, semantic)

### `src/internal/engine/`
- `scheduler.go` — DAG topological scheduler, Ready/Done/Skip/Fail/Inject/IsComplete/HasFailed, edge conditions via SetEdgeConditions/GetChildCondition
- `executor.go` — All 9 node types, OTEL tracing (executor.node, executor.supervisor spans), node.instruction + agent.instruction, max_nodes enforcement, allowed_actions validation
- `runtime.go` — AgentFlowRuntime orchestrating full DAG, edge condition routing, supervisor action handling (retry/redirect/inject/abort), idempotent replay
- `map_runner.go` — Map node with goroutine pool (configurable concurrency), OTEL tracing
- `retry.go` — Exponential backoff per-node retry
- `state_machine.go` — Run/task lifecycle state transitions
- `cron_scheduler.go` — RegisterAgentFlows with cron expressions
- `e2e_test.go` — 9 comprehensive tests

### `src/internal/store/`
- `common.go` — Store interface (AgentFlow definitions, runs, task runs, human approvals, idempotency, supervisor log)
- `sqlite.go` — Full Store impl with SQLite (agentflow_runs, task_runs, etc.)
- `postgres.go` — Full Store impl with PostgreSQL + pgx/v5
- `schema.sql` — PG DDL with agentflow_ prefix tables

### `src/internal/cache/`
- `cache.go` — ICache interface (Get/Set/Delete/Exists/Clear/Close)
- `memory.go` — MemoryCache with configurable capacity and TTL
- `redis.go` — RedisCache using go-redis/v9, auto-detects standalone/cluster/sentinel mode

### `src/internal/queue/`
- `queue.go` — Queue interface with Dequeue (consumer-group subscribe semantics)
- `memory.go` — In-memory Queue with fan-out to all consumer groups
- `mqtt.go` — MQTT Queue using Eclipse Paho (EMQX/Mosquitto), per-group topic subscribe

### `src/internal/memory/`
- `store.go` — Store interface for RAG memories + knowledge base with vector search
- `sqlite_store.go` — SQLite with JSON embedding storage + cosine similarity topK
- `pg_store.go` — PG with JSONB embedding storage + cosine similarity topK
- `helpers.go` — Builder pattern for Memory/KnowledgeEntry, StoreAgentFlowMemory, StoreProceduralMemory

### `src/internal/worker/`
- `worker.go` — Distributed worker: dequeue→execute node→publish result. Pool for concurrent workers.

### `src/internal/llm/`
- `llm.go` — OpenAI-compatible HTTP adapter, per-provider rate limiting, proxy support, modalities, thinking config

### `src/internal/mcp/`
- `mcp.go` — MCP stdio client factory (fixed command arg construction: allArgs = command[1:] + args)

### `src/internal/config/`
- `config.go` — Load/RelaodAgentFlows, BuildAppConfig, hot-reload support

### `src/internal/api/`
- `agentflow.go` — AgentFlow CRUD + trigger endpoints
- `human.go` — Human approval (approve/reject by token)
- `health.go` — Healthz endpoint
- `trigger.go` — Webhook dispatcher (GitHub/GitLab provider+event matching)
- `openapi.go` — OpenAPI 3.1 spec + Swagger UI

### `src/cmd/server/`
- `main.go` — Full wiring: store, otel, cache, mcp, llm, executor, routes, cron, poller, workers, pprof, auth middleware, hot-reload
- `otel.go` — OTel tracer + meter provider (AlwaysSample), endpoint/timeout from config

### `src/cmd/agent/`
- `main.go` — A2A agent server with agent card, task API, run status query

## Config Files (etc/)

- `flowgent.yaml` — L1 system config (storage, otel, llm, orchestration, cache, auth)
- `sample-security-autonomy-fixer.yaml` — L2 security fix agentflow (11 phases)
- `sample-autotest-generation.yaml` — L2 auto test generation agentflow (10 phases, new)
- `sub-fix.yaml` — L2 sub-agentflow (flat format, corrected from workflow: wrapper)

## Test Results (9/9 PASS)

```
TestE2E_BasicAgentFlow           — 6 nodes complete chain
TestE2E_SupervisorAllowedActions — invalid action correctly rejected
TestE2E_MapNodeExecution         — fan-out with concurrency
TestE2E_NodeRetry                — retry after transient failure
TestE2E_DAGScheduler             — topological ordering
TestE2E_DAGInject                — dynamic node injection
TestE2E_ConditionSkip            — condition-based path skipping
TestE2E_HumanApprovalOutput      — on_approve/on_reject routing tokens
TestE2E_WebhookTriggerMatch      — provider/event filtering
```

## Terminology Compliance (Req 14)

- All Go types: AgentFlowRun, AgentFlowSpec, AgentFlowRuntime, etc.
- All DB tables: agentflow_runs, agentflow_definitions, task_runs.agentflow_run_id
- All API paths: /api/v1/agentflows, /api/v1/agentflows/trigger
- All YAML keys: agentflows, agentflow_id, type: agentflow, agentflow: sub/...
- All config sections: storage (was appdb), orchestration.agentflows
- Zero "workflow" references in Go code or YAML configs

## Remaining Gaps (Non-blocking)

1. Nested JSONPath (`${a.b.c}`) — current single-level sufficient for L2 YAMLs
2. Native pgvector `<->` operator — JSONB+Go cosine similarity usable for now
3. PG queue (`pgqueue.go`) — memory and MQTT queues cover local and distributed modes

## Next Steps

- Wire PG vector `<->` operator for production-scale memory search
- Add MQTT queue e2e test against running EMQX broker
- Run security-autonomy-fixer against real MCP servers (SonarQube, Sonatype IQ, Nexus3)
- Run autotest-generation against real Confluence + GitHub
