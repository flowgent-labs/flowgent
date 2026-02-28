# Persistent Distributed Agent Runtime — Implementation Plan

**Date:** 2026-05-12
**Status:** Draft for review
**Scope:** Architectural correction — Flowgent is a persistent agent runtime, not a K8s job orchestrator.

---

## 1. Gap Analysis: Current Code vs Target Architecture

### 1.1 What Exists (and stays)

| Component | File(s) | Status |
|-----------|---------|--------|
| `Node` / `Edge` / `AgentFlowSpec` types | `src/model/agentflow.go` | Keep, minor additions |
| `AgentFlowRun` / `TaskRun` types | `src/model/run.go` | Keep, extend with ExecutionPlan |
| `Store` interface | `src/engine/taskmanager.go` | Keep |
| `TaskManager.ExecuteNode()` | `src/engine/taskmanager.go` | Keep, refactor to slot-worker pattern |
| MQTT queue | `src/queue/mqtt.go` | Keep, becomes central event bus |
| JSONPath resolver | `src/util/` | Keep |
| LLM / MCP clients | `src/llm/`, `src/mcp/` | Keep |
| AgentFlow YAML parsing | `src/config/` | Keep |
| OpenAPI / A2A APIs | `src/api/` | Keep |

### 1.2 What Must Change

| Current | Why | Change |
|---------|-----|--------|
| `KubernetesScheduler` + `batch/v1 Job` | Wrong model — TM is persistent, not one-shot | **Delete `kubernetes_scheduler.go`** |
| `LocalScheduler` inline goroutines | Should go through MQTT queue like distributed mode | **Delete `local_scheduler.go`**, JM always dispatches via queue |
| `JobManager` contains entire DAG + execution loop | JM should be control-plane only | **Refactor**: JM only builds plan + dispatches + monitors |
| `TaskManager.ExecuteNode()` called directly | TM should consume from queue via slot workers | **Refactor**: add SlotWorker loop |
| No ExecutionPlan type | TaskRun is DB model, not a runtime execution primitive | **New**: `src/model/execution_plan.go` |
| No checkpoint | Required for TM failover | **New**: `src/engine/checkpoint.go` |
| No heartbeat/lease | Required for TM failover detection | **New**: `src/engine/heartbeat.go` |
| `cmd/tasklet/` | Wrong naming, wrong model | **Rename**: `cmd/taskmanager-runner/` |
| `src/cmd/core/run.go` | Wrong file name | **Rename**: `src/cmd/core/launch.go` |
| `src/cmd/core/otel.go` | Belongs in common | **Move**: `src/common/tracing/provider.go` |

### 1.3 What Must Be Added

| New | Purpose |
|-----|---------|
| `src/model/execution_plan.go` | `ExecutionPlan` — primary runtime object |
| `src/engine/executor.go` | `TaskExecutor` interface + per-type impls |
| `src/engine/checkpoint.go` | Checkpoint persistence for agent execution state |
| `src/engine/heartbeat.go` | TM heartbeat + lease + JM failover detection |
| `src/engine/config.go` | Engine config (scheduler type, pool sizes, timeouts) |
| `src/common/tracing/provider.go` | OTEL provider init (moved from cmd) |
| `deploy/kubernetes/tm-deployment.yaml` | K8s Deployment for persistent TMs |

---

## 2. New Model: ExecutionPlan

