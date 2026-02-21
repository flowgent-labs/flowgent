# Flowgent Engine Architecture — JobManager / TaskManager / Scheduler

**Date:** 2026-05-11
**Status:** Implemented, tests passing

---

## 1. Architecture Overview

```
AgentFlowSpec (YAML / DB)
        ↓
   JobManager          ← master orchestrator, drives DAG
        ↓
   Scheduler           ← pluggable dispatch layer
    ├── Local          ← goroutine pool (dev/test/all-in-one)
    └── Kubernetes     ← Kubernetes Job per task (production distributed)
        ↓
   TaskManager         ← stateless node executor (agent/tool/vote/etc.)
```

The engine is structured as three loosely coupled layers:

| Component | Responsibility |
|-----------|----------------|
| `JobManager` | Build DAG execution graph, topological loop, dispatch tasks |
| `Scheduler` | Dispatch abstraction, resource management |
| `TaskManager` | Execute individual nodes, no DAG knowledge |

---

## 2. JobManager (`jobmanager.go`)

Replaces the former `DAGScheduler` (passive state container) and `AgentFlowRuntime` (fat orchestrator).

### DAG State Management

```go
type JobManager struct {
    // DAG graph structure
    nodes    []string
    edges    [][2]string
    deps     map[string][]string   // upstream dependencies
    children map[string][]string   // downstream successors

    // Execution state
    completed map[string]bool
    skipped   map[string]bool
    failed    map[string]bool
    pending   map[string]bool
    injected  map[string][]string

    // Condition routing
    conditions     map[string]bool
    edgeConditions map[string]*bool  // "from->to" → condition

    store       Store
    scheduler   Scheduler
    taskManager *TaskManager   // for inline map node execution
    logger      *util.Logger

    timeout        time.Duration
    injectionLimit int
    nodeLimit      int
    maxNodes       int

    nodeOutputs map[string]map[string]any
}
```

### Key Methods

- **`BuildGraphNodes(nodes, edges)`** — initialize DAG from raw node/edge lists (exported for testing)
- **`buildGraph(spec)`** — extract nodes/edges/conditions from AgentFlowSpec
- **`StartJob(ctx, run, spec)`** — main execution entry point:
  1. Generate `ClusterID` for resource tracking
  2. Build DAG graph from spec
  3. Set `run.Status = RUNNING`
  4. Loop: `Ready()` → `scheduler.SubmitTask()` → collect `TaskResult` → `Done(node)` / `Fail(node)`
  5. Handle condition routing (`SetConditionResult` → skip branches)
  6. Handle supervisor actions (retry/redirect/inject/abort)
  7. Complete or fail the run

### DAG State Methods

`Ready()`, `Done()`, `Skip()`, `Fail()`, `IsComplete()`, `HasFailed()`, `Inject()`, `Children()`, `Deps()`, `SetConditionResult()`, `ConditionResult()`, `SetEdgeConditions()`, `GetChildCondition()`

---

## 3. Scheduler

### Interface (`scheduler.go`)

```go
type Scheduler interface {
    Type() SchedulerType              // "local" | "kubernetes"
    SubmitTask(ctx, *TaskSubmit) (*TaskResult, error)
    Close() error
}

type TaskSubmit struct {
    ClusterID   string
    AgentFlowID string
    RunID       string
    NodeID      string
    TaskID      string
    Node        *model.Node
    Input       map[string]any
}

type TaskResult struct {
    Output map[string]any
    Error  string
}
```

### LocalScheduler (`local_scheduler.go`)

- Goroutine pool with configurable concurrency (default 10)
- `SubmitTask` acquires a semaphore slot, calls `TaskManager.ExecuteNode` directly
- Designed for dev/test and all-in-one deployment
- Resource management is the semaphore — no external dependencies

### KubernetesScheduler (`kubernetes_scheduler.go`)

- Creates a Kubernetes `batch/v1 Job` per task execution
- Requires `k8s.io/client-go` for API access
- Config resolution: explicit kubeconfig path → in-cluster config → `~/.kube/config`
- Each Job runs a `tasklet` container image with the `TaskSubmit` serialized into `FLOWGENT_TASK_SUBMIT` env
- Resource requests/limits configurable via `KubernetesSchedulerConfig`
- Automatic Job cleanup via `TTLSecondsAfterFinished`
- Blocks until Job completes or fails (watches via K8s API)
- Resource management delegated to Kubernetes (ResourceQuota, LimitRange)

---

## 4. TaskManager (`taskmanager.go`)

Renamed from `Executor`. Stateless node executor with no DAG awareness.

```go
type TaskManager struct {
    store      Store
    mcpClients map[string]MCPClient
    agents     map[string]*config.AgentDef
    llmClient  LLMClient
    logger     *util.Logger
}
```

### Public Entry Point

- **`ExecuteNode(ctx, task, node, scope) error`** — executes a single node:
  1. Resolve input via JSONPath (`${node.field}`)
  2. Switch on `node.Type`:
     - **`agent`** — call LLM with agent soul + instruction, parse JSON output
     - **`tool`** — call MCP stdio client
     - **`condition`** — evaluate expression via `util.EvalCondition`
     - **`tribunal`** — deterministic vote (majority/unanimous/any/strict)
     - **`human`** — create `HumanApproval` record, return WAITING_HUMAN
     - **`supervisor`** — call LLM, validate `allowed_actions`, return action
     - **`noop`** — no-op
     - **`map`** — dispatched by JobManager, not TaskManager
     - **`agentflow`** — dispatched by JobManager via spec lookup
  3. Persist task result via store

