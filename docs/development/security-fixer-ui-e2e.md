# Security Fixer UI-Only End-to-End Completion

[中文](security-fixer-ui-e2e_ZH.md)

**Status:** Complete — runtime-cluster real UI E2E soak passed 5/5 on 2026-08-18
**Started:** 2026-08-15

## Outcome

Prove the real runtime-cluster-bound `security-autonomy-fixer` path from a clean
deployment without `flowgent console import` or test-side REST seeding:

```text
clean PostgreSQL + Helm redeploy
  -> browser creates LLM -> MCP -> Agents -> runtime Skill -> Flows
  -> browser triggers security-autonomy-fixer
  -> Controller -> JM -> MQTT -> TM/Sandbox -> external systems
  -> API Server -> Jaeger Query -> browser Run/Tracking views
  -> use-case scenarios 11-37 pass
```

Architecture and use-case contracts remain owned by
[`architecture/overview.md`](../architecture/overview.md),
[`architecture/agent-flow.md`](../architecture/agent-flow.md), and
[`use-cases/security-autonomy-fixer/README.md`](../../use-cases/security-autonomy-fixer/README.md).
This plan records implementation progress and verification evidence only.

## Hard Gates

1. Flowgent components, PostgreSQL, MQTT, Kubernetes runtime, Sandbox, Jaeger,
   and external use-case calls MUST be real. Protocol fixtures and seeded
   Run/TaskRun rows do not count.
2. Every use-case resource MUST be submitted through visible browser UI.
   Tests may load canonical manifests into browser memory, but MUST NOT call
   resource REST APIs or `console import` directly.
3. Playwright MAY read secrets from process environment only to fill password
   controls. Secrets MUST be write-only at the API boundary and MUST NOT appear
   in reads, browser traces, reports, screenshots, URLs, or telemetry.
4. The UI MUST trigger the run and verify the real DAG, all persisted TaskRun
   attempts, retry input/output, and Jaeger-backed trace correlation.
5. A valid round MUST start with PostgreSQL reset and full Helm redeploy, then
   pass the complete ordered use-case verifier suite. Failed rounds do not count.
6. Ten consecutive valid rounds are required. Any product fix resets the streak
   to zero and validation restarts from a clean deployment.
7. Phase 2 requires five additional consecutive rounds after the notification
   encryption and JM timestamp changes. Disabled or skipped A2A does not count
   as A2A acceptance.
8. Phase 3 requires five consecutive clean redeploys after RBAC/A2A changes.
   Each round MUST create a namespace service account and exact Flow grant in
   visible UI, prove allowed/denied/revoked credentials, run the standard A2A
   JSON-RPC verifier without SKIP, and retain GitHub-style route screenshots.
9. Phase 4 requires five new consecutive clean redeploys after canonical
   `/{namespace}/{flow}` routes and runtime configuration inheritance. Each
   round MUST configure namespace defaults and Flow overrides in visible
   Settings UI, prove secret readback is impossible, and retain Settings,
   DAG, attempt-I/O, Jaeger, RBAC, and A2A evidence.
10. The current runtime-cluster phase requires five consecutive clean redeploys after deleting every
    production UI mock/fixture/alternate-data path, removing backend silent
    fallbacks, and introducing Flink-style runtime clusters. Each round MUST
    configure runtime mode through visible Flow UI, prove application/session
    runtime-cluster scoped TM and Sandbox workers, pass the real A2A verifier,
    and retain screenshots without secret values.

## Work Status

