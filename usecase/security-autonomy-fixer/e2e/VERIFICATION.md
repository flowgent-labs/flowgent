# 1. Overview

> **Runtime note (2026-08-08):** when `SONARQUBE_URL` is provided, the runner
> health-checks that externally managed service and does not reset a local
> Compose stack. Each E2E round still performs a full Flowgent Helm redeploy.
> EMQX readiness precedes the apiserver runtime rollout so MQTT lifecycle
> publication is verified against a live broker. The apiserver rollout and
> healthz check precede controller/notifier rollout so dependent components do
> not record startup connection-refused errors against an unready API server.
> The deployer also injects cross-namespace FQDNs for MQTT, API server, and
> Jaeger, plus JM/TM/Sandbox images, before workload pods start. Full
> redeploy installs system services in `flowgen-system`, clears stale
> controller-created application runtime-cluster Deployments/pods in
> `flowgent-{namespace}`, and removes legacy workers from `default`, so no
> worker pod survives across DB resets.
> Classic `runner.py` runs are verifier-triggered. UI system acceptance runs are
> browser-triggered and browser-approved; their verifiers must reuse that run
> and must not seed resources or create a replacement run. Importing flow
> definitions is metadata registration and must not create idle JM/TM/Sandbox
> Deployments; Controller starts a per-run application JM only while a real
> application run is `PENDING`, `RUNNING`, or `PAUSED`.
> Flow definitions are imported idempotently as version `1`; repeated runner or
> verifier imports must not create additional flow versions.
> MQTT topic namespace segments use the tenant namespace such as `default`, not
> the K8s workload namespace `flowgent-default`. Active-run application JM and
> runtime-cluster TM/Sandbox pods run in the workload namespace and are verified
> there.
> The controller ClusterRole must hold every permission it grants to the
> application-namespace `flowgent-runtime` Role; otherwise Kubernetes rejects
> Role creation as RBAC privilege escalation and JM can start without being able
> to scale TM/Sandbox workers.
> A completed application runtime cluster is retained for the configured
> `runtime.tm_orphan_timeout` observation window (default `3m`) so final
> verifiers can inspect worker workspace state. Deleting the Flow removes the
> application runtime cluster immediately. Session-mode worker deployments are
> Helm-owned and intentionally excluded from per-run garbage collection.
> Runtime `$share/*` subscriptions are load-balanced both by the broker and by
> any local multi-handler dispatcher inside a component process. Verifier
> subscribers must remain ordinary non-`$share` subscriptions so they observe
> messages without consuming production work.
> Runtime credentials are not inherited implicitly from the host by JM/TM/Sandbox
> pods. The runner sources `~/.bashrc`, `~/.bash_profile`, and
> `~/.wl4gshrc.sec`, creates `flowgent-e2e-runtime-env` in both the system
> namespace and `flowgent-{namespace}`, and Helm sets
> `runtime.credential_env_secret` so Controller-created JM pods and JM-created
> TM/Sandbox pods mount it via optional `envFrom.secretRef`.
> When host proxy variables are present, the same runtime Secret also carries
> `HTTP_PROXY`/`HTTPS_PROXY` plus `NO_PROXY` for JM/TM/Sandbox. Localhost proxy
> values are rewritten to the pod default gateway derived from the node PodCIDR
> (falling back to node InternalIP) so sandbox tasks can reach GitHub from
> inside pods; `NO_PROXY` must include `.svc`,
> `.cluster.local`, and cluster CIDRs so internal Flowgent service calls never
> go through the external proxy.
> MQTT client IDs must be unique per component process/pod. Static configured
> IDs such as `flowgent` are only a base; runtime publishers append a host/pod
> identity so EMQX cannot disconnect another Flowgent component with the same
> client ID.
> Sandbox task results with a non-empty `error` field are task failures, even
> when the sandbox service publishes a result message. Verifiers must not treat
> `SUCCESS` rows with hidden sandbox errors as valid execution.
> Sandbox network allowlists accept host-only, `host:port`, and URL values; the
> seccomp layer resolves them to concrete `host:port` rules, applies user
> notification to outbound `connect`, and sandbox scripts execute from their task
> directory so `input.json` is available. Per-node
> `network_policy.allowed` entries must be concrete host/URL values because
> sandbox seccomp policy construction is not a DAG variable interpolation path.
> Sandbox shell scripts must not embed `${vars.*}` DAG placeholders directly;
> pass needed values through node `args` and read them from `input.json`.
> Seccomp notifier setup is part of sandbox correctness: after re-exec starts,
> the parent must close inherited socketpair fds and either receive the notifier
> fd promptly or fail the sandbox task. A sandbox trigger without a matching
> `sandbox/result` message is a hard failure, even if the child process exits.
> Allowlist/denylist enforcement uses `SECCOMP_FILTER_FLAG_NEW_LISTENER` and
> `SECCOMP_IOCTL_NOTIF_RECV/SEND`; ordinary fd `read/write` is not a valid
> seccomp user-notification transport.
> When sandbox scripts use an outbound proxy, the allowlist must include the
> actual proxy endpoint because seccomp observes `connect(proxy_ip,proxy_port)`,
> not the HTTP CONNECT target such as `github.com:443`.
> Sandbox stdout JSON is semantic node output. The sandbox result keeps
> `stdout`/`stderr` diagnostics, stores parsed JSON under `parsed`, and exposes
> parsed object fields at top level so `${node.field}` DAG references work for
> sandbox-produced values such as `${git-clone.path}`.
> Sandbox result publication is reliable only when `Publish` succeeds; transient
> MQTT publish errors must be retried and must not be logged as successful
> publication. MQTT publish/connect token timeouts are failures, not success.
> The in-cluster EMQX deployment must allow packets larger than the
> `read-source-files` source-context payload. The Helm chart sets
> `emqx.maxPacketSize=8MB`; a broker-side max-packet disconnect is a hard
> infrastructure/config failure, not an acceptable verifier warning.
> The `read-source-files` node intentionally selects a bounded context window
> before `generate-fixes`: skip obvious generated/Swagger files, prefer the
> smallest real source files, and pass at most one file plus its selected issues.
> Verifier expectations should validate that selected issue/file contract rather
> than requiring the LLM to process the full SonarQube issue set in one prompt.
> Component MQTT clients must actively recover from broker-side disconnects:
> failed publishes trigger a reconnect, and reconnect must restore all
> process-local subscriptions before the DAG can be considered healthy.
> The K8s ResourceManager plan timeout must remain longer than the TM
> SandboxExecutor result-file fallback window so a written `result.json` can
> still be converted into `exec/results`. TM SandboxExecutor must listen for
> `sandbox/result` and poll the shared `result.json`; whichever arrives first
> completes the node.
> TM slot workers must treat `exec/results` publication as mandatory. Transient
> MQTT publish errors on the TM→JM callback path require retry; a task persisted
> as `SUCCESS` without a delivered `exec/results` message is not a valid DAG
> progression signal.
> When `sandbox.deployment.enabled=true`, TM pods must not start embedded
> sandbox runners. Dedicated Sandbox pods consume runtime-cluster-scoped
> `sandbox/trigger` messages through `$share/sandbox-{namespace}-{clusterId}`;
> each pod runs `SandboxSlotWorker` slots. Verifiers observe the same messages
> with ordinary non-`$share` subscriptions only.

## 1.0 Current Phase 3 RBAC/A2A UI Acceptance

On 2026-08-16, the Phase 3 real-cluster gate completed five consecutive clean
rounds with authorization enforcement and two A2A replicas enabled. Every
round reset PostgreSQL, removed the previous workload namespace and
regenerable workspace, fully redeployed Helm, and configured LLM provider,
encrypted notification channel, MCPs, Agents, runtime Skills, namespace
principal, exact-Flow access, API key lifecycle, and Flow through visible
Playwright interactions. The browser then triggered and approved the main run,
inspected every persisted TaskRun attempt and full input/output, and followed
each attempt into a real Jaeger trace before all 15 verifiers reused that run.

```bash
source ~/.wl4gshrc.sec
python3 -u ui_e2e_runner.py --rounds 5 --skip-first-build
```

| Round | UI-triggered run ID                    | Attempts persisted/UI/Jaeger | Jaeger spans | A2A real run ID                          | Verifiers |
| ----: | -------------------------------------- | ---------------------------: | -----------: | ---------------------------------------- | --------: |
|     1 | `d90f56f8-5ae1-40bb-a906-d8d6d5cb7480` |                     26/26/26 |           86 | `8029522d-ba6c-472a-8619-430f27c44ab9`   |     15/15 |
|     2 | `dbee2a83-369c-43df-8f0b-e1c8f1e37823` |                     26/26/26 |           86 | `04437c23-4f4a-4ff4-9605-15ea060930a5`   |     15/15 |
|     3 | `e2845a37-65da-406e-a0b1-d2e86a107d04` |                     26/26/26 |           86 | `830f69dc-d0ff-4f99-8026-fad2c9e8266b`   |     15/15 |
|     4 | `2ae93a96-1e97-45eb-bce4-d7d58bb80a5e` |                     26/26/26 |           86 | `f937f4b4-57cb-4571-ad5a-35c5956a0f95`   |     15/15 |
|     5 | `b0589199-7593-4cf1-8078-4fee498454a4` |                     26/26/26 |           87 | `69c517b6-3031-49f8-a372-79aef7e3c7f5`   |     15/15 |

All five main business runs had one attempt per executed node and therefore no
natural runtime retry. The gate still enumerates every persisted attempt and
would fail on any UI/API/Jaeger cardinality mismatch. Deterministic retry
persistence is covered in JobMaster tests, while UI retry ordering and exact
attempt-to-span correlation are covered by component tests and the API-backed
browser fixture; that fixture is not represented as runtime E2E evidence.

Machine-readable status, reports, UI provisioning records, and seven
screenshots per round are under
`e2e/reports/ui-rounds-phase3-rbac-a2a/`; `summary.json` records
`{"result":"PASS","consecutive_successes":5,"required":5}`.

## 1.0.1 Historical Ten-Round Pre-RBAC/A2A UI Acceptance

On 2026-08-15, the real-cluster UI system suite completed ten consecutive clean
rounds. Every round reset the Flowgent PostgreSQL schema, fully redeployed the
Helm release, refreshed the local core image in k3s after teardown, configured
all application resources through visible Playwright interactions, triggered
and approved the run in the UI, inspected every persisted TaskRun attempt, and
queried real Jaeger traces through API Server. Neither `flowgent console import`
nor test-side resource REST writes were used.

```bash
source ~/.wl4gshrc.sec
python3 -u ui_e2e_runner.py --rounds 10 --skip-first-build
```

The command consumes secret values only from process environment and injects
them into the configured Kubernetes runtime Secret. UI/API resources contain
environment-reference names; screenshots, trace data, and evidence files do
not contain secret values.

