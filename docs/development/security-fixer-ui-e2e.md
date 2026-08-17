# Security Fixer UI-Only End-to-End Completion

[中文](security-fixer-ui-e2e_ZH.md)

**Status:** Complete — Phase 5 Resource Pool runtime and GitHub-style shell accepted
**Started:** 2026-08-15

## Outcome

Prove the real Resource-Pool-bound `security-autonomy-fixer` path from a clean
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
[`usecase/security-autonomy-fixer/README.md`](../../usecase/security-autonomy-fixer/README.md).
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
10. Phase 5 requires five consecutive clean redeploys after removing the UI
    mock/runtime-mode paths and introducing Resource Pools. Each round MUST
    create or verify the Pool through visible namespace Settings, bind the Flow,
    prove namespace/pool-scoped TM and Sandbox workers, pass the real A2A
    verifier, and retain screenshots without secret values.

## Work Status

| Workstream               | Status           | Evidence / next action                                                                                                                                                                                             |
| ------------------------ | ---------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Baseline audit           | Complete         | Browser fixture IT remains a component/integration gate; `test:system` is the real-cluster acceptance gate.                                                                                                        |
| Secure LLM/MCP contracts | Complete         | Browser submits env-reference names only; reads are redacted and runtime values come from the configured Kubernetes Secret.                                                                                        |
| Runtime Skill contract   | Complete         | Namespaced CRUD for runtime `kind=skill` definitions is separate from portable Skill Studio drafts.                                                                                                                |
| UI resource authoring    | Complete         | Playwright creates LLM -> MCP -> Agent -> runtime Skill -> Flow through visible UI interactions.                                                                                                                   |
| UI trigger and tracking  | Complete         | The UI triggers/approves a real Run and inspects every TaskRun attempt plus Jaeger trace correlation.                                                                                                              |
| Verifier compatibility   | Complete         | UI-provisioned mode reuses the browser run and prohibits console import, REST seeding, or replacement triggers.                                                                                                    |
| Ten-round soak           | Complete (10/10) | Ten consecutive clean-deploy UI system rounds passed on 2026-08-15; every round passed all 15 verifier scenarios and matched 26 persisted attempts, 26 UI-inspected attempts, and 26 Jaeger attempt links exactly. |
| Notification encryption  | Complete         | Secrets are authored dynamically in UI, stored with AES-256-GCM, redacted on reads, decrypted by the real Notifier, and delivered to an authenticated external receiver.                                           |
| JM lifecycle timestamps  | Complete         | JobMaster authoritatively persists `started_at` and `finished_at`; the UI system test requires both and validates their ordering.                                                                                   |
| Phase-2 five-round soak  | Complete (5/5)   | Five consecutive clean-database Helm redeploys and visible UI provision/trigger runs passed on 2026-08-16, including the strict notification gate and all 15 existing scenarios.                                    |
| Enterprise RBAC          | Complete                  | The deployment is the implicit enterprise; namespace is the org/runtime/data boundary. Global identities, explicit membership, teams, scoped roles, DENY precedence, audit, attenuated service-account keys, exact Flow Settings grants, immutable FlowRelease installs, and GitHub-style routes passed the Phase-3 soak. |
| A2A acceptance           | Complete                  | A2A 0.3 JSON-RPC, Bearer-to-APIServer RBAC propagation, shared caller-isolated task storage, two replicas, real FlowRun scenario 25, and strict health gates passed the Phase-3 soak. |
| Phase-3 five-round soak  | Complete (5/5)            | Five consecutive clean-database Helm redeploys passed on 2026-08-16 with visible UI provisioning, exact-flow RBAC allow/deny/revoke checks, 2/2 A2A replicas, real A2A FlowRuns, all 15 scenarios, and retained screenshots. |
| Settings/runtime configuration | Complete (5/5) | GitHub-style settings sidebars, canonical Flow URLs, namespace→Flow environment/secret inheritance, encrypted write-only storage, least-privilege APIs, and JM/TM/Sandbox checksum rollout passed five consecutive clean-deploy rounds. |
| Resource Pool runtime | Complete (5/5) | Namespace-scoped CRUD/RBAC, immutable Run snapshot, Flow binding guard, pool-scoped MQTT/readiness, shared TM/Sandbox Deployments, full-template reconciliation, per-attempt Flow configuration, and deletion GC passed the Phase-5 soak. |
| GitHub-style shell cleanup | Complete (5/5) | The fixed global sidebar/footer and global `Live API` deployment badge are removed; one top navigation remains, with one local sidebar under namespace/Flow Settings and canonical `/{namespace}/{flow}` routes. Retained screenshots were reviewed after the soak. |
| Phase-5 five-round soak | Complete (5/5) | Five consecutive clean-database Helm redeploys passed on 2026-08-17. Every round used visible UI Pool/Flow authoring, passed browser tests 2/2 and verifier scenarios 15/15, retained retry/tracking screenshots, and completed a separate real A2A FlowRun. |

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
observed. Retry persistence and UI projection are covered by deterministic unit
and fixture integration tests. A dedicated runtime retry probe, if required,
should use a separate intentionally failing E2E-only Flow rather than changing
the production security fixer Flow.

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

