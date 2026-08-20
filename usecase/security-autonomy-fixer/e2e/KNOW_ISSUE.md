# Known Issues & Bug Tracker

> Last updated: 2026-08-10.

## 0.0 Three-Round Verification Closure (2026-08-08)

The 2026-08-08 security-autonomy-fixer fixes marked `FIXED` below were
re-verified by three complete E2E rounds. Each round reset Flowgent PostgreSQL,
rebuilt and re-imported the core image, performed a full Helm redeploy,
imported the use-case resources via console, and ran `runner.py` from s11
through s37 with `16/16 passed`. Capstone run IDs:
`087f2e7d-addb-42c5-8091-1205dcb234d8`,
`1c62a371-ac37-4337-b10d-d391021744c7`, and
`038c678f-21e6-498f-a123-b2bd1f642389`.

| 2026-08-08 | **FIXED**: s31/s32 MQTT audit boundary could miss an early `exec/plans` message because s31 stopped its non-`$share` observer after the first plan and s32 opened a new observer. The verifier now keeps a continuous process-global observer from pre-trigger s31 until the s32 terminal run state, deduplicates saved audit messages, and hard-fails unless every completed task node has both `exec/plans` and `exec/results`. Verified by an additional full redeploy round `dc83b9f0-bc83-4e64-8d9b-37607b7eb843` with `24/24` plan/result coverage and `16/16 passed`. |

## 0.0.1 Three-Round Verification Closure (2026-08-09)

The 2026-08-09 fixes marked `FIXED` below were re-verified by three complete
current-code E2E rounds after the final verifier/kubeconfig adjustments. Each
round reset Flowgent PostgreSQL, rebuilt and re-imported the core image,
performed a full Helm redeploy, imported the use-case resources via console, and
ran `runner.py` from s11 through s37 with `16/16 passed`. Capstone run IDs:
`60cc4c16-e5e3-49b9-b0cb-97c9b240f088`,
`e0949046-430b-4918-b49f-36c5e8f7aea8`, and
`17b8b715-0dcd-4a5d-95cc-a81524eb9f4e`.

The three capstone runs each produced two new Flowgent security-fix commits on
PR #4 after the s31 baseline. s37 verified the pod-internal runtime hierarchy
`/var/flowgent/default/security-autonomy-fixer/{runId}/{taskId}` and the
git-clone task repo path without directly reading or mutating the host
`/mnt/disk1/flowgent/e2e` tree.

## 0.0.2 Active-Run JM/TM/Sandbox Lifecycle Regression (2026-08-10)

Closed by three complete current-code E2E rounds on 2026-08-10. Each round
performed a Flowgent PostgreSQL reset, fresh core build/image import, full Helm
redeploy to `flowgen-system`, console import of all security-autonomy-fixer
resources, and `runner.py` s11 through s37 with `16/16 passed`. Capstone run
IDs: `dcade7bb-92ae-47e4-b73a-f6619c0ad298`,
`fdcd3bb8-7e0a-4b67-95f0-f4a2692fda92`, and
`624cfc51-0250-4e7f-aa88-289166863e01`.

The three capstone runs produced new Flowgent security-fix commits on PR #4:
`19fd7329`/`7c6731ea`, `35e3827f`/`03fd0a3d`, and
`8a7a5ce0`/`7ceb6f8e`. The final run verified
`24/24` `exec/plans`, `24/24` `exec/results`, `3/3` sandbox trigger/result
messages through non-`$share` observer subscriptions, and s37 verified the
pod-internal workspace hierarchy plus `repos/rengine/.git` from inside the
TaskManager pod.

| Date | Status |
|------|--------|
| 2026-08-10 | **FIXED, VERIFIED**: Controller treated flow/skill definition import/update as a legacy per-Flow runtime allocation signal. Importing `security-autonomy-fixer/config` created idle JM pods for every imported flow-like resource and each JM's K8sRM immediately scaled runtime workers, so unrelated imported resources such as `nexus3-retrieval` and `sub-fix` left pods even when only the `security-autonomy-fixer` use case was under test. Fixes verified: `kind: Skill` is filtered from Flow API/Controller runtime reconciliation; `sub-fix.yaml` is imported as a Skill; K8sRM defaults TM/Sandbox min replicas to 0 and creates/scales them only inside `Schedule()` from active task load; Sandbox is JM-owned, flow-scoped, and slot-based via `SandboxSlotWorker`; Controller garbage-collects all JM-owned runtime Deployments (TM and Sandbox) immediately when the flow definition is deleted, and after `runtime.tm_orphan_timeout` (default `3m`) for completed/no-active runtime or unexpected parent-JM disappearance; Helm/E2E now split `flowgen-system` system namespace from `flowgent-{namespaceId}` runtime namespace. |
| 2026-08-10 | **FIXED, VERIFIED**: First rerun after the namespace/sandbox lifecycle change failed in s13 and s23. Controller created the JM, but Kubernetes rejected creation of the application-namespace `flowgent-runtime` Role because the controller ClusterRole did not itself hold the exact runtime permissions it was granting (`pods get/list/watch`, `configmaps list/watch`, and `deployments/scale get/update`). Result: the flow stopped after the first node and s23 timed out waiting for the TM Deployment. Helm now grants the controller those minimal cross-namespace permissions, and controller reconciliation updates existing runtime Role/RoleBinding objects so stale rules do not survive redeploy. |
| 2026-08-10 | **FIXED, VERIFIED**: Next rerun reached the full security-autonomy-fixer path and produced two new PR #4 commits, but reported `14/16` because s13 still read apiserver logs from the legacy `default` namespace and s37 ran after controller had already deleted no-active JM/TM/Sandbox Deployments. s13 now reads apiserver evidence from `config.SYSTEM_NAMESPACE`. Controller now keeps completed/no-active application runtime inside the existing `runtime.tm_orphan_timeout` observation window (default `3m`) so s37 can exec into TM/Sandbox pods and verify `/var/flowgent/{namespaceId}/{flowId}/{runId}/{taskId}`; deleting the flow definition still immediately removes JM/TM/Sandbox. |