| Workstream               | Status           | Evidence / next action                                                                                                                                                                                             |
| ------------------------ | ---------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Baseline audit           | Complete         | The fixture-backed browser suite was deleted; `test:system` is the only browser E2E gate. Isolated unit-test doubles remain test-only.                                                                              |
| Secure LLM/MCP contracts | Complete         | Browser submits env-reference names only; reads are redacted and runtime values come from the configured Kubernetes Secret.                                                                                        |
| Runtime Skill contract   | Complete         | Namespaced CRUD for runtime `kind=skill` definitions is separate from portable Skill Studio drafts.                                                                                                                |
| UI resource authoring    | Complete         | Playwright creates LLM -> MCP -> Agent -> runtime Skill -> Flow through visible UI interactions.                                                                                                                   |
| UI trigger and tracking  | Complete         | The UI triggers/approves a real Run and inspects every TaskRun attempt plus Jaeger trace correlation.                                                                                                              |
| Canonical verifier contract | Complete      | UI-provisioned mode reuses the browser run and prohibits console import, REST seeding, replacement triggers, and obsolete endpoint/payload variants.                                                               |
| Ten-round soak           | Complete (10/10) | Ten consecutive clean-deploy UI system rounds passed on 2026-08-15; every round passed all 15 verifier scenarios and matched 26 persisted attempts, 26 UI-inspected attempts, and 26 Jaeger attempt links exactly. |
| Notification encryption  | Complete         | Secrets are authored dynamically in UI, stored with AES-256-GCM, redacted on reads, decrypted by the real Notifier, and delivered to an authenticated external receiver.                                           |
| JM lifecycle timestamps  | Complete         | JobMaster authoritatively persists `started_at` and `finished_at`; the UI system test requires both and validates their ordering.                                                                                   |
| Phase-2 five-round soak  | Complete (5/5)   | Five consecutive clean-database Helm redeploys and visible UI provision/trigger runs passed on 2026-08-16, including the strict notification gate and all 15 existing scenarios.                                    |
| Enterprise RBAC          | Complete                  | The deployment is the implicit enterprise; namespace is the org/runtime/data boundary. Global identities, explicit membership, teams, scoped roles, DENY precedence, audit, attenuated service-account keys, exact Flow Settings grants, immutable FlowRelease installs, and GitHub-style routes passed the Phase-3 soak. |
| A2A acceptance           | Complete                  | A2A 0.3 JSON-RPC, Bearer-to-APIServer RBAC propagation, shared caller-isolated task storage, two replicas, real FlowRun scenario 25, and strict health gates passed the Phase-3 soak. |
| Phase-3 five-round soak  | Complete (5/5)            | Five consecutive clean-database Helm redeploys passed on 2026-08-16 with visible UI provisioning, exact-flow RBAC allow/deny/revoke checks, 2/2 A2A replicas, real A2A FlowRuns, all 15 scenarios, and retained screenshots. |
| Settings/runtime configuration | Complete (5/5) | GitHub-style settings sidebars, canonical Flow URLs, namespace→Flow environment/secret inheritance, encrypted write-only storage, least-privilege APIs, and JM/TM/Sandbox checksum rollout passed five consecutive clean-deploy rounds. |
| Runtime cluster model | Complete (5/5) | Resource-pool APIs/UI are removed. `runtime_mode` plus `runtime_cluster_id` now drive placement: application runs create per-run JM/TM/Sandbox clusters; session runs use the Helm session JM. Five consecutive real UI system rounds passed on 2026-08-18. |
| GitHub-style shell cleanup | Complete (5/5) | The fixed global sidebar/footer and global `Live API` deployment badge are removed; one top navigation remains, with one local sidebar under namespace/Flow Settings and canonical `/{namespace}/{flow}` routes. Retained screenshots were reviewed after the soak. |
| Frontend real-API contract | Complete       | Production `src/mocks`, the fixture-backed `e2e-real` suite, test-only fake HTTP response suites, mock-mode configuration, response-shape decoders, local analytics synthesis, and alternate repository modes are deleted. One `ApiClient` composition serves every production repository; `e2e-system` is the only browser API acceptance path. |
| Backend strict contract | Complete          | Resource Manager configuration, Flow runtime mode binding, and canonical REST payloads fail fast; no provider downgrade, implicit runtime placement, legacy migration rename, or old human-approval endpoint remains. |
| Previous strict-contract soak | Superseded | The prior pool-model soak passed on 2026-08-17 but no longer represents the current runtime model. Current acceptance is covered by the 2026-08-18 runtime-cluster soak below. |

## Ten-Round Matrix

