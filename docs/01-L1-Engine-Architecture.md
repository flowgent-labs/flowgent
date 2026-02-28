# Flowgent Distributed Engine Architecture

**Date:** 2026-05-21
**Status:** Implemented — Controller + Standard mode DB loading, x402 SDK refactor, all tests passing

---

## 1. Architecture Overview

Flowgent is a multi-tenant AI agent orchestration platform, modeled after Apache Flink's
session/application mode separation. It has **five** first-class runtime components:

```
   External: REST / A2A / Webhook / Cron

   ┌──────────────────────────────────────────────────────────┐
   │                    Controller (sharded)                   │  ← L2 app driver
   │            polls PG, dispatches session/application flows │
   └──────────────────────────┬───────────────────────────────┘
                              │
                              ▼
                         API Server
                    (multi-tenant gateway,
                     Flink-Operator-like)
                              │
                    ┌─────────┴──────────┐
                    ▼                    ▼
              JobManager            JobManager              ── per-tenant or
              (session A)           (session B)                per-application
                    │                    │
                    ▼                    ▼
               Scheduler             Scheduler
                    │                    │
                    ▼                    ▼
              TaskManager(s)        TaskManager(s)          ── elastic K8s Deployments
                    │                    │
                    ▼                    ▼
               MQTT Event Bus       MQTT Event Bus
                    │                    │
                    ▼                    ▼
          ┌─────────────────────────────────────┐
          │  TaskExecutors (agent/tool/vote/…)  │
          └─────────────────────────────────────┘
```

### 1.1 Session Mode (Multi-Tenant Shared Cluster)

Default deployment. Analogous to Flink session mode.

- One shared API Server + JobManager cluster
- TaskManagers are a shared elastic pool
- Multiple tenants/agentflows share resources
- API Server handles auth, rate limiting, webhook routing per tenant

### 1.2 Application Mode (VIP Dedicated Cluster)

For high-tier tenants requiring isolated resources. Analogous to Flink application mode.

- API Server provisions a **dedicated JobManager + TaskManager cluster** per tenant/application
- Each VIP tenant gets its own MQTT topic namespace, Postgres schema, and K8s namespace
- API Server acts like a **Flink Operator**: creates/destroys clusters, manages lifecycle
- Noisy-neighbor isolation guaranteed at K8s node/pod level

| Mode | API Server | JobManager | TaskManager | Isolation |
|------|-----------|------------|-------------|-----------|
| Session | Shared | Shared | Shared pool | Logical (auth + rate limit) |
| Application | Shared (operator) | Dedicated per tenant | Dedicated per tenant | Physical (separate K8s ns) |

---

## 2. API Server — Multi-Tenant Gateway & Operator

### 2.1 Why a Dedicated API Server (Not Merged into JobManager)

The API Server is a **separate, always-on component** distinct from JobManager. Rationale:

1. **Multi-tenancy**: Handles auth (JWT/OIDC/GitHub OAuth), rate limiting, and tenant routing
   BEFORE any agentflow execution. JobManager should not be burdened with auth concerns.
2. **Webhook ingress**: GitHub/GitLab webhooks arrive at a single, stable endpoint. API Server
   validates, authenticates, and routes to the correct tenant's JobManager.
3. **Application mode orchestration**: API Server is the "Flink Operator" — it provisions
   dedicated JM+TM clusters for VIP tenants. This is a meta-scheduling responsibility that
   belongs above any single JobManager.
4. **A2A protocol endpoint**: External AI agents discover Flowgent via `/.well-known/agent.json`.
   The API Server advertises available skills/agentflows and accepts A2A task submissions.
5. **Scale independence**: API Server can scale independently (stateless, 2+ replicas behind
   a load balancer) while JobManager is stateful (leader-elected via Postgres advisory lock).

### 2.2 REST API (Port 9999)

All CRUD paths are tenant-scoped with `{tenant}` in the URL path.
Webhook and human-approval paths are global (token-based).

