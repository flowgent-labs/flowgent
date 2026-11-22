# Controller

[System overview](../overview.md) · [JobManager](jobmanager.md)

**Sandbox target (2026-11-14):** Controller also reconciles registered Sandbox
pools by namespace + `sandbox_id`, independently of active GraphRuns. The
Sandbox ownership/GC statements below describe the current runtime-cluster
implementation. The [fixed-pool contract](sandbox.md) supersedes those
statements for the pending migration.

The Controller owns distributed runtime reconciliation. It discovers AgentGraphs,
active application-mode runs through the API Server, shards ownership across
controller replicas, creates/removes per-GraphRun application JobManagers, and
garbage-collects application runtime Deployments. It never connects to the
database directly.

```text
API Server REST + MQTT lifecycle events
                  │
                  ▼
          Controller shard
                  │
                  ├── create / reconcile ──► per-run application JobManager
                  └── garbage-collect ─────► application JM + TM/Sandbox
```

Deployment namespaces, pod naming, and lifecycle timing are documented in the
[system overview](../overview.md#deployment-namespaces-and-pod-naming).

## Reconciliation and Dispatch

Polls the apiserver REST API (`FlowgentClient.ListAgentGraphs`) for agentgraph definitions,
shards across pods via hash-mod, and ensures one dedicated K8s JobManager only
when an owned AgentGraph has an active run. Cron callbacks create a PENDING run through
the API before reconciliation. The Controller also subscribes to MQTT lifecycle
events for prompt AgentGraph updates. It never calls a JobManager directly.

Before reconciling an active application AgentGraph runtime, the Controller resolves its effective
namespace→AgentGraph runtime configuration through the workload-only API. Public values
are upserted into a per-AgentGraph ConfigMap and secrets into a per-AgentGraph Secret; plaintext
secrets are never logged or stored in workload manifests. A deterministic content
checksum rolls the application JM. The application JM's K8sRM then reconciles
TM/Sandbox Deployments scoped by the run's `runtime_cluster_id`.

### Hash-Mod Sharding

```
shard(agent_graph_id) = fnv64a(agent_graph_id) % total_controller_pods
```

Each pod discovers total count via `IDiscoveryClient` (K8s label selector or env
vars). Only processes graphs where `shard == pod_index`.

### Reconciliation Loop

```
Every 10s:
  1. IDiscoveryClient.DiscoverPeers(labelSelector)
  2. apiClient.ListAgentGraphs(namespace)
  3. For each graph where shard(agent_graph_id) == my_index:
     a. Inspect PENDING/RUNNING/PAUSED application runs
     b. For active application runs, create/reconcile their dedicated JobManager
  4. Re-register owned cron triggers
  5. Garbage-collect idle/deleted application JMs and application TM/Sandbox workers
```

### Dispatch Detail

| Binding | Controller Action | Who Executes |
|---------|-------------------|--------------|
| `AgentGraph.runtime_mode=application` | Preserve Run snapshot, ensure dedicated active-run JM with cluster id `app-{runId}` | TM/Sandbox workers whose subscription and pod labels match namespace + runtime cluster |
| `AgentGraph.runtime_mode=session` | Do not create a JM; session runs are consumed by the Helm session JM | Session TM/Sandbox workers whose labels match the session cluster id |

### Dual Format: Static YAML vs DB JSON

All deployments use `model.AgentGraphSpec` (dual-tagged `json:` + `yaml:`). Static YAML
loaded at startup + hot-reload. DB JSON saved by UI via API, polled by Controller.

---

## Expected Behavior

1. **Given** multiple Controller replicas, **when** they share one peer snapshot,
   **then** hash-mod sharding MUST assign each graph to exactly one replica per
   reconciliation tick.
2. **Given** an imported graph with no active run, **when** Controller reconciles,
   **then** it MUST NOT create JobManager, TaskManager, or Sandbox workloads.
3. **Given** an active run, **when** its owning Controller
   reconciles, **then** the correctly named and labelled JobManager deployment
   MUST exist in the tenant workload namespace.
4. **Given** a completed/deleted graph or expired orphan window, **when** cleanup
   runs, **then** Controller MUST remove the owned runtime without deleting a
   namespace still shared by other graphs.
5. **Given** an API or Kubernetes failure, **when** reconciliation retries,
   **then** operations MUST remain idempotent and MUST NOT create duplicate JMs.
6. **Given** a namespace default or AgentGraph override changes, **when** Controller
   reconciles an active runtime, **then** JM, TM, and Sandbox MUST all receive the
   same effective configuration and stale pods MUST roll by checksum.
