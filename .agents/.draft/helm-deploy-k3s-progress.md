# Flowgent — Implementation Progress

**Date:** 2026-05-25
**Status:** Helm chart with embedded PG/EMQX/Redis. K3s DNS fixed via Alibaba Cloud mirror. Global rename complete (notifier, resourcemanager, sandboxrunner).

---

## 0. Image Mirroring — Alibaba Cloud Registry Transfer

Docker Hub is blocked in this environment. All images must be mirrored via
`ssh root@43.98.165.146` (a jump box with Docker Hub access and Alibaba Cloud
registry credentials).

### Naming Convention

Alibaba Cloud Container Registry free tier supports **2-level** paths:

```
registry.cn-shenzhen.aliyuncs.com/{namespace}/{underscore_name}:{tag}
```

- `{namespace}`: `wl4g` (flowgent infra) or `wl4g-k8s` (K8s addons)
- `{underscore_name}`: use underscores for path separators, e.g. `rancher/mirrored-coredns-coredns:1.14.2` → `rancher_mirrored_coredns_coredns:1.14.2`

### Mirroring Process

```bash
# Step 1: Pull from Docker Hub on the jump box
ssh root@43.98.165.146 "docker pull rancher/mirrored-coredns-coredns:1.14.2"

# Step 2: Tag with Alibaba Cloud naming
ssh root@43.98.165.146 "docker tag rancher/mirrored-coredns-coredns:1.14.2 \
  registry.cn-shenzhen.aliyuncs.com/wl4g/rancher_mirrored_coredns_coredns:1.14.2"

# Step 3: Push to Alibaba Cloud
ssh root@43.98.165.146 "docker push registry.cn-shenzhen.aliyuncs.com/wl4g/rancher_mirrored_coredns_coredns:1.14.2"

# Step 4: Use the Alibaba Cloud image in K3s
kubectl set image deploy/coredns -n kube-system \
  coredns=registry.cn-shenzhen.aliyuncs.com/wl4g/rancher_mirrored_coredns_coredns:1.14.2
```

### Mirrored Images Registry

| Original (docker.io) | Alibaba Cloud Mirror |
|-----------------------|---------------------|
| `rancher/mirrored-coredns-coredns:1.14.2` | `wl4g/rancher_mirrored_coredns_coredns:1.14.2` |
| `rancher/local-path-provisioner:v0.0.35` | `wl4g/rancher_local_path_provisioner:v0.0.35` |
| `rancher/mirrored-metrics-server:v0.8.1` | `wl4g/rancher_mirrored_metrics_server:v0.8.1` |
| `bitnami/postgresql:18.3` | `wl4g/bitnami_postgresql:18.3` |
| `emqx/emqx:5.5.0-elixir` | `wl4g/emqx_emqx:5.5.0-elixir-amd64` |
| `bitnami/redis-cluster:7.0.14` | `wl4g-k8s/bitnami_redis-cluster:7.0.14` |
| `alpine:3.21` | `wl4g/alpine:3.21` |
| flowgent (local build) | `wl4g/flowgent:latest` |

### Flowgent Image Build & Push

```bash
CGO_ENABLED=0 go build -o bin/flowgent ./src/cmd/flowgent/
sudo podman build -t registry.cn-shenzhen.aliyuncs.com/wl4g/flowgent:latest -f deploy/docker/Dockerfile .
sudo podman save registry.cn-shenzhen.aliyuncs.com/wl4g/flowgent:latest | \
  ssh root@43.98.165.146 "docker load && docker push registry.cn-shenzhen.aliyuncs.com/wl4g/flowgent:latest"
```

---

## 1. Project Snapshot

### 1.1 Module & Structure

- **Module:** `github.com/flowgent-labs/flowgent` (Go 1.26)
- **Entry:** `src/cmd/flowgent/main.go` — 11 subcommands via Cobra
- **Config:** `etc/flowgent.yaml` (annotated reference)
- **Deploy:** Helm chart at `deploy/helm/flowgent/` (7 microservices incl. sandbox), Docker images at `deploy/docker/`

### 1.2 CLI Subcommands (11 total)