| Route | Method | Description |
|-------|--------|-------------|
| `/_/healthz` | GET | Health check |
| `/_/openapi.yaml` | GET | OpenAPI 3.1 spec |
| `/_/swagger-ui` | GET | Swagger UI |
| `/_/webhooks/{provider}` | POST | Webhook trigger (non-tenant path) |
| `/api/v1/{tenant}/agents` | GET/POST | List / Create agent definitions |
| `/api/v1/{tenant}/agents/{name}` | GET/PUT/DELETE | Get / Update / Delete agent |
| `/api/v1/{tenant}/agentflows` | GET/POST | List / Create agentflow definitions |
| `/api/v1/{tenant}/agentflows/{id}` | GET/PUT/DELETE | Get / Update / Delete agentflow |
| `/api/v1/{tenant}/agentflows/trigger` | POST | Trigger a run by `agentflow_id` in body |
| `/api/v1/{tenant}/agentflows/{id}/trigger` | POST | Trigger a run by path ID |
| `/api/v1/{tenant}/runs` | GET | List runs (tenant-scoped) |
| `/api/v1/{tenant}/runs/{id}` | GET/DELETE | Get / Delete run |
| `/api/v1/{tenant}/runs/{id}/cancel` | POST | Cancel a running run |
| `/api/v1/{tenant}/runs/{id}/tasks` | GET | List tasks for a run |
| `/api/v1/{tenant}/runs/{id}/tasks/{task_id}` | GET | Get specific task detail |
| `/api/v1/{tenant}/notifications/channels` | GET/POST | List / Create notification channels |
| `/api/v1/{tenant}/notifications/channels/{id}` | GET/PUT/DELETE | Manage notification channel |
| `/api/v1/{tenant}/notifications/test` | POST | Test a notification channel |
| `/api/v1/{tenant}/ws/human-approvals` | GET | WebSocket SSE stream |
| `/api/v1/human/{token}/approve` | POST | Human approval (global) |
| `/api/v1/human/{token}/reject` | POST | Human rejection (global) |

### 2.3 A2A Protocol (Port 9992)

Google Agent-to-Agent protocol for inter-agent interoperability:

| Route | Method | Description |
|-------|--------|-------------|
| `/.well-known/agent.json` | GET | Agent card (skills, schemas, capabilities) |
| `/a2a/tasks` | POST | Submit agentflow for execution |
| `/a2a/tasks/{id}` | GET | Query task result |

### 2.4 Auth & Multi-Tenancy

- **JWT**: ES256 signed, access key (1h) + refresh key (24h)
- **OIDC**: Optional OpenID Connect integration
- **GitHub OAuth**: Built-in GitHub OAuth flow
- **Anonymous paths**: Configurable exemption list (`/healthz`, `/swagger-ui`, etc.)
- **Tenant context**: Extracted from JWT claims or API key header, propagated to JM via
  `X-Flowgent-Tenant` header or MQTT message metadata

### 2.5 Submit Path (API Server → JobManager)

```
POST /api/v1/{tenant}/agentflows/trigger
  → auth middleware validates tenant
  → resolve AgentFlowSpec (cache or store)
  → create AgentFlowRun (status=PENDING) with tenant/priority/namespace metadata
  → publish to MQTT /flowgent/{tenant}/trigger/{runId}
  → return 202 Accepted + runId

JobManager (tenant-scoped) consumes trigger topic:
  → dequeue trigger message
  → jm.Submit(run, spec)
  → spec.Priority >= grade → application mode (dedicated K8s namespace)
  → spec.Priority < grade  → session mode (shared pool)
  → execute DAG
```

The poller goroutine (2s interval) remains as a fallback for missed MQTT messages, but the
primary path is event-driven via MQTT.

---

## 3. JobManager — Control Plane

Per-tenant singleton. Builds the DAG execution graph and drives the topological execution loop.
Analogous to Flink's Dispatcher + JobMaster.

### 3.1 DAG State (JobMaster)

```go
type JobMaster struct {
    store     engine.Store
    rm        scheduler.ResourceManager
    logger    *utils.Logger
    tracer    trace.Tracer

    nodes          []string
    edges          [][2]string
    edgeConditions map[string]*bool
    deps           map[string][]string  // upstream dependencies
    children       map[string][]string  // downstream successors
    completed      map[string]bool
    skipped        map[string]bool
    failed         map[string]bool
    pending        map[string]bool
    conditions     map[string]bool
    nodeErrors     map[string]string    // error messages per failed node

    planMap     map[string]*model.ExecutionPlan
    nodeOutputs map[string]map[string]any
}
```

### 3.2 Key Methods