| Round | UI-triggered run ID                    | Persisted attempts | UI-inspected attempts | Jaeger attempt links | Verifiers |
| ----: | -------------------------------------- | -----------------: | --------------------: | -------------------: | --------: |
|     1 | `f9f75027-ee7a-4a5e-8cd0-8a93abbd1ab8` |                 26 |                    26 |                   26 |     15/15 |
|     2 | `3b9bca2e-8236-4276-a69b-9051a02b0797` |                 26 |                    26 |                   26 |     15/15 |
|     3 | `c704ae0f-7f7c-4f80-92b6-af1eea849423` |                 26 |                    26 |                   26 |     15/15 |
|     4 | `b6946224-9eef-4707-bc5b-9900692edb4b` |                 26 |                    26 |                   26 |     15/15 |
|     5 | `5c1587a8-cae1-47fc-8bda-f6d124598d84` |                 26 |                    26 |                   26 |     15/15 |
|     6 | `a3a767fd-2cf4-4582-8ed2-a5f7f3ec6b6f` |                 26 |                    26 |                   26 |     15/15 |
|     7 | `d7d336cf-a157-428d-9973-374bb1ee421f` |                 26 |                    26 |                   26 |     15/15 |
|     8 | `825fabef-d3a3-4b72-84ad-ef22215391b5` |                 26 |                    26 |                   26 |     15/15 |
|     9 | `5f087bb2-ed33-4197-97c9-afa4433bea16` |                 26 |                    26 |                   26 |     15/15 |
|    10 | `99292d21-23dc-4c78-887b-bc00224abfee` |                 26 |                    26 |                   26 |     15/15 |

The canonical definition has 29 nodes and 33 edges. The accepted branch in
these rounds executed 26 nodes; the UI compared the persisted attempt set and
count with the attempts it actually opened, and verified one Jaeger correlation
link for every attempt. Scenarios 31–37 run immediately after Jaeger validation
so expected terminal-state runtime garbage collection cannot erase ephemeral
Pod/workspace evidence before it is checked. Independent engine scenarios
21–25 run afterward. A2A remains deliberately disabled in this deployment, so
scenario 25 records its designed SKIP/PASS result.

Machine-readable status and all per-round reports/screenshots are archived in
`e2e/reports/ui-rounds/`; `summary.json` records
`{"result":"PASS","consecutive_successes":10,"required":10}`.

## 1.0.2 Historical Three-Round Console-Import Verification

On 2026-08-10, the security-autonomy-fixer E2E suite was run through three
complete current-code rounds after the namespace/lifecycle/Sandbox ResourceManager
changes. A valid round means PostgreSQL reset, fresh core build and image import,
full Helm uninstall/install redeploy, component readiness, console import of all
use-case resources, and `runner.py` execution from scenario 11 through scenario 37.

| Round | Capstone run ID                        | Result         | s31 PR baseline -> s35 PR commits | New Flowgent security-fix commits |
| ----- | -------------------------------------- | -------------- | --------------------------------- | --------------------------------- |
| 1     | `dcade7bb-92ae-47e4-b73a-f6619c0ad298` | `16/16 passed` | 92 -> 94                          | `19fd7329`, `7c6731ea`            |
| 2     | `fdcd3bb8-7e0a-4b67-95f0-f4a2692fda92` | `16/16 passed` | 96 -> 98                          | `35e3827f`, `03fd0a3d`            |
| 3     | `624cfc51-0250-4e7f-aa88-289166863e01` | `16/16 passed` | 100 -> 102                        | `8a7a5ce0`, `7ceb6f8e`            |

The final report is `e2e/reports/00_summary.md` with duration `240.4s` for the
third current-code round. The first two reports are archived under
`e2e/reports/archived-20260810-025000/` and
`e2e/reports/archived-20260810-030524/`. The same current-code round set also
verified:

- Helm rendered `runtime.tm_orphan_timeout: "3m"` and
  `runtime.credential_env_secret: "flowgent-e2e-runtime-env"` into the live
  ConfigMap.
- System services ran in `flowgen-system`; JM/TM/Sandbox ran in
  `flowgent-default`. Imported Skills (`nexus3-retrieval` and `sub-fix`) stayed
  metadata-only and did not create independent JM/TM/Sandbox runtime.
- JM ResourceManager scaled TM and Sandbox Deployments from zero only after
  active task demand. Sandbox pods subscribed with
  `$share/sandbox-{namespace}-{clusterId}` and executed work through
  `SandboxSlotWorker` slots.
- `s31` through `s34` used ordinary non-`$share` MQTT audit subscriptions and
  observed `exec/plans`, `exec/results`, `sandbox/trigger`, and
  `sandbox/result` without joining L1 worker consumer groups.
- `s32` observed `24/24` `exec/plans`, `24/24` `exec/results`, and `3/3`
  `sandbox/trigger` plus `sandbox/result` messages for the final run.
- `s32` required non-empty SonarQube issue aggregation, git-clone output, and
  source-file context before remediation.
- `s35` required new Flowgent security-fix commits after the s31 PR baseline,
  not historical PR commits.
- `s37` verified the shared workspace only via `kubectl exec` inside the
  TaskManager pod, checking `/var/flowgent`, the current run hierarchy
  `/var/flowgent/{namespaceId}/{flowId}/{runId}/{taskId}`, and the
  git-clone task repo path
  `/var/flowgent/default/security-autonomy-fixer/{run_id}/{git_clone_task_id}/repos/rengine/.git`.
  No verifier directly inspected or mutated the host
  `/mnt/disk1/flowgent/e2e` tree.

## 1.1 Purpose

> **Core goal**: Verify that Flowgent's `security-autonomy-fixer` agent flow can
> **autonomously** discover, analyze, fix, review, and create a PR for real
> SonarQube security issues on a target repository.

> The test subject is Flowgent's `security-autonomy-fixer` workflow. This workflow must fix all issues in Rengine's sonarqube scan. You are prohibited from directly intervening in fixing any issues in the Rengine repository. The ultimate goal is that this workflow must be able to autonomously and truly fix and submit to PR#4 without any human intervention.

## 1.2 Test Environment

| Component          | Value                                                                                                                                                                     |
| ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Platform           | K8S single-node                                                                                                                                                           |
| Namespace          | `flowgen-system` (system services), `flowgent-default` (JM/TM/Sandbox runtime for tenant namespace `default`)                                                             |
| Mode               | Application only                                                                                                                                                          |
| Flow Under Test    | `security-autonomy-fixer` (29 nodes, 33 edges, 11 phases)                                                                                                                 |
| Shared workspace   | `/var/flowgent` inside TM/Sandbox pods; runtime tree is `/var/flowgent/{namespaceId}/{flowId}/{runId}/{taskId}` and Rengine clone is under the `git-clone` task directory |
| MCP GitHub         | `https://api.githubcopilot.com/mcp/` (Streamable HTTP)                                                                                                                    |
| MCP SonarQube      | `http://172.29.235.101:18080/mcp` (local, configured the Upstream Bearer Auth, you can call directly.)                                                                    |
| Test target PR     | `https://github.com/wl4g/rengine/pull/4` (Really PR)                                                                                                                      |
| Test target branch | `fix/flowgent_sec_auto_fix`                                                                                                                                               |
| Sonarqube UI       | `http://172.29.235.101:9000` (local deploy)                                                                                                                               |

## 1.3 Two-Step Workflow → Now One-Step

Previously two entry points. Now `runner.py` handles both pipeline setup and verification.

```bash
# Full pipeline + verify all scenarios
cd usecase/security-autonomy-fixer/e2e
python3 runner.py

# Pipeline only (skip verification)
python3 runner.py --no-verify

# Verify only (assume infra is already up)
python3 runner.py --skip-sonarqube --skip-build --skip-import --skip-deploy
```

`runner.py` automatically sources `~/.bashrc`, `~/.bash_profile`, and
`~/.wl4gshrc.sec` before importing E2E config or running `console import`.
Missing files are ignored. This keeps non-interactive executions aligned with
interactive SSH shells and lets config placeholders such as
`${DEEPSEEK_API_KEY_FLOWGENT}` resolve from the host environment without logging
secret values.
When `KUBECONFIG` is unset or points at an empty file, runner/deployer/shared
verifier config use the first non-empty candidate from `KUBECONFIG`,
`~/.kube/config`, and `/etc/rancher/k3s/k3s.yaml`, avoiding both root-only
fallback surprises and zero-byte user kubeconfig failures after k3s restarts.

| Script                 | Purpose                                                                                                                                                                                                                                                |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `e2e/runner.py`        | Single entry point: resets SonarQube PG, builds the binary, imports all config YAMLs, deploys via Helm, then runs all scenario checks (11–37) against the live system.                                                                                 |
| `e2e/ui_e2e_runner.py` | Real UI acceptance: resets Flowgent PG, redeploys Helm, provisions LLM/MCP/Agent/Runtime Skill/Flow through Playwright, triggers and approves the run in the UI, verifies attempts and Jaeger, then runs all scenario checks for each requested round. |

### Verification Quick Start

```bash
# Real UI system acceptance (clean deploy per round; no console import/API seed)
source ~/.wl4gshrc.sec
python3 -u ui_e2e_runner.py --rounds 10

# Run all verification scenarios (pipeline + verify)
python3 runner.py

# Verify only — skip all pipeline steps
python3 runner.py --skip-sonarqube --skip-build --skip-import --skip-deploy

# Run specific scenarios (ordered by layer)
python3 runner.py --skip-sonarqube --skip-build --skip-import --skip-deploy -s 11  # L0 Infra: Infrastructure readiness
python3 runner.py --skip-sonarqube --skip-build --skip-import --skip-deploy -s 21  # L1 Engine: API Server CRUD
python3 runner.py --skip-sonarqube --skip-build --skip-import --skip-deploy -s 31  # L2 App: E2E Seed & Trigger

# List all scenarios
python3 runner.py -l
python3 runner.py --api http://10.0.0.1:9999 --pg postgres://u:p@h/db
```

**All verification scenario execution MUST go through `runner.py`.** Do not
manually execute individual `verifier/*.py` files.

---

# 2. Pre-Execution Requirements

## 2.1 Environment Reset

Before every real (non-dry-run) execution against a live K8S cluster, reset
shared state to avoid false positives/negatives from stale Deployments, PG rows,
and MQTT messages.

`runner.py` performs this reset automatically for full pipeline runs: it clears
the Flowgent PostgreSQL `public` schema before Helm redeploy/import, then imports
only the current `security-autonomy-fixer/config` resources. Verify-only mode
(`--skip-sonarqube --skip-build --skip-import --skip-deploy`) does not reset PG.