```go
// src/model/execution_plan.go

type TaskState string

const (
    TaskPending   TaskState = "PENDING"
    TaskRunning   TaskState = "RUNNING"
    TaskSuccess   TaskState = "SUCCESS"
    TaskFailed    TaskState = "FAILED"
    TaskSkipped   TaskState = "SKIPPED"
    TaskRetrying  TaskState = "RETRYING"
)

type TaskType string

const (
    TaskAgent       TaskType = "agent"
    TaskCondition   TaskType = "condition"
    TaskTool        TaskType = "tool"
    TaskSupervisor  TaskType = "supervisor"
    TaskSubflow     TaskType = "subflow"
    TaskTribunal    TaskType = "tribunal"
    TaskMap         TaskType = "map"
    TaskJoin        TaskType = "join"
    TaskHuman       TaskType = "human"
    TaskNoop        TaskType = "noop"
)

// ExecutionPlan is the primary runtime object — the unit of work
// that flows through the system. Serializable, resumable,
// retryable, reassignable between TaskManagers.
type ExecutionPlan struct {
    PlanID          string           `json:"plan_id"`
    AgentFlowRunID  string           `json:"agentflow_run_id"`
    TaskID          string           `json:"task_id"`
    TaskType        TaskType         `json:"task_type"`
    NodeID          string           `json:"node_id"`

    State           TaskState        `json:"state"`
    RetryCount      int              `json:"retry_count"`
    MaxRetries      int              `json:"max_retries"`

    AssignedTMID    string           `json:"assigned_tm_id,omitempty"`
    LeaseExpireAt   *time.Time       `json:"lease_expire_at,omitempty"`

    Input           map[string]any   `json:"input"`
    Result          *TaskResult      `json:"result,omitempty"`
    Checkpoint      *TaskCheckpoint  `json:"checkpoint,omitempty"`

    NodeSpec        *NodeSpec        `json:"node_spec"`

    CreatedAt       time.Time        `json:"created_at"`
    StartedAt       *time.Time       `json:"started_at,omitempty"`
    FinishedAt      *time.Time       `json:"finished_at,omitempty"`
}

// NodeSpec is the simplified node definition embedded in an ExecutionPlan.
// It decouples the plan from the full AgentFlowSpec.
type NodeSpec struct {
    ID               string              `json:"id"`
    Type             NodeType            `json:"type"`
    Agent            string              `json:"agent,omitempty"`
    AgentFlowID      string              `json:"agentflow,omitempty"`
    Tool             string              `json:"tool,omitempty"`
    Source           string              `json:"source,omitempty"`
    Expression       string              `json:"expression,omitempty"`
    Instruction      string              `json:"instruction,omitempty"`
    Strategy         map[string]any      `json:"strategy,omitempty"`
    Concurrency      int                 `json:"concurrency,omitempty"`
    Retry            *RetryPolicy        `json:"retry,omitempty"`
    Approval         *HumanApprovalConfig `json:"approval,omitempty"`
    SupervisorConfig *SupervisorConfig   `json:"supervisor_config,omitempty"`
    ChildNode        *NodeSpec           `json:"child_node,omitempty"`
}

// TaskCheckpoint captures agent execution state for resume-after-failure.
type TaskCheckpoint struct {
    Messages       []MessageEntry  `json:"messages,omitempty"`
    Scratchpad     string          `json:"scratchpad,omitempty"`
    ToolCallState  map[string]any  `json:"tool_call_state,omitempty"`
    LastStep       int             `json:"last_step"`
    Intermediates  []any           `json:"intermediates,omitempty"`
    CheckpointedAt time.Time       `json:"checkpointed_at"`
}

type MessageEntry struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}
```

### Extended AgentFlowRun

```go
// Add to existing AgentFlowRun:
type AgentFlowRun struct {
    // ... existing fields ...

    // NEW: shared memory visible to all ExecutionPlans in this run
    SharedMemory    map[string]any            `json:"shared_memory,omitempty"`

    // NEW: all ExecutionPlans for this run, keyed by plan_id
    ExecPlans       map[string]*ExecutionPlan `json:"exec_plans,omitempty"`
}
```

---

## 3. Architecture Diagram

```
[API / Trigger / Cron]
        │
        ▼
┌───────────────────────────────────┐
│        JobManager (control plane) │
│  - Parse agentflow → DAG          │
│  - Create ErxecutionPlans         │
│  - Dispatch plans → MQTT          │
│  - Track state                    │
│  - Monitor TM heartbeats          │
│  - Failover: requeue expired plans│
└────────┬──────────────────────────┘
         │ enqueue ExecutionPlans
         ▼
┌────────────────────────┐
│     MQTT (EMQX)        │
│  flowgent/exec/        │
│  flowgent/status/      │
│  flowgent/heartbeat/   │
└─────┬──────────┬───────┘
      │          │ dequeue
      ▼          ▼
┌──────────┐ ┌──────────┐ ┌──────────┐
│TM pod #1 │ │TM pod #2 │ │TM pod #N │  ← K8s Deployment (fixed pool)
│SlotWork. │ │SlotWork. │ │SlotWork. │
│ Executor │ │ Executor │ │ Executor │
│Heartbeat │ │Heartbeat │ │Heartbeat │
└──────────┘ └──────────┘ └──────────┘
      │
      │ emit downstream ready tasks
      ▼
   MQTT (flowgent/exec/)
```

### Key Design Decisions

**1. JM never executes tasks directly.** All task execution flows through MQTT → TM.

**2. TM pods are long-lived K8s Deployments.** Not batch Jobs. They persist across runs.