- **`BuildGraphNodes(nodes, edges)`** — initialize DAG from raw node/edge lists
- **`buildGraph(spec)`** — extract nodes, edges, conditions from AgentFlowSpec
- **`Submit(ctx, run, spec)`** — main entry point:
  1. Generate `ClusterID` (`cluster-{tenant}-{agentflow_id}-{run_id}`)
  2. Build DAG graph
  3. Set `run.Status = RUNNING`
  4. Topological loop: `Ready()` → `rm.Schedule(plan)` → collect `TaskResult` → `Done()`/`Fail()`
  5. Handle condition routing (skip branches via `SetConditionResult`)
  6. Handle supervisor actions (retry/redirect/inject/abort)
  7. Complete or fail the run

### 3.3 DAG State Methods

`Ready()`, `Done()`, `Skip()`, `Fail()`, `IsComplete()`, `HasFailed()`, `Inject()`,
`Children()`, `Deps()`, `SetConditionResult()`, `ConditionResult()`, `SetEdgeConditions()`,
`GetChildCondition()`

### 3.4 JobMaster

Per-run orchestrator, created by `JobManager.Submit()`. Holds the full in-memory DAG state
for one agentflow execution. No shared state between runs. A new JobMaster is created for
each `Submit()` call.

---

## 3.1 Controller — Distributed Flow Driver (Sharded)

The Controller is the **Layer 2 application driver** — it polls PostgreSQL for
agentflow definitions (saved by the future Flowgent UI via API Server as JSON) and
dispatches their execution in a distributed, sharded manner.

### 3.1.1 Architecture

```
  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐
  │ Controller-0 │  │ Controller-1 │  │ Controller-2 │   ← K8s Deployment (replicas=N)
  │ shard 0,3,6  │  │ shard 1,4,7  │  │ shard 2,5,8  │   ← hash(flow_id) % N
  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘
         │                 │                 │
         └─────────────────┼─────────────────┘
                           │
             ┌─────────────┴─────────────┐
             │   PostgreSQL (shared)      │
             │   agentflow_definitions    │
             │   agentflow_runs           │
             └───────────────────────────┘
```

### 3.1.2 Hash-Mod Sharding

Each controller pod owns a subset of flows determined by:

```
shard(flow_id) = fnv64a(flow_id) % total_controller_pods
```