## 0. E2E Runner Middleware and Build Timeouts (MEDIUM)

**Bug**: the runner reset a local Docker Compose SonarQube stack even when
`SONARQUBE_URL` selected a healthy external service. This host has Podman but
the Compose v1 provider requires a nonexistent Docker socket. A cold Go build
also exceeded the runner's fixed 120-second timeout.

**Fix**: use external `SONARQUBE_URL` for health verification without Compose
lifecycle operations, and allow 600 seconds for `make build:core`. No secret
values are logged. An external instance may deny `/api/system/health`; in that
case an `UP` result from `/api/system/status` is the readiness evidence.
The image recipe additionally requires `IN_CN_GFW=true` alongside the proxy
variable; the runner supplies both only to `make build:image:core`.

## 0.1 Deployment Readiness Ordering (CRITICAL)

**Bug**: Helm started the API server concurrently with EMQX. If the API server
won that race, its lifecycle MQTT publisher stayed unavailable and lifecycle
verification could falsely test a degraded deployment.

**Fix**: the E2E deployer waits for EMQX rollout before it applies runtime
environment variables that roll the API server. This makes broker readiness a
hard prerequisite for API lifecycle verification.

## 1. Sandbox Pod Deployment Bugs (CRITICAL — 3 bugs)

### 1.1 Sandbox Config Path Ignored (`pkg/cmd/pkg/sandbox.go:50-52`)

**Bug**: The sandbox binary reads `FLOWGENT__CONFIG__FILE` env var for config path, and **ignores the `-c` CLI flag**. The K8sRM `ensureSandboxDeployment()` passes `-c /etc/flowgent/flowgent.yaml` in the container command, but this flag has no effect — the sandbox defaults to `etc/flowgent.yaml` (relative to CWD, which is `/`), causing `open etc/flowgent.yaml: no such file or directory`.

**Workaround**: Set `FLOWGENT__CONFIG__FILE=/etc/flowgent/flowgent.yaml` env var on the sandbox deployment.

**Fix needed**: Either read the `-c` flag from cobra/CLI args in `startSandboxService()`, or set the env var in the deployment spec.

### 1.2 Sandbox Image Missing from Helm Values (`deploy/helm/flowgent/values.yaml`)

**Bug**: The Helm values.yaml has `sandbox.deployment.enabled: true` but **no `sandbox.deployment.image` field**. The K8sRM reads `svcCfg.Sandbox.Deployment.Image` which is empty string, causing `spec.template.spec.containers[0].image: Required value` when creating the sandbox Deployment.

**Workaround**: Add `image: localhost/flowgent-core:latest` to the `sandbox.deployment` section in the ConfigMap (or Helm values).

**Fix needed**: Add `image: ""` default in Helm values with comment, or fall back to `global.image` when `sandbox.deployment.image` is empty.

### 1.3 Sandbox Workspace PVC Uses ReadWriteMany on local-path (`kubernetes.go:588`)

**Bug**: The K8sRM `ensureSandboxDeployment()` creates a PVC with `PersistentVolumeClaimVolumeSource` using `s.deployName + "-sandbox-workspace"` (i.e., `flowgent-taskmanager-sandbox-workspace`). The storage class `local-path` (default in k3s) only supports `ReadWriteOnce` and `ReadWriteOncePod`, not `ReadWriteMany`. This causes the PVC to never bind and sandbox pods stay `Pending`.

**Workaround**: Manually create sandbox deployment with `hostPath` volume instead of PVC. Use `/mnt/disk1/flowgent/e2e` as hostPath source.

**Fix needed**: In single-node k3s environments, use `hostPath` for sandbox workspace volume instead of PVC. Or change the PVC access mode to `ReadWriteOnce`.

## 2. JM Deployment Bugs (MEDIUM — 2 bugs)

### 2.1 ImagePullPolicy Set to Always (`controller.go buildJMDeployment`)

**Bug**: The Controller creates JM deployments with `imagePullPolicy: Always`, which forces k3s containerd to pull from the `localhost` registry. In offline/single-node environments without a running registry, this causes `ImagePullBackOff` even though the image is already imported into containerd via `k3s ctr images import`.

**Workaround**: After controller creates JM deployments, patch them:
```bash
kubectl patch deploy -n flowgent-default -l app=flowgent-jobmanager \
  -p '{"spec":{"template":{"spec":{"containers":[{"name":"jobmanager","imagePullPolicy":"IfNotPresent"}]}}}}'
```

**Fix needed**: Set `imagePullPolicy: IfNotPresent` in `buildJMDeployment()` when building the container spec.

### 2.2 JM Deployments Need Cross-Namespace ConfigMap Copy

**Bug**: JM pods run in `flowgent-{namespace_id}` namespace (e.g., `flowgent-default`) but the `flowgent-config` ConfigMap only exists in the `default` namespace. The controller needs to copy it. Same for `flowgent-taskmanager-config` used by sandbox deployment.

**Workaround**: After Helm install, copy the ConfigMap:
```bash
kubectl get configmap flowgent-config -n default -o yaml | \
  sed 's/namespace: default/namespace: flowgent-default/' | kubectl apply -f -
```

**Fix needed**: The controller's `ensureApplicationInfra()` should copy the ConfigMap when creating the application namespace.

## 3. ConfigMap YAML Corruption (LOW)

**Bug**: When the ConfigMap's embedded `flowgent.yaml` is processed through PyYAML load/dump cycle, duplicate `image` keys can appear (e.g., in `mgmt.otel.image` AND `sandbox.deployment.image`), causing Go's YAML parser to fail with `mapping key "image" already defined`.