```bash
# 0. Source secrets
for f in ~/.bashrc ~/.bash_profile ~/.wl4gshrc.sec; do
  [ -f "$f" ] && source "$f" || true
done
export KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config}"
if [ ! -s "$KUBECONFIG" ] && [ -s /etc/rancher/k3s/k3s.yaml ]; then
  export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
fi
export GITHUB_TOKEN="${GITHUB_TOKEN:-${GH_TOKEN:-}}"
export DEEPSEEK_API_KEY_FLOWGENT="${DEEPSEEK_API_KEY_FLOWGENT:-${DEEPSEEK_API_KEY:-}}"

# 1. Tear down Helm release
sudo helm uninstall flowgent -n default --kubeconfig ~/.kube/config

# 2. Delete leftover runtime-cluster workloads and workload namespaces
sudo kubectl delete deployment -A -l flowgent.io/runtime-boundary=flow-jobmanager --force --grace-period=0
sudo kubectl delete deployment -A -l flowgent.io/managed-by=runtime-cluster --force --grace-period=0
sudo kubectl delete pod -A -l flowgent.io/runtime-boundary=flow-jobmanager --force --grace-period=0 --wait=false
sudo kubectl delete pod -A -l flowgent.io/managed-by=runtime-cluster --force --grace-period=0 --wait=false
sudo kubectl delete pod -n default -l 'flowgent/role in (worker,sandbox-worker)' --force --grace-period=0 --wait=false
sudo kubectl delete ns -l 'flowgent.io/runtime-boundary=namespace' --force --grace-period=0

# 3. Wipe PostgreSQL data (external PG via docker)
PG_IP=$(sudo docker inspect sigbot_e2e_164364_postgres --format '{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}' | awk '{print $1}')
sudo docker exec sigbot_e2e_164364_postgres sh -c \
  "PGPASSWORD=test psql -U test -d flowgent -c 'DROP SCHEMA public CASCADE; CREATE SCHEMA public;'"

# 4. Rebuild and re-import image
# runner.py checks root disk headroom around image build/import and prunes only
# transient Docker builder cache plus /tmp/go-build* when close to kubelet
# eviction thresholds.
HTTPS_PROXY=http://127.0.0.1:8800 make build:core
sudo DOCKER_BUILDKIT=1 docker build --build-arg BUILD_TAGS="x402" \
  -t localhost/flowgent-core:latest -f deploy/docker/Dockerfile.core .
sudo docker save localhost/flowgent-core:latest | sudo k3s ctr images import -

# 4b. Mirror runtime credentials into both namespaces without printing values.
# runner.py performs this automatically as flowgent-e2e-runtime-env.

# 5. Import config YAMLs into PG (before Helm install)
./bin/flowgent-core --config etc/flowgent.yaml console import \
  usecase/security-autonomy-fixer/config

# 6. Fresh Helm install (system services + session runtime cluster)
sudo helm install flowgent deploy/helm/flowgent \
  --kubeconfig ~/.kube/config \
  --set global.image.repository=localhost/flowgent-core \
  --set global.image.tag=latest \
  --set postgresql.enabled=false \
  --set emqx.enabled=true \
  --set redis.enabled=false \
  --set apiserver.replicas=1 \
  --set controller.replicas=1 \
  --set notifier.replicas=1 \
  --set a2a.enabled=true \
  --set a2a.replicas=2 \
  -n default --wait --timeout 300s

# 7. Post-install fixes (BUG WORKAROUNDS — see §8)

# 7a. Set PG + MQTT env vars on all deployments
PG_IP=$(sudo docker inspect sigbot_e2e_164364_postgres --format '{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}' | awk '{print $1}')
EMQX_IP=$(sudo kubectl get svc flowgent-emqx -n default -o jsonpath='{.spec.clusterIP}')
for depl in flowgent-apiserver flowgent-controller flowgent-notifier flowgent-a2a; do
  sudo kubectl set env deployment/$depl -n default \
    FLOWGENT__STORAGE__TYPE=POSTGRE \
    "FLOWGENT__STORAGE__POSTGRES__DSN=postgres://test:test@${PG_IP}:5432/flowgent?sslmode=disable" \
    "FLOWGENT__STORAGE__POSTGRES__HOST=${PG_IP}" \
    FLOWGENT__STORAGE__POSTGRES__PORT=5432 \
    FLOWGENT__STORAGE__POSTGRES__USERNAME=test \
    FLOWGENT__STORAGE__POSTGRES__PASSWORD=test \
    FLOWGENT__STORAGE__POSTGRES__DATABASE=flowgent \
    FLOWGENT__MESSAGER__TYPE=mqtt \
    "FLOWGENT__MESSAGER__MQTT__BROKER=tcp://${EMQX_IP}:1883"
done

# 7b. Enable MCPs
API_IP=$(sudo kubectl get svc flowgent-apiserver -n default -o jsonpath='{.spec.clusterIP}')
for mcp in github sonarqube; do
  curl -s -X PUT "http://${API_IP}:9999/api/v1/default/mcp/${mcp}" \
    -H "Content-Type: application/json" -d '{"enabled": true}'
done

# 7c. Copy ConfigMap to flowgent-default namespace (required before JM deploys)
sudo kubectl get configmap flowgent-config -n default -o yaml | \
  sed 's/namespace: default/namespace: flowgent-default/' | \
  sudo kubectl apply -f -

# 7d. Set imagePullPolicy on JM pods after controller creates them (wait first)
sleep 30
sudo kubectl patch deploy -n flowgent-default -l app=flowgent-jobmanager \
  -p '{"spec":{"template":{"spec":{"containers":[{"name":"jobmanager","imagePullPolicy":"IfNotPresent"}]}}}}'
# Also set PG + MQTT env vars on JM deployments
for dep in $(sudo kubectl get deploy -n flowgent-default -o name); do
  sudo kubectl set env $dep -n flowgent-default \
    FLOWGENT__STORAGE__TYPE=POSTGRE \
    "FLOWGENT__STORAGE__POSTGRES__DSN=postgres://test:test@${PG_IP}:5432/flowgent?sslmode=disable" \
    "FLOWGENT__STORAGE__POSTGRES__HOST=${PG_IP}" \
    FLOWGENT__STORAGE__POSTGRES__PORT=5432 \
    FLOWGENT__STORAGE__POSTGRES__USERNAME=test \
    FLOWGENT__STORAGE__POSTGRES__PASSWORD=test \
    FLOWGENT__STORAGE__POSTGRES__DATABASE=flowgent \
    FLOWGENT__MESSAGER__TYPE=mqtt \
    "FLOWGENT__MESSAGER__MQTT__BROKER=tcp://${EMQX_IP}:1883" \
    FLOWGENT__RUNTIME__TM_IMAGE=localhost/flowgent-core:latest \
    FLOWGENT__RUNTIME__JM_IMAGE=localhost/flowgent-core:latest \
    FLOWGENT__SANDBOX__DEPLOYMENT__IMAGE=localhost/flowgent-core:latest
done

# 7e. Historical manual sandbox deployment patch point.
sudo kubectl apply -f - << 'SBOXEOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: flowgent-sandbox
  namespace: default
  labels:
    app.kubernetes.io/component: sandbox
    flowgent/role: sandbox-worker
spec:
  replicas: 2
  selector:
    matchLabels:
      flowgent/role: sandbox-worker
  template:
    metadata:
      labels:
        flowgent/role: sandbox-worker
    spec:
      containers:
      - name: sandbox
        image: localhost/flowgent-core:latest
        imagePullPolicy: IfNotPresent
        command: ["/app/flowgent", "sandbox", "start"]
        env:
        - name: FLOWGENT__CONFIG__FILE
          value: /etc/flowgent/flowgent.yaml
        - name: FLOWGENT__MESSAGER__MQTT__BROKER
          value: "tcp://${EMQX_IP}:1883"
        volumeMounts:
        - name: config
          mountPath: /etc/flowgent
        - name: sandbox-workspace
          mountPath: /var/flowgent
      volumes:
      - name: config
        configMap:
          name: flowgent-taskmanager-config
      - name: sandbox-workspace
        hostPath:
          path: /mnt/disk1/flowgent/e2e
          type: DirectoryOrCreate
SBOXEOF

# 7f. Fix sandbox ConfigMap (BUG: sandbox.image missing in config, taskmanager-config needed)
sudo kubectl get configmap flowgent-config -n default -o yaml | \
  sed 's/name: flowgent-config/name: flowgent-taskmanager-config/' | \
  python3 -c "
import sys,yaml
cm=yaml.safe_load(sys.stdin)
inner=yaml.safe_load(cm['data']['flowgent.yaml'])
inner['sandbox']['deployment']['image']='localhost/flowgent-core:latest'
cm['data']['flowgent.yaml']=yaml.dump(inner, default_flow_style=False)
print(yaml.dump(cm, default_flow_style=False))
" | sudo kubectl replace --force -f -

# 8. Wait for all pods
sudo kubectl wait --for=condition=Ready pod \
  -l app.kubernetes.io/instance=flowgent -n default --timeout=300s

# 9. Wait for JM pods (controller needs time)
sleep 30
sudo kubectl wait --for=condition=Ready pod \
  -l app=flowgent-jobmanager -n flowgent-default --timeout=120s
```

## 2.2 Config Bootstrapping

After a clean reset (which wipes PG), the database is empty. Every E2E run must
re-seed agents and MCPs. **The GitHub MCP has a `${GITHUB_TOKEN}` placeholder
in its YAML that must be resolved from the environment before seeding.**

### 2.2.1 Set GitHub and LLM Environment

The canonical secret source for E2E execution is the host shell environment
after sourcing `~/.bashrc`, `~/.bash_profile`, and `~/.wl4gshrc.sec`. The runner
does this automatically. Manual commands should use the same pattern:

```bash
for f in ~/.bashrc ~/.bash_profile ~/.wl4gshrc.sec; do
  [ -f "$f" ] && source "$f" || true
done
export GITHUB_TOKEN="${GITHUB_TOKEN:-${GH_TOKEN:-}}"
export DEEPSEEK_API_KEY_FLOWGENT="${DEEPSEEK_API_KEY_FLOWGENT:-${DEEPSEEK_API_KEY:-}}"

# Non-secret presence checks only:
[ -n "${GITHUB_TOKEN:-}" ] && echo "GITHUB_TOKEN=set"
[ -n "${DEEPSEEK_API_KEY_FLOWGENT:-}" ] && echo "DEEPSEEK_API_KEY_FLOWGENT=set"
```

`config/llmproviders/deepseek.yaml` resolves the DeepSeek API key from
`${DEEPSEEK_API_KEY_FLOWGENT}` during `flowgent-core console import`.
JM/TM/Sandbox pods get runtime credentials only through the K8s Secret named by
`runtime.credential_env_secret`; host env alone is insufficient once execution
enters Kubernetes.
If `HTTP_PROXY`/`HTTPS_PROXY` are present in the sourced host environment, the
E2E deployer injects them into the same runtime Secret after rewriting
`localhost`/`127.0.0.1` to the pod default gateway derived from the node PodCIDR
(with node InternalIP as a fallback). This is required for sandbox `git-clone`
to reach GitHub in GFW environments. The deployer also merges
Flowgent/Kubernetes service domains into `NO_PROXY` and exports
`FLOWGENT_E2E_PROXY_ALLOWLIST_ENTRY={pod_proxy_host}:{port}` for console import.
The `git-clone` node probes the injected proxy before cloning, retries transient
clone failures, and falls back to the pod default gateway proxy with the same
proxy port if the primary injected proxy is unreachable.

