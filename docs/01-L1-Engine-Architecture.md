# Flowgent Distributed Engine Architecture

**Date:** 2026-05-22
**Status:** Implemented — three-phase architecture (Design→Schedule→Execute), E2E verified on k3s

---

## 1. Architecture Overview

Flowgent is a distributed multi-tenant AI agent orchestration engine. The data flow
spans three phases: **Design/Trigger** (sync — users author flows via UI/API/A2A,
persisted to PG), **Scheduling** (async — Controller polls definitions, generates
runs, launches JM pods for Application mode), and **Execution** (async — JM parses
the flow JSON into a DAG, creates one ExecutionPlan per node in PG, and dispatches
to TM pods via the ResourceManager).

```
═══════════════════════════════════════════════════════════════════════════
PHASE 1 — Flow Design & Trigger (Sync)
═══════════════════════════════════════════════════════════════════════════

  AI App Developers                    External Systems
  (Flowgent UI, design flows)          (REST API / A2A / Webhook)
       │                                        │
       └────────────────┬───────────────────────┘
                        │ Flow/Agent CRUD + Trigger
                        ▼
               ┌──────────────────┐
               │   API Server     │  Multi-Tenant Gateway
               │  Auth · Rate     │  Trigger → INSERT PENDING run
               │  Limit · Tenant  │
               └────────┬─────────┘
                        │ INSERT / UPDATE
                        ▼
               ┌──────────────────────────────────────────┐
               │     Store (PostgreSQL / SQLite)      │
               │                                      │
               │  agentflow_definition (flow spec)    │
               │    └── agentflow_run (1:N)           │
               │          └── execution_plan (1:N)    │
               │                one plan per DAG node │
               └──────────┬───────────┬───────────────┘
                          │           │
═════════════════════════════         │
PHASE 2 — Scheduling (Async)         │
═════════════════════════════         │
                          │           │
      poll agentflow_definitions      │
      (every 10s, hash-mod shard)     │
                          │           │
                          ▼           │
               ┌──────────────────────────────┐
               │  Controller (N sharded pods) │
               │  hash(flow_id) % N → owner   │
               │                              │
               │  Session mode:               │
               │    INSERT agentflow_runs     │
               │    (PENDING, namespace="")   │
               │                              │
               │  Application mode:           │
               │    Create K8s JM Deployment  │
               │    + INSERT agentflow_runs   │
               │    (PENDING, namespace={ns}) │
               └──────────────┬───────────────┘
                              │
                              │ INSERT agentflow_runs (PENDING)
                              │
══════════════════════════════╪══════════════════════════════════
PHASE 3 — Execution (Async)   │
══════════════════════════════╪══════════════════════════════════
                              │
                              │  poll agentflow_runs (every 2s)
                              │  namespace-filtered
                              ▼
               ┌─────────────────────────────────────────────┐
               │         JobManager Pod(s)                   │
               │                                             │
               │  1. Parse AgentFlowSpec JSON                │
               │     → Build DAG from Nodes + Edges          │
               │  2. Create one ExecutionPlan per node       │
               │     → Save each ExecutionPlan to PG         │
               │  3. For each ready node (topo order):       │
               │     rm.Schedule(plan)                       │
               └──────────────────┬──────────────────────────┘
                                  │ Schedule(plan)
                                  ▼
               ┌─────────────────────────────────────────────┐
               │          ResourceManager                    │
               │                                             │
               │  Evaluate pending plans vs free slots:      │
               │                                             │
               │  LocalRM (all-in-one):                      │
               │    In-process goroutine pool                │
               │    INSUFFICIENT_RESOURCES if all slots busy │
               │                                             │
               │  K8sRM (production):                        │
               │    MQTT dispatch + manage TM Deployment     │
               │    Auto-scale TM replicas by queue depth    │
               │    (ensure enough pods for pending plans)   │
               └──────────────────┬──────────────────────────┘
                                  │ dispatch ExecutionPlan
                                  ▼
               ┌─────────────────────────────────────────────┐
               │        TaskManager Pods (K8s Deployment)    │
               │                                             │
               │  ┌──────────┐  ┌──────────┐  ┌──────────┐  │
               │  │ TM Pod 1 │  │ TM Pod 2 │  │ TM Pod N │  │
               │  │ Slot 1   │  │ Slot 1   │  │ Slot 1   │  │
               │  │ Slot 2   │  │ Slot 2   │  │ Slot 2   │  │
               │  │ Slot 3   │  │ Slot 3   │  │ Slot 3   │  │
               │  │ Slot 4   │  │ Slot 4   │  │ Slot 4   │  │
               │  └──────────┘  └──────────┘  └──────────┘  │
               │                                             │
               │  1 Slot = 1 ExecutionPlan = 1 DAG Node     │
               │  1 ExecutionPlan ∈ 1 AgentFlowRun          │
               │  1 AgentFlowRun ∈ 1 AgentFlowDefinition    │
               │                                             │
               │  ExecutorRouter:                            │
               │  agent | tool | supervisor | human          │
               │  tribunal | condition | map | join         │
               │  sandbox | skill | agentflow | noop  | ...│
               │                                             │
               │  Result → MQTT/Channel → back to JM         │
               └─────────────────────────────────────────────┘
```