| Round | Clean deploy | UI resources | UI trigger | Run/trace | Verifiers | Result / run ID                        |
| ----: | ------------ | ------------ | ---------- | --------- | --------- | -------------------------------------- |
|     1 | Pass         | Pass         | Pass       | 26/26/26  | 15/15     | `f9f75027-ee7a-4a5e-8cd0-8a93abbd1ab8` |
|     2 | Pass         | Pass         | Pass       | 26/26/26  | 15/15     | `3b9bca2e-8236-4276-a69b-9051a02b0797` |
|     3 | Pass         | Pass         | Pass       | 26/26/26  | 15/15     | `c704ae0f-7f7c-4f80-92b6-af1eea849423` |
|     4 | Pass         | Pass         | Pass       | 26/26/26  | 15/15     | `b6946224-9eef-4707-bc5b-9900692edb4b` |
|     5 | Pass         | Pass         | Pass       | 26/26/26  | 15/15     | `5c1587a8-cae1-47fc-8bda-f6d124598d84` |
|     6 | Pass         | Pass         | Pass       | 26/26/26  | 15/15     | `a3a767fd-2cf4-4582-8ed2-a5f7f3ec6b6f` |
|     7 | Pass         | Pass         | Pass       | 26/26/26  | 15/15     | `d7d336cf-a157-428d-9973-374bb1ee421f` |
|     8 | Pass         | Pass         | Pass       | 26/26/26  | 15/15     | `825fabef-d3a3-4b72-84ad-ef22215391b5` |
|     9 | Pass         | Pass         | Pass       | 26/26/26  | 15/15     | `5f087bb2-ed33-4197-97c9-afa4433bea16` |
|    10 | Pass         | Pass         | Pass       | 26/26/26  | 15/15     | `99292d21-23dc-4c78-887b-bc00224abfee` |

## Phase-2 Five-Round Matrix

These rounds cover dynamic encrypted notification configuration, real external
delivery, and authoritative JM Run timestamps. “15/15” is the legacy aggregate;
its skipped A2A scenario is not evidence of A2A success.

| Round | Clean deploy | UI resources/trigger | TaskRun/UI/Jaeger | Notification | Verifiers | Result / run ID                        |
| ----: | ------------ | -------------------- | ----------------- | ------------ | --------- | -------------------------------------- |
|     1 | Pass         | Pass                 | 26/26/26          | Pass         | 15/15     | `f667ecc7-8fce-4e2e-8614-9e0f2beeae33` |
|     2 | Pass         | Pass                 | 26/26/26          | Pass         | 15/15     | `c3d221b6-fbb5-405b-a23f-fa2960c2ee2d` |
|     3 | Pass         | Pass                 | 26/26/26          | Pass         | 15/15     | `c53f6950-697d-4081-8e3f-db06afd8fdf1` |
|     4 | Pass         | Pass                 | 26/26/26          | Pass         | 15/15     | `7213423a-4fb1-41bf-b094-a7a17fc9de02` |
|     5 | Pass         | Pass                 | 26/26/26          | Pass         | 15/15     | `e9ffa166-bfc4-4553-95d1-232a613d8c98` |

## Phase-3 Five-Round Matrix

Each round reset PostgreSQL, redeployed Helm, provisioned all resources through
visible UI, proved exact-Flow allow/deny/revocation, inspected all 26 persisted
attempts and their Jaeger links, passed all 15 verifiers, and executed a real
A2A FlowRun through a healthy two-replica deployment.

| Round | Main Run ID                            | Jaeger spans | A2A Run ID                             | Result |
| ----: | -------------------------------------- | -----------: | -------------------------------------- | ------ |
|     1 | `d90f56f8-5ae1-40bb-a906-d8d6d5cb7480` |           86 | `8029522d-ba6c-472a-8619-430f27c44ab9` | Pass   |
|     2 | `dbee2a83-369c-43df-8f0b-e1c8f1e37823` |           86 | `04437c23-4f4a-4ff4-9605-15ea060930a5` | Pass   |
|     3 | `e2845a37-65da-406e-a0b1-d2e86a107d04` |           86 | `830f69dc-d0ff-4f99-8026-fad2c9e8266b` | Pass   |
|     4 | `2ae93a96-1e97-45eb-bce4-d7d58bb80a5e` |           86 | `f937f4b4-57cb-4571-ad5a-35c5956a0f95` | Pass   |
|     5 | `b0589199-7593-4cf1-8078-4fee498454a4` |           87 | `69c517b6-3031-49f8-a372-79aef7e3c7f5` | Pass   |

All five main runs had 26 nodes with one attempt per node; no natural retry was
observed. Deterministic unit tests cover retry projection, while the later
system phases add a separate intentionally failing E2E-only Flow that proves
real persistence, UI attempt I/O, and Jaeger correlation without changing the
production security fixer Flow.

## Phase-4 Five-Round Matrix