| Subcommand | Binary | Notes |
|------------|--------|-------|
| `all-in-one` | single process | API+JM+TM+Controller+Sandbox, SQLite+memory queue |
| `apiserver` | standalone | REST (9999) + A2A (9992) + mgmt (9991) |
| `a2a` | standalone | Google A2A protocol |
| `wallet` | standalone | x402 key management (9901) |
| `taskmanager` | standalone | SlotWorker pool, MQTT consumer |
| `jobmanager` | standalone | Control plane, run poller |
| `controller` | standalone | Sharded flow driver, K8s-aware |
| `notification` | standalone | WS push + multi-channel |
| **`sandbox`** | **standalone** | **Secure script execution (python3/bash/node) with network isolation** |
| `console` | interactive | REPL for store queries |
| `version` | info | Print build version |
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
| `committee` | `committee` | `committee.go` | yes |
| `human` | `human` | `human.go` | yes |
| `map` | `map` | `map.go` | yes |
| `agentflow` | `subflow` | `subflow.go` | yes |
| `noop` | `noop` | `noop.go` | yes |
| `sandbox` | `sandbox` | `sandbox.go` | yes (2026-05-23) |
| `skill` | `skill` | `skill.go` | yes (2026-05-23) |
| — | `join` | `join.go` | yes |

**Gap 1 (FIXED):** `skill` and `sandbox` executors now registered in TM router.

**Gap 2 (FIXED):** `docs/01` section 6.1 updated to "12 Node Types".

**Gap 3 (OPEN):** TaskType is `"subflow"` but NodeType is `"agentflow"` — naming inconsistency.

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

### 2.4 Wallet / x402 (`pkg/core/pkg/client`, `pkg/wallet/pkg`)

x402 economic layer: TM-side 402 parsing/policy/facilitator flow in `pkg/core/pkg/client`, with wallet key management and MQTT signing in `pkg/wallet/pkg`. Matches `docs/02`.

### 2.5 Sandbox (`src/engine/sandbox/`) — NEW 2026-05-23

| File | Purpose |
|------|---------|
| `worker.go` | SandboxWorker — consumes plans from queue, executes in isolated env |
| `inline.go` | InlineWorker — synchronous execution for all-in-one mode |

**Security model:** 3-level policy override (global → flow → node):
- Network: `none` (default), `allowlist`, `denylist`
- Runtime allowlists (python3, bash, node)
- Banned command patterns (curl, wget, etc.)
- Resource limits (CPU, memory, timeout)

**Execution modes:**
- Docker: `docker run --rm --network=none --memory=X --cpus=Y`
- Inline (all-in-one): subprocess with env isolation

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
| 8 | `docs/01` §16.2 references `etc/flows/`, `etc/agents/`, `etc/skills/` — these directories don't exist | Actual files are in `use-cases/` |
| 9 | `docs/20` references `etc/flows/` for flow YAMLs — doesn't exist | Should be `use-cases/flows/` |

### 3.3 Low Priority (stale references)

| # | Issue | Where |
|---|-------|-------|
| 10 | `docs/00` references `src/internal/`, `src/web/`, `src/worker/`, `src/util/` — never existed | Historical draft |
| 11 | `docs/20` references `deploy/Dockerfile.jobmanager` — doesn't exist | |
| 12 | `docs/20` references `src/store/store_agents.go` — doesn't exist | |

---

## 4. Doc Inventory (2026-05-23)

| Doc | Content | Status |
|-----|---------|--------|
| `00-Initial-Design-Draft.md` | Original human-authored spec (2026-05-10) | Historical — old terminology, keep as reference |
| `01-L1-Engine-Architecture.md` | Engine architecture (merged skills/sandbox/config §13-16) | Current — some gaps (see §3) |
| `02-L1-x402-Economic-Support.md` | x402 payment protocol support | Current — aligned to `pkg/core/pkg/client` + `pkg/wallet/pkg` |
| `04-DEPLOY-Build-Deps-Images.md` | Docker build guide for facilitator/anvil/solana | Current |
| `10-USE-CASES.md` | Use case catalog linking to use-cases/ configs | Current |
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
4. **Fix `docs/20`** — DeepSeek env var, stale file references (`etc/flows/` → `use-cases/flows/`)
5. **Decide on `etc/` vs `use-cases/`** — if `etc/` is runtime config and `use-cases/` is sample configs, document the distinction clearly
