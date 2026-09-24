# Controller

[System overview](../overview.md) · [JobManager](jobmanager.md)

The Controller owns distributed runtime reconciliation. It discovers Flows,
active application-mode runs through the API Server, shards ownership across
controller replicas, creates/removes per-FlowRun application JobManagers, and
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

Polls the apiserver REST API (`FlowgentClient.ListFlows`) for agentflow definitions,
shards across pods via hash-mod, and ensures one dedicated K8s JobManager only
when an owned Flow has an active run. Cron callbacks create a PENDING run through
the API before reconciliation. The Controller also subscribes to MQTT lifecycle
events for prompt Flow updates. It never calls a JobManager directly.

Before reconciling an active application Flow runtime, the Controller resolves its effective
namespace→Flow runtime configuration through the workload-only API. Public values
are upserted into a per-Flow ConfigMap and secrets into a per-Flow Secret; plaintext
secrets are never logged or stored in workload manifests. A deterministic content
checksum rolls the application JM. The application JM's K8sRM then reconciles
TM/Sandbox Deployments scoped by the run's `runtime_cluster_id`.

### Hash-Mod Sharding

```
shard(flow_id) = fnv64a(flow_id) % total_controller_pods
```

Each pod discovers total count via `IDiscoveryClient` (K8s label selector or env
vars). Only processes flows where `shard == pod_index`.

### Reconciliation Loop

```
Every 10s:
  1. IDiscoveryClient.DiscoverPeers(labelSelector)
  2. apiClient.ListFlows(namespace)
  3. For each flow where shard(flow_id) == my_index:
     a. Inspect PENDING/RUNNING/PAUSED application runs
     b. For active application runs, create/reconcile their dedicated JobManager
  4. Re-register owned cron triggers
  5. Garbage-collect idle/deleted application JMs and application TM/Sandbox workers
```

### Dispatch Detail

| Binding | Controller Action | Who Executes |
|---------|-------------------|--------------|
| `Flow.runtime_mode=application` | Preserve Run snapshot, ensure dedicated active-run JM with cluster id `app-{runId}` | TM/Sandbox workers whose subscription and pod labels match namespace + runtime cluster |
| `Flow.runtime_mode=session` | Do not create a JM; session runs are consumed by the Helm session JM | Session TM/Sandbox workers whose labels match the session cluster id |

### Dual Format: Static YAML vs DB JSON

All deployments use `model.AgentFlowSpec` (dual-tagged `json:` + `yaml:`). Static YAML
loaded at startup + hot-reload. DB JSON saved by UI via API, polled by Controller.

---

## Expected Behavior

1. **Given** multiple Controller replicas, **when** they share one peer snapshot,
   **then** hash-mod sharding MUST assign each flow to exactly one replica per
   reconciliation tick.
2. **Given** an imported flow with no active run, **when** Controller reconciles,
   **then** it MUST NOT create JobManager, TaskManager, or Sandbox workloads.
3. **Given** an active run, **when** its owning Controller
   reconciles, **then** the correctly named and labelled JobManager deployment
   MUST exist in the tenant workload namespace.
4. **Given** a completed/deleted flow or expired orphan window, **when** cleanup
   runs, **then** Controller MUST remove the owned runtime without deleting a
   namespace still shared by other flows.
5. **Given** an API or Kubernetes failure, **when** reconciliation retries,
   **then** operations MUST remain idempotent and MUST NOT create duplicate JMs.
6. **Given** a namespace default or Flow override changes, **when** Controller
   reconciles an active runtime, **then** JM, TM, and Sandbox MUST all receive the
   same effective configuration and stale pods MUST roll by checksum.