**Workaround**: Use `kubectl create configmap --from-file` instead of PyYAML pipelines when modifying the ConfigMap.

## 4. MQTT Topic Uses K8s Namespace (MEDIUM)

**Bug**: When triggering via `POST /api/v1/default/runs`, the run's `namespace_id` is set to `flowgent-default` (K8s namespace) rather than `default` (tenant namespace). MQTT topics use the tenant namespace from the trigger endpoint. The `POST /api/v1/default/flows/{flow_id}/trigger` correctly uses `default`.

**Workaround**: Always trigger via `/flows/{flow_id}/trigger` instead of `/runs`.

## 5. GH CLI Auth Mismatch (LOW)

**Bug**: `GH_TOKEN` in `~/.wl4gshrc.sec` is a fine-grained PAT configured for `api.githubcopilot.com/mcp/`, not the standard GitHub REST API. `gh` CLI and direct GitHub API calls fail with 401.

**Workaround**: Use MCP tools within the flow DAG for GitHub operations. Stage 10 verifier should use the flow's own PR output URL rather than making direct GitHub API calls.

## 6. Changelog

| Date | Change |
|------|--------|
| 2026-08-01 | Documented 8 critical/medium bugs. |
| 2026-08-08 | **FIXED**: external SonarQube support and cold-build timeout in the E2E runner. |
| 2026-08-01 | **FIXED**: §1.1 Sandbox config path — added `FLOWGENT__CONFIG__FILE` env var in sandbox deployment spec (`kubernetes.go:571`). |
| 2026-08-01 | **FIXED**: §1.2 Sandbox image missing — added `defaultIfEmpty(cfg.SandboxImage, cfg.TMImage)` fallback (`kubernetes.go:169`). |
| 2026-08-01 | **FIXED**: §1.3 Sandbox PVC RWX — changed to `hostPath` with `DirectoryOrCreate` type (`kubernetes.go:590`). |
| 2026-08-01 | **FIXED**: §2.1 JM imagePullPolicy — added `ImagePullPolicy: corev1.PullIfNotPresent` in `buildJMDeployment` (`controller.go:443`). |
| 2026-08-01 | **FIXED**: §2.2 Cross-namespace ConfigMap — changed `ensureSandboxDeployment` ConfigMap name to `flowgent-config` (was `flowgent-taskmanager-config`), and controller now checks for ConfigMap on every reconcile tick (not just namespace creation). |
| 2026-08-01 | **VERIFIED**: Round 1 complete — 27-node DAG executed, 135 MQTT messages via non-$share subscription. Stages 1-9 passed. Stage 10 partial (flow runs but no new commits visible on PR#4 — likely MCP tool behavior or token scope). Workspace empty in TM/sandbox pods (inline sandbox used; files at different path). |
| 2026-08-01 | **NEW**: §4 MQTT topic namespace mismatch, §5 GH CLI auth mismatch. |
| 2026-08-01 | **NEW**: §6 YAML node format — flow YAML used `data:` nesting for node configs which is lost during YAML→JSON→Go conversion. Fixed by flattening to top-level fields (tool/args/script directly under node, not nested under `data:`). |
| 2026-08-01 | **NEW**: §7 `NodeToTaskType(n.Type)` uses `n.Type` which is empty in JSON-unmarshalled nodes. All 27 nodes executed as noop. Fixed by changing to `NodeToTaskType(n.Kind)` (`pkg/core/pkg/engine/jobmanager/jobmaster.go:322`). |
| 2026-08-01 | **NEW**: §8 Tasks execute as noop despite correct `kind` — all 27 nodes complete in <1s with SUCCESS status. TM logs show `task_type=""` errors before JM restart; after JM fix, tasks "succeed" but no real LLM/MCP/sandbox execution occurs. Root cause TBD. |
| 2026-08-01 | **NEW**: §9 TaskRun status mismatch — engine writes `SUCCESS` but verifiers expect `COMPLETED`. Fixed verifiers to accept both. |
| 2026-08-01 | **NEW**: §10 JM cross-namespace DNS — JM in `flowgent-default` resolves `flowgent-apiserver` internally (not FQDN). Fix: set `FLOWGENT__RUNTIME__API_SERVER_URL=http://flowgent-apiserver.default.svc.cluster.local:9999` on JM deployments. |
| 2026-08-01 | **NEW**: §11 TM pods missing `FLOWGENT__RUNTIME__API_SERVER_URL` env var — defaults to `http://flowgent-apiserver:9999` (correct in same namespace). |
| 2026-08-01 | **NEW**: §12 Verifier import paths — s31-s34 use `import _common as c` (implicit relative import). Fixed to `from verifier import _common as c`. |
| 2026-08-01 | **NEW**: §13 Verifier s12 uses `kind` column not in `orh_agentflow` schema. Fixed queries to use `mode` and `agentflow_id` instead. |
| 2026-08-01 | **NEW**: §14 Verifier s21 `updated_at > created_at` check too strict — fails when PG timestamps match within same transaction. Changed to `>=`. |
| 2026-08-01 | **ROUND1**: Round 1 complete — redeploy OK, console import OK, API CRUD mostly OK (3 tests fixed), MQTT topics verified, flow triggered. Flow executes instantly (all 27 nodes noop in <1s). PR #4 has no flowgent fix commits. See §8 for core noop issue. |
| 2026-08-02 | **NEW**: §15 LLM provider console import broken — `data:` block fields (provider, endpoint, apikey) not unwrapped into entity. Provider stored with empty provider type, no endpoint, no apikey. Workaround: register provider via REST API directly. Root cause: console import doesn't recursively unwrap data/ metadata/ structure. |
| 2026-08-02 | **NEW**: §16 MCP `enabled: false` after console import — MCP YAMLs have `enabled: false` by default; TM pods check MCP via FlowgentClient REST on startup and skip disabled ones. Must enable MCPs via PUT API + restart TM after each console import. |
| 2026-08-02 | **NEW**: §17 LLM provider `status` case sensitivity — `LlmProviderManager.registerDB` checks `dbP.Status != "ACTIVE"` (uppercase) but console import writes `active` (lowercase). Providers silently skipped. **Root cause**: `status` field comparison is case-sensitive; YAML specifies `status: active` but code expects `ACTIVE`. |
| 2026-08-02 | **FIXED**: §8 Tasks execute as noop — **REAL root cause was §17 (LLM provider not loaded due to status case mismatch) + §16 (MCPs disabled) + variable substitution bug (§18)**. After fixing all three, flow executes real LLM/MCP/sandbox work: SonarQube returned 1411 issues, DeepSeek generated real patches, GitHub MCP pushed commits. |
| 2026-08-02 | **FIXED**: §18 Variable `${vars.repo}` not resolved — `Args` were merged into `plan.Input` AFTER `resolveInput()` was called, so `${vars.x}` references in Args remained literal. Fix: merge Args into RawInput BEFORE resolveInput (`jobmaster.go:417-428`). |
| 2026-08-02 | **NEW**: §19 Flow creates commits on wrong branch — `create-branch` for `fix/flowgent_sec_auto_fix` fails with "422 Reference already exists" (expected), then `commit-fixes` creates NEW branch `security-bot/fix-{run_id}` instead of committing to existing branch. Flow needs to detect existing branch and commit to it directly. PR #4 receives no new commits. |
| 2026-08-02 | **NEW**: §20 Review committee rejects all patches (0/3 approve) — quality reviewers find issues with generated patches ("not minimal", "introduce constants for repeated strings"). On second iteration, `generate-fixes` FAILS (null output). LLM quality tuning needed. |
| 2026-08-02 | **NEW**: §21 Verifier s32/s33/s34 strict pass/fail — verifiers fail hard when tasks not reached, but flow may still be executing or in loopback. Should accept partial results or poll for longer. |
| 2026-08-02 | **ROUND2**: Full deployment + real flow execution achieved. LLM generates patches, MCP tools call GitHub/SonarQube. 24/24 tasks complete (NOT noop). Variable resolution fixed. Flow creates commits pushed to GitHub (wrong branch). PR #4 still has no new commits from this round. |
| 2026-08-08 | **FIXED**: §21 verifier false positives — s32/s33/s34 now use fixed expected check counts for reached nodes, current 27-node DAG mapping, branch-aware PR delivery checks, and robust MQTT payload node extraction. |
| 2026-08-08 | **FIXED**: s11/s12 soft warnings — infra readiness and console-import resource counts now raise assertions, so a round cannot pass with unready Helm pods, failed apiserver health, missing binary, failed import, or missing DB resources. |
| 2026-08-08 | **FIXED**: s13 OTEL verifier runtime bugs — added missing `urllib.request` import, synchronized expected node coverage with `git-clone/read-source-files`, and switched trigger path to `/flows/security-autonomy-fixer/trigger`. |
| 2026-08-08 | **FIXED**: MCP seeding idempotency — s13 and shared s31+ seeding now PUT existing GitHub/SonarQube MCP definitions with `enabled=true` so console-import defaults cannot leave TM without MCP tools. |
| 2026-08-08 | **FIXED**: Round 1 preflight failure — EMQX rollout completed before MQTT 1883 accepted connections, causing apiserver lifecycle MQTT publisher to initialize disabled. Deployer now waits for EMQX ClusterIP TCP readiness before apiserver runtime rollout. |
| 2026-08-08 | **FIXED**: Round 1 cleanup hang — `flowgent-default` namespace stayed Terminating because metrics.k8s.io discovery was stale. Deployer now deletes application namespaces with `--wait=false` and finalizes stuck empty terminating namespaces through the Kubernetes finalize API. |
| 2026-08-08 | **FIXED**: Round 1 port-forward leak — stale `kubectl port-forward` on localhost 9999 masked tunnel readiness. Runner now kills stale Flowgent service port-forwards before verification and requires all forwarded ports to stay reachable with the process alive. |
| 2026-08-08 | **FIXED**: Round 1 Jaeger local port collision — host already had 16686 listening, causing verifier tunnel setup to fail. Runner now reuses already reachable local ports instead of rebinding them. |
| 2026-08-08 | **FIXED**: Round 1 JM pods stuck ContainerCreating — application namespace was recreated without `flowgent-config`, so JM pods could not mount config. Deployer now creates/labels `flowgent-default` and copies `flowgent-config` during Helm redeploy. |
| 2026-08-08 | **FIXED**: Round 1 stale flow fan-out — Flowgent PG was not reset, so controller recreated historical `test-flow-*` JM deployments. Runner now resets the Flowgent PostgreSQL `public` schema before full redeploy/import. |
| 2026-08-08 | **FIXED**: Round 1 tunnel drop cascade — apiserver/EMQX port-forwards died after initial readiness and later scenarios failed with connection refused. Runner now revalidates and recreates required port-forwards before every scenario. |
| 2026-08-08 | **FIXED**: Round 1 TM/Sandbox image missing — copied ConfigMap still lacked runtime image and cross-namespace broker/API settings, so JM created invalid TM/Sandbox deployments. Deployer now injects MQTT/API FQDNs plus JM/TM/Sandbox images into both default and application ConfigMaps before rollout. |
| 2026-08-08 | **FIXED**: Round 1 tunnel ready false positive — port-forward binding could be observable before apiserver/EMQX/Jaeger probes were stable, causing s11 to fail immediately after runner printed ready. Runner now requires repeated health probes before every scenario. |
| 2026-08-08 | **FIXED**: Round 1 LLM provider skipped — console import copied `metadata.status: active` into embedded `BaseEntity.Status`, but `LlmProviderInfo.Status` shadows that field and stayed empty in DB. Import now normalizes the provider status to `ACTIVE`, so TM can register `OPENAI/deepseek-chat`. |
| 2026-08-08 | **FIXED**: Round 1 JM OTEL short DNS — application JM pods used `flowgent-jaeger:4318` from the copied ConfigMap, which does not resolve from `flowgent-default`. Deployer now injects `flowgent-jaeger.default.svc.cluster.local:4318` into runtime env and copied ConfigMaps. |
| 2026-08-08 | **FIXED**: Round 1 incomplete redeploy — controller-created TM/Sandbox deployments live in the tenant namespace and lacked `flowgent.io/mode=application`, so full redeploy left old workers running across DB resets. Deployer now force-deletes `app.kubernetes.io/component in (taskmanager,sandbox)` in the tenant namespace before Helm reinstall. |
| 2026-08-08 | **FIXED**: Round 1 verifier-trigger interference — imported `security-autonomy-fixer` automatic schedule/webhook triggers caused controller-created runs to race with verifier-created runs. The E2E resource now leaves run creation to verifier scenarios only. |
| 2026-08-08 | **FIXED**: Round 1 OTEL false positive — Flowgent logged tracing initialization but Jaeger had no Flowgent services. The tracing provider now emits and force-flushes an `otel.startup` span during initialization, so exporter failures are visible and s13 can verify real Jaeger ingestion. |
| 2026-08-08 | **FIXED**: Round 1 stale runtime image cache — Dockerfile uses a bind-mounted source tree in the builder stage, but the image build did not vary a cache key with source changes. `build-image-core` now passes `BUILD_TS` so runtime pods receive freshly built code. |
| 2026-08-08 | **FIXED**: Round 1 duplicate flow versions — `SaveSpec` created `MAX(version)+1` on every console/API import, so runner import, s12 import, and s13 setup produced multiple definitions and controller-created duplicate runs. Flow `SaveSpec` now performs deterministic version-1 upsert for Postgres and SQLite. |
| 2026-08-08 | **FIXED**: Round 1 wrong Jaeger target — runner reused host port `16686`, which was already occupied by a Docker Jaeger unrelated to the K8s Flowgent deployment. K8s Jaeger is now port-forwarded to local `16687`, and verifiers query that URL by default. |
| 2026-08-08 | **FIXED**: Round 1 OTEL node coverage missing — Jaeger contained only `jobmaster.execute`/startup spans, so s13 could not prove DAG node execution. JobMaster now creates one `jobmaster.node` span per scheduled node with `flowgent.node_id`, task type, plan ID, task ID, run ID, and flow ID attributes. |
| 2026-08-08 | **FIXED**: Round 1 AgentFlow CRUD verifier expected legacy version increments — `SaveSpec` is now deterministic version-1 upsert, so s21 now verifies that the single version-1 definition row is updated instead of expecting a second version row. |
| 2026-08-08 | **FIXED**: Round 1 TaskRun list 500 — Postgres `ListByFlowRun` used `SELECT *` plus pgx struct mapping, which failed on embedded `BaseEntity` fields such as `namespace_id`. TaskRun Postgres lookups now use `utils.Columns` and `utils.ScanStruct`; s21 also reports non-JSON API errors explicitly. |
| 2026-08-08 | **FIXED**: Round 1 `ctrl/run/created` MQTT audit missing — trigger-created runs publish control events on tenant namespace topics. The apiserver was publishing to `flowgent-default` because `run.Namespace` stores the K8s application namespace for JM polling. `publishRunCreatedEvent` now uses the flow spec tenant namespace for the MQTT topic while keeping K8s namespace in payload diagnostics. |
| 2026-08-08 | **FIXED**: Round 1 s32/s37 TM/Sandbox namespace mismatch — verifiers expected TM/Sandbox pods/deployments in `flowgent-default`, but runtime workers are shared deployments in tenant namespace `default`. Shared worker readiness and workspace checks now target the tenant namespace. |
| 2026-08-08 | **FIXED**: Round 1 Knowledge search returned no results — store search used the whole query as one ILIKE string, so multi-word queries and RAG prompts rarely matched. Knowledge search now tokenizes the query and OR-matches title/content terms for Postgres and SQLite. |
| 2026-08-08 | **FIXED**: Round 1 s13 path-dependent span false failure — core node OTEL coverage passed, but s13 still required 27 spans for all DAG nodes even when human/branch nodes were skipped. s13 now hard-gates only the always-executed core path and reports path-dependent nodes informationally. |
| 2026-08-08 | **FIXED**: Round 1 s32 shared worker namespace typo — verifier used undefined `K8S_NAMESPACE` instead of `NAMESPACE`, so s32 aborted before polling the flow and downstream scenarios read partial state. |
| 2026-08-08 | **FIXED**: Round 1 `$share` local fan-out — MQTTMessager registered one broker subscription per topic but invoked every local handler for `$share` subscriptions, causing all TM slots to execute the same plan. MQTT and Local messagers now round-robin one local handler for `$share/*` topics and keep fan-out for ordinary topics. |
| 2026-08-08 | **FIXED**: Round 1 git-clone workspace mismatch — flow script cloned into `/workspace/rengine`, outside the shared `/var/flowgent` mount required by s37. The flow now clones and reads source from `/var/flowgent/repos/rengine`. |
| 2026-08-08 | **FIXED**: Round 1 sandbox script runtime dependencies missing — runtime image lacked `git` and `python3`, which git-clone/read-source-files require once sandbox results are checked correctly. Dockerfile now installs `git python3` alongside the existing shell tools. |
| 2026-08-08 | **FIXED**: Diagnostic run hid sandbox failures — TM marked sandbox tasks `SUCCESS` when `TaskResult.Error` was non-empty, so `git-clone` and `read-source-files` appeared successful while DB rows contained errors. SlotWorker and direct TaskManager execution now persist `FAILED` and publish error state when a sandbox result has `Error`. |
| 2026-08-08 | **FIXED**: seccomp allowlist rejected host-only and URL entries — `git-clone` used `github.com`, and `wait-rescan` used `${vars.sonarqube_url}` with a scheme/port, but seccomp required raw `host:port`. The seccomp parser now accepts host-only, `host:port`, and URL entries with sensible default ports. |
| 2026-08-08 | **FIXED**: sandbox scripts ran from the shared workspace root instead of the plan span directory, so scripts using relative `input.json` failed. In-process/seccomp sandbox commands now run with cwd set to the script path, matching Docker sandbox behavior. |
| 2026-08-08 | **FIXED**: s32/s33 verifier output assumptions were stale — GitHub/SonarQube MCP outputs are often JSON wrapped in `text`, GitHub commit uses `sha`, `generate-fixes` returns full file contents rather than diffs, and committee `decision=false` was treated as missing. Common output parsing and s32/s33 assertions now match the current flow contract. |
| 2026-08-08 | **FIXED**: s37 could pass from stale workspace evidence or fail without current-run context. It now loads `.last_run_id`, checks the current run plans directory, and requires `/var/flowgent/repos/rengine/.git` inside the pod via `kubectl exec` only. |
| 2026-08-08 | **FIXED**: seccomp allowlist deadlocked sandbox scripts — the BPF filter intercepted `sendmsg`, but sandbox-child uses `sendmsg` to pass the seccomp notifier fd to the parent after installing the filter. The allowlist/denylist filter now uses user notification only for `connect`, leaving fd transfer and DNS send paths unblocked. |
| 2026-08-08 | **FIXED**: TM pods incorrectly started embedded sandbox runners even when `sandbox.deployment.enabled=true`, competing with the dedicated Sandbox pods and violating the L1 cluster-routed sandbox consumer design. `StartTaskManager` now passes `SandboxDeploymentEnabled` from config into `TaskManagerConfig`; Sandbox pods remain the distributed `$share` consumers. |
| 2026-08-08 | **FIXED**: sandbox seccomp re-exec target was not wired into the CLI. `ScriptCmd` launches `/proc/self/exe sandbox-child ...`, but the root command did not dispatch `sandbox-child` to `seccomp.ChildMain()`. A hidden `sandbox-child` command now handles the child process path. |
| 2026-08-08 | **FIXED**: sandbox seccomp notifier handshake could hang forever — after `cmd.Start()`, the parent process still held the child's socketpair fd. If `sandbox-child` exited before sending the notifier fd, the parent-side `Recvmsg` never saw EOF, `executeInProcess` blocked before `cmd.Wait()`, child processes became zombies, and no `sandbox/result` was published. Sandbox execution now closes inherited `ExtraFiles` in the parent after start and fails fast if notifier handshake does not complete. |
| 2026-08-08 | **FIXED**: seccomp user-notification listener flag used the wrong bit. The code used `1<<2` (`SECCOMP_FILTER_FLAG_SPEC_ALLOW`) instead of `1<<3` (`SECCOMP_FILTER_FLAG_NEW_LISTENER`), so allowlist/denylist filters with `RET_USER_NOTIF` did not create a listener and sandbox-child failed before fd handoff. The constant is corrected and covered by an allowlist notifier smoke test. |
| 2026-08-08 | **FIXED**: seccomp-child missed `PR_SET_NO_NEW_PRIVS` and passed the wrong pointer type to `seccomp(SECCOMP_SET_MODE_FILTER)`. The re-exec path now sets no-new-privs and passes a `sock_fprog`, matching the non-reexec install path. |
| 2026-08-08 | **FIXED**: seccomp notifier incorrectly used ordinary `read/write` on the listener fd. Linux seccomp user notifications require `SECCOMP_IOCTL_NOTIF_RECV` and `SECCOMP_IOCTL_NOTIF_SEND`; the notifier loop now uses ioctl structs and returns `CONTINUE` for allowed connects or negative errno for denied connects. |
| 2026-08-08 | **FIXED**: sandbox result MQTT publish could fail transiently with `connection reset by peer`, while SandboxManager still logged `sandbox result published`. TM then waited for `sandbox/result`, JM timed out the plan, and verifiers missed the non-`$share` result message even though `result.json` existed in `/var/flowgent`. Sandbox result publish now retries with backoff and only logs success after `Publish` succeeds. |
| 2026-08-08 | **FIXED**: K8s ResourceManager default plan timeout (5m) was shorter than TM SandboxExecutor result fallback (10m). If a sandbox result MQTT message was lost, TM could recover from `result.json` only after JM had already timed out. The default K8s plan timeout is now 12m, longer than the sandbox fallback window. |
| 2026-08-08 | **FIXED**: MQTT `Publish()` treated token wait timeout as success. Sandbox result retry stopped after a timed-out publish even though subscribers did not receive the message. MQTT connect/publish now return explicit timeout errors, so callers retry or fail instead of reporting false success. |
| 2026-08-08 | **FIXED**: TM SandboxExecutor only read `result.json` after its full timeout, so a successfully written sandbox result could not unblock the DAG if the MQTT result callback was missed. SandboxExecutor now polls the shared result file while also listening to MQTT and returns as soon as either path completes. |
| 2026-08-08 | **FIXED**: TM slot workers published `exec/results` once and ignored publish errors. A transient MQTT `connection reset by peer` after `read-source-files` left JM waiting even though TM had persisted task success and sandbox results existed. Slot workers now retry TM→JM result publication with backoff and log final failure explicitly. |
| 2026-08-08 | **FIXED**: raw lifecycle MQTT publishers reused the static configured client ID and treated connect/publish timeouts as success. Apiserver/all-in-one lifecycle publishers now use hostname-qualified client IDs and return timeout errors instead of masking a broken MQTT path. |
| 2026-08-08 | **FIXED**: MQTT clients did not actively recover after broker-side connection resets; Paho auto-reconnect alone left TM heartbeat and `exec/results` publishes timing out indefinitely. `MQTTMessager` now owns reconnect/re-subscribe itself and disables Paho auto-reconnect to avoid competing reconnect loops with the same client ID. |
| 2026-08-08 | **FIXED**: E2E deployer rolled apiserver, controller, and notifier at the same time after env injection. Controller could log an initial apiserver connection-refused error before healthz was available, causing s11 to fail even though the next poll recovered. Deployer now rolls out and health-checks apiserver before starting dependent Helm deployments. |
| 2026-08-08 | **FIXED**: Diagnostic run `bf6c009c-09e2-49e1-aa3b-f03135099ddb` stalled after `read-source-files` because concurrent TM/Sandbox publish paths could replace/disconnect the shared Paho client while another goroutine was publishing, causing repeated EMQX `connection reset by peer` failures for `sandbox/result` and `exec/results`. MQTT publish/subscribe and client lifecycle are now serialized, and publish failure forces a fresh client dial before retry. |
| 2026-08-08 | **FIXED**: Diagnostic run `1c57974d-1e17-4228-a7cd-e9cd4bfe4a8d` showed reset persisted because Paho `OnConnect` performed asynchronous re-subscribe on the same freshly reconnected client while the caller immediately retried publish. `MQTTMessager` now performs re-subscribe synchronously after a successful dial while still holding the connection lifecycle lock. |
| 2026-08-08 | **FIXED**: Diagnostic run `63df5889-2450-4a1c-85e9-fefdf637c155` proved the persistent EMQX reset was payload-size related: `read-source-files` persisted ~1.18MB output, exceeding the in-cluster broker packet limit and causing broker-side disconnects on `sandbox/result` and `exec/results`. The Helm chart now exposes `emqx.maxPacketSize` and defaults the in-cluster EMQX deployment to `8MB`; `VERIFICATION.md` documents this as mandatory infra config. |
| 2026-08-08 | **FIXED**: Diagnostic run `b4152b96-be2c-43a6-b9f6-64c4e849452c` passed the large MQTT result boundary, then failed at `generate-fixes` with `agent output JSON: unexpected end of JSON input`. The flow prompt was too large/noisy because it handed the full issue/file context to the fixer LLM. `read-source-files` now selects a bounded context window (max 3 files, max 6 issues, max 12KB per file) and `generate-fixes` consumes the selected `read-source-files.issues`. |
| 2026-08-08 | **FIXED**: Diagnostic run `9f45f41b-a286-4b8d-9d01-ca7388bc2b2f` still failed at `generate-fixes` with truncated JSON. The bounded window selected large generated Swagger files and the runtime ignored `AgentInfo.MaxTokens`, so the fixer could not reliably emit a full-file JSON response. LLM provider calls now honor `max_tokens`, fixer output is constrained to compact JSON, and `read-source-files` skips generated code, prefers the smallest real source file, and passes a single-file issue window. |
| 2026-08-08 | **FIXED**: Diagnostic run `7e8b1b4d-5e12-4b44-ab33-11c66adfdd8f` reached PR delivery and rescan but failed at `wait-rescan`: sandbox seccomp tried to resolve the literal `${vars.sonarqube_url}` from `network_policy.allowed`. Per-node network policies are not DAG variable interpolation paths. The flow now sets the E2E SonarQube URL to `https://sonarqube.wl4g.com` and uses the same concrete URL in `wait-rescan.network_policy.allowed`. |
| 2026-08-08 | **FIXED**: Diagnostic run `d5a57c33-2f83-4d59-875e-cced01a4bc2b` fixed the seccomp allowlist but `wait-rescan` still failed because its bash script embedded `${vars.sonarqube_url}` and `${vars.repo}`, which bash treated as invalid substitutions. `wait-rescan` now receives `sonarqube_url` and `repo` through node args, reads them from `input.json`, and treats an absent SonarQube CE task as a completed no-op wait. |
| 2026-08-08 | **FIXED**: Diagnostic run `ddb73948-fcf8-44d9-92c7-a1653d14b36c` stalled at `git-clone` because the shared repo already existed and the flow attempted `git pull origin master`, which blocked inside the Sandbox pod during GitHub network access. The flow now treats an existing `/var/flowgent/repos/{repo}/.git` as ready, only checks out `master` locally, and applies a 90-second command timeout to first-time clones. |
| 2026-08-09 | **FIXED**: Runtime workspace hierarchy drifted from the architecture contract. SandboxExecutor used `/var/flowgent/{namespace}/{flow}/runs/{run}/plans/{plan}/{span}`, and s37 only checked that old layout. SandboxExecutor now writes `/var/flowgent/{namespaceId}/{flowId}/{runId}/{taskId}`, `exec_id` is persisted as the task ID, and s37 verifies the current-run task directories plus absence of the legacy `runs/plans` path. |
| 2026-08-09 | **FIXED**: security-autonomy-fixer cloned Rengine into global `/var/flowgent/repos/rengine`, outside any run/task ownership boundary. The `git-clone` sandbox node now clones into its own task workspace under `repos/rengine`, and downstream nodes consume `${git-clone.path}`. |
| 2026-08-09 | **FIXED**: JM-owned TM Deployments could outlive their parent JM because KubernetesResourceManager scaled back to min replicas on shutdown and Controller only garbage-collected JM Deployments. K8sRM now labels per-flow TM Deployments with parent JM metadata, and Controller deletes TMs immediately on normal flow/JM deletion or after `runtime.tm_orphan_timeout` (default `3m`) when the parent JM disappears unexpectedly. |
| 2026-08-09 | **FIXED**: Multiple JM instances could poll and submit the same PENDING flow run because StartRunPoller did not claim runs with the existing distributed lock facility. JM pollers now acquire and renew a per-run lock before starting JobMaster; Helm chooses postgres locks automatically for `storage.type=POSTGRE`. |
| 2026-08-09 | **FIXED**: `Dockerfile.core` copied a non-existent `examples/` directory, so `make build:image:core` failed and runner redeployed a stale cluster image. The runtime image now copies only the built binary and existing `etc/` config tree. |
| 2026-08-09 | **FIXED**: Host shell credentials were loaded for console import, but JM/TM/Sandbox pods did not inherit them. `github` MCP initialization failed in TM with `server returned 4xx` because `GITHUB_TOKEN` was empty. E2E redeploy now creates `flowgent-e2e-runtime-env` in both tenant and application namespaces, Helm renders `runtime.credential_env_secret`, Controller mounts it into JM, and K8sRM mounts it into TM/Sandbox via optional `envFrom.secretRef`. |
| 2026-08-09 | **FIXED**: Soft-deleted flows remained visible through API `GET/LIST` and Controller reconciliation. The generic stores now hide `del_flag=true` rows, flow stores apply active-row filters to versioned queries, and `SaveSpec` restores `status=ACTIVE,del_flag=false` on idempotent re-import. s21 and s23 now hard-fail if DELETE leaves resources readable or if Controller does not garbage-collect the JM Deployment. |
| 2026-08-09 | **FIXED**: After generic soft-delete filtering, MCP/Provider/Channel `GET` returned HTTP 500 for not-found store errors. API handlers now map SQL/no-row/not-found errors to HTTP 404 so s21 can assert DELETE hides resources without accepting 5xx responses. |
| 2026-08-09 | **FIXED**: s21/s23 verifier-created `test-flow-*` resources could leak into later scenarios and s11 only reported them as a warning. s21 now deletes setup flows in `finally`, s23 cleans up failed lifecycle flows, the deployer force-deletes legacy per-Flow runtime pods/deployments during full redeploy, and s11 fails if any `test-flow-*` runtime pod/deployment survives after redeploy/import. |
| 2026-08-09 | **FIXED**: Repeated image builds consumed enough host ephemeral storage to trigger K8s `DiskPressure` eviction and leave Helm pods Pending. The host was recovered by pruning Docker/Podman builder cache and `/tmp/go-build*`; runner now checks root disk headroom around image build/import and cleans only transient build cache when free space is below 12GiB or usage exceeds 88%. |
| 2026-08-09 | **FIXED**: After k3s restart, `kubectl` defaulted to root-only `/etc/rancher/k3s/k3s.yaml` when `KUBECONFIG` was unset; invalid round 2 later exposed the opposite edge case where `~/.kube/config` existed but was 0 bytes and caused API discovery failures. Runner, deployer, and shared verifier config now accept only non-empty kubeconfig files and fall back to `/etc/rancher/k3s/k3s.yaml` when the user kubeconfig is missing or empty. |
| 2026-08-09 | **FIXED**: Diagnostic runs `8fefdbfc-3730-451a-87ee-656aedf4890e`, `2c7a4029-7f24-4d7d-9e14-1e9c8106f9cd`, `8e17deec-e48b-4120-8631-da0e3f4bab41`, and `e50086ca-d657-41df-b818-e83ae446a041` failed at the `git-clone` sandbox node. The workspace path was correct, but pod-local `git clone https://github.com/wl4g/rengine.git` either had no pod proxy, hit a connection refusal to the injected proxy, or lacked a seccomp allowlist entry for the actual proxy endpoint. The E2E deployer now mirrors host proxy env into `flowgent-e2e-runtime-env`, rewrites `localhost`/`127.0.0.1` proxy hosts to the pod default gateway derived from node PodCIDR (with node InternalIP fallback), merges Flowgent/Kubernetes service domains into `NO_PROXY`, exports `FLOWGENT_E2E_PROXY_ALLOWLIST_ENTRY`, and s12/s23 hard-fail if configured proxy values remain unresolved or pod-unreachable. The `git-clone` node now probes the selected proxy, retries transient clone failures with correct exit-code handling, and falls back to the pod default gateway proxy with the same proxy port when the primary proxy is unreachable. |
| 2026-08-09 | **FIXED**: Diagnostic runs `e4f82bb8-c890-4038-a427-f1b024c073b1` and `c17f3853-a822-4ccb-a86d-d09ec903ba02` completed the DAG but `generate-fixes` returned empty `files`/`patches`. The reproduced root cause was upstream of the LLM: `git-clone` wrote clone progress to stdout before its JSON result, so sandbox output was not parsed and `${git-clone.path}` stayed unresolved; `read-source-files` therefore selected zero files/issues, and GitHub commit tools later returned hidden failure text while s34/s35 still accepted historical PR commits. Sandbox results now parse the final stdout JSON object, expose parsed fields at top level for `${node.field}`, `git-clone` sends clone logs to stderr, s31/s13 resolve env placeholders when seeding flows through the API, s32 requires non-empty selected source context, s34 rejects hidden GitHub MCP failure text, and s35 requires new Flowgent security-fix commits after the s31 PR baseline. |
| 2026-08-09 | **FIXED**: Invalid round `8100a863-a27e-4d7c-a5f7-54404c0d230f` produced complete Jaeger trace evidence (25 spans, all 12 core nodes covered), but s13 failed because it treated the startup log line `OTEL tracing enabled` as the only acceptable OTEL-enabled proof. s13 now keeps Jaeger trace existence and core span coverage as mandatory gates, while allowing the infra evidence to come from either recent logs or the live pod/config runtime state. |
