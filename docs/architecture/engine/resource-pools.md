# Resource Pools

[System overview](../overview.md) · [Controller](controller.md) · [JobManager](jobmanager.md) · [TaskManager](taskmanager.md)

A Resource Pool is the namespace-scoped scheduling and capacity boundary for
Flow execution. Every Flow declares exactly one `resource_pool_id`; every FlowRun snapshots that value
when it is created.

## Contract

```yaml
data:
  id: security-autonomy-fixer
  resource_pool_id: critical
```

```json
{
  "name": "critical",
  "replicas": 3,
  "slots_per_pod": 4,
  "resources": {"cpu": "1000m", "memory": "2Gi"},
  "sandbox_replicas": 2,
  "sandbox_slots_per_pod": 2,
  "sandbox_resources": {"cpu": "2000m", "memory": "4Gi"},
  "priority_class_name": "flowgent-critical",
  "node_selector": {"workload.flowgent.io/tier": "critical"}
}
```

Pool names and Flow names use the public resource-name contract:
`^[A-Za-z][A-Za-z0-9_-]{0,31}$`. Names are unique case-insensitively inside a
namespace. A pool cannot be deleted while a Flow or active run references it,
and a Flow cannot change pools while it has a PENDING, RUNNING, or PAUSED run.

## Runtime topology

```text
namespace
├── Flow A ─ dedicated JobManager ─┐
├── Flow B ─ dedicated JobManager ─┼─► pool critical TM/Sandbox Deployments
└── Flow C ─ dedicated JobManager ────► pool economy TM/Sandbox Deployments
```

The Controller creates a dedicated JobManager only for a Flow with an active
run. TaskManager and Sandbox Deployments are shared by all Flows bound to the
same namespace/pool and are labelled:

```yaml
flowgent.io/namespace: default
flowgent.io/resource-pool: critical
flowgent.io/managed-by: resource-pool
```

ExecutionPlan and SandboxTrigger MQTT topics include both namespace and pool.
Workers use a shared subscription scoped to those two values and reject a
payload whose embedded scope differs. A worker can therefore consume only work
assigned to its own pool; priority and SLA capacity do not depend on consumers
filtering an already-shared queue.

Pool edits roll active JobManagers by a deterministic pool checksum. The new
JobManager reconciles the complete worker pod template: image, slot count,
resources, PriorityClass, node selector, environment, volumes, and labels.
Pool workers are created lazily on demand and remain available for later runs;
the Controller removes them after the pool is deleted.

## Configuration and secrets

Resource pools configure capacity and placement only. Namespace and Flow
environment/secrets remain in the runtime-configuration API. A shared
TaskManager resolves effective namespace→Flow configuration for each Sandbox
attempt and passes it only in that attempt's trigger. It never installs
Flow-specific values into the long-lived worker process environment. Secret
values are write-only to humans, available only through `flow.runtime.use` to
internal Controller/TaskManager service identities, and are never logged.

## Invariants

1. A FlowRun's `resource_pool_id` is immutable after creation.
2. A plan is published only to `namespace/resource-pool` scoped topics.
3. A TaskManager or Sandbox worker rejects a mismatched pool payload.
4. Pool capacity and placement updates reconcile the whole pod template.
5. Pool deletion is blocked by both definition and active-run references.
6. Flow-specific secrets are resolved per attempt and never become shared-pool
   process state.