## Phase-5 Five-Round Matrix

Every round reset PostgreSQL, fully redeployed Helm, created the complete
resource inventory through visible UI interactions, bound
`security-autonomy-fixer` to `security-critical`, and proved that TM/Sandbox
workers and MQTT dispatch were isolated by namespace and Pool. Each round also
ran a deterministic real three-attempt retry probe, inspected its request and
response in the UI, correlated it with an eight-span Jaeger trace, passed all
15 verifier scenarios, and completed a separate authenticated A2A FlowRun.

| Round | Main Run ID                            | Jaeger spans | A2A Run ID                             | Result |
| ----: | -------------------------------------- | -----------: | -------------------------------------- | ------ |
|     1 | `3ce6bbe8-5486-4dbc-80a9-de1430d0f4a3` |           86 | `abb56adf-c1c1-4a17-a42c-2828c3d77afd` | Pass   |
|     2 | `bef7b197-71d3-42e4-afdc-1a52a05e844c` |           87 | `f7fe7071-f534-456c-bf4d-b22dd7128c34` | Pass   |
|     3 | `33f1d3d0-626a-455e-bd2d-db776684f157` |           87 | `ef28d58f-1714-4241-9b95-7ca9246b67a2` | Pass   |
|     4 | `08f9f386-082f-4ef4-87df-2562f898780d` |           87 | `9fe90fb3-7867-4e44-9671-b66c1700d0a1` | Pass   |
|     5 | `2b2c7ae8-ecfe-4157-af74-377519419da9` |           87 | `2caddddb-a8e7-4e89-86cf-55efb7d5c20e` | Pass   |

The machine-readable summary is `PASS` with `5/5` consecutive successes.
All main runs had 26 persisted attempts, 26 UI-inspected attempts, 26 Jaeger
links, and complete `exec/plans`/`exec/results` MQTT coverage. The Pool
lifecycle verifier independently proved scale-up, cancellation, deletion, and
garbage collection of the JM, TM, Sandbox, ConfigMap, and Secret resources.

## Discovered Gaps

