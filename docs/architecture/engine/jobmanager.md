# JobManager

[System overview](../overview.md) · [TaskManager](taskmanager.md) · [Sandbox](sandbox.md)

JobManager is the per-run DAG orchestrator. It reads flow/run state through the
API Server, turns runnable DAG nodes into `ExecutionPlan` messages, consumes
state-only execution results, and owns TaskManager and Sandbox capacity through
a pluggable ResourceManager. It never connects to the database directly.

```text
API Server REST ──► JobManager / JobMaster
                         │
                         ├── ExecutionPlan ──MQTT──► TaskManager
                         ├── exec/result ◄──MQTT──── TaskManager
                         └── scale / cleanup ──────► TM + Sandbox deployments
```

## DAG Orchestration

The JM is the singleton control plane for one active Flow. It polls
the apiserver REST API (`FlowgentClient.ListRuns`) for PENDING runs (with namespace
filtering), parses the `AgentFlowSpec` JSON, and spawns a **JobMaster** per run.
Each JobMaster builds a DAG from `AgentFlowSpec.Nodes` + `Edges`, persists execution
state via the apiserver REST API, and executes nodes in topological order via the
ResourceManager.

### JobMaster — Per-Run DAG Orchestrator

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
    result := scheduleNodeWithRetry(ctx, plan)
      → before each attempt: persist a distinct PENDING TaskRun via REST
      → rm.Schedule(ctx, plan)            // dispatch to TM (MQTT or standalone)
      → persist attempt output/error and retry if the node policy permits
    state.UpdateRun(ctx, run)             // update run status via apiserver REST
    Done(nodeID)
    if condition → SetConditionResult → Skip(false-branch)
    if supervisor → validate action → Inject/Retry/Abort

  // Step 3: Finalize
  → run.Status = COMPLETED | FAILED
  → Persist final state via apiserver REST
```

JobMaster is the authoritative owner of execution timestamps. It sets
`started_at` on the transition into execution and `finished_at` on every terminal
transition, then sends status, error, and both timestamps in one lifecycle update
through the API Server. The Controller and browser may create or observe a run,
but must not synthesize execution time. The API rejects an update whose finish
time precedes its start time.

### DAG State Methods

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

### JobManager Scope

The Controller starts `jobmanager start --flow-id <id>`. `StartRunPoller`
filters `ListRuns` by that Flow ID and Kubernetes namespace, loading the Flow
through the API Server when necessary. Before scheduling, the JobManager uses
the FlowRun's immutable `resource_pool_id` snapshot rather than a later Flow
edit.

### DAG Dependency Coordination — Iteration Loop + Blocking Schedule

The JM is the **sole dependency controller** for a flow run. It does not just
fire off ExecutionPlans and hope they execute in order — it actively enforces
the DAG's topological constraints through a **two-level loop pattern**: an outer
iteration loop that repeatedly discovers newly-ready nodes, and an inner per-node
blocking `Schedule()` that waits for the TM to complete each node before
dispatching the next.

Here, an **iteration** is a scheduler readiness pass. It does not reset node
state: once a node is completed or skipped, a back-edge cannot make it execute
again in the same run. See the
[AgentFlow loop limitation](../agent-flow.md#flexibility-and-current-limits).

#### Execution Loop (Pseudocode)

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
      result := scheduleNodeWithRetry(plan)
        state.SaveTask(attempt)      // one durable row per attempt
        rm.Schedule(plan)            // ← BLOCKING: waits for TM result
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

#### Node Attempts and Retry Ownership

JobManager owns node-level retry because only the DAG orchestrator can decide
whether a failed node may be re-dispatched without advancing its children. With
no node retry policy, a node executes exactly once. With a policy, it executes
at most `retry.max + 1` attempts using context-cancellable exponential backoff.

Each attempt has a distinct bounded task ID, a one-based `sequence`, a zero-based
`retry_count`, and `parent_task_run_id` pointing to the prior failed attempt.
The resolved input, exact output/error and timestamps are persisted for every
attempt through API Server REST. The ExecutionPlan carries the parent attempt ID
to TaskManager so the retry chain remains intact even if JobManager fails after
the worker writes its result.

Every attempt also creates a `jobmaster.node.attempt` span. Its attributes carry
run/node/task/attempt IDs and bounded input/output metadata, not payload content.
Executor-internal correction loops, such as model schema correction, are nested
spans and do not create extra TaskRun rows because they are not DAG-node retries.

#### Dependency Resolution — `depsDone()` Algorithm

```
depsDone(node):
  if node has zero dependencies → true (root node)

  classify every inbound edge:
    unconditional + completed source → active
    unconditional + pending source   → unresolved (block)
    conditional + unevaluated source → dormant
    conditional + matching result    → active
    conditional + nonmatching result → inactive
    skipped source                    → inactive

  if any active edge exists → ready (dormant feedback does not block it)
  if no active edge and a dormant edge exists → unresolved (wait)
  if every edge is inactive → resolved-inactive (skip node)
