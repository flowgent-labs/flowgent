# Runtime Clusters

[System overview](../overview.md) · [Controller](controller.md) · [JobManager](jobmanager.md) · [Sandbox pools](sandbox.md)

**Status:** application/session JM/TM clusters are implemented. The independent
Sandbox fixed-pool boundary was approved on 2026-11-14 and is not implemented yet.
The table below describes the resulting target ownership; the final section
identifies the current Sandbox difference.

## Runtime Modes and Ownership

| Resource | `application` | `session` |
| --- | --- | --- |
| JobManager | Controller creates one per active FlowRun | Helm deploys the shared session JM |
| TaskManager | JM/RM owns workers for `app-{runId}` | Session JM/RM owns workers for its session cluster |
| Sandbox | Flow explicitly references a Controller-owned fixed pool | Flow explicitly references a Controller-owned fixed pool |

`runtime_cluster_id` remains the JM/TM execution boundary. Sandbox's execution
boundary is `(namespace_id, sandbox_id)`, independent of runtime mode. Authorized
Flows from different runtime clusters may reference the same pool within its
namespace and trust boundary. Run completion removes application JM/TM capacity,
but MUST NOT remove a referenced Sandbox pool.

## Configuration Ownership

- Helm supplies session JM resources and JM/TM runtime defaults.
- Application Flows may override `resources.jobmanager` and
  `resources.taskmanager` pod sizes; TM replicas/slots remain platform defaults.
- The Sandbox resource owns its image, replicas/slots, CPU/memory, storage
  references and ingress/egress policy. Controller reconciles it independently.
- Every Flow declares `runtime_mode` and references `sandbox_id`. A Run preserves
  its chosen binding; later Flow edits cannot silently move it to another pool.
- Legacy `resources.sandbox`, `sandbox_policy` and per-node network overrides
  require explicit migration, not silent fallback or ignored fields.

The [Sandbox design](sandbox.md) owns pool persistence, admission, policy changes,
workspace/credential boundaries and the proposed trigger/result contracts.
Checkpoint/recovery semantics are not changed by this ownership decision.

## Messaging Boundary

JM-to-TM plans keep their runtime-cluster routing:

```text
flowgent/v1/{namespace}/clusters/{clusterId}/flows/{flowId}/runs/{runId}/exec/plans
flowgent/v1/{namespace}/flows/{flowId}/runs/{runId}/exec/results
```

TM-to-Sandbox dispatch uses the selected Sandbox-ID topic and its per-pool MQTT
shared group. Sandbox results correlate the exact NodeRun attempt, not just the
Run. `runtime_cluster_id` can remain origin metadata, but does not select Sandbox
consumers. The exact [pool topics](sandbox.md#proposed-mqtt-contract) are defined
once in the owning Sandbox document.

## Current Implementation Difference

Current code still gives JM's ResourceManager ownership of runtime-cluster
Sandbox Deployments, routes Sandbox triggers with `clusterId` and uses per-run
results. Its global/Flow/Node policy fields do not implement the Sandbox catalog
or fixed-pool API. Those paths and their deployment/E2E assets must migrate
alongside Controller, model/store/API/UI and TM/worker messages; this document
does not claim the new boundary has already shipped.