**3. ExecutionPlan is the wire format.** Everything a TM needs is in the plan. No need to look up the AgentFlowSpec.

**4. Event-driven downstream scheduling.** After a task completes, the TM emits the next ready tasks directly into MQTT. JM only tracks metadata.

**5. MQTT topics:**

| Topic | Purpose |
|-------|---------|
| `flowgent/exec/{agentflow_id}` | Execution plans dispatched by JM → consumed by TMs |
| `flowgent/status/{agentflow_id}` | Status updates from TM → consumed by JM |
| `flowgent/heartbeat/{tm_id}` | Heartbeat pings from TM → consumed by JM |

---

## 4. TaskManager Redesign

### Current (to be changed)

```go
// Current: TaskManager is a passive executor
type TaskManager struct {
    store      Store
    mcpClients map[string]MCPClient
    agents     map[string]*config.AgentDef
    llmClient  LLMClient
    logger     *util.Logger
}

func (tm *TaskManager) ExecuteNode(ctx, task, node, scope) error {
    // runs one node, returns
}
```

### New: Persistent worker with slots

```go
// TaskManager is a persistent worker that consumes ExecutionPlans
// from MQTT and executes them via slot workers.
type TaskManager struct {
    ID            string
    slotWorkers   []*SlotWorker
    executors     map[TaskType]TaskExecutor
    heartbeat     *HeartbeatPump
    queue         queue.Queue
    store         Store
    logger        *util.Logger
}

// NewTaskManager creates a TM with N slot workers.
func NewTaskManager(cfg *TaskManagerConfig) (*TaskManager, error) { ... }

// Start begins the persistent consume loop on all slot workers.
func (tm *TaskManager) Start(ctx context.Context) error { ... }

// Stop shuts down gracefully, completing in-flight tasks.
func (tm *TaskManager) Stop() { ... }

// SlotWorker runs a persistent loop consuming from MQTT.
type SlotWorker struct {
    id       string
    tmID     string
    queue    queue.Queue
    executor *TaskExecutorRouter
    logger   *util.Logger
}

func (sw *SlotWorker) Loop(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        default:
        }
        plan := sw.dequeue(ctx)           // from MQTT
        sw.acquireLease(ctx, plan)        // claim ownership
        result := sw.execute(ctx, plan)   // run task
        sw.saveCheckpoint(ctx, plan)      // persist checkpoint
        sw.emitDownstream(ctx, plan)      // publish downstream ready plans
        sw.releaseLease(ctx, plan)        // mark done
    }
}
```

---

## 5. TaskExecutor Abstraction

```go
// src/engine/executor.go — NEW FILE

type TaskExecutor interface {
    TaskType() TaskType
    Execute(ctx context.Context, plan *ExecutionPlan) (*TaskResult, error)
    // SaveCheckpoint persists incremental execution state.
    // Called by AgentExecutor during long-running agent tasks.
    SaveCheckpoint(ctx context.Context, plan *ExecutionPlan) error
    // RestoreCheckpoint rebuilds execution state from a previous checkpoint.
    RestoreCheckpoint(ctx context.Context, plan *ExecutionPlan) (any, error)
}

// Per-type implementations:

type AgentExecutor struct {
    llmClient  LLMClient
    agents     map[string]*config.AgentDef
    store      Store
}
// → calls LLM, persists message history, checkpoints per task boundary

type ConditionExecutor struct{}
// → evaluates expression, returns result

type ToolExecutor struct {
    mcpClients map[string]MCPClient
}
// → calls MCP tool

type SupervisorExecutor struct {
    llmClient LLMClient
    agents    map[string]*config.AgentDef
    store     Store
}
// → calls LLM, returns action decision

type TribunalExecutor struct{}
// → deterministic vote calculation

type MapExecutor struct{}
// → creates child ExecutionPlans, dispatches them

type JoinExecutor struct{}
// → waits for all map children, aggregates results

type SubflowExecutor struct{}
// → creates ExecutionPlans for sub-agentflow nodes

// TaskExecutorRouter dispatches an ExecutionPlan to the correct executor.
type TaskExecutorRouter struct {
    executors map[TaskType]TaskExecutor
}

func (r *TaskExecutorRouter) Execute(ctx context.Context, plan *ExecutionPlan) (*TaskResult, error) {
    exec, ok := r.executors[plan.TaskType]
    if !ok {
        return nil, fmt.Errorf("no executor for task type %s", plan.TaskType)
    }
    return exec.Execute(ctx, plan)
}
```