### 2.2.2 Automatic Seeding (via e2e/runner.py console import)

The use-case `e2e/runner.py` step 3 imports all config YAMLs into the Flowgent
database via `flowgent-core console import`. This seeds agents, flows, MCPs,
LLM providers, notifiers, and skills from the `config/` directory.

| Resource                 | Source                                        |
| ------------------------ | --------------------------------------------- |
| Agents                   | `config/agents/*.yaml`                        |
| Flows & Skills           | `config/flows/*.yaml`, `config/skills/*.yaml` |
| MCPs (github, sonarqube) | `config/mcps/*.yaml`                          |
| LLM Providers            | `config/llmproviders/*.yaml`                  |
| Notifiers                | `config/notifiers/*.yaml`                     |

### 2.2.3 Manual Bootstrap (alternative to console import)

```bash
for f in ~/.bashrc ~/.bash_profile ~/.wl4gshrc.sec; do
  [ -f "$f" ] && source "$f" || true
done
export GITHUB_TOKEN="${GITHUB_TOKEN:-${GH_TOKEN:-}}"

# Register GitHub MCP with resolved token
curl -s -X POST http://localhost:9999/api/v1/default/mcp \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"github\",
    \"type\": \"http\",
    \"url\": \"https://api.githubcopilot.com/mcp/\",
    \"headers\": {\"Authorization\": \"Bearer ${GITHUB_TOKEN}\"},
    \"enabled\": true
  }"

# Register SonarQube MCP (no auth config needed — upstream bearer auth)
curl -s -X POST http://localhost:9999/api/v1/default/mcp \
  -H "Content-Type: application/json" \
  -d '{
    \"name\": \"sonarqube\",
    \"type\": \"http\",
    \"url\": \"http://172.29.235.101:18080/mcp\",
    \"enabled\": true
  }'

# Register agents from config/agents/*.yaml (example)
curl -s -X POST http://localhost:9999/api/v1/default/agents \
  -H "Content-Type: application/json" \
  -d @usecase/security-autonomy-fixer/config/agents/issue-detector.yaml
```

### 2.2.4 Verify MCP Registration

```bash
curl -s http://localhost:9999/api/v1/default/mcp | python3 -m json.tool
# Both "github" and "sonarqube" should show enabled=true
# GitHub should have a real Bearer token (not ${GITHUB_TOKEN} literal)
```

**IMPORTANT**: After seeding MCPs, the TM pods must be restarted to pick up
the new definitions (TM loads MCPs once at startup):

```bash
kubectl rollout restart deployment/flowgent-taskmanager -n flowgent-default
kubectl rollout status deployment/flowgent-taskmanager -n flowgent-default
```

---

# 3. Verification Architecture

## 3.1 Three-Layer Model

Verification scenarios are organized into three layers, each identified by a
numbered prefix:

| Layer | Prefix | Name            | Focus                                                                |
| ----- | ------ | --------------- | -------------------------------------------------------------------- |
| L0    | 1x_    | Infra Readiness | K8S cluster, Helm, pods, console import, OTEL infrastructure         |
| L1    | 2x_    | Flowgent Engine | API Server CRUD, Controller, Messager, Notifier, A2A                 |
| L2    | 3x_    | Use case        | E2E security fixer pipeline, PR verification, knowledge RAG, volumes |

## 3.2 Scenario File Index

| #   | File                                | Layer | Description                                          |
| --- | ----------------------------------- | ----- | ---------------------------------------------------- |
| 11  | `s11_infra_readiness_verifier.py`   | L0    | Helm deployment, pod readiness, healthz              |
| 12  | `s12_console_import_verifier.py`    | L0    | Console binary, import command, DB verification      |
| 13  | `s13_otel_verifier.py`              | L0    | OTEL infrastructure + Jaeger span coverage           |
| 21  | `s21_apiserver_verifier.py`         | L1    | REST CRUD + lifecycle events                         |
| 22  | `s22_notifier_verifier.py`          | L1    | Multi-channel delivery                               |
| 23  | `s23_controller_verifier.py`        | L1    | Application runtime cluster lifecycle                |
| 24  | `s24_messager_verifier.py`          | L1    | MQTT topics + sandbox chain                          |
| 25  | `s25_a2a_protocol_verifier.py`      | L1    | Agent card discovery, task submit                    |
| 31  | `s31_seed_trigger_verifier.py`      | L2    | E2E: agent/MCP registration, flow creation, trigger  |
| 32  | `s32_discovery_analyze_verifier.py` | L2    | E2E: commit fetch, SonarQube scan, issue aggregation |
| 33  | `s33_remediation_verifier.py`       | L2    | E2E: fix gen, review, vote, supervisor, human gate   |
| 34  | `s34_delivery_report_verifier.py`   | L2    | E2E: branch/commit/PR, rescan, report, PG final      |
| 35  | `s35_pr_commit_verifier.py`         | L2    | Agent-produced fix commits on target PR              |
| 36  | `s36_knowledge_verifier.py`         | L2    | RAG retrieval, injection, post-handle                |
| 37  | `s37_volume_workspace_verifier.py`  | L2    | PVC, mount, git clone & file RW                      |

Shared utilities for the split E2E (31-34) sub-verifiers live in `verifier/_common.py`.

---

# 4. Scenario Specifications

## 4.1 Scenario 11 — Infrastructure & Pre-Deployment (L0 Infra)

### 4.1.1 Purpose

Validate K8S cluster health, external dependency availability, Helm deployment
state, and pod readiness before running any functional scenarios.

### 4.1.2 Prerequisites

- K8S cluster accessible (`KUBECONFIG` set)
- `kubectl`, `helm`, `docker` on PATH
- Python `requests` library installed

### 4.1.3 Steps

| Step | Action                                                                                        | Expected Input                         | Expected Output                                       |
| ---- | --------------------------------------------------------------------------------------------- | -------------------------------------- | ----------------------------------------------------- |
| 1.1  | `kubectl get nodes`                                                                           | KUBECONFIG pointing to K8S             | At least 1 node with `Ready` status                   |
| 1.2  | `kubectl get pods -n kube-system`                                                             | K8S cluster running                    | All system pods `Running`                             |
| 1.3  | `GET {SONARQUBE_URL}/api/system/health`                                                       | `config.SONARQUBE_URL`                 | HTTP 200, body contains `health` or `status` field    |
| 1.4  | TCP connect to `EMQX_HOST:EMQX_PORT`                                                          | `config.EMQX_HOST`, `config.EMQX_PORT` | Connection accepted (non-blocking)                    |
| 1.5  | `docker images flowgent-core`                                                                 | Docker daemon running                  | Image `flowgent-core` listed                          |
| 2.1  | `helm list -n {namespace} -o json`                                                            | Helm binary on PATH                    | Release `flowgent` with status `deployed`             |
| 2.2  | `kubectl get deploy,svc,configmap -l app.kubernetes.io/instance=flowgent`                     | Helm release deployed                  | Resources listed (≥1 each of deploy/svc/configmap)    |
| 3.1  | `kubectl get pods -l app.kubernetes.io/instance=flowgent -o json`                             | Pods running                           | All pods `phase=Running`, all containers `ready=true` |
| 3.2  | `GET {API_URL}/_/healthz`                                                                     | API server pod Running                 | HTTP 200                                              |
| 3.3  | `kubectl logs -l app.kubernetes.io/component={c} --tail=20` for apiserver/controller/notifier | Pod logs accessible                    | No ERROR/FATAL/PANIC in recent logs                   |

### 4.1.4 Pass/Fail Criteria

- **PASS**: All checks return expected output (WARN for non-critical items like SonarQube unreachable is tolerated)
- **FAIL**: Critical infrastructure missing (no Ready nodes, API server unreachable, all pods CrashLoopBackOff)

---

## 4.2 Scenario 12 — Console Import (L0 Infra)

### 4.2.1 Purpose

Validate the `flowgent-core console import` command: binary execution, directory
tree import, and PostgreSQL verification that config rows were created.

### 4.2.2 Prerequisites

- `flowgent-core` binary built and available
- PostgreSQL reachable
- Config YAMLs in `config/` directory

### 4.2.3 Steps

| Step | Action                                             | Expected Input           | Expected Output                                                                                                                 |
| ---- | -------------------------------------------------- | ------------------------ | ------------------------------------------------------------------------------------------------------------------------------- |
| 1.1  | Execute `console import` against `config/`         | Config directory, PG DSN | Binary exits 0                                                                                                                  |
| 1.2  | Verify PG: `orh_agentflow`                         | —                        | `security-autonomy-fixer` flow exists                                                                                           |
| 1.3  | Verify PG: `llm_agent`                             | —                        | Agent rows created                                                                                                              |
| 1.4  | Verify PG: `llm_mcp`                               | —                        | MCP rows created                                                                                                                |
| 1.5  | Verify PG: `llm_providers`                         | —                        | LLM provider rows created                                                                                                       |
| 1.6  | Verify imported `git-clone.network_policy.allowed` | Sourced/deployer env     | No unresolved `${FLOWGENT_E2E_PROXY_ALLOWLIST_ENTRY}` placeholder; includes `github.com` and the resolved proxy allowlist entry |

### 4.2.4 Pass/Fail Criteria

- **PASS**: Binary exits 0, all config categories have PG rows, and the imported
  `git-clone` sandbox network allowlist contains the resolved proxy endpoint
- **FAIL**: Binary crashes, PG rows missing for any config category, or the
  `git-clone` proxy allowlist placeholder remains unresolved after import

---

## 4.3 Scenario 13 — OTEL Tracing (L0 Infra)

> **This scenario is MANDATORY.** Failure to find a Jaeger trace or missing core
> node span coverage is treated as a verification failure (not opportunistic).

### 4.3.1 Purpose

Validate OTEL infrastructure: Jaeger Query API accessibility, exporter
configuration, and that traces can be delivered to the Jaeger backend.
s13 triggers one real `security-autonomy-fixer` run and treats the Jaeger trace
for that `run.id` plus phases 1-7 node span coverage as hard gates.
Recent startup logs are accepted as OTEL-enabled evidence when present; if those
logs have rotated or the pod was replaced, the verifier falls back to the live
pod/config runtime state. This fallback does not replace the mandatory Jaeger
trace and core span checks.

### 4.3.2 Prerequisites

- K8s Flowgent Jaeger Query API reachable on local port `16687`
- OTEL exporter configured in `mgmt.otel` ConfigMap section or equivalent pod
  runtime env override

### 4.3.3 Steps

