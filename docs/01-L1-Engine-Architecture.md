# Flowgent Distributed Engine Architecture

**Date:** 2026-05-22
**Status:** Implemented — Helm chart, Controller≈FlinkOperator, multi-tenant naming, E2E verified on k3s

---

## 1. Architecture Overview

Flowgent is a distributed multi-tenant AI agent orchestration engine, modeled after
Apache Flink's session/application separation. Five runtime components:

```
  External: REST / A2A / Webhook / Cron
                              │
                              ▼
  ┌───────────────────────────────────────────────────────────┐
  │  Controller (≈ Flink Operator, sharded PG scan)          │  ← L2 app driver
  │  polls agentflow_definitions, hash-mod shards across pods │
  └──────────────────────────┬────────────────────────────────┘
                             │ INSERT agentflow_runs / create K8s JM Deployments
                             ▼
                        API Server
                   (multi-tenant gateway)
                             │
               ┌─────────────┴──────────────┐
               ▼                            ▼
         JobManager (session)         JobManager (application)
         ─ Helm-deployed, shared       ─ Controller-created, per-tenant-flow
               │                            │
               ▼                            ▼
          Scheduler                     Scheduler
               │                            │
               ▼                            ▼
         TaskManager(s)               TaskManager(s)
         ─ manual scale               ─ JM auto-scale
               │                            │
               ▼                            ▼
          MQTT Event Bus              MQTT Event Bus
               │                            │
               ▼                            ▼
     ┌──────────────────────────────────────────┐
     │  TaskExecutors (agent / tool / vote / …) │
     └──────────────────────────────────────────┘
```

### 1.1 Session vs Application — The Only Difference Is JM Lifecycle

Both modes use the **same binary, same poller, same DAG execution logic**.
The sole architectural difference is **who starts the JM and when**:

| | Session | Application |
|---|---|---|
| **JM started by** | Helm / Admin (platform init) | Controller (on flow discovery) |
| **JM naming** | `flowgent-{release}-jobmanager` | `flowgent-jm-{tenant}-{flow}` |
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
Pod names carry `tenant_id` + `flow_id` for observability within 63-char limit:

```
Session pods (shared pool, platform namespace):
  flowgent-{release}-apiserver-{hash}
  flowgent-{release}-jobmanager-{hash}
  flowgent-{release}-taskmanager-{hash}

Application pods (per-tenant namespace, dedicated):
  flowgent-jm-{tenant}-{flow}-{hash}
  flowgent-tm-{tenant}-{flow}-{hash}
```

Labels on all pods:
```yaml
flowgent.io/tenant: "default"
flowgent.io/flow:   "security-fixer"   # empty for session pods
flowgent.io/mode:   "session" | "application"
```

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
| `/api/v1/{tenant}/notifications/channels` | GET/POST | Notification channel CRUD |
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

### 2.4 Submit Path (API → JM)

```
POST /api/v1/{tenant}/agentflows/trigger
  → agentFlowHandler.TriggerWithVars()
    → spec := flows[agentFlowID]
    → run := { AgentFlowID, Vars, Status:PENDING, Tenant }
    → store.CreateAgentFlowRun(run)
    → JM's runPoller picks up pending run
```

---

## 3. JobManager — Control Plane

The JM is the singleton control plane (per session or per application). Receives
agentflow run submissions and spawns a **JobMaster** per run. Each JobMaster
builds a DAG from `AgentFlowSpec.Nodes` + `Edges` and executes nodes in
topological order.

### 3.1 JobMaster — Per-Run DAG Orchestrator

JobMaster holds per-run DAG state: nodes, edges, dependencies, completion/failure/
skip flags, node outputs, and conditions. Created per `Submit()` call. No shared
state between runs.