### 1.1 Session vs Application — The Only Difference Is JM Lifecycle

Both modes use the **same binary, same poller, same DAG execution logic**.
The sole architectural difference is **who starts the JM and when**:

| | Session | Application |
|---|---|---|
| **JM started by** | Helm / Admin (platform init) | Controller (on flow discovery) |
| **JM naming** | `flowgent-jobmanager-{tenantId}-{hash}` | `flowgent-jm-{tenantId}-{flowId}-{runId}-{hash}` |
| **JM lifecycle** | Persistent, shared across tenants | Per-flow, destroyed on completion |
| **TM scale** | **Manual** (admin-managed capacity) | **Auto** (JM's K8s RM scales TMs) |
| **Slot exhaustion** | Run stays PENDING, admin adds TMs | JM auto-scales TM replicas |
| **Resource isolation** | Logical (tenant_id + rate limit) | Physical (dedicated K8s namespace) |
| **Flink analogy** | Session Cluster | Application Cluster |

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
Pod names carry `tenant_id` + `agentflow_id` + `agentflow_run_id` for observability:

```
Session (shared pool, {hash}=K8s suffix):
  flowgent-{component}-{tenantId}-{hash}

Application (dedicated per-run, {hash}=K8s suffix):
  flowgent-jm-{tenantId}-{flowId}-{runId}-{hash}
  flowgent-tm-{tenantId}-{flowId}-{runId}-{hash}
  flowgent-sandbox-{tenantId}-{flowId}-{runId}-{hash}
```

**Why `agentflow_run_id` not `{hash}` for Application pods?** Each application-mode run
spawns a dedicated JM+TM cluster. The run ID uniquely identifies the pod — no need
for a random suffix. Session pods use a K8s `{hash}` because they are shared across
many runs and scaled via Helm/Dynamic.

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
| **Controller uses PG shard-scan, not K8s CRD watch** | Flow catalog lives in PG (transactional, no CRD complexity); hash-mod sharding (Apache ShardingSphere pattern) scales horizontally without leader election |
| **Session TM admin-managed, Application TM auto-scale** | Economic boundary: shared = fixed capacity (admin controls cost), dedicated = elastic (VIP isolation) |
| **Agent memory scoped by (flow_id, node_id), not run_id** | Persists across restarts; no cross-flow knowledge sharing (KISS); content accumulates monotonically for RAG-style recall |
| **UI → PG → Controller (three-phase async)** | Decouples authoring from execution; Controller is the only component that writes runs; JM is the only component that executes them |
| **JM unification: same binary, same poller, same DAG for both modes** | `FLOWGENT_NAMESPACE` is the only variable; avoids code duplication, bugs fix uniformly |
| **A2A uses `a2aproject/a2a-go` types directly, not ADK's `adka2a` wrapper** | ADK's A2A server binds to `session.Session`, `genai.Content`, and ADK internal types — all incompatible with Flowgent's DAG orchestration model. The official `a2aproject/a2a-go` SDK provides clean protocol types (`AgentCard`, `Task`, `Message`) without opinionated framework coupling |

---

## 2. API Server — Multi-Tenant Gateway

A **separate, always-on component** distinct from JobManager. Rationale:
1. **Multi-tenancy**: Auth (JWT/OIDC/GitHub OAuth), rate limiting, tenant routing
   BEFORE execution — JM is not burdened with auth concerns
2. **Webhook ingress**: Single stable endpoint for GitHub/GitLab → validated, routed
3. **A2A protocol**: External AI agents discover Flowgent via `/.well-known/agent.json`
4. **Scale independence**: API Server scales independently (stateless, 2+ replicas);
   JM is stateful (leader-elected)

### 2.1 REST API (Port 9999)

All CRUD paths tenant-scoped via `{tenant}` in URL path. Webhook/human-approval
paths are global (token-based).

| Route | Method | Description |
|-------|--------|-------------|
| `/_/healthz` | GET | Health check |
| `/_/webhooks/{provider}` | POST | Webhook trigger (GitHub/GitLab) |
| `/api/v1/{tenant}/agents` | GET/POST | List / Create agent definitions |
| `/api/v1/{tenant}/agents/{name}` | GET/PUT/DELETE | Agent CRUD |
| `/api/v1/{tenant}/agentflows` | GET/POST | List / Create flow definitions |
| `/api/v1/{tenant}/agentflows/{id}` | GET/PUT/DELETE | Flow CRUD |
| `/api/v1/{tenant}/agentflows/trigger` | POST | Trigger run by `agentflow_id` in body |
| `/api/v1/{tenant}/runs` | GET | List runs (tenant-scoped) |
| `/api/v1/{tenant}/runs/{id}` | GET/DELETE | Get / Delete run |
| `/api/v1/{tenant}/runs/{id}/cancel` | POST | Cancel a running run |
| `/api/v1/{tenant}/runs/{id}/tasks` | GET | List tasks for a run |
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

## 3. JobManager — Control Plane

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
    result := rm.Schedule(ctx, plan)      // dispatch to TM (MQTT or local)
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

## 4. Controller — Distributed Flow Driver (≈ Flink Operator)

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

### 5.1 LocalResourceManager (all-in-one mode)

Bounded in-process goroutine pool using a channel semaphore (`make(chan struct{},
poolSize)`). `Schedule()` acquires a slot via non-blocking select — if all slots are
busy, returns `INSUFFICIENT_RESOURCES` immediately. When a slot is acquired, calls
`tm.ExecutePlan()` synchronously in the same goroutine.

### 5.2 KubernetesResourceManager (production mode)

Two responsibilities: **plan dispatch** and **TM pod management**.

**Dispatch**: `Schedule()` serializes the ExecutionPlan to JSON and publishes it to
an MQTT topic. TM pods subscribed to the topic dequeue and execute plans. Returns
immediately after publish (fire-and-forget).

**TM pod management**: Manages a K8s Deployment (`flowgent-taskmanager`). On init,
`ensureDeployment()` checks if the Deployment exists and creates it if not. A
background `scalingLoop` periodically evaluates `pendingPlans / slotsPerTM` against
current replicas and scales the Deployment up or down via the K8s API.

`AutoScale` flag:
- `false` (session): admin-managed TM replicas (Helm `replicas`), scaling loop is no-op
- `true` (application): JM auto-scales TM replicas based on queue depth

---

## 6. TaskManager — Persistent Worker

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

JM ↔ TM communication via MQTT pub/sub. Topic structure:

```
flowgent/exec/{runID}/{planID}      — Execution plan dispatch
flowgent/notify/pod/{podID}/ws/+    — WebSocket routing
flowgent/notify/queue/{tenant}/{flow} — Notifier queue
```

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

Single process: API Server + JM + TM (LocalRM, goroutine pool). SQLite + Memory
cache. For development and small-scale local testing only.

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
(`flowgent-jm-{tenantId}-{flowId}-{runId}-{hash}`) in tenant namespace → JM auto-scales TMs.
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
   → Application mode: kubectl create deploy flowgent-jm-{tenantId}-{flowId}-{runId}-{hash}
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
     result := rm.Schedule(plan)  → MQTT (K8s) or local slot (all-in-one)
     Done(node) / Fail(node)
     condition → SetConditionResult → Skip(false-branch)
     supervisor → validate action → Inject/Retry/Abort

5. RM DISPATCH
   → Evaluates pending plans vs free slots
   → LocalRM: goroutine pool, returns INSUFFICIENT_RESOURCES if full
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

## 11. Notifier Service — Queue Consumer + Multi-Channel Push

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
Messages load-balanced across notification pods. Each message dispatched to
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

## 15. Key Design Constraints

| Constraint | Rationale |
|-----------|-----------|
| Only `agent` and `supervisor` use LLM | All other nodes must be deterministic |
| Vote must be deterministic | LLM "judges", vote "decides" |
| Agent output must be JSON | Machine-readable, auditable |
| Supervisor actions constrained | redirect/retry/inject/abort only |
| Human node must persist + timeout | DB-backed, resume via API |
| Map must support nesting | Multi-level fan-out |

---

## 16. File Map

| File | Role |
|------|------|
| `src/cmd/flowgent/main.go` | CLI entry (cobra): all-in-one, apiserver, wallet, controller, etc. |
| `src/cmd/flowgent/launch.go` | Subsystem init + JM/TM/Controller/Notifier startup |
| `src/cmd/flowgent/wallet.go` | Wallet daemon (separate for security isolation) |
| `src/engine/discovery/` | IDiscoveryClient interface + K8s/static implementations |
| `src/engine/jobmanager/` | JobManager + per-run JobMaster DAG orchestrator |
| `src/engine/resourcemanager/` | ResourceManager interface + Local + Kubernetes implementations |
| `src/engine/trigger/` | ScheduleTrigger — cron-based flow triggering |
| `src/engine/taskmanager/` | SlotWorker pool + heartbeat |
| `src/engine/executor/` | 10 TaskExecutor implementations |
| `src/api/server.go` | REST route registration |
| `src/api/agentflow.go` | AgentFlow CRUD + trigger handlers |
| `src/store/` | PostgreSQL + SQLite store implementations |
| `src/notification/` | Notifier service + channel senders |
| `src/model/` | Shared types: AgentFlowSpec, Node, Edge, Run, etc. |
| `deploy/helm/flowgent/` | Helm chart (6 microservices × 2 replicas) |

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

The sandbox subsystem securely executes scripts (Python, Bash, Node) generated by
agents or skills. It runs as a standalone microservice (`flowgent sandbox start`),
consuming lightweight trigger messages from the queue and reading/writing files
through a single persistent **workspace** volume shared with TaskManager pods.

### 15.1 Dual Persistence Model

Flowgent distinguishes two types of persistent data:

| Type | Storage | Survives | Example |
|------|---------|----------|---------|
| **Work data** (files) | Workspace volume (hostPath/PVC) | Pod restart, cluster reboot | Code patches, scripts, build artifacts |
| **Shared memory** (knowledge) | Storage (SQLite / PostgreSQL) | Everything | Agent conversation history, run progress, architecture docs |

The workspace volume is mounted to **every TM and Sandbox pod** so any pod can
access the files for any run. The storage layer is queried by `agentflow_definition_id`
to retrieve accumulated knowledge across runs.

### 15.2 Workspace Path Convention

```
{workspace}/
  └── {tenant}/
        └── {definition_id}/          ← agentflow definition (e.g. "security-autonomy-fixer-v3")
              └── runs/
                    └── {run_id}/      ← one execution (e.g. "run-abc123")
                          └── plans/
                                └── {plan_id}/   ← one node (e.g. "plan-xyz789")
                                      └── {span_id}/  ← one execution attempt (OTEL span)
                                            ├── script.{py,sh,js}
                                            ├── result.json
                                            ├── status
                                            └── original/   (pre-modification snapshot)
```

**Concrete example** — Security Fixer V3 fixing Rengine (3 runs, each fixing different issues):

```
/var/flowgent/workspace/
  └── default/
        └── security-autonomy-fixer-v3/
              ├── runs/run-001/plans/plan-fetch-sq/abc123/     # run 1: fetched 700 issues
              ├── runs/run-001/plans/plan-generate-fix/def456/  # run 1: patched 5 BLOCKERs
              ├── runs/run-002/plans/plan-fetch-sq/ghi789/     # run 2: re-scanned, 695 remain
              ├── runs/run-002/plans/plan-generate-fix/jkl012/  # run 2: patched 3 CRITICALs
              └── runs/run-003/plans/...                        # run 3: final pass
```

- **Session mode**: Helm creates one cluster-wide workspace `hostPath`. All flows share it, isolated by `{tenant}/{definition_id}` subdirectories.
- **Application mode**: Controller detects slot shortage, creates a dedicated PVC per `{tenant}/{definition_id}`, and sets `FLOWGENT_SANDBOX_WORKSPACE` env var on new TM/Sandbox pods to point to that PVC.
- **span_id**: 16-char hex identifier. Each sandbox execution gets a unique span, aligning with OTEL distributed tracing.

### 15.2 CLI

```bash
./bin/flowgent sandbox start
./bin/flowgent sandbox stop
./bin/flowgent sandbox restart
```

### 15.3 Security Policy (3-Level Override)

| Level | Config Source | Scope |
|-------|--------------|-------|
| Global | `flowgent.yaml` → `sandbox.policy` | All sandbox executions |
| Flow | `AgentFlowSpec.sandbox_policy` | All nodes in a flow |
| Node | `Node.network_policy` | Single node |

```yaml
sandbox:
  enabled: true
  image: "flowgent-sandbox:latest"
  workspace: "/var/flowgent/workspace"   # single persistent volume
  policy:
    network:
      mode: none
      allowed: []
    allowed_runtimes: [python3, bash, node]
    banned_commands: []
    default_timeout: 120s
    max_timeout: 600s
    default_resources:
      cpu: "500m"
      memory: "256Mi"
```

### 15.4 Sandbox Node Type

```yaml
nodes:
  - id: run-audit
    type: sandbox
    runtime: python3
    script: |
      import subprocess, json
      result = subprocess.run(["pip-audit", "--format", "json"], capture_output=True, text=True)
      print(result.stdout)
    timeout: 120s
    resources:
      cpu: "500m"
      memory: "256Mi"
    network_policy:
      mode: allowlist
      allowed: ["pypi.org:443"]
    workspace: "/home/agent/rengine"   # optional: data dir the sandbox reads/writes
```

### 15.5 Execution Model

```
TM: SandboxExecutor
  → build workspace path: {workspace}/{tenant}/{flow_id}/runs/{run_id}/plans/{plan_id}/{span_id}/
  → write script to path
  → if node.workspace set: snapshot original files to {path}/original/
  → push lightweight trigger to queue (path + metadata, no script content)
  → pop result from queue

Sandbox Worker
  → dequeue trigger
  → read script from workspace path
  → execute in Docker container:
      -v {workspace_path}:/sandbox:rw          ← script + result
      -v {node.workspace}:/workspace:rw        ← data dir (if set)
      --network=none (or allowlist)
  → validate policy
  → write result.json + status to workspace path
  → push result to queue
```

- **K8s**: Sandbox runs as a Deployment. Workspace mounted via hostPath (session) or PVC (application).
- **All-in-one**: Everything in-process via subprocess. Workspace is a local directory.

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

```
etc/
├── flowgent.yaml
├── agents/              # 01-supervisor.yaml, 02-issue-detector.yaml, ...
├── skills/              # 01-dependency-scan.yaml, ...
└── flows/               # 01-security-autonomy-fixer.yaml, ...
```

`01-` prefix is a convention for human readability and deterministic load order. The loader sorts files alphabetically.

### 16.3 Generic Loader

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