| Step | Action                              | Expected Input                       | Expected Output                      | Mandatory     |
| ---- | ----------------------------------- | ------------------------------------ | ------------------------------------ | ------------- |
| 1.1  | Verify JM/API OTEL-enabled evidence | Recent logs or live pod/config state | OTEL enabled for both components     | **YES**       |
| 1.2  | `GET /api/services`                 | Jaeger Query API                     | HTTP 200, service list returned      | **YES**       |
| 1.3  | Query Jaeger traces by `run.id`     | Triggered run ID                     | At least one matching trace          | **YES**       |
| 1.4  | Validate core node spans            | Trace spans                          | All phases 1-7 nodes have spans      | **YES**       |
| 1.5  | Sample span attributes              | Trace spans                          | `flowgent.node_id`/task type visible | Informational |

### 4.3.4 Pass/Fail Criteria

- **PASS**: OTEL is enabled in runtime evidence, Jaeger Query API is reachable,
  at least one trace matches the run ID, and every phases 1-7 core node has a span
- **FAIL**: Runtime OTEL evidence missing, Jaeger unreachable, run trace missing,
  or any core node span is absent

---

## 4.4 Scenario 21 — API Server Module (L1 Engine)

### 4.4.1 Purpose

Validate complete REST API CRUD operations across all entity types, verify
PostgreSQL persistence, and confirm MQTT lifecycle event publishing.

### 4.4.2 Prerequisites

- Scenario 11 passed (API server running on `:9999`)
- EMQX reachable (MQTT tests)
- PostgreSQL reachable (PG direct tests)

### 4.4.3 Entity CRUD Steps

| Step | Entity    | Table             | REST Path                                         | Expected Input                 | Expected Output                                                                            |
| ---- | --------- | ----------------- | ------------------------------------------------- | ------------------------------ | ------------------------------------------------------------------------------------------ |
| 2.1  | AgentFlow | `orh_agentflow`   | `POST /api/v1/{namespace}/flows`                  | `{id, nodes, edges, priority}` | HTTP 200/201, body contains `id`                                                           |
| 2.2  | AgentFlow | `orh_agentflow`   | `GET /api/v1/{namespace}/flows`                   | —                              | JSON array of flows                                                                        |
| 2.3  | AgentFlow | `orh_agentflow`   | `GET /api/v1/{namespace}/flows/{id}`              | Flow ID                        | Flow object with `version`, `nodes`, `edges`                                               |
| 2.4  | AgentFlow | `orh_agentflow`   | `PUT /api/v1/{namespace}/flows/{id}`              | `{description}`                | HTTP 200, deterministic version `1` row updated                                            |
| 2.5  | AgentFlow | `orh_agentflow`   | `DELETE /api/v1/{namespace}/flows/{id}`           | Flow ID                        | HTTP 200/204, soft-deleted (`del_flag=true`) and no longer visible through `GET` or `LIST` |
| 2.6  | FlowRun   | `orh_flowrun`     | `POST /api/v1/{namespace}/runs`                   | `{agentflow_id, priority}`     | HTTP 201, body contains `id`, `status=PENDING`                                             |
| 2.7  | FlowRun   | `orh_flowrun`     | `GET /api/v1/{namespace}/runs/{id}`               | Run ID                         | Run object with `status`, `created_at`                                                     |
| 2.8  | TaskRun   | `task_runs`       | `GET /api/v1/{namespace}/runs/{id}/tasks`         | Run ID                         | JSON array of task runs (may be empty)                                                     |
| 2.9  | Agent     | `llm_agent`       | `POST /api/v1/{namespace}/agents`                 | `{name, model, instruction}`   | HTTP 200/201, body contains `name`                                                         |
| 2.10 | Agent     | `llm_agent`       | `GET /api/v1/{namespace}/agents/{name}`           | Agent name                     | Agent object with `model`, `instruction`                                                   |
| 2.11 | MCP       | `llm_mcp`         | `POST /api/v1/{namespace}/mcp`                    | `{name, type, url, enabled}`   | HTTP 201, body contains `id`                                                               |
| 2.12 | MCP       | `llm_mcp`         | `PUT /api/v1/{namespace}/mcp/{name}`              | `{enabled: true/false}`        | HTTP 200, `enabled` toggled                                                                |
| 2.13 | Provider  | `llm_providers`   | `POST /api/v1/{namespace}/llm/providers`          | `{type, endpoint, models[]}`   | HTTP 201, body contains `id`                                                               |
| 2.14 | Approval  | `human_approvals` | `POST /api/v1/human/approvals`                    | `{run_id, node_id}`            | HTTP 201, body contains `token`                                                            |
| 2.15 | Channel   | `nfy_channel`     | `POST /api/v1/{namespace}/notifications/channels` | `{name, channel_type, config}` | HTTP 201, body contains `id`                                                               |

### 4.4.4 MQTT Lifecycle Event Steps

| Step | REST Action          | Expected MQTT Topic                                              | Expected Payload                               |
| ---- | -------------------- | ---------------------------------------------------------------- | ---------------------------------------------- |
| 4.1  | `POST /flows`        | `flowgent/v1/{namespace}/flows/{id}/ctrl/flow/updated`           | `{action: "created", agentflow_id, version}`   |
| 4.2  | `PUT /flows/{id}`    | `flowgent/v1/{namespace}/flows/{id}/ctrl/flow/updated`           | `{action: "updated", agentflow_id, version++}` |
| 4.3  | `DELETE /flows/{id}` | `flowgent/v1/{namespace}/flows/{id}/ctrl/flow/deleted`           | `{action: "deleted", agentflow_id}`            |
| 4.4  | `POST /runs`         | `flowgent/v1/{namespace}/flows/{id}/runs/{rid}/ctrl/run/created` | `{action: "created", run_id}`                  |

### 4.4.5 Pass/Fail Criteria

- **PASS**: All CRUD endpoints return correct status codes, PG rows match API responses, soft-deleted rows disappear from API `GET/LIST`, MQTT events published for lifecycle changes, and verifier-created setup flows are cleaned up
- **FAIL**: Any CRUD endpoint returns 5xx, PG/API data mismatch, DELETE leaves a resource readable/listed, verifier setup flows leak, or MQTT lifecycle events are missing

---

## 4.5 Scenario 22 — Notifier Module (L1 Engine)

### 4.5.1 Purpose

Validate EMQX broker connectivity and notification topic subscription.

### 4.5.2 Prerequisites

- EMQX running (port 1883, dashboard 18083)

### 4.5.3 Steps

| Step | Action                          | Expected Input                                                 | Expected Output                |
| ---- | ------------------------------- | -------------------------------------------------------------- | ------------------------------ |
| 8.1  | EMQX dashboard API              | `GET http://localhost:18083/api/v5/status`                     | HTTP 200, EMQX status          |
| 8.2  | MQTT connect + subscribe        | `$share/notify-pool/flowgent/v1/+/flows/+/runs/+/notify/event` | Subscription confirmed         |
| 8.3  | Message receipt (opportunistic) | Pending notification events                                    | Message delivered if any exist |

### 4.5.4 Pass/Fail Criteria

- **PASS**: EMQX reachable, MQTT subscription established
- **FAIL with grace**: Notifier not enabled → SKIP

---

## 4.6 Scenario 23 — Controller Module (L1 Engine)

### 4.6.1 Purpose

Validate the application runtime-cluster lifecycle: Controller treats Flow CRUD
as metadata registration, creates a per-run application JM only for active
application runs, and garbage-collects the JM/TM/Sandbox cluster according to
its runtime-cluster owner labels. Session runs are consumed by the Helm session
JM and are outside this Controller create/delete path.

### 4.6.2 Prerequisites

- Scenario 11 passed (Controller pod Running, K8s RBAC configured)

### 4.6.3 Steps — Flow CREATE Lifecycle

| Step | Action                                        | Expected Input                                              | Expected Output                                                                                                                                                                                                                                        |
| ---- | --------------------------------------------- | ----------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 4.1  | `POST .../flows` | `{id, runtime_mode: application, nodes: [short sandbox], edges: [], resources: {...}}` | HTTP 200/201; no application JM/TM/Sandbox Deployment exists before trigger |
| 4.2  | `POST /api/v1/{namespace}/flows/{id}/trigger` | Empty/manual trigger payload                                | `run_id` returned                                                                                                                                                                                                                                      |
| 4.3  | Wait for Deployments                          | Runtime cluster ID derived from the Run ID                   | `flowgent-jobmanager-{namespace}-{flow_id}-{run_id}`, `flowgent-taskmanager-{namespace}-{cluster_id}`, and `flowgent-sandbox-{namespace}-{cluster_id}` exist in `flowgent-{namespace}` with `runtime-cluster` labels |
| 4.4  | Verify JM Deployment spec                     | Deployment name                                             | Container env includes `FLOWGENT__RUNTIME__AGENT_FLOW_ID={flow_id}` and `envFrom.secretRef=flowgent-e2e-runtime-env`; when host proxy env is present, the Secret contains pod-reachable `HTTP_PROXY`/`HTTPS_PROXY` and merged `NO_PROXY`               |
| 4.5  | Wait for JM/TM/Sandbox Pods                   | Flow/run labels for JM; namespace/runtime-cluster labels for workers | Application JM and runtime-cluster TM/Sandbox pods reach `Running` |
| 4.6  | Cancel Run; delete Flow                       | Run ID, Flow ID                                             | HTTP 200/204 and subsequent Flow `GET` returns 404 |
| 4.7  | Wait for GC                                   | —                                                           | Application JM/config and runtime-cluster TM/Sandbox Deployments deleted |

### 4.6.4 Steps — Flow UPDATE Lifecycle

| Step | Action                               | Expected Input                                     | Expected Output                                         |
| ---- | ------------------------------------ | -------------------------------------------------- | ------------------------------------------------------- |
| 5.1  | Create metadata-only Flow            | `{id, runtime_mode: application, nodes: [noop], edges: []}` | HTTP 200/201 and no idle application JM Deployment |
| 5.2  | `PUT /api/v1/{namespace}/flows/{id}` | `{description: "updated", version: 2}`             | HTTP 200                                                |
| 5.3  | Re-check idle resources              | Flow-scoped JM name                                | Still no Flow JM Deployment without an active Run |

### 4.6.5 Steps — MQTT Event Verification

| Step | Action                           | Expected Input        | Expected Output                                                      |
| ---- | -------------------------------- | --------------------- | -------------------------------------------------------------------- |
| 6.1  | Subscribe to `ctrl/flow/updated` | MQTT broker reachable | Event received within 5s of flow CREATE with `agentflow_id` matching |
| 6.2  | Subscribe to `ctrl/flow/deleted` | MQTT broker reachable | Event received within 5s of flow DELETE                              |

### 4.6.6 Pass/Fail Criteria

- **PASS**: Controller creates JM Deployment in the correct workload namespace
  with correct env vars and runtime credential Secret reference; runtime proxy
  values are pod-reachable when configured; API DELETE hides the flow; the JM
  Deployment is deleted on flow delete; MQTT events published
