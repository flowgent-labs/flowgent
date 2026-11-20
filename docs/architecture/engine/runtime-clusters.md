# Runtime Clusters

[System overview](../overview.md) · [Controller](controller.md) · [JobManager](jobmanager.md)

Flowgent uses a Flink-style runtime topology. The scheduling boundary is a
`runtime_cluster_id`, not a namespace-wide worker pool.

## Runtime Modes

| Mode | Lifecycle owner | Scope | Use case |
|---|---|---|---|
| `application` | Controller creates one JobManager per active FlowRun; that JM/RM creates its own TM and Sandbox Deployments | namespace + Flow + Run + runtime_cluster_id | Strong isolation, predictable cleanup, long or sensitive workflows |
| `session` | Helm deploys a shared session JobManager; that JM/RM owns only the TM/Sandbox Deployments carrying the same cluster id | namespace + session runtime_cluster_id | Short jobs and shared low-isolation workloads |

Both modes use the same JobManager → ResourceManager → TaskManager/Sandbox
control line. The difference is lifecycle, not ownership semantics. A TM or
Sandbox pod MUST accept work only for its own `runtime_cluster_id`.

## Configuration Ownership

- Helm owns session JobManager pod resources and session TM/Sandbox defaults.
- Helm owns application-mode JM/TM/Sandbox default pod resources.
- Helm owns application-mode TM/Sandbox replica and slot defaults.
- A Flow definition owns only:
  - `runtime_mode`
  - optional top-level `resources.jobmanager/taskmanager/sandbox` pod-size
    overrides for application mode.

Application-mode Flow resources MUST NOT define replicas or slots. Those remain
platform defaults so placement policy stays centralized.

## Messaging Boundary

Cluster-routed work topics use:

```text
flowgent/v1/{namespace}/clusters/{clusterId}/flows/{flowId}/runs/{runId}/exec/plans
flowgent/v1/{namespace}/clusters/{clusterId}/flows/{flowId}/runs/{runId}/sandbox/trigger
flowgent/v1/{namespace}/clusters/{clusterId}/runtime/{role}/{workerId}/ready
```

Result callbacks stay per run:

```text
flowgent/v1/{namespace}/flows/{flowId}/runs/{runId}/exec/results
flowgent/v1/{namespace}/flows/{flowId}/runs/{runId}/sandbox/result
```

This keeps dispatch load-balanced inside one runtime cluster while preserving
flow/run observability in the topic path.
