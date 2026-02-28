# Flowgent — Implementation Progress

**Date:** 2026-05-23
**Status:** Architecture docs consolidated. Gaps identified between docs and code.

---

## 1. Project Snapshot

### 1.1 Module & Structure

- **Module:** `github.com/flowgent-labs/flowgent` (Go 1.26)
- **Entry:** `src/cmd/flowgent/main.go` — 10 subcommands via Cobra
- **Config:** `etc/flowgent-dev.yaml` (dev), `etc/flowgent.yaml.fully.sample` (reference)
- **Deploy:** Helm chart at `deploy/helm/flowgent/` (6 microservices), Docker images at `deploy/docker/`

### 1.2 CLI Subcommands (10 total)

| Subcommand | Binary | Notes |
|------------|--------|-------|
| `all-in-one` | single process | API+JM+TM+Controller, SQLite+memory queue |
| `apiserver` | standalone | REST (9999) + A2A (9992) + mgmt (9991) |
| `a2a` | standalone | Google A2A protocol |
| `wallet` | standalone | x402 key management (9901) |
| `taskmanager` | standalone | SlotWorker pool, MQTT consumer |
| `jobmanager` | standalone | Control plane, run poller |
| `controller` | standalone | Sharded flow driver, K8s-aware |
| `notification` | standalone | WS push + multi-channel |
| `console` | interactive | REPL for store queries |
| `version` | info | Print build version |

**Doc bug:** `docs/01` section 15.1 documents `flowgent sandbox` CLI — **not implemented**.

---

## 2. Engine Core

### 2.1 Node Types & Executors

**Model definitions** (`src/model/`):

| NodeType (11) | TaskType (12) | Executor file | Wired in router? |
|--------------|---------------|---------------|:---:|
| `agent` | `agent` | `agent.go` | yes |
| `tool` | `tool` | `tool.go` | yes |
| `condition` | `condition` | `condition.go` | yes |
| `supervisor` | `supervisor` | `supervisor.go` | yes |
| `tribunal` | `tribunal` | `tribunal.go` | yes |
| `human` | `human` | `human.go` | yes |
| `map` | `map` | `map.go` | yes |
| `agentflow` | `subflow` | `subflow.go` | yes |
| `noop` | `noop` | `noop.go` | yes |
| `sandbox` | `sandbox` | `sandbox.go` | **NO** |
| `skill` | `skill` | `skill.go` | **NO** |
| — | `join` | `join.go` | yes |

**Gap 1:** `skill` and `sandbox` executors exist on disk but are **not registered** in the TaskManager router (`src/engine/taskmanager/taskmanager.go`). Any DAG node with `type: skill` or `type: sandbox` will fail at runtime.

**Gap 2:** `docs/01` section 6.1 says "10 Node Types" — missing `skill` and `join` from the table.

**Gap 3:** TaskType is `"subflow"` but NodeType is `"agentflow"` — naming inconsistency between model packages.

### 2.2 Scheduler / ResourceManager (2 implementations)

| Implementation | Location | Mode |
|---------------|----------|------|
| `LocalResourceManager` | `scheduler/local.go` | Goroutine pool + channel semaphore |
| `KubernetesResourceManager` | `scheduler/kubernetes.go` | MQTT dispatch + K8s Deployment auto-scale |

Factory `NewResourceManager()` falls back to local if K8s is unreachable. Matches docs.

### 2.3 Persistence

| Interface | SQLite | PostgreSQL | File |
|-----------|--------|------------|------|
| `Store` | `SQLiteStore` | `PostgresStore` | `store/sqlite.go`, `store/postgres.go` |
| `MemoryStore` | `SQLiteMemStore` | `PgMemStore` | `store/memory_sqlite.go`, `store/memory_postgres.go` |
| `Queue` | `MemoryQueue` | `MQTTQueue` | `queue/memory.go`, `queue/mqtt.go` |
| `ICache` | `MemoryCache` | `RedisCache` | `cache/memory.go`, `cache/redis.go` |
| `DistributedLock` | `MemoryLock` | `PostgresLock` + `RedisLock` | `lock/lock.go`, `lock/pg.go`, `lock/redis.go` |

Matches docs. Lock has 3 implementations (richer than documented).

### 2.4 Payments (`src/payments/`)

x402 economic layer: wallet key management, facilitator client, spending policies, payment approvals, receipts, PWF runtime. Matches `docs/02`.

---

## 3. Doc vs Code Discrepancies

### 3.1 High Priority (would break users)

| # | Issue | Where |
|---|-------|-------|
| 1 | `skill` executor not wired in router | `taskmanager.go:41-50` — missing `TaskSkill` and `TaskSandbox` cases |
| 2 | `sandbox` executor not wired in router | Same as above |
| 3 | `flowgent sandbox` CLI documented but never implemented | `docs/01` §15.1, `src/cmd/flowgent/main.go` has no sandbox subcommand |
| 4 | DeepSeek env var typo: `ANTHROPIC_AUTH_TOKEN` should be `DEEPSEEK_API_KEY` | `docs/20` line 22 |