- **FAIL**: JM Deployment not created within 30s, wrong namespace, env vars
  missing, runtime proxy values point at localhost/127.0.0.1 inside pod env,
  DELETE leaves the flow readable, or Controller fails to delete the JM
  Deployment within 60s

---

## 4.7 Scenario 24 — Messager Module (L1 Engine)

### 4.7.1 Purpose

Validate MQTT topic connectivity and message routing for all inter-component
topics.

### 4.7.2 Prerequisites

- EMQX running (port 1883)
- `paho-mqtt` Python library installed

### 4.7.3 Topic Verification Steps

Dispatch topics use
`flowgent/v1/{namespace}/clusters/{clusterId}/flows/{flowId}/runs/{runId}/`
so only workers in the runtime cluster compete for work. Point-to-point
callbacks and notification topics use
`flowgent/v1/{namespace}/flows/{flowId}/runs/{runId}/`.

| Step | Topic Suffix      | Publisher | Subscriber                    | Expected Payload                                    |
| ---- | ----------------- | --------- | ----------------------------- | --------------------------------------------------- |
| 7.1  | `exec/plans`      | JM        | TM ($share/tm-{ns}-{clusterId}) | `{plan_id, node_id, input, task_type}`             |
| 7.2  | `exec/results`    | TM        | JM                            | `{plan_id, node_id, state}` (state only, NO output) |
| 7.3  | `sandbox/trigger` | TM        | Sandbox ($share/sandbox-{ns}-{clusterId}) | `{plan_id, runtime, script}`            |
| 7.4  | `sandbox/result`  | Sandbox   | TM                            | `{plan_id, exit_code, stdout}`                      |
| 7.5  | `notify/event`    | Publisher | Notifier ($share/notify-pool) | `{event_type, channel, payload}`                    |
| 7.6  | `notify/result`   | Notifier  | Publisher                     | `{event_id, status, error}`                         |

