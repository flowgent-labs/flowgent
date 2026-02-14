# Flowgent Engine Architecture — JobManager / TaskManager / Scheduler

**Date:** 2026-05-11
**Status:** Implemented, tests passing

---

## 1. Architecture Overview (Flink-Aligned)

```
AgentFlowSpec (YAML / DB)
        ↓
   JobManager          ← master orchestrator, drives DAG
        ↓
   Scheduler           ← pluggable dispatch layer
    ├── Standalone     ← goroutine pool (dev/test/all-in-one)
    └── K8s            ← Kubernetes pod per task (stub)
        ↓
   TaskManager         ← stateless node executor (agent/tool/vote/etc.)
```

| Flowgent | Flink Analogy | Responsibility |
|----------|---------------|----------------|
| `JobManager` | JobManager | Build DAG graph, topological loop, dispatch tasks |
| `Scheduler` | Scheduler (Standalone/K8s) | Dispatch abstraction, resource management |
| `TaskManager` | TaskManager | Execute individual nodes, no DAG knowledge |
| `ClusterID` | Cluster ID | Resource pool identifier per job |

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

    // Dependencies
    store       Store
    scheduler   Scheduler
    taskManager *TaskManager   // for inline map node execution
    logger      *util.Logger

    // Limits
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

### DAG State Methods (previously on DAGScheduler)

`Ready()`, `Done()`, `Skip()`, `Fail()`, `IsComplete()`, `HasFailed()`, `Inject()`, `Children()`, `Deps()`, `SetConditionResult()`, `ConditionResult()`, `SetEdgeConditions()`, `GetChildCondition()`

---

## 3. Scheduler Interface (`scheduler.go` + implementations)

### Interface

```go
type Scheduler interface {
    Type() SchedulerType              // "standalone" | "k8s"
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

### StandaloneScheduler (`standalone_scheduler.go`)

- Goroutine pool with configurable concurrency (default 10)
- `SubmitTask` acquires a semaphore slot, calls `TaskManager.ExecuteNode` directly
- Designed for dev/test and all-in-one deployment
- Zero serialization overhead — same process, same memory

### K8sScheduler (`k8s_scheduler.go`)

- Stub implementation for production distributed mode
- `SubmitTask` would launch a Kubernetes Job (pod) per node execution
- Pod contains a TaskManager that receives the serialized `TaskSubmit`
- Not yet implemented — returns descriptive error

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

### Public Method

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
        │   │                    ├── Standalone: goroutine │
        │   │                    │   → TaskManager        │
        │   │                    │                        │
        │   │                    ├── K8s (stub): pod      │
        │   │                    │   → TaskManager        │
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
| — | `standalone_scheduler.go` | New: goroutine pool |
| — | `k8s_scheduler.go` | New: K8s stub |
| `worker/worker.go` | — | Deleted (absorbed by StandaloneScheduler) |

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

E2E tests (9 total, 6 DAG-related + 3 pipeline):

| Test | What it covers |
|------|---------------|
| `TestE2E_BasicAgentFlow` | Full agent → fix → review → vote flow |
| `TestE2E_SupervisorAllowedActions` | Supervisor action validation |
| `TestE2E_MapNodeExecution` | Map fan-out with concurrency |
| `TestE2E_NodeRetry` | Retry after transient failure |
| `TestE2E_DAGExecutor` | Topological ordering (e2e) |
| `TestE2E_DAGInject` | Dynamic injection (e2e) |
| `TestE2E_ConditionSkip` | Condition-based path skipping |

---

## 8. ClusterID and Resource Management

Each `StartJob` call generates a `ClusterID` (format: `cluster-{agentflow_id}-{run_id}`) that identifies the resource pool for all tasks in this job execution. The `TaskSubmit` carries the `ClusterID` to the `TaskManager` for:

- **Standalone mode**: ClusterID maps to the goroutine pool semaphore
- **K8s mode** (future): ClusterID maps to a Kubernetes namespace/label selector for pod grouping and resource quotas