```
Execute(run, spec):
  buildExecutionGraph(spec, runID)
  → for each node in topological order:
      plan := buildPlan(node, inputs)
      result := rm.Schedule(plan)       // dispatch to TM
      nodeOutputs[nodeID] = result
      Done(nodeID)
      if condition → SetConditionResult
      if supervisor → validate action
  → run.Status = COMPLETED | FAILED
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

Polls PG for agentflow definitions, shards across pods via hash-mod, dispatches
session (INSERT run) or application (create K8s JM Deployment + INSERT run).

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

| Priority | Mode | Controller Action | TM |
|----------|------|-------------------|-----|
| low/medium/high | Session | `INSERT agentflow_runs` (namespace="") | Admin-managed pool |
| grade | Application | `kubectl create deploy flowgent-jm-{tenant}-{flow}` + `INSERT` (namespace={tenant}) | JM auto-scales |

### 4.4 Dual Format: Static YAML vs DB JSON

Both modes use `model.AgentFlowSpec` (dual-tagged `json:` + `yaml:`). Static YAML
loaded at startup + hot-reload. DB JSON saved by UI via API, polled by Controller.

---

## 5. ResourceManager / Scheduler — Pluggable Dispatch

Single entry point: `Schedule(ctx, plan) → TaskResult`. JM calls Schedule() and RM
internally handles capacity, TM selection, and deployment.

```go
type ResourceManager interface {
    Provider() engine.Provider
    Validate(ctx) error
    Schedule(ctx, plan) (*TaskResult, error)
    Shutdown(ctx) error
}
```

### 5.1 LocalResourceManager

In-process goroutine pool. Non-blocking slot acquisition — returns
`INSUFFICIENT_RESOURCES` when full (session mode capacity limit).

### 5.2 KubernetesResourceManager

Dispatches plans via MQTT to TM pods. `AutoScale` flag controls scaling:
- `false` (session): admin-managed TM replicas, scaling loop is no-op
- `true` (application): JM auto-scales TM Deployment via K8s API

---

## 6. TaskManager — Persistent Worker

Long-running K8s Deployment. Consumes `ExecutionPlan` messages from MQTT queue.
Each TM has N `SlotWorker` goroutines (typically 4). Slots execute plans via
`TaskExecutorRouter` and report results back to JM.

### 6.1 TaskExecutorRouter — 10 Node Types

| Type | Executor | Description |
|------|----------|-------------|
| `agent` | AgentExecutor | LLM call via configured provider |
| `tool` | ToolExecutor | MCP tool invocation |
| `condition` | ConditionExecutor | Boolean expression evaluation |
| `supervisor` | SupervisorExecutor | LLM-based action decision |
| `tribunal` | TribunalExecutor | Majority vote across inputs |
| `human` | HumanExecutor | Approval gate with timeout |
| `map` | MapExecutor | Fan-out over list (with nesting) |
| `agentflow` | SubflowExecutor | Nested agentflow execution |
| `noop` | NoopExecutor | Terminal node, passthrough |
| `sandbox` | SandboxExecutor | Script execution (python3/bash/node) |

### 6.2 Heartbeat & Failover

TMs send periodic heartbeats. JM's KubernetesResourceManager detects dead TMs
and re-dispatches orphaned plans.

---

## 7. MQTT Event Bus

JM ↔ TM communication via MQTT pub/sub. Topic structure:

```
flowgent/exec/{runID}/{planID}      — Execution plan dispatch
flowgent/notify/pod/{podID}/ws/+    — WebSocket routing
flowgent/notify/queue/{tenant}/{flow} — Notification queue
```

---

## 8. ExecutionPlan & Checkpoint

### 8.1 ExecutionPlan

Serializable task description dispatched to TMs. Contains: PlanID, NodeID,
AgentFlowRunID, TaskType, Input, RetryPolicy, and Agent/Tool references.

### 8.2 TaskCheckpoint

Periodic snapshot of DAG state (completed nodes, node outputs, pending set).
Enables resume-after-failure without re-executing completed nodes.

---

## 9. Deployment Topologies

### 9.1 All-in-One (Daemon Mode)

```bash
flowgent daemon start -c etc/flowgent.yaml
```

Single process: API Server + JM + TM (LocalRM, goroutine pool). SQLite + Memory
cache. For development and small-scale testing.

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
(`flowgent-jm-{tenant}-{flow}`) in tenant namespace → JM auto-scales TMs.
Flow completes → Controller cleans up Deployment.

---

## 10. Execution Flow (End to End)

```
1. TRIGGER
   → REST/A2A/Cron/Webhook → POST /api/v1/{tenant}/agentflows/trigger
   → agentFlowHandler validates spec exists
   → INSERT INTO agentflow_runs (status=PENDING)