Additional topics (heartbeat, ctrl/*, cross-pod WS) are validated by their
respective scenarios (23, 13).

### 4.7.4 Skill/Sandbox E2E Chain

```
JM → exec/plans → TM
       ├→ sandbox/trigger → Sandbox (seccomp) → sandbox/result → TM
       ├→ PUT /api/v1/{namespace}/runs/{id}/tasks/{task_id} (persist output via REST)
       └→ exec/results (state only) → JM
```

### 4.7.5 Pass/Fail Criteria

- **PASS**: All topics self-publish and self-subscribe successfully; state-only
  callback constraint verified (`exec/results` contains no `output` field)
- **FAIL**: MQTT broker unreachable, topic routing broken, output data leaked in
  state-only callback

---

## 4.8 Scenario 25 — A2A Protocol Module (L1 Engine)

### 4.8.1 Purpose

Validate authenticated A2A 0.3 interoperability, shared task persistence across
two replicas, APIServer RBAC propagation, and a real Flowgent run.

### 4.8.2 Prerequisites

- Scenario 11 passed (API Server and both A2A replicas healthy on `:9992`)
- `FLOWGENT_E2E_AUTH_TOKEN` is injected from the test-only Kubernetes Secret

### 4.8.3 Steps

| Step | Action | Expected input | Expected output |
| ---- | ------ | -------------- | --------------- |
| 3.1 | `GET /_/healthz` and `GET /.well-known/agent.json` | A2A port reachable | HTTP 200; protocol `0.3.0`, JSONRPC, Bearer scheme, ≥9 skills |
| 3.2 | `POST /` JSON-RPC `message/send` then `tasks/get` | Bearer token and `list_flows` action | Completed protocol Task readable from shared DB store |
| 3.3 | `message/send` `create_flow` + `start_run` | Minimal real Sandbox Flow | APIServer accepts caller RBAC and returns a real `run_id` |
| 3.4 | Repeated `get_run`, then `list_runs` | Real `run_id` | Run reaches `COMPLETED` and appears in persisted list |
| 3.5 | `delete_flow` | Temporary Flow ID | Cleanup succeeds through the same A2A/RBAC path |

### 4.8.4 Pass/Fail Criteria

- **PASS**: every authenticated standard JSON-RPC, shared-store, RBAC, and real
  execution assertion succeeds.
- **FAIL**: any unreachable service, invalid card, protocol error, missing Task,
  RBAC bypass/failure, or non-completing FlowRun. Scenario 25 never returns SKIP.

---

## 4.10 Scenarios 31-34 — Security Fixer E2E Capstone (L2 Application)

The capstone E2E verification of the `security-autonomy-fixer` flow is split into
4 sub-verifiers, each covering a natural execution stage of the 11-phase pipeline.
Shared utilities live in `verifier/_common.py`.

### Flow Execution Stages

```
Stage A (Scenario 31): trigger → get-commit → scan-sonarqube → aggregate-issues
Stage B (Scenarios 32-33): aggregate-issues → generate-fixes → [3 reviews] → committee
                           → supervisor-check → is-approved → human-approval
Stage C (Scenario 34):     human-approval → check-existing-pr → pr-exists
                           → [new PR | existing PR delivery branch]
Stage D (Scenario 34):     delivery merge → trigger-rescan → wait-rescan → check-resolved
                           → compare-results → fix-complete → summary-report → end
```

The current runtime has one-shot node state. Negative review still goes through
the persisted human gate, and an incomplete re-scan still reaches the report;
the executable manifest intentionally contains no repeated-node back-edge.

### 4.10.1 Scenario 31 — Seed & Trigger

**Purpose**: Register agents and MCPs, create the flow definition from YAML,
trigger the flow run, and verify the run_id is correctly recorded.

| Step | Action                                  | Expected Input                                                | Expected Output                                        |
| ---- | --------------------------------------- | ------------------------------------------------------------- | ------------------------------------------------------ |
| 11.1 | Seed agents from `config/agents/*.yaml` | Agent YAML files                                              | Agents registered (API 200/201)                        |
| 11.2 | Seed MCPs from `config/mcps/*.yaml`     | MCP YAML files (token resolved)                               | MCPs registered (API 201)                              |
| 11.3 | Create flow definition via API          | `security-autonomy-fixer.yaml` with env placeholders resolved | Flow created                                           |
| 11.4 | Capture PR baseline                     | GitHub PR #4 commits before trigger                           | `.last_pr_baseline.json` stores commit count/head/shas |
| 11.5 | Trigger flow run                        | Manual trigger or webhook fallback                            | run_id returned and persisted to `.last_run_id`        |
| 11.6 | Verify PG: `orh_flowrun`                | run_id                                                        | Row exists with correct `status`                       |

### 4.10.2 Scenario 32 — Discovery & Analyze

**Purpose**: Poll the run until tasks are available, then verify the DISCOVERY
and ANALYZE phases completed correctly.

**Dependency**: Scenario 31 must have run first (`.last_run_id` file required).

| Step | Action                       | Expected Input                                | Expected Output                                                                                                                                                                                                                                 |
| ---- | ---------------------------- | --------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 32.1 | Poll run status              | `GET /api/v1/{namespace}/runs/{run_id}`       | Status reaches terminal state                                                                                                                                                                                                                   |
| 32.2 | Fetch task list              | `GET /api/v1/{namespace}/runs/{run_id}/tasks` | Task array returned                                                                                                                                                                                                                             |
| 32.3 | Verify `get-commit`          | Task output                                   | `commit_sha` or GitHub MCP `sha` populated                                                                                                                                                                                                      |
| 32.4 | Verify `scan-sonarqube`      | Task output                                   | Issues array present, including JSON wrapped in MCP `text`                                                                                                                                                                                      |
| 32.5 | Verify `aggregate-issues`    | Task output                                   | Normalized issues with source/rule/severity fields                                                                                                                                                                                              |
| 32.6 | Verify sandbox analyze nodes | Task rows + MQTT audit                        | `git-clone` and `read-source-files` completed without hidden sandbox errors; `read-source-files` skipped generated code and selected a bounded single-file `issues[]`/`files[]` context; non-`$share` audit saw sandbox trigger/result messages |

### 4.10.3 Scenario 33 — Remediation

**Purpose**: Verify the core remediation pipeline: fix generation, triple review,
committee vote, supervisor safety gate, and human approval routing.

| Step | Action                    | Expected Input | Expected Output                                                                                                                      |
| ---- | ------------------------- | -------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| 33.1 | Verify `generate-fixes`   | Task output    | Compact JSON with at most one changed full-file entry in `files[]` and matching `patches[]` metadata (`file`, `rule`, `description`) |
| 33.2 | Verify `review-security`  | Task output    | Decision + reason fields                                                                                                             |
| 33.3 | Verify `review-quality`   | Task output    | Decision + reason fields                                                                                                             |
| 33.4 | Verify `review-arch`      | Task output    | Decision + reason fields                                                                                                             |
| 33.5 | Verify `committee`        | Task output    | Committee vote decision                                                                                                              |
| 33.6 | Verify `supervisor-check` | Task output    | Safety gate action (approved/rejected)                                                                                               |
| 33.7 | Verify `is-approved`      | Task status    | Condition node evaluated                                                                                                             |
| 33.8 | Verify `human-approval`   | Task status    | Gate auto-approved or skipped                                                                                                        |

### 4.10.4 Scenario 34 — Delivery & Report

**Purpose**: Verify branch/commit/PR creation, SonarQube re-scan chain, summary
report generation, and PG final state coverage across all phases.

| Step | Action                                       | Expected Input                          | Expected Output                                                                                |
| ---- | -------------------------------------------- | --------------------------------------- | ---------------------------------------------------------------------------------------------- |
| 34.1 | Verify `create-branch`                       | Task status                             | COMPLETED                                                                                      |
| 34.2 | Verify `commit-fixes` / `commit-to-existing` | Task status + output text               | COMPLETED and GitHub MCP output contains no `failed to`/`validation failed`/`error:` marker    |
| 34.3 | Verify `create-pr`                           | Task status + output text               | COMPLETED and no hidden MCP failure on the selected branch                                     |
| 34.4 | Verify re-scan chain                         | 5 nodes (trigger-rescan → fix-complete) | All COMPLETED                                                                                  |
| 34.5 | Verify `summary-report`                      | Task output                             | Report with content when the report path is reached; otherwise path-dependent skip is recorded |
| 34.6 | Verify `notify-pr`                           | Task status                             | COMPLETED                                                                                      |
| 34.7 | Verify PG: all phases                        | `task_runs` table                       | Every non-optional phase has ≥1 completed task                                                 |

### 4.10.5 Pass/Fail Criteria (E2E capstone, all 31-34)

- **PASS**: Flow reaches `COMPLETED`; all nodes in executed phases complete;
  `task_runs` rows persisted with output; REST task APIs return correct data;
  PG state consistent with API responses; non-`$share` MQTT audit observes
  `exec/plans`, `exec/results`, `sandbox/trigger`, and `sandbox/result`
- **FAIL**: Flow hangs (timeout), no task rows persisted, REST/PG mismatch
- **FAIL**: Any task row has a hidden sandbox error despite success status,
  or PR #4 receives no Flowgent-produced fix commit in scenario 35

---

## 4.11 Scenario 35 — PR Commit Verification (L2 Application)

### 4.11.1 Purpose

Verify that the security-autonomy-fixer L2 agents produced actual fix commits
on the target repository's pull request. This is the **work product**
verification — it checks what agents delivered, not just pipeline execution.

### 4.11.2 Prerequisites

- GitHub API access (public repo; token optional but recommended)
- PR #4 on `wl4g/rengine` exists
- Branch `fix/flowgent_sec_auto_fix` exists

### 4.11.3 Steps

| Step | Action                                                        | Expected Input                  | Expected Output                                                                        |
| ---- | ------------------------------------------------------------- | ------------------------------- | -------------------------------------------------------------------------------------- |
| 35.1 | `GET /repos/{owner}/{repo}/pulls/{number}`                    | `wl4g/rengine`, PR #4           | PR metadata: `title`, `state` (open/merged), `changed_files`, `additions`, `deletions` |
| 35.2 | Load `.last_pr_baseline.json`                                 | Scenario 31 output              | Pre-trigger commit count/head/shas available                                           |
| 35.3 | `GET /repos/{owner}/{repo}/pulls/{number}/commits`            | PR #4                           | Full PR commit list with SHA, author, message                                          |
| 35.4 | Classify commits                                              | Commit messages + baseline shas | At least one new Flowgent security-fix commit after s31 baseline                       |
| 35.5 | `GET /repos/{owner}/{repo}/pulls/{number}/files?per_page=100` | PR #4                           | File list with `filename`, `additions`, `deletions`, `status`                          |
| 35.6 | Classify files                                                | File paths                      | Test files vs source files                                                             |

### 4.11.4 Pass/Fail Criteria

- **PASS**: PR accessible, PR has commits, and at least one new Flowgent
  security-fix commit appears after the Scenario 31 baseline
- **FAIL**: PR not accessible, baseline missing, branch has no commits, or PR #4
  contains only historical Flowgent commits with no new security-fix commit from
  the current run

---

## 4.12 Scenario 36 — Knowledge RAG Verification (L2 Application)

### 4.12.1 Purpose

Validate the Knowledge retrieval-augmented generation (RAG) pipeline: knowledge
CRUD via the REST API, keyword and semantic search, pre-LLM knowledge injection,
and asynchronous knowledge extraction after flow runs.

### 4.12.2 Prerequisites

- Scenarios 12, 21 passed (API server running, PG accessible)
- Knowledge table created in PostgreSQL
- Flow with at least one LLM (agent) node registered

### 4.12.3 Steps

| Step  | Action                                      | Expected Input                                           | Expected Output                                       |
| ----- | ------------------------------------------- | -------------------------------------------------------- | ----------------------------------------------------- |
| 36.1  | `POST /api/v1/{namespace}/knowledge`        | `{title, content, tags: ["security", "java"]}`           | HTTP 201, knowledge entry with `id`                   |
| 36.2  | `GET /api/v1/{namespace}/knowledge`         | —                                                        | JSON array with created entry                         |
| 36.3  | `GET /api/v1/{namespace}/knowledge/{id}`    | Knowledge ID                                             | Entry with correct `title`, `content`, `tags`         |
| 36.4  | `PUT /api/v1/{namespace}/knowledge/{id}`    | `{content: "updated content"}`                           | HTTP 200, content updated                             |
| 36.5  | `POST /api/v1/{namespace}/knowledge/search` | `{query: "SQL injection parameterized query", top_k: 5}` | Matching entries returned by tokenized keyword search |
| 36.6  | `GET /api/v1/{namespace}/knowledge/tags`    | —                                                        | JSON array with all distinct tags                     |
| 36.7  | `DELETE /api/v1/{namespace}/knowledge/{id}` | Knowledge ID                                             | HTTP 200/204                                          |
| 36.8  | Trigger flow with LLM node                  | Flow with `knowledge_search` config                      | LLM node system prompt contains injected knowledge    |
| 36.9  | Wait for run completion                     | Run ID                                                   | Run reaches `COMPLETED`                               |
| 36.10 | Verify knowledge updated                    | `GET /api/v1/{namespace}/knowledge?source=flow_run`      | New entries created from node outputs                 |

### 4.12.4 Knowledge Injection Verification

Before each LLM node executes, the engine retrieves relevant knowledge entries and
injects them into the system prompt. Verification checks:

- **Pre-execution**: Knowledge entries matching the node's domain are retrieved
- **Injection format**: Entries are formatted as context blocks in the system prompt
- **Post-execution**: Node outputs are upserted as new knowledge entries with
  `source=flow_run` and `source_ref={flow_id}:{run_id}:{node_id}`

### 4.12.5 Pass/Fail Criteria

- **PASS**: All CRUD operations return correct responses, search returns relevant
  results, knowledge is injected into LLM prompts, and new knowledge is created
  after flow completion
- **FAIL**: CRUD endpoints return 5xx, search returns empty for known terms,
  knowledge not injected, or post-run knowledge not created

---

## 4.13 Scenario 37 — Volume Workspace Verification (L2 Application)

### 4.13.1 Purpose

Validate hostPath volume mount, workspace writability, and git clone evidence
within **TM/Sandbox Pod containers** (NOT on the host filesystem).

### 4.13.2 CRITICAL Constraints

> **IMPORTANT — 以下约束必须严格遵守:**
>
> 1. **工作区目录 `/mnt/disk1/flowgent/e2e/` 必须由 security-autonomy-fixer 的
>    DAG tasks (如 `git-clone` sandbox node) 在执行时自动创建。**
>    verifier 脚本绝不可直接在宿主机上操作此目录（不创建、不写入、不删除）。
>
> 2. **所有校验必须通过 `kubectl exec` 进入 TM Pod 或 Sandbox Pod 容器内部进行。**
>    宿主机上的 `/mnt/disk1/flowgent/e2e/` 只是 hostPath 挂载源，
>    容器内看到的才是真实的工作区状态。
>
> 3. **验证 rengine clone 证据时，必须在容器内查找 `.git` 目录、源码文件等，**
>    绝不能直接在宿主机路径上 `ls`、`find` 或 `git clone`。
>
> 4. **在完整 s11-s37 轮次中，工作区必须已由当前 flow run 创建。**
>    `s37` 会读取 `.last_run_id`，并在容器内检查当前 run 的
>    `/var/flowgent/{namespaceId}/{flowId}/{runId}/{taskId}` 目录和
>    git-clone task 下的 `repos/rengine/.git`。若未找到，完整轮次必须 FAIL。

### 4.13.3 Prerequisites

- Scenario 11 passed (K8S running, Helm deployed)
- Shared TM or Sandbox pod Running in tenant namespace (`default`)
- `security-autonomy-fixer` flow triggered and DAG execution reached `git-clone` node

### 4.13.4 Steps

| Step | Action                                                                                | Expected Input                                                                                  | Expected Output                                                                        |
| ---- | ------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| 37.1 | Load `.last_run_id`                                                                   | Scenario 31 output                                                                              | Current run ID available                                                               |
| 37.2 | Find running TM/Sandbox pod via `kubectl get pods -n default -l flowgent/role=worker` | KUBECONFIG                                                                                      | Pod name and namespace returned                                                        |
| 37.3 | `kubectl exec` into pod: verify workspace dir inside container                        | Container workspace path                                                                        | Directory exists                                                                       |
| 37.4 | Verify workspace volumeMount in deployment spec                                       | TM/Sandbox deployment YAML                                                                      | workspace volumeMount and hostPath source present                                      |
| 37.5 | `kubectl exec` into pod: verify writable flag                                         | Container workspace path                                                                        | `test -w` succeeds without creating files                                              |
| 37.6 | `kubectl exec` into pod: inspect current run subdirectories                           | `/var/flowgent/default/security-autonomy-fixer/{run_id}`                                        | Sandbox task directories exist for `git-clone`, `read-source-files`, and `wait-rescan` |
| 37.7 | `kubectl exec` into pod: verify legacy path absent                                    | `/var/flowgent/default/security-autonomy-fixer/runs/{run_id}`                                   | Legacy `runs/plans/span` workspace absent for the current run                          |
| 37.8 | `kubectl exec` into pod: locate cloned repo at expected path                          | `/var/flowgent/default/security-autonomy-fixer/{run_id}/{git_clone_task_id}/repos/rengine/.git` | `.git` directory found exactly at expected repo path                                   |
| 37.9 | `kubectl exec` into pod: `ls` + `git log` + file count inside repo dir                | Repo directory path                                                                             | Source files present, git history visible, `pom.xml` exists                            |

### 4.13.5 Pass/Fail Criteria

- **PASS**: TM/Sandbox pod found, workspace volume mount configured, writable, current-run task directories present, rengine `.git` found under the git-clone task directory with source files
- **FAIL**: No TM/Sandbox pod running, workspace volumeMount missing, current run workspace missing, legacy `runs/plans` workspace still used, or the git-clone task repo `.git` is absent

---

# 5. Troubleshooting

## 5.1 Common Issues

| Symptom                                                                                | Likely Cause                                                                                                  | Resolution                                                                                                                                                                                    |
| -------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Connection refused :9999`                                                             | API Server not running                                                                                        | `kubectl logs deploy/flowgent-apiserver`                                                                                                                                                      |
| `MQTT publish timeout`                                                                 | EMQX pod not ready                                                                                            | `kubectl get pods \| grep emqx`                                                                                                                                                               |
| `PG connection failed`                                                                 | PostgreSQL credentials wrong                                                                                  | Check `config.py` PG_* vars                                                                                                                                                                   |
| `ImagePullBackOff`                                                                     | flowgent-core image missing                                                                                   | Re-run `docker save \| k3s ctr images import`                                                                                                                                                 |
| `JM Deployment not created`                                                            | Controller not running or RBAC missing                                                                        | Check ClusterRoleBinding for namespace namespace                                                                                                                                              |
| Deleted `test-flow-*` JM keeps reappearing                                             | Soft-deleted Flow is still visible to API/Controller, or Flow-JM cleanup missed old workloads | Verify API `GET /flows/{id}` returns 404, `LIST /flows` omits it, and redeploy deletes `flowgent.io/runtime-boundary=flow-jobmanager` resources |
| `GET` after DELETE returns HTTP 500                                                    | Handler mapped store not-found/no-row errors to internal server error                                         | Map not-found store errors to HTTP 404; s21 treats any 5xx after DELETE as failure                                                                                                            |
| `init MCP github ... server returned 4xx`                                              | TM pod lacks `GITHUB_TOKEN` because runtime credential Secret was missing or not mounted                      | Verify `flowgent-e2e-runtime-env` exists in `default` and `flowgent-default`, and JM/TM pod templates use `envFrom.secretRef`                                                                 |
| `git-clone` sandbox task times out or shows `Failed to connect to github.com port 443` | Sandbox pod did not receive a pod-reachable HTTPS proxy                                                       | Verify `flowgent-e2e-runtime-env` contains `HTTPS_PROXY` rewritten to the pod default gateway or another pod-reachable host, not `127.0.0.1`, and `NO_PROXY` includes `.svc`/`.cluster.local` |
| `Jaeger trace not found`                                                               | OTEL not configured                                                                                           | Verify `OTEL_EXPORTER_OTLP_ENDPOINT` env                                                                                                                                                      |
| `Jaeger services only contains jaeger-all-in-one`                                      | Flowgent exporter did not ingest a startup span                                                               | Check Flowgent OTEL init logs; provider should fail startup if ForceFlush cannot export                                                                                                       |
| s13 has Jaeger spans but cannot find `OTEL tracing enabled` in logs                    | Startup log line rotated or pod log window changed                                                            | s13 should use live pod/config OTEL evidence as fallback while still requiring Jaeger trace and core span coverage                                                                            |
| `Human approval timeout`                                                               | WebSocket not connected                                                                                       | Check notifier pod logs                                                                                                                                                                       |
| `MCP not found: github/sonarqube`                                                      | MCPs not registered or TM not restarted                                                                       | Verify `GET /api/v1/{namespace}/mcp`, restart TM pods                                                                                                                                         |
| `LLM provider not found: default`                                                      | LLM provider missing, inactive, or imported with non-runtime status                                           | Verify `llm_providers.status='ACTIVE'` after console import                                                                                                                                   |
| `DiskPressure taint`                                                                   | Node disk >95%                                                                                                | Clean Docker images, journald logs, caches                                                                                                                                                    |
| `.last_run_id not found`                                                               | Scenario 31 not run before 32/33/34                                                                           | Run `python3 runner.py --skip-sonarqube --skip-build --skip-import --skip-deploy -s 31` first                                                                                                 |

## 5.2 Log Collection

```bash
# API Server logs
kubectl logs deploy/flowgent-apiserver -n default --tail=100

# JobManager logs (find the active Flow JM pod first)
JM_POD=$(kubectl get pods -n flowgent-default -l app=flowgent-jobmanager -o name | head -1)
kubectl logs $JM_POD -n flowgent-default --tail=100

# TaskManager logs
kubectl logs deploy/flowgent-taskmanager -n flowgent-default --tail=100

# EMQX dashboard
open http://localhost:18083  # admin / public

# K8s Flowgent Jaeger UI (runner port-forward)
open http://localhost:16687

# PostgreSQL query
kubectl exec -it deploy/flowgent-postgres -- psql -U flowgent -d flowgent -c \
  "SELECT id, status, created_at FROM orh_flowrun ORDER BY created_at DESC LIMIT 5;"
```

---

# 6. Performance Benchmarks

Expected execution times (K8S single-node, 4 CPU, 8GB RAM):

| Scenario                 | Duration | Bottleneck                                    |
| ------------------------ | -------- | --------------------------------------------- |
| 11 — Infrastructure      | 10-15s   | K8s API queries                               |
| 12 — Console Import      | 5-10s    | Binary startup + DB writes                    |
| 13 — OTEL                | 10-15m   | Full flow trigger + Jaeger span queries       |
| 21 — API Server CRUD     | 20-30s   | DB writes + MQTT events                       |
| 22 — Notifier            | 5-10s    | MQTT connect + subscribe                      |
| 23 — Controller          | 40-60s   | K8s Deployment creation                       |
| 24 — Messager Topics     | 30-45s   | MQTT round-trips                              |
| 25 — A2A Protocol        | 5-10s    | HTTP round-trips                              |
| 31 — Seed & Trigger      | 10-15s   | Agent/MCP registration + flow creation        |
| 32 — Discovery & Analyze | 60-180s  | SonarQube scan + git clone/read-source + poll |
| 33 — Remediation         | 120-300s | LLM calls (fix gen + reviews)                 |
| 34 — Delivery & Report   | 60-120s  | Git ops + re-scan + report                    |
| 35 — PR Verification     | 5-10s    | GitHub API calls                              |
| 36 — Knowledge RAG       | 15-30s   | Knowledge CRUD + search + flow run            |
| 37 — Volume Workspace    | 10-20s   | Pod-internal mount + clone evidence           |

**Total suite runtime**: ~25-60 minutes (dominated by scenarios 13, 32, and 33 full-flow/LLM calls)

---

# 7. Architecture Compliance

## 7.1 Key Design Decisions (v3.0)

### 7.1.1 TM → JM Communication

- **State-only callback**: `exec/results` contains `{plan_id, node_id, state}` only
- NO output data in MQTT message
- Output persisted via REST `PUT /api/v1/{namespace}/runs/{id}/tasks/{task_id}`

### 7.1.2 Lifecycle Event Publisher

- Only API Server publishes `ctrl/*` events (`ctrl/flow/updated`, `ctrl/flow/deleted`,
  `ctrl/run/created`, `ctrl/run/status`)
- TM and JM do NOT publish lifecycle events

### 7.1.3 Runtime Clusters

- A Flow definition alone does not allocate runtime compute.
- Application-mode runs create one isolated runtime cluster per Run. The
  Controller starts the per-run JM in `{prefix}{namespace}` and passes
  `runtime_cluster_id=app-{run_id}`; that JM's ResourceManager owns only
  TM/Sandbox Deployments carrying the same runtime-cluster label.
- Session-mode runs are consumed by the Helm-deployed session JM. Session
  TM/Sandbox worker deployments are Helm-owned and remain scoped by
  `runtime_cluster_id=session`.
- Scheduled/webhook/API triggers create Runs; imports do not.

### 7.1.4 DAG Dependency Coordination

- JM outer-loop calls `Ready()` to discover newly-unblocked nodes
- `Schedule()` blocks on per-node Go channel until TM publishes `exec/results`
- Siblings dispatched sequentially (one-at-a-time, blocking)
- Dormant conditional edges skipped in `depsDone()`
- Deadlock detection: `Ready()` empty + `IsComplete()` false → break

### 7.1.5 Table Naming

- `orh_*` prefix: orchestration entities (`orh_agentflow`, `orh_flowrun`)
- `llm_*` prefix: AI entities (`llm_agent`, `llm_mcp`, `llm_providers`)
- `task_runs`: task execution records
- `human_approvals`: human-in-the-loop approvals

## 7.2 Verifier Contract

- `runner.py` is the only supported scenario entry point; every real round must
  execute the full ordered suite `s11` through `s37`.
- `s11` and `s12` are hard gates: Helm pod readiness, apiserver health,
  provisioning evidence, and resource counts fail the suite on mismatch.
  Classic mode verifies console import; UI-provisioned mode verifies the
  browser-created inventory and explicitly forbids console import.
- `s11` must fail if a full redeploy/import leaves verifier-created application
  runtime-cluster workloads behind, or if metadata-only imports allocate idle
  application runtime resources.
- Runner-owned localhost tunnels must repeatedly pass apiserver health, EMQX TCP,
  and Jaeger Query probes before any scenario starts.
- MQTT audit subscribers used by `s31` through `s34` are ordinary non-`$share`
  subscriptions. They observe real `ctrl/run/created`, `exec/plans`,
  `exec/results`, `sandbox/trigger`, and `sandbox/result` topics without
  joining the L1 worker consumer groups. The observer uses a `clusters/+`
  wildcard for cluster-routed `exec/plans` and `sandbox/trigger`; callbacks
  remain on the Flow/Run path.
- Runtime checks require controller-created JM pods only after a real Run exists.
  Application TM and Sandbox readiness must be namespace/runtime-cluster scoped
  in `flowgent-{namespace}` and match the declared replicas and slots.
- Runtime credential checks require `flowgent-e2e-runtime-env` in both the
  system namespace and the workload namespace, and `envFrom.secretRef` on
  Controller-created JM plus JM-created TM/Sandbox pod templates. When host
  proxy env is present, the same checks require pod-reachable proxy values in
  that Secret because sandbox `git-clone` cannot use host-local `127.0.0.1`.
- Scenario 31 API seeding must resolve the same environment placeholders as
  console import; it must not overwrite the imported flow with literal E2E
  placeholders such as `${FLOWGENT_E2E_PROXY_ALLOWLIST_ENTRY}`.
- API CRUD checks must treat soft-delete visibility as a hard contract:
  after DELETE, the resource may remain in PG with `del_flag=true`, but API
  `GET` must return 404 and API `LIST` must not include it.
- With Sandbox workers enabled, only Sandbox pods in the selected runtime
  cluster should subscribe to its shared `.../sandbox/trigger` consumer group;
  TM embedded sandbox runners are disabled.
- The security-autonomy-fixer DAG currently has 29 nodes and 33 edges. The existing PR path is
  `check-existing-pr -> pr-exists -> commit-to-existing -> trigger-rescan`; the
  new-PR path is `create-branch -> commit-fixes -> create-pr -> trigger-rescan`.
- In Kubernetes, each active application JM is labelled with its Flow, Run,
  runtime mode, and runtime cluster. TM/Sandbox Deployments are labelled as
  runtime-cluster-owned. Controller deletes the application JM and its
  TM/Sandbox deployments when the Flow is deleted or after the completed Run
  exceeds the observation window. Session deployments are Helm-owned.
- JM pollers must claim a run through the configured distributed lock before
  starting a JobMaster. Helm renders `lock.provider=postgres` when
  `storage.type=POSTGRE`, otherwise `memory` for local SQLite deployments.
- `s37` verifies the shared workspace only via `kubectl exec` inside TM/Sandbox
  containers at `/var/flowgent`; verifier code must not create, modify, or scan
  the host path `/mnt/disk1/flowgent/e2e`. It must verify the current
  `.last_run_id` workspace and the expected git-clone task repo path, not
  arbitrary stale `.git` directories.

---

# 8. References

- **Architecture**: `docs/architecture/overview.md`
- **Use Cases**: `docs/use-cases/overview.md`
- **Known Issues**: `usecase/security-autonomy-fixer/e2e/KNOW_ISSUE.md`
- **Flow Definition**: `usecase/security-autonomy-fixer/config/flows/security-autonomy-fixer.yaml`
- **Agent Definitions**: `usecase/security-autonomy-fixer/config/agents/*.yaml`
- **MCP Servers**: `usecase/security-autonomy-fixer/config/mcps/`
- **Shared Utilities**: `usecase/security-autonomy-fixer/e2e/verifier/_common.py`
- **Deploy Scripts**: `usecase/security-autonomy-fixer/e2e/deploy/*.py`
- **Use-Case Runner**: `usecase/security-autonomy-fixer/e2e/runner.py` (pipeline + verification)
- **UI System Runner**: `usecase/security-autonomy-fixer/e2e/ui_e2e_runner.py` (clean redeploy + browser provisioning/run/tracking + verification)
- **Scenario Scripts**: `usecase/security-autonomy-fixer/e2e/verifier/*.py`