- Uses FNV-64a hash (consistent with PG's `hashtext()` for portability)
- Each pod **only** processes flows where `shard == pod_index`
- On scale-up/down, flows naturally rebalance (no explicit rebalance needed —
  the new pod picks up new flows, old flows complete on current owners)

### 3.1.3 K8s Service Discovery

Pods discover their peers via the K8s API:

```go
pods, _ := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
    LabelSelector: "app=flowgent-controller",
})
totalPods = len(pods.Items)
podIndex  = indexOf(podName, pods)
```

Fallback: `FLOWGENT_CONTROLLER_INDEX` / `FLOWGENT_CONTROLLER_TOTAL` env vars
for non-K8s deployments.

### 3.1.4 Reconciliation Loop

```
Every 10s:
  1. Re-discover pod count (handles scale events)
  2. SELECT * FROM agentflow_definitions (latest version per flow_id)
  3. For each flow in my shard:
     a. If already running → skip
     b. If priority=grade → Application Mode (dedicated JM+TM K8s cluster)
     c. Else → Session Mode (create pending run for shared JM pool)
  4. Poll agentflow_runs for completion, then clean up
```

### 3.1.5 Session vs Application Dispatch

| Priority | Mode | Dispatch Mechanism |
|----------|------|-------------------|
| low / medium / high | Session | `INSERT INTO agentflow_runs` → shared JM's runPoller picks up |
| grade | Application | `kubectl create deployment flowgent-jm-{flow_id}` + dedicated TM |

Session mode reuses the shared JM+TM pool (like Flink Session Mode).
Application mode creates a dedicated K8s Namespace + JM Deployment + TM Deployment
(like Flink Application Mode).

**Key design: the SAME binary + code path runs in both modes.**

The only difference is the `FLOWGENT_NAMESPACE` env var:
- Session JM (namespace=""): runPoller only picks up runs with empty namespace
- Application JM (namespace="flowgent-<id>"): runPoller only picks up runs in that namespace

Both JMs use the identical `startRunPoller → BuildGraph → ExecutionPlan → Submit` pipeline.
The Controller simply creates the dedicated JM pod and inserts a pending run — the JM does
the rest via the standard code path. This avoids code duplication and ensures bug fixes
apply uniformly.

### 3.1.6 CLI

```bash
flowgent controller start -c etc/flowgent.yaml
flowgent controller stop
flowgent controller restart
```

Env vars:
- `FLOWGENT_DATABASE_URL` — PostgreSQL connection string (required)
- `FLOWGENT_CONTROLLER_INDEX` — pod index (fallback when K8s API unavailable)
- `FLOWGENT_CONTROLLER_TOTAL` — total pods (fallback)
- `FLOWGENT_CONTROLLER_LABEL` — K8s label selector (default: `app=flowgent-controller`)
- `FLOWGENT_JM_IMAGE` — JobManager container image for application mode

### 3.1.7 Dual Format: Static YAML vs DB JSON

The Controller reads from the same `agentflow_definitions` table as the API Server's
Standard mode (see `docs/20-TEST-e2e-guide.md` §10):

- **Static (YAML)**: File-based, GitOps-friendly, loaded at startup + hot-reload
- **Standard (JSON)**: DB-backed, saved by UI via API Server, polled by Controller

Both use `model.AgentFlowSpec` (dual-tagged `json:` + `yaml:`).

---

## 4. ResourceManager / Scheduler — Pluggable Dispatch

Bridge between JobManager (which builds DAGs) and TaskManager (which executes nodes).
Inspired by Flink's `SchedulerNG`.

### 4.1 Interface

```go
type ResourceManager interface {
    Provider() engine.Provider                      // "local" | "kubernetes"
    Validate(ctx context.Context) error
    Schedule(ctx context.Context, plan *model.ExecutionPlan) (*model.TaskResult, error)
    Shutdown(ctx context.Context) error
}
```

### 4.2 LocalResourceManager

- **Mode**: All-in-one, single process
- **Mechanism**: Embedded `TaskManager` + goroutine semaphore pool (default 10 slots)
- **Flow**: `Schedule()` acquires semaphore → calls `tm.ExecutePlan()` inline → returns result
- **Use case**: Development, testing, single-node deployments

### 4.3 KubernetesResourceManager

- **Mode**: Distributed, K8s-backed
- **Mechanism**: Manages a K8s Deployment of TM pods; dispatches plans via MQTT
- **Flow**:
  1. `Schedule()` serializes `ExecutionPlan` → MQTT topic `/flowgent/{tenant}/{flowId}/exec/{runId}/{planId}`
  2. TM SlotWorker dequeues, executes, publishes result
  3. `Schedule()` blocks (with timeout) awaiting result on status topic
- **Auto-scaling**: `reconcile()` loop scales TM Deployment between `minTMs` and `maxTMs`
  based on pending plan count vs current slot capacity
- **Self-healing**: Auto-creates TM Deployment if missing (`flowgent taskmanager start` image)
- **K8s config**: explicit kubeconfig → in-cluster config → `~/.kube/config`
- **Graceful fallback**: If K8s init fails, falls back to LocalResourceManager

---

## 5. TaskManager — Persistent Worker

Long-lived worker process (K8s Deployment pod or embedded goroutine). Has NO DAG awareness —
it receives isolated `ExecutionPlan` objects and executes them.

### 5.1 SlotWorker Pool

```go
type TaskManager struct {
    store      Store
    mcpClients map[string]MCPClient
    agents     map[string]*config.AgentDef
    llmClient  LLMClient
    logger     *slog.Logger
}
```

- Configurable `SlotWorker` goroutines (default 4 per TM pod)
- Each SlotWorker runs a persistent **dequeue→execute→ack** loop:
  1. `queue.Dequeue()` — block until a plan arrives on MQTT topic
  2. Unmarshal `ExecutionPlan` from message
  3. `TaskExecutorRouter.Execute(plan)` — dispatch by `TaskType`
  4. `queue.Ack()` or `queue.Nack()` — acknowledge or requeue
  5. Persist plan state + checkpoint to store
  6. Emit status on MQTT status topic for JM to consume

### 5.2 TaskExecutorRouter — 10 Node Types

| Executor | Node Type | Deterministic | Description |
|----------|-----------|:---:|-------------|
| `AgentExecutor` | `agent` | ✗ | LLM call with agent soul + instruction + output schema |
| `ToolExecutor` | `tool` | ✓ | MCP stdio client invocation |
| `ConditionExecutor` | `condition` | ✓ | Expression evaluation (`${vote.decision == true}`) |
| `TribunalExecutor` | `tribunal` | ✓ | Majority/unanimous/any/strict voting |
| `SupervisorExecutor` | `supervisor` | ✗ | LLM with constrained actions (redirect/retry/inject/abort) |
| `MapExecutor` | `map` | ✓ | Fan-out marker; JM handles concurrent dispatch |
| `JoinExecutor` | `join` | ✓ | Fan-in synchronization barrier |
| `SubflowExecutor` | `agentflow` | — | Recursive sub-agentflow (inline or sandboxed) |
| `HumanExecutor` | `human` | ✓ | Creates approval record, blocks until external API call |
| `NoopExecutor` | `noop` | ✓ | No-op |

### 5.3 Heartbeat & Failover

- **HeartbeatPump**: Each TM publishes periodic liveness messages to `/flowgent/heartbeat/{tmId}`
- **HeartbeatMonitor**: JM-side consumer detects expired TMs (no heartbeat for N intervals)
- **Failover flow**: Expired TM's in-flight plans are requeued → another TM SlotWorker picks up
  → resumes from last checkpoint

---

## 6. MQTT Event Bus

The central nervous system of the distributed runtime. All JM↔TM communication flows through
MQTT (EMQX broker).

### 6.1 Topic Structure

```
/flowgent/{tenant}/
  exec/{flowId}/{runId}/{planId}       — JM→TM: dispatch ExecutionPlan
  status/{flowId}/{runId}/{planId}     — TM→JM: completion status + output
  checkpoint/{flowId}/{runId}/{planId} — TM→store: incremental checkpoint
  trigger/{runId}                      — API→JM: trigger new run
/flowgent/heartbeat/{tmId}             — TM→JM: liveness
```

### 6.2 Async Tracing (W3C TraceContext)

- Inject `traceparent` + `tracestate` into MQTT message headers
- Extract on receive → create child span
- On retry: **same traceId, new spanId** — Jaeger shows full execution chain with retry branches

---

## 7. ExecutionPlan & Checkpoint

### 7.1 ExecutionPlan

The primary runtime object flowing through MQTT. Serializable, resumable, retryable,
and reassignable between TMs.

```go
type ExecutionPlan struct {
    PlanID     string
    TaskType   TaskType
    NodeSpec   NodeSpec          // decoupled from AgentFlowSpec
    Input      map[string]any
    Checkpoint *TaskCheckpoint   // for resume
    Lease      *TaskLease        // TM ownership, expiry
}
```

### 7.2 TaskCheckpoint

Agent resume state — messages, scratchpad, tool_call_state, last step.
Default granularity: checkpoint per task boundary.

### 7.3 Retry & Resume

- Node-level retry with exponential backoff (configurable `max_node_retries`)
- On TM failure: lease expires → plan requeued → another TM resumes from checkpoint
- Supervisor can explicitly `retry` a failed node

---

## 8. Deployment Topologies

### 8.1 All-in-One (Daemon Mode)

```bash
flowgent daemon start
```

- Single process: API Server + JobManager + LocalResourceManager (embedded TM)
- SQLite + memory queue
- No external dependencies
- **Use case**: Development, CI, single-node evaluation

### 8.2 Distributed K8s (Session Mode)

```
┌──────────────────────────────────────────────┐
│ K8s Cluster                                   │
│                                               │
│  ┌─────────────────┐  ┌────────────────────┐ │
│  │ API Server       │  │ EMQX (MQTT)        │ │
│  │ Deployment: 2    │  │ StatefulSet: 1-3   │ │
│  │ Ports: 9999,9992 │  │ Ports: 1883,18083 │ │
│  └────────┬────────┘  └─────────┬──────────┘ │
│           │                     │             │
│  ┌────────▼────────────────────▼──────────┐  │
│  │ JobManager                              │  │
│  │ Deployment: 1 (leader-elected)          │  │
│  │ Leader election via Postgres advisory   │  │
│  └────────┬────────────────────────────────┘  │
│           │                                    │
│  ┌────────▼────────────────────────────────┐  │
│  │ TaskManager                              │  │
│  │ Deployment: auto-scaled (min=2, max=20)  │  │
│  │ Scaled by KubernetesResourceManager      │  │
│  └──────────────────────────────────────────┘  │
│                                               │
│  ┌──────────────────────────────────────────┐ │
│  │ PostgreSQL                                │ │
│  └──────────────────────────────────────────┘ │
└──────────────────────────────────────────────┘
```

### 8.3 Application Mode (VIP Dedicated Cluster)

```
┌── Tenant A (shared session cluster) ──┐
│  JM-A + TM pool (auto-scaled)         │
└────────────────────────────────────────┘

┌── Tenant VIP (dedicated cluster) ──────┐
│  ┌──────────────────────────────────┐  │
│  │ Dedicated JM-VIP                 │  │
│  │ Dedicated TM-VIP (fixed or auto) │  │
│  │ Dedicated MQTT namespace         │  │
│  │ Dedicated Postgres schema        │  │
│  │ Separate K8s namespace           │  │
│  └──────────────────────────────────┘  │
└────────────────────────────────────────┘

API Server (shared) ── provisions & routes to both
```

---

## 9. Execution Flow (End to End)

```
1. TRIGGER (REST / A2A / Webhook / Cron)
   → API Server authenticates tenant
   → Creates AgentFlowRun (PENDING) with tenant/priority/namespace metadata
   → Publishes trigger to MQTT or poller picks it up

2. DISPATCH
   → JobManager dequeues trigger
   → checks spec.EffectiveMode() — PriorityGrade → application mode
   → jm.Submit(run, spec) spawns JobMaster
   → Builds DAG graph
   → run.Status = RUNNING

3. TOPOLOGICAL LOOP (JobMaster.Execute)
   ┌─────────────────────────────────────────┐
   │  ready := jm.Ready()                     │
   │  for each nodeID in ready:              │
   │    plan.Input = merge(yamlInput, resolvedDeps) │
   │    rm.Schedule(plan)                    │
   │      ├── Local: tm.ExecutePlan() inline │
   │      └── K8s: publish to MQTT exec/*    │
   │    collect TaskResult                   │
   │    Done(node) / Fail(node)              │
   │                                         │
   │  ConditionNode? → SetConditionResult    │
   │  SupervisorAction? → retry/redirect/    │
   │                      inject/abort       │
   └─────────────────────────────────────────┘

4. COMPLETION
   → IsComplete() → run.Status = COMPLETED
   → HasFailed()  → run.Status = FAILED, run.Error = collectFirstError()
   → Persist final state to store
```

### 9.1 TM Failover Sequence

```
1. HeartbeatMonitor detects TM-X expired (no heartbeat for 30s)
2. Leases held by TM-X are marked expired
3. In-flight ExecutionPlans for TM-X are requeued to MQTT exec topics
4. Next available TM SlotWorker dequeues plan
5. Plan contains last checkpoint → TM resumes from checkpoint
6. New TM assumes ownership, updates lease
```

---

## 10. Metrics & Observability

Per-component OTEL meters:

| Meter | Component |
|-------|-----------|
| `flowgent/apiserver` | Request latency, status codes, tenant routing |
| `flowgent/jobmanager` | DAG builds, run durations, node completions/failures |
| `flowgent/taskmanager` | Plan executions, slot utilization, heartbeat latency |
| `flowgent/executor` | Per-node-type execution latency and errors |
| `flowgent/queue` | Enqueue/dequeue rates, message latency, redeliveries |
| `flowgent/checkpoint` | Checkpoint save/load latency, size |

Config: `mgmt.otel.enabled` + `mgmt.otel.sample_rate`. Traces visible in Jaeger.
pprof: `mgmt.pprof.enabled` → port 6669.

---

## 11. Key Design Constraints

| Constraint | Rationale |
|------------|-----------|
| Only `agent` and `supervisor` use LLM | All other nodes must be deterministic |
| Vote must be deterministic | LLM "judges", vote "decides" |
| All agent output must be JSON | Machine-readable, validatable, audit-trail friendly |
| Supervisor actions are constrained | redirect/retry/inject/abort only — no tool execution, no DAG expansion |
| Human node must persist + timeout | DB-backed, resume via API, configurable timeout |
| Map must support nesting | Multi-level fan-out for hierarchical batch processing |

---

## 12. File Map

| File | Role |
|------|------|
| `src/cmd/flowgent/main.go` | CLI entry point (cobra): daemon, apiserver, a2a, wallet, etc. |
| `src/cmd/flowgent/launch.go` | Subsystem init: store, queue, RM, JM, API server startup |
| `src/api/server.go` | REST route registration (tenant-scoped paths) |
| `src/api/agentflow.go` | AgentFlow CRUD + trigger handlers |
| `src/api/agent.go` | Agent CRUD handler |
| `src/api/run.go` | Run query + lifecycle handler |
| `src/api/notification.go` | Notification channel CRUD handler |
| `src/api/websocket.go` | WS Bridge (SSE push via notification service) |
| `src/api/middleware.go` | JWT/OIDC/GitHub OAuth middleware |
| `src/engine/jobmanager/jobmanager.go` | Control-plane JM (mode routing, job submission) |
| `src/engine/jobmanager/jobmaster.go` | Per-run DAG orchestrator |
| `src/engine/scheduler/resourcemanager.go` | ResourceManager interface + factory + validation |
| `src/engine/scheduler/local.go` | Goroutine pool (all-in-one) |
| `src/engine/scheduler/kubernetes.go` | K8s Deployment scale + MQTT dispatch |
| `src/engine/taskmanager/taskmanager.go` | Persistent TM with slot workers + heartbeat |
| `src/engine/executor/*.go` | 10 TaskExecutor implementations + router |
| `src/model/agentflow.go` | AgentFlowSpec, Node, Edge, TriggerDef, Priority, ExecutionMode |
| `src/model/execution_plan.go` | ExecutionPlan, NodeSpec (with RawInput + OutputSchema), TaskResult |
| `src/model/run.go` | AgentFlowRun, TaskRun, HumanApproval (+ tenant/priority fields) |
| `src/model/agent.go` | AgentDef (DB-backed agent definition) |
| `src/model/notification.go` | NotificationChannel, SubscriptionRoute |
| `src/model/websocket.go` | WSMessage types |
| `src/queue/mqtt.go` | MQTT queue (EMQX) |
| `src/queue/memory.go` | In-memory queue (dev/all-in-one) |
| `src/store/store.go` | Unified Store interface |
| `src/store/postgres.go` | Postgres implementation |
| `src/store/sqlite.go` | SQLite implementation |
| `src/store/store_agents.go` | Agent + agentflow dynamic CRUD methods |
| `src/store/store_notifications.go` | Notification + subscription route methods |
| `src/llm/llm.go` | OpenAI-compatible LLM client (temperature, topk, modalities, thinking) |
| `src/llm/mcp.go` | MCP client factory (env override, initialize, call tool) |
| `src/notification/` | Notification service (telegram, dingtalk, slack, email, webhook) |
| `src/common/utils/schema.go` | JSON Schema validator |
| `src/common/tracing/provider.go` | OTEL init |
| `examples/mcp-*/` | Example MCP servers (e2e testing only) |
| `examples/agents/`, `examples/flows/` | Example agent + flow YAML definitions |

---

## 13. Test Coverage

Engine unit tests: 14/14 PASS.

| Test | Coverage |
|------|----------|
| `TestJobManager_BasicTopology` | Linear A→B→C DAG, Ready/Done/IsComplete |
| `TestJobManager_ParallelReady` | Fan-out A→B, A→C |
| `TestJobManager_Skip` | Skipped nodes allow downstream readiness |
| `TestJobManager_Fail` | Failed nodes stop execution |
| `TestJobManager_Inject` | Dynamic node injection via supervisor |
| `TestJobManager_EdgeCondition` | Edge-level condition storage/lookup |
| `TestJobManager_ConditionResult` | Condition result set/get |
| `TestJobManager_RetryBackoff` | Retry with exponential backoff |
| `TestJobManager_StateTransitions` | Full state machine transitions |

E2E tests: 9+2 PASS (full security-autonomy-fixer pipeline, MQTT integration).

---

> **Design inspiration:** The JobManager/Scheduler/TaskManager separation is modeled after
> Apache Flink's session-mode architecture. The API Server is a Flowgent-original addition
> for multi-tenant platform operation — analogous to how managed Flink platforms (Ververica,
> Confluent) deploy a control plane above individual Flink clusters. The goal is to make
> Flowgent the "AI Agent world's" equivalent of Flink — an open, flexible, predictable
> orchestration engine for autonomous agents, operable at platform scale.