### Tasklet (`cmd/tasklet/main.go`)

Kubernetes Job entry point for distributed node execution:
1. Read `TaskSubmit` from `FLOWGENT_TASK_SUBMIT` env var
2. Connect to shared Postgres store + LLM + MCP clients
3. Load `TaskRun` + build scope
4. Call `TaskManager.ExecuteNode()`
5. Result persisted to store → Job exits → controller detects completion

---

## 5. Execution Flow

```
User / Trigger / Scheduler
        │
        ▼
JobManager.StartJob(ctx, run, spec)
        │
        ├── buildGraph(spec)
        ├── run.Status = RUNNING
        │
        │   Execution Loop:
        │   ┌─────────────────────────────────────────────┐
        │   │  ready := jm.Ready()                        │
        │   │  for each nodeID in ready:                  │
        │   │    │                                        │
        │   │    ├── MapNode? → MapRunner.runMap()        │
        │   │    │               └── TaskManager per item │
        │   │    │                                        │
        │   │    └── Other? → Scheduler.SubmitTask()      │
        │   │                    │                        │
        │   │                    ├── Local: goroutine pool│
        │   │                    │   → TaskManager        │
        │   │                    │                        │
        │   │                    ├── Kubernetes: Job pod  │
        │   │                    │   → tasklet → TM       │
        │   │                    │                        │
        │   │                    └── Collect TaskResult   │
        │   │                                             │
        │   │  ConditionNode? → branch routing            │
        │   │  SupervisorAction? → retry/redirect/inject   │
        │   │  Done(node) / Fail(node)                    │
        │   └─────────────────────────────────────────────┘
        │
        ├── IsComplete() → run.Status = COMPLETED
        └── HasFailed() → run.Status = FAILED
```

---

## 6. Previous → New File Mapping

| Old File | New File | Change |
|----------|----------|--------|
| `dag_scheduler.go` | — | Merged into `jobmanager.go` |
| `runtime.go` | — | Merged into `jobmanager.go` |
| `executor.go` | `taskmanager.go` | Renamed type `Executor` → `TaskManager` |
| — | `scheduler.go` | New: interface + types |
| — | `local_scheduler.go` | New: goroutine pool |
| — | `kubernetes_scheduler.go` | New: K8s Job per task + tasklet binary |
| `worker/worker.go` | — | Deleted (absorbed by LocalScheduler) |

---

## 7. Test Coverage

Engine unit tests: 7/7 PASS

| Test | What it covers |
|------|---------------|
| `TestJobManager_BasicTopology` | Linear A→B→C DAG, Ready/Done/IsComplete |
| `TestJobManager_ParallelReady` | Fan-out A→B, A→C |
| `TestJobManager_Skip` | Skipped nodes allow downstream readiness |
| `TestJobManager_Fail` | Failed nodes stop execution |
| `TestJobManager_Inject` | Dynamic node injection via supervisor |
| `TestJobManager_EdgeCondition` | Edge-level condition storage/lookup |
| `TestJobManager_ConditionResult` | Condition result set/get |

E2E tests (9+2):

| Test | What it covers |
|------|---------------|
| `TestE2E_BasicAgentFlow` | Full agent → fix → review → vote flow |
| `TestE2E_SupervisorAllowedActions` | Supervisor action validation |
| `TestE2E_MapNodeExecution` | Map fan-out with concurrency |
| `TestE2E_NodeRetry` | Retry after transient failure |
| `TestE2E_DAGExecutor` | Topological ordering |
| `TestE2E_DAGInject` | Dynamic injection |
| `TestE2E_ConditionSkip` | Condition-based path skipping |
| `TestE2E_SecurityFixPipeline_Local` | 13-node full pipeline e2e |
| `TestE2E_MQTTQueue_Local` | MQTT broker integration |

---

## 8. ResourceManager

A separate `ResourceManager` abstraction is **not** implemented. Resource
management is embedded in each scheduler implementation:

- **LocalScheduler**: goroutine pool semaphore controls concurrency
- **KubernetesScheduler**: Kubernetes ResourceQuota, LimitRange, and
  per-container resource requests/limits handle resource allocation

This keeps the architecture minimal while allowing each scheduler to use
the resource model most natural to its environment. A cross-scheduler
ResourceManager interface can be extracted later if scheduling backends
need unified resource accounting (e.g., hybrid deployments).

---

## 9. ClusterID

Each `StartJob` call generates a `ClusterID` (`cluster-{agentflow_id}-{run_id}`)
that identifies the resource pool for all tasks in this job execution. The
`TaskSubmit` carries the `ClusterID` to the scheduler for resource tracking.

---

> **Note on design inspiration:** The JobManager/Scheduler/TaskManager
> separation is inspired by distributed computing frameworks that separate
> job orchestration from task execution. The goal is to make Flowgent
> the "AI Agent world's" equivalent of those systems — an open,
> flexible, and predictable orchestration engine for autonomous agents.