2. DISPATCH (JM runPoller)
   → Polls agentflow_runs for PENDING (namespace-filtered)
   → jm.Submit(run, spec) spawns JobMaster
   → BuildGraphNodes from spec → DAG state initialized
   → run.Status = RUNNING

3. TOPOLOGICAL LOOP (JobMaster.Execute)
   ready := jm.Ready()
   for each ready node:
     plan := buildPlan(node, inputs, resolved vars)
     result := rm.Schedule(plan)  → MQTT or local slot
     Done(node) / Fail(node)
     condition → SetConditionResult → Skip(false-branch)
     supervisor → validate action → Inject/Retry/Abort

4. COMPLETION
   → IsComplete() → run.Status = COMPLETED
   → HasFailed()  → run.Status = FAILED
   → Persist final state to PG
```

### 10.1 TM Failover

```
1. TM sends heartbeat every 5s
2. JM's K8s RM detects missing heartbeat after 30s
3. Dead TM's leases expire → plans re-claimed by other TMs
4. K8s Deployment controller restarts dead TM pod
5. Orphaned plans re-executed from checkpoint
```

---

## 11. Notification Service — Queue Consumer + Multi-Channel Push

Bridges internal agentflow events to external communication channels.

### 11.1 Architecture

```
  ┌────────────────────────────────────────────┐
  │       Notification Service (2+ pods)        │
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

## 12. Metrics & Observability

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

## 13. Key Design Constraints

| Constraint | Rationale |
|-----------|-----------|
| Only `agent` and `supervisor` use LLM | All other nodes must be deterministic |
| Vote must be deterministic | LLM "judges", vote "decides" |
| Agent output must be JSON | Machine-readable, auditable |
| Supervisor actions constrained | redirect/retry/inject/abort only |
| Human node must persist + timeout | DB-backed, resume via API |
| Map must support nesting | Multi-level fan-out |

---

## 14. File Map

| File | Role |
|------|------|
| `src/cmd/flowgent/main.go` | CLI entry (cobra): daemon, apiserver, wallet, controller, etc. |
| `src/cmd/flowgent/launch.go` | Subsystem init + JM/TM/Controller/Notification startup |
| `src/cmd/flowgent/wallet.go` | Wallet daemon (separate for security isolation) |
| `src/engine/discovery/` | IDiscoveryClient interface + K8s/static implementations |
| `src/engine/jobmanager/` | JobManager + per-run JobMaster DAG orchestrator |
| `src/engine/scheduler/` | LocalResourceManager + KubernetesResourceManager |
| `src/engine/taskmanager/` | SlotWorker pool + heartbeat |
| `src/engine/executor/` | 10 TaskExecutor implementations |
| `src/api/server.go` | REST route registration |
| `src/api/agentflow.go` | AgentFlow CRUD + trigger handlers |
| `src/store/` | PostgreSQL + SQLite store implementations |
| `src/notification/` | Notification service + channel senders |
| `src/model/` | Shared types: AgentFlowSpec, Node, Edge, Run, etc. |
| `deploy/helm/flowgent/` | Helm chart (6 microservices × 2 replicas) |
| `deploy/kubernetes/` | Standalone K8s manifests |