Each round reset PostgreSQL, fully redeployed Helm, and used visible UI actions
for 23 writes: LLM, notification channel, MCPs, agents, runtime skills, Flow,
namespace/Flow RBAC, namespace runtime defaults, Flow environment/secret
overrides, API-key revocation, and the real run trigger. Every round proved
write-only secret reads, namespace-to-Flow inheritance, 26 persisted/UI/Jaeger
attempt correlations, all 15 verifiers, and a separate real A2A FlowRun.

| Round | Main Run ID                            | Jaeger spans | A2A Run ID                             | Result |
| ----: | -------------------------------------- | -----------: | -------------------------------------- | ------ |
|     1 | `125f809e-bf7f-4e88-984c-50a44ec76008` |           86 | `4320b2ab-48fd-4ea7-bb8a-31f61762d4ce` | Pass   |
|     2 | `4f248755-713a-48b0-953b-c88b2f9da557` |           86 | `7b35fb14-026d-4e65-9862-a0262d4f2d6b` | Pass   |
|     3 | `df99669c-0b83-48d6-bd3a-38d539f8ddc4` |           87 | `783a36d1-fc56-4705-942d-b12eb0ebfefd` | Pass   |
|     4 | `d968f94e-b685-48f3-a922-65de5a3aa5f6` |           87 | `4781485e-148e-4974-97f4-908b5f270cbb` | Pass   |
|     5 | `9dd12442-c98c-4ca6-93a2-e10b46f17b08` |           86 | `442a6493-206a-46c8-999f-e843dc8ac40f` | Pass   |

The machine-readable result is `PASS` with five consecutive successes. Each
round configured two namespace environment defaults, one namespace secret, one
Flow environment override, and one Flow secret override. All 26 runtime node
attempts were inspected in the UI and linked to Jaeger; these production runs
did not naturally retry. After the soak, a separate E2E-only Flow was created
and triggered through the visible UI to fail deterministically. Run
`90766811-9f76-43c5-b389-b6bd6d15872f` persisted three attempts, rendered
`2 retries` plus three selectable request/response views, and correlated all
attempts to a real Jaeger trace with eight spans. The probe Flow and its runtime
configuration were deleted after verification.

## Runtime-Cluster Five-Round Matrix

Each round reset PostgreSQL, fully redeployed Helm, provisioned all
security-fixer resources through visible UI, triggered the main Flow through UI
as an application-mode run, and passed the ordered 15-scenario verifier suite.
The suite verified legacy placement cleanup fallout, the Helm session JobManager
health gate, application runtime-cluster JM/TM/Sandbox creation and cleanup,
cluster-routed MQTT dispatch topics, Jaeger correlation, redacted UI evidence,
and the dedicated real A2A verifier.

| Round | Main Run ID                            | Jaeger spans | A2A Run ID                             | Result |
| ----: | -------------------------------------- | -----------: | -------------------------------------- | ------ |
|     1 | `319bf35b-7cac-4fc8-9ad0-26d10fadef45` |           87 | `67baecad-81c6-4a74-8855-e89619be1bec` | Pass   |
|     2 | `c8d09038-3fcc-4dd8-9f9d-328163f80b25` |           86 | `18f66ea4-dda2-4411-a2d5-4c75104dab3a` | Pass   |
|     3 | `a5a95dca-439d-4a22-ae0b-4076c4af3a14` |           86 | `7634e8e0-4296-40b1-9bf3-d37e31f73570` | Pass   |
|     4 | `d41703a0-e1c9-4d9b-85a7-d63baf7ee084` |           87 | `79019066-a4ab-4b91-91a5-9cdcb705a9e7` | Pass   |
|     5 | `e14e61c1-d9cd-42d7-865c-50326105588c` |           86 | `3d3f61ad-3e2a-41d1-94ed-e9fcaee3420e` | Pass   |

The machine-readable summary is `PASS` with `5/5` consecutive successes at
`use-cases/security-autonomy-fixer/e2e/reports/ui-rounds-phase6-runtime-clusters/summary.json`.
Each round retained `00_summary.md`, all 15 scenario reports, Playwright HTML
output, and screenshots. Representative round-05 screenshots include
`ui-run-dag.png`, `ui-attempt-io.png`, `ui-jaeger-attempt.png`,
`ui-real-retry-attempts.png`, `ui-real-retry-jaeger.png`,
`ui-flow-settings-environment.png`, `ui-flow-settings-secrets.png`,
`ui-namespace-settings-environment.png`, `ui-flow-access.png`, and
`ui-notification-channel.png`.