---

## 6. Heartbeat + Failover

```go
// src/engine/heartbeat.go — NEW FILE

// HeartbeatPump runs in each TM, periodically publishing heartbeats.
type HeartbeatPump struct {
    tmID    string
    queue   queue.Queue  // publishes to flowgent/heartbeat/
    store   Store
    interval time.Duration
    timeout  time.Duration
}

type HeartbeatMessage struct {
    TMID      string    `json:"tm_id"`
    Timestamp time.Time `json:"timestamp"`
    Load      int       `json:"load"`     // active slot count
    Capacity  int       `json:"capacity"` // total slots
}

// HeartbeatMonitor runs in the JM, consuming heartbeats and detecting failures.
type HeartbeatMonitor struct {
    queue         queue.Queue
    activeTMs     map[string]*TMState
    mu            sync.Mutex
    leaseTimeout  time.Duration
}

type TMState struct {
    TMID       string
    LastBeat   time.Time
    Capacity   int
    Load       int
    IsAlive    bool
}

func (hm *HeartbeatMonitor) Loop(ctx context.Context) {
    // 1. Subscribe to flowgent/heartbeat/#
    // 2. Receive heartbeat messages
    // 3. Update activeTMs map
    // 4. Periodically check for expired leases
    // 5. On lease expiry:
    //    a. Mark TM dead
    //    b. Find all ExecutionPlans assigned to dead TM
    //    c. Reset plans to PENDING, clear AssignedTMID
    //    d. Re-enqueue plans into flowgent/exec/
}
```

### Lease-based failover flow

```
1. TM slot worker dequeues plan from MQTT
2. TM calls: store.ClaimLease(planID, tmID, leaseDuration)
3. TM executes task, updating plan.State and plan.Checkpoint
4. TM calls: store.ReleaseLease(planID, tmID)
5. If TM crashes mid-execution:
   a. Heartbeat stops
   b. JM HeartbeatMonitor detects lease expiration (no heartbeat for 2x interval)
   c. JM marks TM dead
   d. JM finds plans with ExpiredLease for dead TM
   e. JM resets plan state to PENDING, clears AssignedTMID
   f. JM re-enqueues plan into MQTT → another TM picks it up
   g. New TM restores checkpoint, resumes from last checkpoint
```

---

## 7. Checkpoint System

```go
// src/engine/checkpoint.go — NEW FILE

// Checkpointer persists execution state at task boundaries.
// Default: CheckpointPerTask (save at task completion boundary).
type Checkpointer struct {
    store Store
}

func (c *Checkpointer) Save(ctx context.Context, plan *ExecutionPlan) error {
    plan.Checkpoint.CheckpointedAt = time.Now()
    return c.store.SaveCheckpoint(ctx, plan.PlanID, plan.Checkpoint)
}

func (c *Checkpointer) Load(ctx context.Context, planID string) (*TaskCheckpoint, error) {
    return c.store.LoadCheckpoint(ctx, planID)
}
```

### AgentExecutor checkpoint behavior

The AgentExecutor is the only executor that needs incremental checkpointing (LLM calls can be long-running). All other executors are stateless and only checkpoint at task completion.

```
AgentExecutor.Execute():
  1. Restore checkpoint if present (messages, scratchpad, tool state)
  2. Build LLM prompt from checkpoint context
  3. Call LLM
  4. Parse response
  5. Save incremental checkpoint (new messages + updated scratchpad)
  6. If LLM requires tool call:
     a. Execute tool
     b. Save checkpoint with tool_call_state
     c. Continue LLM loop
  7. Return final result
```

---

## 8. Event-Driven DAG Execution

Instead of the JM polling `Ready()` and iterating, downstream task emission is event-driven.

### Current (polling, centralized)

```go
// JM execution loop:
for {
    ready := scheduler.Ready()             // JM computes topological order
    for each nodeID in ready:
        executeNode(nodeID)                // JM triggers execution
        Done(nodeID)                       // JM marks done
}
```

### New (event-driven, decentralized)

```
JM (on StartJob):
  1. Parse spec → build DAG → compute initial ready nodes
  2. For each ready node: create ExecutionPlan → enqueue to MQTT

TM (on task complete):
  1. Execute task
  2. Save result to plan.Result
  3. Compute downstream ready nodes (dependencies satisfied?)
  4. For each newly-ready downstream node: create ExecutionPlan → enqueue to MQTT
  5. Publish status update to flowgent/status/

JM (on status update):
  1. Receive status update
  2. Update AgentFlowRun state
  3. If all plans complete → mark run COMPLETED
  4. If any plan failed → mark run FAILED
```

