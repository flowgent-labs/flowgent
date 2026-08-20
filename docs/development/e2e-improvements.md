# Integration and End-to-End Test Improvements

[中文](e2e-improvements_ZH.md)

## Required Outcome

`tests/it` MUST verify the real end-to-end architecture defined by
`docs/architecture/overview.md`. Only external-system APIs may be mocked: LLM
providers, GitHub, SonarQube, Telegram, LDAP/OIDC providers, and comparable
third-party boundaries.

Database, middleware, messaging, Flowgent runtime components, and deployment
topology MUST use real local instances. Distributed coverage therefore includes
PostgreSQL, an MQTT broker, API Server, Controller, JobManager, TaskManager,
Sandbox, Notifier, Kubernetes, session runtime clusters, and per-run
application runtime clusters.

Core components MUST NOT be replaced by in-memory implementations, fakes,
stubs, or mocks. A test may do so only when it is explicitly classified as a
standalone/local smoke test, and it MUST NOT claim distributed E2E coverage. A
fresh clone MUST be able to start the documented dependencies and run the suite
reliably.

## Mandatory Constraints

| Constraint | Requirement |
|---|---|
| Architecture | Implementations MUST conform to `docs/architecture/overview.md`; test shortcuts MUST NOT violate component boundaries. |
| Stable structure | The existing `tests/it` directory and test-file organization SHOULD remain stable. Files MUST NOT be moved, renamed, or broadly reorganized without a concrete benefit. |
| Design quality | Test code MUST remain cohesive, loosely coupled, and modular. Large flows and helpers MUST NOT be copied between scenarios. |
| Naming | Test names MUST identify behavior that is actually exercised and MUST NOT name components that were never started. |
| Assertions | A completed run alone MUST NOT prove core behavior. Tests MUST assert relevant state, events, persistence, routing, or output. |
| Completion | Work that does not satisfy these requirements MUST NOT be marked complete. |

## Current Baseline

The current runner starts a real API Server, a PostgreSQL-backed store, a local
JobManager poller, and `ProviderStandalone`. It does not start Controller,
KubernetesResourceManager, an MQTT broker, an independent TaskManager, an
independent Sandbox, or a Notifier consumer. The existing suite is therefore a
standalone/local smoke suite and MUST NOT be described as distributed E2E.

## Required Improvements

| # | Problem | Required change | Acceptance criteria |
|---|---|---|---|
| 1 | Harness scope is inaccurate | Preserve the current layout. Distinguish standalone smoke from distributed E2E in README, Makefile, and CI; add or extend a distributed harness. | Documentation no longer overstates coverage. PostgreSQL, MQTT, and Kubernetes requirements are explicit. No unnecessary restructuring occurs. |
| 2 | No real MQTT round trip | Start a real local MQTT broker. Run JM through its distributed MQTT path and let a real TM consume the cluster-routed `$share/tm-{namespace}-{clusterId}/.../exec/plans` subscription. | The test asserts JM publish → TM consume → REST `SaveTask` → TM result publish → JM unblock. Incorrect routing or subscribe order fails the test. |
| 3 | `exec/results` contract is unverified | Fix and verify the state-only result contract; output MUST be persisted through REST/task state. | `exec/results` contains only JM routing/state fields. Large-output leakage fails the test while output remains queryable from task state. |
| 4 | Controller scenarios are not real | Start a real Controller and verify `ListFlows`, hash-mod sharding, active-Flow JM creation, run routing, and cleanup. | A missing Controller, incorrect shard, or missing JM create/cleanup fails the test. Test names remain accurate. |
| 5 | No runtime-cluster Kubernetes E2E | Provide a k3s/kind/Helm profile that starts real API Server, Controller, Notifier, MQTT, PostgreSQL, session JM, and application runtime components. | The test verifies namespace isolation, runtime-cluster labels, per-run application JM, session JM routing, TM/Sandbox capacity, and Controller cleanup. |
| 6 | DAG assertions are too weak | Add strong assertions for linear, fan-out/fan-in, condition, committee, map, join, supervisor, and subflow behavior. | Tests verify task rows, ordering, skip state, and output. Incorrect branches, decisions, or persistence fail. |
| 7 | Failure paths are missing | Cover node failure, timeout, deadlock, invalid dependency, retry exhaustion, and cancellation. | Every boundary has deterministic setup, uses no arbitrary long sleep, and produces an observable error. |
| 8 | Sandbox is not real | Distributed tests MUST start a real Sandbox worker and verify workspace, trigger/result, stdout/stderr, and exit code. | One scenario MUST NOT accept both success and failure. Network denial and seccomp are tested when supported, otherwise explicitly skipped. |
| 9 | Notifier is not real | Start a real Notifier consumer. Use local mock Telegram/webhook/email endpoints and verify event delivery plus `notify/result`. | A missing Notifier or absent expected payload at the local endpoint fails the test. |
| 10 | Trigger coverage is incomplete | Cover REST trigger, positive and unmatched cases for implemented webhook providers, A2A, and implemented cron/interval/Controller triggers. | Every path verifies run creation, variable propagation, and JM pickup. Unimplemented behavior is recorded explicitly. |
| 11 | API-Server-only database contract is unverified | Direct DB access is allowed only from test verification code. Non-API components MUST write through REST/MQTT. | Distributed E2E does not inject a direct DB store into JM, TM, Sandbox, or Notifier; state remains verifiable through REST and DB observations. |
| 12 | Fixtures are duplicated | Consolidate duplicated fixtures such as `securityFixerFlow()` and use small, explicit per-scenario overrides. | Flow fixtures are cohesive, share no mutable state, and a single update reaches every consumer. |
| 13 | Isolation is incomplete | Clean namespaces, flows, runs, tasks, providers, MCPs, knowledge, approvals, and channels. Explain fixed ports and `-p 1` when retained. | A fresh clone runs successfully, and repeated runs require no manual database cleanup. |

## Required Test Layers

| Layer | Scope |
|---|---|
| `standalone` | Local API Server + PostgreSQL + standalone RM; named only smoke/local integration. |
| `distributed` | Real PostgreSQL + MQTT + API Server + JM + TM + Sandbox/Notifier processes. |
| `k8s` | Helm/kind/k3s coverage of real runtime clusters, Controller, and K8sRM. |
| `auth` | Local LDAP/OIDC provider containers. |
| `usecase` | Real external-system verification under `usecase/`; not part of portable integration tests. |

## Implementation Order

1. Correct documentation, naming, and harness classification.
2. Add the MQTT JM/TM round trip and state-only result contract.
3. Add real Controller, runtime-cluster, and Kubernetes E2E coverage.
4. Add Sandbox, Notifier, trigger, and failure-path coverage.
5. Strengthen DAG assertions, consolidate fixtures, and make cleanup and
   fresh-clone execution reliable.