### 3.2 Medium Priority (doc cleanup)

| # | Issue | Where |
|---|-------|-------|
| 5 | `docs/00` uses old terminology: `WorkflowSpec`, `WorkflowRun`, `workflow` node type, `vote` node type | ~20+ occurrences |
| 6 | `docs/01` §6.1 says "10 Node Types" but code has 12 | Table missing `skill` and `join` |
| 7 | `docs/01` §6.1 calls it `agentflow` executor; code file is `subflow.go`, TaskType is `"subflow"` | Naming drift |
| 8 | `docs/01` §16.2 references `etc/flows/`, `etc/agents/`, `etc/skills/` — these directories don't exist | Actual files are in `examples/` |
| 9 | `docs/20` references `etc/flows/` for flow YAMLs — doesn't exist | Should be `examples/flows/` |

### 3.3 Low Priority (stale references)

| # | Issue | Where |
|---|-------|-------|
| 10 | `docs/00` references `src/internal/`, `src/web/`, `src/worker/`, `src/util/` — never existed | Historical draft |
| 11 | `docs/02` references `src/cmd/core/wallet.go`, `src/cmd/core/main.go` — actual path is `src/cmd/flowgent/` | Renamed after doc write |
| 12 | `docs/02` references `deploy/docker-compose.all-in-one.yml` — doesn't exist | |
| 13 | `docs/20` references `deploy/Dockerfile.jobmanager` — doesn't exist | |
| 14 | `docs/20` references `src/store/store_agents.go` — doesn't exist | |

---

## 4. Doc Inventory (2026-05-23)

| Doc | Content | Status |
|-----|---------|--------|
| `00-Initial-Design-Draft.md` | Original human-authored spec (2026-05-10) | Historical — old terminology, keep as reference |
| `01-L1-Engine-Architecture.md` | Engine architecture (merged skills/sandbox/config §13-16) | Current — some gaps (see §3) |
| `02-L1-x402-Economic-Support.md` | x402 payment protocol support | Current — some stale paths |
| `04-DEPLOY-Build-Deps-Images.md` | Docker build guide for facilitator/anvil/solana | Current |
| `10-USE-CASES.md` | Use case catalog linking to examples/ configs | Current |
| `20-TEST-e2e-guide.md` | E2E test guide | Current — some stale paths |

---

## 5. Implemented Packages (full list)

```
src/
├── cmd/flowgent/       # CLI (main.go, launch.go, wallet.go, console.go)
├── api/                # REST handlers (agentflow, human, health, trigger, openapi)
├── engine/
│   ├── jobmanager/     # JobManager + JobMaster (DAG orchestrator)
│   ├── taskmanager/    # TaskManager + SlotWorker pool + router
│   ├── scheduler/      # LocalResourceManager + KubernetesResourceManager
│   ├── executor/       # 12 executor files (2 not wired)
│   └── discovery/      # IDiscoveryClient (K8s + static)
├── model/              # AgentFlowSpec, Node, Edge, Run, Task, Memory, Config
├── store/              # Store + MemoryStore (SQLite + PG for each)
├── queue/              # Queue (memory + MQTT)
├── cache/              # ICache (memory + Redis)
├── lock/               # DistributedLock (memory + PG + Redis)
├── config/             # Viper-based YAML config loader
├── llm/                # OpenAI-compatible HTTP adapter (rate limit, proxy, thinking)
├── common/
│   ├── tracing/        # OpenTelemetry setup
│   └── utils/          # Logger, schema validation, expression evaluator
├── notification/       # Sender interface + 5 channel implementations
└── payments/           # x402 economic layer (wallet, facilitator, policy, pwf, receipts, approvals, providers)
```

---

## 6. Test State

### Unit Tests
- `tests/e2e/` — E2E tests for engine flow (basic, supervisor, map, retry, DAG, condition, human, webhook, security fixer pipeline, MQTT)
- Engine unit tests in `src/engine/jobmanager/*_test.go`

### Build Status
- `go build ./src/...` — clean

---

## 7. Action Items (Priority Order)

1. **Wire `skill` and `sandbox` executors** into the TaskManager router, or remove them if intentionally deferred
2. **Drop or implement `flowgent sandbox` CLI**, align with docs
3. **Fix `docs/01` §6.1** — add `skill` and `join` to node type table; rename `agentflow` → `subflow` or vice versa for consistency
4. **Fix `docs/20`** — DeepSeek env var, stale file references (`etc/flows/` → `examples/flows/`)
5. **Fix `docs/02`** — stale paths (`src/cmd/core/` → `src/cmd/flowgent/`, docker-compose reference)
6. **Decide on `etc/` vs `examples/`** — if `etc/` is runtime config and `examples/` is sample configs, document the distinction clearly