This eliminates the centralized polling loop in JM. The DAG progresses through TMs pushing the next wave of ready tasks into MQTT.

---

## 9. Custom Metrics — Production Observability

All metrics use the OTEL `metric.MeterProvider` already configured. Each component
declares its own `metric.Meter` (named `"flowgent/<component>"`) and registers
instruments in its constructor. No global metrics registry — each component owns
its own metric space, maximizing cohesion and avoiding import cycles.

### 9.1 Meter Hierarchy

```
flowgent/jobmanager    — JM lifecycle, dispatch, failover
flowgent/taskmanager   — TM slot utilization, heartbeat
flowgent/executor      — per-task-type execution timing
flowgent/queue         — MQTT queue depth, latency
flowgent/checkpoint    — checkpoint save/load, size
```

### 9.2 JobManager Metrics (`src/engine/jobmanager_metrics.go`)

All declared in `NewJobManager()` constructor, updated inline during operations.

| Name | Type | Labels | Description |
|------|------|--------|-------------|
| `flowgent.jm.runs.total` | Int64 Counter | `agentflow_id`, `status` | Total runs started/finished |
| `flowgent.jm.runs.active` | Int64 UpDownCounter | — | Currently running agentflows |
| `flowgent.jm.plans.dispatched` | Int64 Counter | `agentflow_id` | ExecutionPlans enqueued to MQTT |
| `flowgent.jm.plans.completed` | Int64 Counter | `agentflow_id`, `status` | Plans that reached terminal state |
| `flowgent.jm.failover.events` | Int64 Counter | `tm_id` | TM death → plan requeue events |
| `flowgent.jm.dag.build.duration` | Float64 Histogram | `agentflow_id` | DAG parsing + plan creation time (ms) |
| `flowgent.jm.lease.expirations` | Int64 Counter | `tm_id` | Lease expiry detections |

### 9.3 TaskManager Metrics (`src/engine/taskmanager_metrics.go`)

Emitted by the SlotWorker loop and heartbeat pump.

| Name | Type | Labels | Description |
|------|------|--------|-------------|
| `flowgent.tm.slots.busy` | Int64 UpDownCounter | `tm_id` | Currently occupied slots |
| `flowgent.tm.slots.total` | Int64 ObservableGauge | `tm_id` | Total slot capacity |
| `flowgent.tm.tasks.executed` | Int64 Counter | `tm_id`, `task_type` | Total tasks executed |
| `flowgent.tm.tasks.duration` | Float64 Histogram | `tm_id`, `task_type` | Task wall-clock time (ms) |
| `flowgent.tm.heartbeat.interval` | Float64 Histogram | `tm_id` | Actual heartbeat interval (ms) |
| `flowgent.tm.heartbeat.missed` | Int64 Counter | `tm_id` | Heartbeat send failures |
| `flowgent.tm.dequeue.latency` | Float64 Histogram | `tm_id` | MQTT dequeue→acquire time (ms) |
| `flowgent.tm.lease.acquire.errors` | Int64 Counter | `tm_id` | Lease claim failures (conflict) |

### 9.4 Executor Metrics (`src/engine/executor_metrics.go`)

Per `TaskExecutor` — instruments decorated transparently around `Execute()`.

| Name | Type | Labels | Description |
|------|------|--------|-------------|
| `flowgent.exec.tasks.total` | Int64 Counter | `task_type`, `status` | Execution attempts by outcome |
| `flowgent.exec.tasks.duration` | Float64 Histogram | `task_type` | Execute() time (ms) |
| `flowgent.exec.llm.calls` | Int64 Counter | `task_type`, `model` | LLM API calls per executor |
| `flowgent.exec.llm.latency` | Float64 Histogram | `task_type`, `model` | LLM round-trip (ms) |
| `flowgent.exec.llm.tokens` | Int64 Counter | `task_type`, `model`, `direction` | Token usage (input/output) |
| `flowgent.exec.checkpoint.bytes` | Int64 Histogram | `task_type` | Checkpoint payload size (bytes) |
| `flowgent.exec.checkpoint.duration` | Float64 Histogram | `task_type` | Checkpoint save time (ms) |
| `flowgent.exec.agent.iterations` | Int64 Histogram | `task_type` | LLM/tool loop iterations per task |