```

**Conditional edge handling**: `Ready()` evaluates edge activity and propagates
resolved-inactive nodes to `SKIPPED` until a fixed point. A convergence node
remains reachable when at least one inbound edge is active, while an unselected
branch and all of its single-path descendants are skipped. A dormant conditional
feedback edge does not block an already-active initial path, but it cannot make
a node ready by itself. This is scheduling compatibility only: a completed DAG
node is never re-executed, so conditional back-edges do not implement iteration;
iterative workflows require an explicit future loop/subflow contract.

#### K8s Mode: Subscribe-Before-Publish + Per-Node Channel Routing

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

#### Round-Trip Timing Diagram (Single Node)

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

#### Edge Cases

| Scenario | Behavior |
|----------|----------|
| **Root nodes (no deps)** | `Ready()` returns them immediately — first iteration |
| **Parallel siblings** | All siblings become `Ready()` in the same iteration after their shared parent is `Done()`. Dispatched sequentially but each blocks independently. |
| **Condition node → nonmatching branch** | `Ready()` propagates inactive edges to `SKIPPED` at a fixed point; merge nodes still run when another inbound edge is active. |
| **Node fails** | `Fail(node)` — children stay blocked (never become ready). `HasFailed()` → flow terminates. |
| **Deadlock** (Ready=∅, !IsComplete, !HasFailed) | Some nodes have permanently unsatisfied deps (e.g. a dependency was never created). Loop breaks, returns nil — nodes left in PENDING. |
| **TM timeout** | `planTimeout` (default 5 min) — `Schedule()` returns error, node is marked failed, flow terminates. |
| **TM crash mid-execution** | JM heartbeat monitor detects dead TM after 30s. Orphaned plans are re-dispatched (failover). |

---


## Resource Management and Scheduling

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

### StandaloneResourceManager (all-in-one mode)

Bounded in-process goroutine pool using a channel semaphore (`make(chan struct{},
poolSize)`). `Schedule()` acquires a slot via non-blocking select — if all slots are
busy, returns `INSUFFICIENT_RESOURCES` immediately. When a slot is acquired, calls
`tm.ExecutePlan()` synchronously in the same goroutine.

### KubernetesResourceManager (production mode)

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
next is dispatched), not concurrently. See
[DAG Dependency Coordination](#dag-dependency-coordination-iteration-loop-blocking-schedule)
for the full design.

**TM pod management**: Reconciles the selected Pool Deployment
`flowgent-taskmanager-{namespaceId}-{poolId}`. It applies the Pool's fixed
replicas, slots, resources, PriorityClass, and NodeSelector, waits for runtime
readiness, then publishes to the namespace/pool topic.

**Sandbox pod management**: The same K8sRM reconciles
`flowgent-sandbox-{namespaceId}-{poolId}` from Pool sandbox replicas, resources,
and slots. Each pod's shared subscription is scoped to namespace/pool.

A `scalingLoop` reconciles observed replicas and readiness. Pool capacity is an
explicit operator-owned SLA boundary; a Flow cannot silently burst into another
Pool.

---

## Expected Behavior

1. **Given** a flow run, **when** JobMaster builds its graph, **then** every
   configured node MUST map to one stable ExecutionPlan identity for that run.
2. **Given** unsatisfied dependencies, **when** readiness is evaluated, **then**
   the dependent node MUST remain pending; root and fully satisfied nodes alone
   may become ready.
3. **Given** a conditional edge, **when** its source resolves, **then** only the
   matching branch may execute and the opposite direct child MUST be skipped.
4. **Given** a dispatched plan, **when** its result is delayed or misrouted,
   **then** JobManager MUST wait for the matching `(runID,nodeID)` result and
   MUST fail on the configured timeout rather than advancing the DAG.
5. **Given** task or sandbox demand, **when** capacity is reconciled, **then**
   replicas, slots, resources, PriorityClass, and NodeSelector MUST match the
   run's selected Resource Pool.
6. **Given** a failed node, **when** the execution loop observes failure, **then**
   the run MUST become failed and children MUST NOT execute as if successful.
7. **Given** several ready siblings, **when** the current scheduler executes
   them, **then** observable dispatch MUST remain sequential until concurrent
   scheduling is explicitly implemented and tested.
8. **Given** a node retry policy, **when** an attempt fails, **then** JobManager
   MUST persist that failed attempt before backoff and MUST use a distinct,
   parent-linked TaskRun for the next attempt.