| Gap                                   | Determination                                                                                                            | Resolution status                                                                                                                                                     |
| ------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| LLM/MCP UI was mock/blocked           | Backend needed opaque env references and redacted reads.                                                                 | Resolved                                                                                                                                                              |
| Runtime Skill UI was unavailable      | Runtime Skills needed a distinct `kind=skill` REST lifecycle.                                                            | Resolved                                                                                                                                                              |
| Current `e2e-real` overstated realism | It seeds execution records and serves fixture Jaeger JSON.                                                               | Reclassified as fixture IT; `e2e-system` is the acceptance gate.                                                                                                      |
| Verifier assumed console import       | Scenarios 12/31 needed UI-provisioned mode with unchanged assertions.                                                    | Resolved                                                                                                                                                              |
| Notification channel secrets          | External notification secrets must be mutable through UI without plaintext storage or readback.                          | Resolved with schema-defined secret fields, versioned AES-256-GCM envelopes, preserve/explicit-clear semantics, real decrypted delivery, and UI/E2E gates.             |
| k3s local-image GC during soak        | The unreferenced image disappeared between uninstall and the next install.                                               | Resolved in the E2E deploy lifecycle; the image is refreshed after teardown and before Helm install.                                                                  |
| Terminal Run/Task polling race        | Run polling could observe `COMPLETED` and stop Task polling while its cache still lacked final attempts.                 | Resolved: each terminal Run revision forces one authoritative TaskRun refresh; system E2E requires persisted and UI-inspected attempt sets/counts to match exactly.   |
| Terminal runtime evidence GC race     | Slow UI inspection plus independent verifier scenarios could outlive the Flow-JM observation window.  | Resolved: run-scoped scenarios 31–37 execute immediately after Jaeger verification; independent engine scenarios 21–25 run afterward without weakening any assertion. |
| Run lifecycle timestamps              | Real completed runs previously returned null `started_at` and `finished_at`.                                             | Resolved: JM is the sole lifecycle time authority; API persists JM updates and the UI system test requires ordered non-null timestamps.                                |
| Enterprise RBAC resource model        | The confirmed model is deployment → namespace/org → exact resource, without Enterprise/Workspace rows.                  | Resolved with explicit namespace membership, team/role bindings, Flow Settings, immutable producer releases, and consumer-owned installs.                              |
| Legacy A2A verifier                   | It used obsolete REST paths and treated unreachable as SKIP/PASS; Deployment health probes were also misaligned.         | Resolved in implementation: standard A2A 0.3 JSON-RPC, strict real execution verifier, shared task DB, Bearer RBAC propagation, and required 2/2 replica health.        |
| Empty runtime configuration response  | Empty secret-key collections serialized as `null`, which violated the UI collection contract on a clean database.       | Resolved at both API serialization and UI repository boundaries, with backend and frontend regression tests.                                                          |
| Runtime workload reconciliation permission | A Flow JM reconciles its bound Pool worker templates, but the workload `flowgent-runtime` Role lacked Deployment `update`. | Resolved by granting the same-namespace runtime Role only the required Deployment verb; regression tests, Helm lint, and five live redeploy rounds pass. |
| Pool-routed MQTT audit                 | The observer still subscribed to the pre-Pool `exec/plans` and `sandbox/trigger` paths, so a successful run could be misclassified. | Resolved by observing non-shared `/{namespace}/pools/+/{flow}/...` dispatch topics while keeping point-to-point callbacks on the Flow path; all five rounds proved 26/26 plan/result coverage. |
| MQTT evidence secret persistence       | Sandbox trigger audit payloads contain attempt-scoped environment values even though application APIs and screenshots are redacted. | Resolved at the evidence serialization boundary: all environment values and recursively named secret/token/password/auth/cookie/API-key fields are replaced with `<redacted>` before disk writes; the current artifact was sanitized without printing values. |
| Notifier verifier pod selection       | A redeploy could leave an older failed pod first in an unordered list.                                                    | Resolved by selecting the newest Ready pod from a Running-only query; encrypted real-delivery assertions remain unchanged.                                             |
| Cold core build timeout               | The constrained clean core image build can legitimately exceed the old ten-minute generic timeout.                      | Resolved with a core-build-specific 20-minute bound; other command timeouts remain unchanged.                                                                          |
| Orphan runtime configuration           | Deleting a Flow could leave its materialized per-Flow runtime ConfigMap and Secret after the runtime Deployments were gone. | Resolved with controller-scoped garbage collection that deletes only controller-managed artifacts for missing Flows; scenario 23 now proves both materialization and deletion. |
| Broker subscription readiness race     | Kubernetes `AvailableReplicas` did not prove that a newly reconciled TM/Sandbox had completed its MQTT subscriptions, so the first plan could be lost. | Resolved with Namespace/Pool/role-scoped `RuntimeReady` leases emitted only after subscriptions are registered; the resource manager waits for fresh readiness before the first plan/trigger. |
| Legacy deployment selector              | A global selector cannot express business-team SLA capacity without coupling placement to orchestration semantics. | Replaced by Namespace Resource Pools; every Flow binds one Pool and every Run snapshots it. |
| Shared worker Flow configuration         | Mounting Flow env/secrets into a Pool Deployment would leak values between Flows. | Resolved by TaskManager fetching effective configuration per Sandbox attempt and placing it only in that attempt trigger. |

## Evidence Policy

- Per-round summaries remain under the use-case `e2e/reports/` evidence tree.
- Review screenshots and the machine-readable summary are archived per round
  under `usecase/security-autonomy-fixer/e2e/reports/ui-rounds/`.
- Phase-2 evidence is archived under
  `usecase/security-autonomy-fixer/e2e/reports/ui-rounds-phase2/`; every round
  retains `round.json`, `ui-provision.json`, 15 scenario reports, the Playwright
  HTML report, and UI screenshots.
- Phase-3 RBAC/A2A evidence is archived separately under
  `usecase/security-autonomy-fixer/e2e/reports/ui-rounds-phase3-rbac-a2a/`.
- Phase-4 Settings/runtime-configuration evidence is archived under
  `usecase/security-autonomy-fixer/e2e/reports/ui-rounds-phase4-settings-runtime-config/`;
  each round retains `round.json`, `ui-provision.json`, all 15 scenario reports,
  the Playwright HTML report, and redacted UI screenshots.
- The real three-attempt retry probe screenshots are retained in the Phase-4
  evidence tree under `retry-probe/`.
- Phase-5 Resource Pool evidence is archived under
  `usecase/security-autonomy-fixer/e2e/reports/ui-rounds-phase5-resource-pools/`;
  each round retains `round.json`, `ui-provision.json`, all 15 scenario reports,
  and screenshots for Pool configuration, GitHub-style Settings, DAG,
  attempt I/O, RBAC, redacted Secrets, and Jaeger correlation.
- No secret value may be written into this plan or generated evidence.