## Previous Strict-Contract Five-Round Matrix (Superseded)

This matrix is historical evidence for the previous pool-model runtime. It is
kept only to preserve the audit trail. It no longer proves the current
runtime-cluster contract. Current acceptance requires a fresh clean-deploy soak
covering `runtime_mode=application`, the Helm session JobManager, and
`runtime_cluster_id`-scoped TM/Sandbox routing.

| Round | Main Run ID                            | Jaeger spans | A2A Run ID                             | Result |
| ----: | -------------------------------------- | -----------: | -------------------------------------- | ------ |
|     1 | `88436405-b677-4e78-8efc-cb4ff22ce5e6` |           87 | `049ec41f-a5dd-4c11-809f-3b10ece7455a` | Pass   |
|     2 | `88b80c6d-56da-4a3a-bbbb-d4ebc94edac9` |           86 | `28082455-f089-4bca-a65e-a946fec8a5cc` | Pass   |
|     3 | `00db538c-08fa-4a52-b45f-cebdbf5e3de4` |           87 | `e08d5768-4b3f-4a9c-818c-3b6fdd27a262` | Pass   |
|     4 | `b10c35ef-6345-48d3-8396-1d9293eacf79` |           86 | `f4c22e16-6bc5-48e3-b697-8c3a37c96b28` | Pass   |
|     5 | `faa971e1-51cd-4098-9f70-b0b6be7cc9cd` |           86 | `7a4a31a7-76dc-4ff6-a95c-8dd3550197e1` | Pass   |

The machine-readable summary is `PASS` with `5/5` consecutive successes.
All main runs had 26 persisted attempts, 26 UI-inspected attempts, 26 Jaeger
links, and complete `exec/plans`/`exec/results` MQTT coverage under the previous
runtime model.

## Discovered Gaps

