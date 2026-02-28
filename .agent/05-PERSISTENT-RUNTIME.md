# Persistent Distributed Agent Runtime — Architecture & Implementation

**Date:** 2026-05-13
**Status:** Implemented

Flowgent is a persistent distributed agent runtime, not a K8s job orchestrator. TM pods are long-lived Deployments; dispatch is MQTT-centric; execution is event-driven.

---

## 1. Architecture Layers

```
[API / Trigger / Cron]
        │
        ▼
  JobManager (control plane, per-agentflow-run)
    ├── builds DAG → creates ExecutionPlans
    ├── calls Scheduler.EnsureCapacity(neededSlots)
    └── dispatches plans via Scheduler.SubmitTask()
        │
  Scheduler (TM slot management)
    ├── LocalScheduler: goroutine pool (all-in-one mode)
    └── KubernetesScheduler: scale K8s Deployment (production)
        │
  MQTT (event bus)
    ├── /flowgent/{flowId}/exec/{runId}/{planId}     — dispatch
    ├── /flowgent/{flowId}/status/{runId}/{planId}   — status
    └── /flowgent/heartbeat/{tmId}                  — liveness
        │
  TaskManager (persistent worker, K8s Deployment pod)
    ├── SlotWorker pool (N goroutines consuming from MQTT)
    ├── TaskExecutorRouter (agent/condition/tool/tribunal/etc.)
    └── HeartbeatPump (periodic liveness)
```

## 2. Key Types

### ExecutionPlan — primary runtime object
- Serializable, resumable, retryable, reassignable between TMs
- Contains: PlanID, TaskType, NodeSpec, Input, Checkpoint, Lease
- Flows through MQTT; TM doesn't need original AgentFlowSpec

### NodeSpec — embedded in ExecutionPlan
- Decouples plan from spec — TM only needs the plan

### TaskCheckpoint — agent resume state
- Messages, scratchpad, tool_call_state, LastStep
- Default: CheckpointPerTask boundary

### TaskExecutor — per-type execution
- AgentExecutor, ConditionExecutor, ToolExecutor, SupervisorExecutor
- TribunalExecutor, MapExecutor, JoinExecutor, SubflowExecutor, HumanExecutor

## 3. MQTT Topics (IMPORTANT)

```
/flowgent/{agentFlowId}/
  exec/{runId}/{planId}       — JM→TM dispatch
  status/{runId}/{planId}     — TM→JM completion status
  checkpoint/{runId}/{planId} — incremental checkpoint
/flowgent/heartbeat/{tmId}    — TM liveness
```

### Async Tracing (W3C TraceContext)
- Inject `traceparent` + `tracestate` into MQTT message headers
- Extract on receive → create child span
- On retry: **same traceId, new spanId** — Jaeger shows full execution chain

## 4. Scheduler + TM Lifecycle

JM never executes tasks directly. Flow:
1. JM: `scheduler.EnsureCapacity(readyPlanCount)`
2. LocalScheduler: returns goroutine pool cap
3. KubernetesScheduler: scales TM Deployment up if needed (`kubectl scale deploy/{name} --replicas=N`)
4. JM: `scheduler.SubmitTask(plan)` per ready node
5. Local: calls TM.ExecutePlan() inline; K8s: publishes plan to MQTT
6. TM slot worker: dequeue → TaskExecutorRouter.Execute → emit downstream ready plans
7. HeartbeatMonitor detects expired TMs → requeues plans → another TM resumes from checkpoint

### Deployment Modes
- `flowgent daemon` → all-in-one: JM + LocalScheduler + API + inline TM (SQLite, memory queue)
- `flowgent daemon --mode=k8s` → JM + KubernetesScheduler + API (Postgres, MQTT)
- `flowgent taskmanager` → TM pod process (K8s Deployment)

## 5. File Map

| File | Role |
|------|------|
| `src/model/execution_plan.go` | ExecutionPlan, NodeSpec, TaskCheckpoint |
| `src/engine/jobmanager.go` | Control-plane JM (DAG + dispatch) |
| `src/engine/scheduler.go` | Scheduler interface |
| `src/engine/local_scheduler.go` | Goroutine pool (all-in-one) |
| `src/engine/kubernetes_scheduler.go` | K8s Deployment scale + MQTT dispatch |
| `src/engine/taskmanager.go` | Persistent TM with slot workers + heartbeat |
| `src/engine/executor.go` | TaskExecutor interface + 9 implementations |
| `src/engine/slot_worker.go` | Dequeue→execute→emit loop |
| `src/engine/heartbeat.go` | HeartbeatPump + HeartbeatMonitor |
| `src/engine/checkpoint.go` | Checkpoint save/load |
| `src/engine/*_metrics.go` | Per-component OTEL meters |
| `src/queue/metrics.go` | Queue instrumentation decorator |
| `src/api/server.go` | REST + A2A route registration |
| `src/common/tracing/provider.go` | OTEL init |
| `src/cmd/core/launch.go` | Main entry: init modules → start JM + API |

## 6. Metrics

Per-component OTEL meters: `flowgent/jobmanager`, `flowgent/taskmanager`, `flowgent/executor`, `flowgent/queue`, `flowgent/checkpoint`. Config under `mgmt.metrics` + `mgmt.otel.sample_rate`.

## 7. Test Coverage

14/14 engine unit tests. Covers: DAG topology, parallel ready, skip, fail, inject, edge conditions, condition results, retry backoff, state machine transitions.