### 9.5 Queue Metrics (`src/queue/metrics.go`)

Instrumented at the `Queue` interface level — a thin wrapper that decorates `Push` / `Dequeue`.

| Name | Type | Labels | Description |
|------|------|--------|-------------|
| `flowgent.queue.depth` | Int64 UpDownCounter | `topic` | Messages in queue (push - ack) |
| `flowgent.queue.push.total` | Int64 Counter | `topic` | Total messages pushed |
| `flowgent.queue.dequeue.total` | Int64 Counter | `topic`, `consumer_group` | Total messages dequeued |
| `flowgent.queue.ack.total` | Int64 Counter | `topic` | Successful acks |
| `flowgent.queue.nack.total` | Int64 Counter | `topic` | Failed → requeue events |
| `flowgent.queue.dequeue.latency` | Float64 Histogram | `topic` | Time spent in queue before pickup (ms) |

### 9.6 Checkpoint Metrics (`src/engine/checkpoint_metrics.go`)

Emitted by the `Checkpointer` component.

| Name | Type | Labels | Description |
|------|------|--------|-------------|
| `flowgent.checkpoint.saves` | Int64 Counter | `task_type` | Checkpoint save operations |
| `flowgent.checkpoint.loads` | Int64 Counter | `task_type` | Checkpoint restore operations |
| `flowgent.checkpoint.duration` | Float64 Histogram | `task_type`, `op` | Save/load duration (ms) |
| `flowgent.checkpoint.bytes` | Int64 Histogram | `task_type` | Payload bytes per checkpoint |

### 9.7 Design Rules

1. **No global registry.** Each `Meter` is created via `otel.Meter("flowgent/<component>")` in the component constructor. No import cycle risk — metrics files import only `otel/metric`.

2. **Instrumented at source.** JM dispatches a plan → counter is incremented right there, not in a callback. TM finishes a task → histogram recorded in `SlotWorker` loop.

3. **Minimal overhead in hot paths.** Histograms use explicit boundaries matching expected ranges (e.g., task duration: `[10, 50, 100, 500, 1000, 5000, 30000, 60000]` ms). No string allocations for labels that can be pre-computed.

4. **Metrics file per component.** `<component>_metrics.go` lives next to `<component>.go`:
   ```
   src/engine/jobmanager.go
   src/engine/jobmanager_metrics.go
   src/engine/taskmanager.go
   src/engine/taskmanager_metrics.go
   ...
   ```

5. **Export via Prometheus HTTP.** OTEL SDK already exposes via OTLP; add a lightweight
   `promhttp` handler on the management port (`:9991`) for direct Prometheus scraping:
   ```go
   // src/api/metrics.go
   import "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
   // Expose on mgmt server: /metrics → Prometheus text format
   ```

6. **No separate metrics collection service.** Just Prometheus scrape endpoint + OTEL
   OTLP export. The Grafana/Jaeger dashboards consume from the same OTEL pipeline.

### 9.8 Example: TaskManager SlotWorker with Metrics

```go
func (sw *SlotWorker) Loop(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        default:
        }

        dequeueStart := time.Now()
        plan := sw.dequeue(ctx)
        sw.metrics.DequeueLatency.Record(ctx, float64(time.Since(dequeueStart).Milliseconds()))

        sw.metrics.SlotsBusy.Add(ctx, 1)

        execStart := time.Now()
        result, err := sw.Execute(ctx, plan)
        sw.metrics.TaskDuration.Record(ctx, float64(time.Since(execStart).Milliseconds()),
            metric.WithAttributes(attribute.String("task_type", string(plan.TaskType))),
        )

        sw.metrics.SlotsBusy.Add(ctx, -1)
        sw.emitDownstream(ctx, plan)
    }
}
```

### 9.9 Configuration: Converge under `mgmt`

All observability config lives under `mgmt` — no scattered top-level keys.

Current config:
```yaml
mgmt:
  enabled: true
  host: 0.0.0.0
  port: 9991
  pprof:
    enabled: true
    server-bind: "0.0.0.0:6669"
  otel:
    enabled: true
    endpoint: "http://localhost:4317"
    protocol: grpc
    timeout: 10000
```

New `mgmt.metrics` section + `mgmt.otel` extended:

```yaml
mgmt:
  enabled: true
  host: 0.0.0.0
  port: 9991

  pprof:
    enabled: true
    server-bind: "0.0.0.0:6669"

  otel:
    enabled: true
    endpoint: "http://localhost:4317"
    protocol: grpc
    timeout: 10000
    sample_rate: 1.0             # NEW: trace sampling rate (1.0 = always)

  metrics:                        # NEW: metrics-specific config
    enabled: true                 #   enable/disable all metrics collection
    prometheus: true              #   expose /metrics on mgmt port (Prometheus scrape)
    export_interval: 15s          #   OTEL periodic reader interval
    histogram_boundaries:         #   default histogram buckets (ms)
      task: [10, 50, 100, 500, 1000, 5000, 30000, 60000]
      llm:   [100, 500, 1000, 5000, 15000, 30000, 60000, 120000]
      queue: [1, 5, 10, 50, 100, 500, 1000]
    labels:                       #   optional fixed labels applied to all metrics
      cluster: "production"
      region: "us-east-1"
```

Go structs:

```go
// src/model/config.go

type MgmtConfig struct {
    Enabled bool          `json:"enabled" yaml:"enabled"`
    Host    string        `json:"host" yaml:"host"`
    Port    int           `json:"port" yaml:"port"`
    PProf   PProfConfig   `json:"pprof" yaml:"pprof"`
    OTEL    OTELConfig    `json:"otel" yaml:"otel"`
    Metrics MetricsConfig `json:"metrics" yaml:"metrics"`  // NEW
}

type OTELConfig struct {
    Enabled    bool    `json:"enabled" yaml:"enabled"`
    Endpoint   string  `json:"endpoint" yaml:"endpoint"`
    Protocol   string  `json:"protocol" yaml:"protocol"`
    Timeout    int     `json:"timeout" yaml:"timeout"`
    SampleRate float64 `json:"sample_rate" yaml:"sample_rate"` // NEW
}

type MetricsConfig struct {                                    // NEW
    Enabled             bool                `json:"enabled" yaml:"enabled"`
    Prometheus          bool                `json:"prometheus" yaml:"prometheus"`
    ExportInterval      time.Duration       `json:"export_interval" yaml:"export_interval"`
    HistogramBoundaries MetricsBoundaries   `json:"histogram_boundaries" yaml:"histogram_boundaries"`
    Labels              map[string]string   `json:"labels" yaml:"labels"`
}

type MetricsBoundaries struct {                                // NEW
    Task  []float64 `json:"task" yaml:"task"`
    LLM   []float64 `json:"llm" yaml:"llm"`
    Queue []float64 `json:"queue" yaml:"queue"`
}
```

**Usage at startup** (`src/common/tracing/provider.go`):

```go
func NewMeterProvider(cfg *config.MetricsConfig, res *resource.Resource) (*metric.MeterProvider, error) {
    if !cfg.Enabled {
        return metric.NewMeterProvider(), nil // no-op
    }

    opts := []metric.PeriodicReaderOption{
        metric.WithInterval(cfg.ExportInterval),
    }

    // OTLP exporter
    exporter, _ := otlpmetricgrpc.New(ctx)
    reader := metric.NewPeriodicReader(exporter, opts...)

    // Optional Prometheus endpoint (served on mgmt port, no reader needed)
    if cfg.Prometheus {
        // promhttp handler registered on mgmt mux separately
    }

    return metric.NewMeterProvider(
        metric.WithResource(res),
        metric.WithReader(reader),
    ), nil
}
```

**Design rules:**
1. All metrics config lives under `mgmt.metrics` — no top-level `metrics:` key
2. `mgmt.otel` extended only for trace-specific settings (`sample_rate`)
3. `MetricsBoundaries` allows per-component histogram tuning from YAML
4. `labels` injects fixed key=value pairs into every metric (cluster, region, env)
5. When `mgmt.metrics.enabled: false`, all metric instruments are no-ops (zero overhead)
6. Prometheus endpoint served on the existing mgmt port (`:9991/metrics`), no new port

---

## 10. File Change Summary

### New files

```
src/model/execution_plan.go          — ExecutionPlan, TaskCheckpoint, NodeSpec types
src/engine/executor.go               — TaskExecutor interface + per-type implementations
src/engine/executor_metrics.go       — Per-task-type execution metrics
src/engine/checkpoint.go             — Checkpointer for resume-after-failure
src/engine/checkpoint_metrics.go     — Checkpoint save/load metrics
src/engine/heartbeat.go              — HeartbeatPump (TM) + HeartbeatMonitor (JM)
src/engine/slot_worker.go            — SlotWorker persistent consume loop
src/engine/jobmanager_metrics.go     — JM dispatch/failover metrics
src/engine/taskmanager_metrics.go    — TM slot/heartbeat/dequeue metrics
src/queue/metrics.go                 — Queue depth/latency/ack metrics (decorator)
src/common/tracing/provider.go       — OTEL provider init (moved from cmd)
src/cmd/taskmanager-runner/main.go   — K8s Deployment pod entry point
deploy/kubernetes/tm-deployment.yaml — TM K8s Deployment manifest
```