| Gap                                   | Determination                                                                                                            | Resolution status                                                                                                                                                     |
| ------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| LLM/MCP UI was mock/blocked           | Backend needed opaque env references and redacted reads.                                                                 | Resolved                                                                                                                                                              |
| Runtime Skill UI was unavailable      | Runtime Skills needed a distinct `kind=skill` REST lifecycle.                                                            | Resolved                                                                                                                                                              |
| Fixture-backed browser test path      | `e2e-real` seeded execution records and served fixture Jaeger JSON, so it could be mistaken for system evidence.         | Deleted completely; `e2e-system` is the sole browser E2E path and requires the live API Server, PostgreSQL, Kubernetes runtime, MQTT, Jaeger, and external services.   |
| Verifier assumed console import       | Scenarios 12/31 needed UI-provisioned mode with unchanged assertions.                                                    | Resolved                                                                                                                                                              |
| Notification channel secrets          | External notification secrets must be mutable through UI without plaintext storage or readback.                          | Resolved with schema-defined secret fields, versioned AES-256-GCM envelopes, preserve/explicit-clear semantics, real decrypted delivery, and UI/E2E gates.             |
| k3s local-image GC during soak        | The unreferenced image disappeared between uninstall and the next install.                                               | Resolved in the E2E deploy lifecycle; the image is refreshed after teardown and before Helm install.                                                                  |
| Terminal Run/Task polling race        | Run polling could observe `COMPLETED` and stop Task polling while its cache still lacked final attempts.                 | Resolved: each terminal Run revision forces one authoritative TaskRun refresh; system E2E requires persisted and UI-inspected attempt sets/counts to match exactly.   |
| Terminal runtime evidence GC race     | Slow UI inspection plus independent verifier scenarios could outlive the Flow-JM observation window.  | Resolved: run-scoped scenarios 31–37 execute immediately after Jaeger verification; independent engine scenarios 21–25 run afterward without weakening any assertion. |
| Run lifecycle timestamps              | Real completed runs previously returned null `started_at` and `finished_at`.                                             | Resolved: JM is the sole lifecycle time authority; API persists JM updates and the UI system test requires ordered non-null timestamps.                                |
| Enterprise RBAC resource model        | The confirmed model is deployment → namespace/org → exact resource, without Enterprise/Workspace rows.                  | Resolved with explicit namespace membership, team/role bindings, Flow Settings, immutable producer releases, and consumer-owned installs.                              |
| Legacy A2A verifier                   | It used obsolete REST paths and treated unreachable as SKIP/PASS; Deployment health probes were also misaligned.         | Resolved in implementation: standard A2A 0.3 JSON-RPC, strict real execution verifier, shared task DB, Bearer RBAC propagation, and required 2/2 replica health.        |
| Empty runtime configuration response  | Empty secret-key collections serialized as `null`, which violated the UI collection contract on a clean database.       | Resolved at both API serialization and UI repository boundaries, with backend and frontend regression tests.                                                          |
| Runtime workload reconciliation permission | A Flow JM reconciles runtime-cluster worker templates, and its Role needs Deployment update/scale. | Resolved: templates grant the minimal Deployment verbs for session JM and application runtime workers; the runtime-cluster soak passed five clean redeploy rounds. |
| Cluster-routed MQTT audit                 | The observer must subscribe to `/clusters/+` `exec/plans` and `sandbox/trigger` paths. | Resolved: verifier code observes cluster-routed dispatch topics while keeping point-to-point callbacks on the Flow path; the runtime-cluster soak passed five clean redeploy rounds. |
| MQTT evidence secret persistence       | Sandbox trigger audit payloads contain attempt-scoped environment values even though application APIs and screenshots are redacted. | Resolved at the evidence serialization boundary: all environment values and recursively named secret/token/password/auth/cookie/API-key fields are replaced with `<redacted>` before disk writes; the current artifact was sanitized without printing values. |
| Notifier verifier pod selection       | A redeploy could leave an older failed pod first in an unordered list.                                                    | Resolved by selecting the newest Ready pod from a Running-only query; encrypted real-delivery assertions remain unchanged.                                             |
| Cold core build timeout               | The constrained clean core image build can legitimately exceed the old ten-minute generic timeout.                      | Resolved with a core-build-specific 20-minute bound; other command timeouts remain unchanged.                                                                          |
| Orphan runtime configuration           | Deleting a Flow could leave its materialized per-Flow runtime ConfigMap and Secret after the runtime Deployments were gone. | Resolved with controller-scoped garbage collection that deletes only controller-managed artifacts for missing Flows; scenario 23 now proves both materialization and deletion. |
| Broker subscription readiness race     | Kubernetes `AvailableReplicas` did not prove that a newly reconciled TM/Sandbox had completed its MQTT subscriptions, so the first plan could be lost. | Resolved with Namespace/Cluster/role-scoped `RuntimeReady` leases emitted only after subscriptions are registered; the resource manager waits for fresh readiness before the first plan/trigger. |
| Legacy deployment selector              | A global selector cannot express isolation without coupling placement to orchestration semantics. | Replaced by `runtime_mode` and `runtime_cluster_id`; application creates per-run clusters and session uses Helm cluster scope. |
| Shared worker Flow configuration         | Mounting Flow env/secrets into a shared worker Deployment would leak values between Flows. | Flow effective configuration remains attempt-scoped for sandbox triggers; runtime cluster ownership does not broaden secret visibility. |

## Evidence Policy

- Per-round summaries remain under the use-case `e2e/reports/` evidence tree.
- Review screenshots and the machine-readable summary are archived per round
  under `use-cases/security-autonomy-fixer/e2e/reports/ui-rounds/`.
- Phase-2 evidence is archived under
  `use-cases/security-autonomy-fixer/e2e/reports/ui-rounds-phase2/`; every round
  retains `round.json`, `ui-provision.json`, 15 scenario reports, the Playwright
  HTML report, and UI screenshots.
- Phase-3 RBAC/A2A evidence is archived separately under
  `use-cases/security-autonomy-fixer/e2e/reports/ui-rounds-phase3-rbac-a2a/`.
- Phase-4 Settings/runtime-configuration evidence is archived under
  `use-cases/security-autonomy-fixer/e2e/reports/ui-rounds-phase4-settings-runtime-config/`;
  each round retains `round.json`, `ui-provision.json`, all 15 scenario reports,
  the Playwright HTML report, and redacted UI screenshots.
- The real three-attempt retry probe screenshots are retained in the Phase-4
  evidence tree under `retry-probe/`.
- Superseded Phase-5 pool-model evidence is historical evidence for the
  previous model, not acceptance evidence for runtime clusters.
- Current runtime-cluster acceptance evidence must be archived under
  `use-cases/security-autonomy-fixer/e2e/reports/ui-rounds-phase6-runtime-clusters/`.
- No secret value may be written into this plan or generated evidence.
