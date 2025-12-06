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

## Economic Layer (Phase 1) — 2026-05-09

### New Packages: `src/payments/`

**Core types** (`src/payments/model.go`):
- `X402PaymentRequest` — parsed x402 HTTP 402 response with `Validate()`
- `PaymentIntent` — intent created before authorization (PENDING→APPROVED→PAID)
- `PaymentReceipt` — returned by facilitator after settlement
- `PaymentAuthorization` — signed auth sent to facilitator
- `PaymentError` — structured error type (PAYMENT_DENIED, PAYMENT_REQUIRES_APPROVAL, etc.)

**Configuration** (`src/payments/config.go`):
- `PaymentsConfig` — top-level enable/disable toggle
- `PoliciesConfig` — spending limits, domain allowlist/blocklist, approval thresholds
- `WalletConfig` — wallet service endpoint and auth
- `SecretStoreConfig` — provider selection (default/vault), master key resolution
- `X402Config` — default facilitator, timeout, max retries

**x402 Parser** (`src/payments/x402/`):
- `Parse()` — reads `X402-Payment` header from 402 response, unmarshals JSON
- `IsX402Response()` — detects x402 payment responses
- `SetAuthorizationHeader()` — attaches `X402-Authorization` token for retry

**Spending Policy Engine** (`src/payments/policy/`):
- `Engine.Allow()` — evaluates asset/chain allowlists, max single payment, domain rules, daily budget
- `Engine.RequiresHumanApproval()` — checks against `require_human_approval_above_usd` threshold
- `Engine.RecordSpend()` — accumulates daily spend for budget enforcement
- Domain matching with wildcard support (`*.example.com`)

**Wallet Abstraction** (`src/payments/wallet/`):
- `Wallet` interface — `Address()`, `SignAuthorization()`, `Balance()`
- `Manager` — multi-wallet management, default wallet selection
- `SignPaymentAuthorization()` — creates signed `PaymentAuthorization` for facilitator

**Secret Store** (`src/payments/secretstore.go`, `src/payments/providers/`):
- `SecretStoreProvider` interface — `GetSecret`, `PutSecret`, `DeleteSecret`, `ListSecrets`
- `DefaultSecretStoreProvider` — AES-256-GCM encryption, DB-backed (SQLite/Postgres), master key from ENV/file
- `VaultSecretStoreProvider` — Hashicorp Vault contract (SDK wiring at construction time)

**PayableWebFetch Runtime** (`src/payments/pwf/`):
- `Runtime.Fetch()` — full x402 payment flow: detect 402 → parse → create intent → evaluate policy → optional human approval → sign → facilitator authorize → retry
- Implements the core economic-aware fetch primitive (NOT just a tool wrapper)

**Facilitator Client** (`src/payments/facilitator/`):
- `Client.Authorize()` — POSTs signed `PaymentAuthorization` to Coinbase x402 facilitator
- `Client.Health()` — facilitator health check

**Payment Approvals** (`src/payments/approvals/`):
- `PaymentApprover` — implements `pwf.ApprovalHandler` reusing existing `HumanApproval` store
- Does NOT create another approval subsystem

**Payment Receipts** (`src/payments/receipts/`):
- `Store` — persists receipts to DB, query by intent ID or date range

### New CMD: `cmd/flowgent-wallet/`

- Standalone daemon for secure key management and Ed25519 signing
- Exposes REST API: `/health`, `/api/v1/wallet/sign`, `/api/v1/wallet/address`, `/api/v1/wallet/balance`
- Private keys stored encrypted via `DefaultSecretStoreProvider` (AES-256-GCM)
- `--generate-key` flag for Ed25519 keypair generation
- Flowgent runtime NEVER directly stores raw private keys

### Config Integration

- `ServiceConfig` now includes optional `Payments *payments.PaymentsConfig` field
- When `payments.enabled: false` (or nil), all payment features are no-ops
- Flowgent remains usable as pure orchestration engine without payments

### Deploy Updates

- `deploy/facilitator/docker-compose.yml` — standalone Coinbase x402 facilitator
- `deploy/docker-compose.all-in-one.yml` — full stack: flowgent + wallet + facilitator + optional postgres/emqx
- `deploy/kubernetes/wallet-deployment.yaml` — wallet Deployment + Service + Secret
- `deploy/kubernetes/facilitator-deployment.yaml` — facilitator Deployment + Service
- `deploy/kubernetes/flowgent-deployment.yaml` — apiserver + worker + a2a Deployments

### Makefile

- `build-wallet` target for `bin/flowgent-wallet`
- `build` now includes `build-wallet`

### Dependencies Added

- `github.com/shopspring/decimal` v1.4.0 — fixed-point decimal for payment amounts

### Design Boundaries Enforced

- Flowgent is consumer-side economic runtime ONLY — NO settlement/facilitator/bridging/escrow
- Settlement delegated to Coinbase x402 facilitator
- Economic layer isolated from orchestration runtime (`src/payments/` tree)
- Wallet subsystem isolated (`cmd/flowgent-wallet/` daemon)
- Human approval reuses existing `model.HumanApproval` store — no duplicate subsystem

### Test Results (8/8 PASS, payments packages need dedicated tests)

```
ok  api      0.014s
ok  cache    5.034s
ok  config   0.008s
ok  engine   0.015s
ok  model    0.006s
ok  queue    0.423s
ok  store    0.107s
ok  util     0.005s
```

### Build Status

- All packages compile clean: `go build ./src/...` passes
- `bin/flowgent-wallet` builds successfully

## Next Steps

- Wire PG vector `<->` operator for production-scale memory search
- Add MQTT queue e2e test against running EMQX broker
- Run security-autonomy-fixer against real MCP servers (SonarQube, Sonatype IQ, Nexus3)
- Run autotest-generation against real Confluence + GitHub
- Add dedicated unit tests for payments packages (x402, policy, pwf, wallet, facilitator)
- Wire Vault SDK in `VaultSecretStoreProvider` for production deployments
- Integrate PWF runtime into engine tool node type as optional payment-aware fetch