### Deleted files

```
src/engine/kubernetes_scheduler.go   — no more K8s batch Jobs
src/engine/local_scheduler.go        — no more in-process scheduler
src/engine/scheduler.go              — interface no longer needed
src/cmd/tasklet/main.go             — renamed to taskmanager-runner
```

### Modified files

```
src/model/run.go                    — add SharedMemory, ExecPlans to AgentFlowRun
src/model/agentflow.go              — minor additions
src/config/config.go                — MgmtConfig.Metrics, OTELConfig.SampleRate, MetricsConfig, MetricsBoundaries
src/engine/jobmanager.go            — refactor: control-plane only
src/engine/taskmanager.go           — refactor: slot workers + heartbeat
src/engine/map_runner.go            — integrate into MapExecutor
src/engine/testing.go               — update for new architecture
src/engine/store interfaces         — add Checkpoint/Lease methods
src/store/postgres.go               — implement Checkpoint/Lease methods
src/store/sqlite.go                 — implement Checkpoint/Lease methods
src/cmd/core/launch.go              — renamed from run.go, simplified
src/cmd/core/otel.go                — deleted, moved to src/common/tracing/
```

---

## 11. Implementation Order (5 phases)

### Phase 1: Model + Types (foundation)
1. Create `src/model/execution_plan.go`
2. Extend `AgentFlowRun` with `SharedMemory`, `ExecPlans`
3. Create `src/engine/executor.go` — TaskExecutor interface
4. Define `NodeSpec` conversion from existing `Node`

### Phase 2: TaskManager refactor
1. Refactor `taskmanager.go` — add SlotWorker, heartbeat
2. Implement per-type executors (Agent, Condition, Tool, etc.)
3. Create `slot_worker.go` — persistent consume loop
4. Create `heartbeat.go` — HeartbeatPump + monitor

### Phase 3: JobManager refactor
1. Refactor `jobmanager.go` — control-plane only
2. Build ExecutionPlans from AgentFlowSpec
3. Dispatch into MQTT (no more inline execution)
4. Consume status updates from MQTT
5. Remove `local_scheduler.go`, `kubernetes_scheduler.go`, `scheduler.go`

### Phase 4: Checkpoint + Failover
1. `checkpoint.go` — store integration
2. AgentExecutor checkpoint support
3. Lease claim/release in store
4. HeartbeatMonitor expiry detection + plan requeue

### Phase 5: CMD + Config + Deploy cleanup
1. Add `MetricsConfig` + `MetricsBoundaries` to `src/config/config.go`; extend `MgmtConfig` + `OTELConfig`
2. Refactor `src/common/tracing/provider.go` to consume `MetricsConfig` + `OTELConfig` (sample rate, boundaries, Prometheus)
3. Rename `cmd/core/run.go` → `launch.go`
4. Move `otel.go` → `src/common/tracing/`
5. Rename `cmd/tasklet/` → `cmd/taskmanager-runner/`
6. TM Runner reads config + starts `TaskManager.Start()`
7. K8s Deployment manifest for TM pods
8. Update all tests
9. Update all `.agent/` docs

---

## 12. Sizing Estimates

| Component | Lines (approx) | Complexity |
|-----------|----------------|------------|
| `execution_plan.go` | ~120 | Low |
| `executor.go` (interface + impls) | ~400 | Medium |
| `slot_worker.go` | ~80 | Low |
| `heartbeat.go` | ~200 | Medium |
| `checkpoint.go` | ~100 | Low |
| `jobmanager.go` refactor | ~300 delta | High |
| `taskmanager.go` refactor | ~200 delta | Medium |
| CMD + deploy cleanup | ~100 | Low |
| **Total net new** | **~1500** | |

---

## 13. Dependencies

```
No new external dependencies needed:
- MQTT: already in go.mod (paho.mqtt.golang)
- Postgres/SQLite: already in go.mod
- OTEL: already in go.mod
- k8s.io/client-go: already added (for optional deploy, not needed in core)
```
