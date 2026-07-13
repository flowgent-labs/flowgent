# E2E Verification — Security Autonomy Fixer

**Status**: 🔬 **IN VERIFICATION** (Testing with real PR)  
**Version**: v3.2  
**Date**: 2026-07-13

> **IMPORTANT: Core Goal of This Verification: Test the Flowgent `security-autonomy-fixer` agent flow can AUTONOMOUSLY discover, analyze, fix, review, and create a PR for real SonarQube security issues — MUST fix to `rengine` by Flowgent this agent flow, but NOT you directly fixing the target project's vulnerabilities.**

> This document describes the complete end-to-end verification strategy for the Security Autonomy Fixer use case, covering 11 phases, 24 nodes, and all inter-component communication paths.

## Core Purpose

**The goal is NOT to manually fix the target project's SonarQube vulnerabilities.**
The goal is to **verify that Flowgent's security-autonomy-fixer workflow can AUTONOMOUSLY
discover, analyze, fix, review, and create a PR** for security issues found by SonarQube —
using a real-world PR as the test input.

The test subject is the **Flowgent workflow itself**, not the target project's code quality.

**Test PR**: https://github.com/wl4g/rengine/pull/4 (branch: `fix/flowgent_sec_auto_fix`)
- 3 files changed, +111/−1 lines
- Adds SonarQube quality gate CI check, removes deprecated `sonar.language` property
- Target project `rengine` has 10 open SonarQube issues (vulnerabilities + bugs), last analyzed 2026-07-11

**Note**: This is a comprehensive test plan. Scenario scripts are ready for execution but not yet run. All verification checkpoints are designed based on v3.0 architecture decisions.

---

## Verification Required

```bash
# Run all verification scenarios
python3 runner.py

# Run specific modules (ordered by runtime dependency)
python3 runner.py -s 01  # Infrastructure verification
python3 runner.py -s 02  # API Server CRUD
python3 runner.py -s 03  # A2A protocol validation
python3 runner.py -s 04  # Controller -> JM pod lifecycle
python3 runner.py -s 05  # Engine DAG scheduling
python3 runner.py -s 06  # Basic node executors
python3 runner.py -s 07  # Messager topics + Sandbox chain
python3 runner.py -s 08  # Notifier connectivity
python3 runner.py -s 09  # Wallet key mgmt + MQTT signing
python3 runner.py -s 10  # OTEL / Jaeger tracing
python3 runner.py -s 11  # E2E security fixer capstone

# List all scenarios
python3 runner.py -l

# Custom endpoints
python3 runner.py --api http://10.0.0.1:9999 --pg postgres://u:p@h/db
```

**Environment:** K3s single-node, namespace `default`, Application mode only  
**Flow:** `security-autonomy-fixer` (24 nodes, 11 phases)  
**SonarQube:** `http://172.29.235.101:9000`

## ⛔ MANDATORY: Use `runner.py` for ALL E2E Verification Execution

**The `runner.py` script is the SOLE entry point for running any and all E2E verification
scenarios. Do NOT manually execute individual scenario files or attempt ad-hoc
infrastructure setup, deployment, or testing outside of `runner.py`.**

### Rules (non-negotiable):

1. **All scenario execution MUST go through `runner.py`** — `python3 runner.py -s NN`
2. **The runner.py orchestrates everything** — it imports and runs each scenario's
   `run()` function in the correct order, tracks pass/fail status, and reports results.
3. **No manual infrastructure steps** — do not manually run `helm install`, `kubectl`,
   `make build-image-core`, or any other setup commands outside of what a scenario
   script does internally. The scenario scripts (especially `01_infra_verifier.py`)
   are responsible for verifying infrastructure readiness.
4. **If a scenario fails**, fix the root cause (code, config, or infrastructure),
   then re-run that scenario via `runner.py -s NN`. Do NOT manually patch things and
   skip the scenario check.
5. **Before running any scenario**, ensure the environment is clean per the
   [Environment Reset](#environment-reset-required-before-every-real-e2e-run) section
   below.
6. **⛔ NO MOCK SERVICES — all external APIs MUST be called for real.** The only
   external services involved are **GitHub API** (via `https://api.githubcopilot.com/mcp/`)
   and **SonarQube API** (via `http://172.29.235.101:18080/mcp` → upstream
   `http://172.29.235.101:9000`). K3s/K8s, EMQX, PostgreSQL, and Redis are local
   infrastructure (not mock targets). SonarQube MCP authentication is backend static
   bearer token — the MCP client (TM) does not handle auth. No mock server, stub,
   or simulated API may be introduced for any scenario. If a test discovers a
   missing external API endpoint or capability, **do NOT create a mock — ask first**.

### Why this matters:
- Scenarios are designed as **verification checkpoints**, not as loose scripts.
  Each one validates specific architecture invariants (table names, MQTT topics,
  state-only callbacks, etc.) that manual steps cannot reliably verify.
- Bypassing `runner.py` and manually fixing infrastructure hides bugs in the
  deployment automation (Helm chart, configmap template, image build, etc.).
- The runner provides a single source of truth for pass/fail across all 11 scenarios.

### Quick reference:
```bash
python3 runner.py           # All 11 scenarios (ordered)
python3 runner.py -s 01     # Infrastructure only
python3 runner.py -s 11     # E2E capstone only
python3 runner.py -l        # List all scenarios
```

**⚠️ Every real (non-mocked) run of this suite against a live cluster MUST start
from a clean redeploy.** See [Environment Reset](#environment-reset-required-before-every-real-e2e-run)
below — this is not optional troubleshooting, it is a hard prerequisite. The
suite creates real Deployments, PG rows, and MQTT traffic; state left over from
a previous run (orphaned JM Deployments, stale `test-flow-*` rows, retained
MQTT messages, or a Controller pod that has been running for a while and
already dispatched every flow's current version) will cause false
positives/negatives — most importantly it will make scenario 04's "Controller
dispatches on flow CREATE" checks unreliable, since a Controller that has been
running against the same apiserver across multiple suite runs may have already
seen and GC'd resources for flow IDs from a previous run.

**Scenario Files** (numbered by functional module execution order):
- `01_infra_verifier.py` — Infrastructure & deployment checks
- `02_apiserver_verifier.py` — API Server CRUD + lifecycle events
- `03_a2a_protocol_verifier.py` — A2A protocol validation
- `04_controller_verifier.py` — Controller Application mode lifecycle (JM pods)
- `05_engine_verifier.py` — Engine DAG scheduling + voting
- `06_basic_nodes_verifier.py` — Basic node execution (after engine dispatch)
- `07_messager_verifier.py` — MQTT topics + Sandbox chain
- `08_notifier_verifier.py` — Multi-channel notifier connectivity
- `09_wallet_verifier.py` — Wallet key management + MQTT signing
- `10_otel_verifier.py` — OTEL / Jaeger 24-node tracing
- `11_e2e_security_fixer.py` — **Capstone**: full security fixer pipeline white-box

---

## Environment Reset (Required Before Every Real E2E Run)

Every scenario in this suite mutates real, shared state: PostgreSQL rows,
Kubernetes Deployments/Pods, and MQTT traffic. Running the suite twice against
the same live cluster **without resetting state in between produces
unreliable results** — passes and failures both become suspect. Before every
real (non-dry-run) execution against a live K3s cluster:

```bash
# 1. Tear down the release completely (deletes all Flowgent Deployments,
#    Services, ConfigMaps, RBAC — but NOT the postgresql/emqx PVCs unless
#    you pass --set postgresql.persistence.enabled=false, see step 2)
helm uninstall flowgent -n default

# 2. Delete any leftover JM Deployments the Controller may not have GC'd yet
#    (e.g. if the Controller pod was killed mid-reconcile, or a previous
#    suite run crashed before scenario 04's cleanup ran). These live in each
#    flow's TENANT namespace (default "flowgent-{tenantID}", e.g.
#    "flowgent-default" — see tenant.namespace_prefix; every flow of the same
#    tenant shares one namespace, disambiguated by Deployment name), NOT in
#    "default" — use -A (all namespaces).
kubectl delete deployment -A -l flowgent.io/mode=application

# 2b. Delete the per-tenant namespaces themselves (only safe if nothing else
#     lives in them — they're labeled flowgent.io/mode=application so this
#     only targets Controller-created tenant namespaces). ensureApplicationInfra
#     lazily creates them on first dispatch for a tenant but the Controller
#     never deletes them (only the Deployment inside is GC'd per-flow), so
#     they accumulate across runs.
kubectl get ns -l flowgent.io/mode=application -o name | xargs -r kubectl delete

# 3. Wipe PostgreSQL data (orh_agentflow/orh_flowrun/task_runs/human_approvals/
#    supervisor_log all carry rows from previous runs — the canonical
#    `security-autonomy-fixer` flow ID in particular is fixed, not random, so
#    stale rows under that ID from a previous partial run WILL corrupt
#    scenario 11's assertions). Easiest: drop and let migrations recreate.
kubectl exec -it deploy/flowgent-postgres -n default -- \
  psql -U flowgent -d flowgent -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"
# (or: helm uninstall + delete the postgresql PVC, if postgresql.enabled=true
#  and it's an in-cluster ephemeral instance)

# 4. Rebuild and re-import the image (picks up any code changes)
make build-image-core
docker save flowgent-core:latest | sudo k3s ctr images import -

# 5. Fresh install. NOTE: there is no global.mode Helm value — the chart only
#    ever deploys Application-mode components (Session mode's shared
#    jobmanager/taskmanager templates were removed; see entities.Priority
#    doc comment and docs/01-L1-Engine-Architecture.md §1.1). Do not pass
#    --set global.mode=... — it does not exist in values.yaml and is a no-op.
helm install flowgent deploy/helm/flowgent \
  --set global.image.repository=localhost/flowgent-core \
  --set global.image.tag=latest \
  --set postgresql.enabled=false \
  --set emqx.enabled=true \
  --set redis.enabled=false \
  -n default

# 6. Wait for all pods Ready before running scenario 01
kubectl wait --for=condition=Ready pod -l app.kubernetes.io/name=flowgent -n default --timeout=120s
```

**Why this matters for specific scenarios**:
- **04 (Controller)**: The Controller only auto-dispatches a run the first
  time it observes a given flow-definition version ("on-new-definition"
  trigger — see `pkg/controller/pkg/controller.go` `shouldDispatch`). A
  Controller pod that survived from a previous suite run has an in-memory
  `dispatchedVersion` cache and will silently skip re-dispatch for flow IDs
  it has already seen at the same version. This is only a real problem for
  fixed/reused flow IDs (like `security-autonomy-fixer`); scenarios that
  generate random `test-flow-{uuid}` IDs are unaffected by this specific
  cache, but still leave orphaned K8s/PG state if a previous run didn't
  clean up after a failure.
- **02/05/06 (random test-flow-* IDs)**: Safe to re-run without a full reset
  in most cases (IDs are randomized per run), but a full reset is still
  recommended to avoid slow PG/Deployment accumulation across many runs.
- **11 (capstone, fixed `security-autonomy-fixer` ID)**: Always run against a
  clean PG — leftover `task_runs`/`human_approvals` rows from a prior partial
  run under the same flow ID will make phase-checkpoint and output assertions
  unreliable (e.g. a stale PENDING `human_approvals` row from a previous
  aborted run could be auto-approved instead of the current run's).

---

## Verification Architecture

### Layer Model

| Layer | Name | Focus | Scenarios |
|-------|------|-------|-----------|
| **L1** | Pre-Deployment | K3s health, image availability, external dependencies | `01` |
| **L2** | Infrastructure | Helm deployment, pod readiness, healthz checks | `01` |
| **L3** | API Server | REST CRUD, lifecycle events, data persistence | `02` |
| **L4** | A2A Protocol | Agent Card + task submit | `03` |
| **L5** | Controller | Flow lifecycle, K8s JM pod creation | `04` |
| **L6** | Engine | DAG scheduling, voting strategies | `05` |
| **L7** | Basic Nodes | Node executor coverage (after engine dispatch) | `06` |
| **L8** | Messager + Sandbox | MQTT topics, sandbox chain, state-only callback | `07` |
| **L9** | Notifier | Multi-channel delivery, WebSocket push | `08` |
| **L10** | Wallet | x402 key management and async signing | `09` |
| **L11** | OTEL Tracing | Jaeger span coverage | `10` |
| **L12** | E2E Pipeline | Capstone: full security fixer flow (24 nodes) | `11` |

---

## Scenario Catalog

### 01 — Pre-Deployment & Infrastructure (L1-L2)

**Purpose**: Validate cluster health and deployment prerequisites

**Checks**:
- ✅ K3s node Ready, system pods healthy
- ✅ SonarQube `/api/system/health` accessible
- ✅ EMQX port 1883 reachable
- ✅ Docker image `flowgent-core:latest` present
- ✅ Helm release `flowgent` deployed (Application mode — the only mode)
- ✅ All pods Running (no CrashLoopBackOff)
- ✅ API Server `/healthz` returns 200
- ✅ apiserver/controller/notifier logs clean (no ERROR/FATAL/PANIC) — JM/TM
  are not Helm-deployed (no shared pool in Application mode), so they're only
  checked once a test flow creates a dedicated JM (see scenario 04)

**Command**: `python3 runner.py -s 01`

**Manual Redeploy** (if infrastructure broken):
```bash
# Clean up
kubectl delete pod --field-selector=status.phase=Failed -n default --force
helm uninstall flowgent -n default

# Rebuild and import image
make build-image-core
docker save flowgent-core:latest | sudo k3s ctr images import -

# Install (Application mode is the only mode — no global.mode value exists)
helm install flowgent deploy/helm/flowgent \
  --set global.image.repository=localhost/flowgent-core \
  --set global.image.tag=latest \
  --set postgresql.enabled=false \
  --set emqx.enabled=true \
  --set redis.enabled=false \
  -n default
```

---

### 02 — API Server Module (L3)

**Purpose**: Validate REST API completeness and data consistency

**Entity Coverage**:
| Entity | Table | REST Path | Key Tests |
|--------|-------|-----------|-----------|
| AgentFlow | `orh_agentflow` | `/api/v1/{tenant}/flows` | CRUD, versioning, soft delete |
| FlowRun | `orh_flowrun` | `/api/v1/{tenant}/runs` | Trigger, status transitions |
| TaskRun | `task_runs` | `/api/v1/{tenant}/runs/{run_id}/tasks` | Nested CRUD, output persistence |
| Agent | `llm_agent` | `/api/v1/{tenant}/agents` | Name uniqueness |
| Skill | `llm_skill` | (no REST endpoint — import/console only) | Table existence verification |
| MCP | `llm_mcp` | `/api/v1/{tenant}/mcp` | Enable/disable |
| Provider | `llm_providers` | `/api/v1/{tenant}/llm/providers` | Multi-model config |
| Approval | `human_approvals` | `/api/v1/human/approvals` | Token-based access |
| Channel | `nfy_channel` | `/api/v1/{tenant}/notifications/channels` | Multi-channel config |

**Lifecycle Event Validation**:
- ✅ `POST /flows` → MQTT `flowgent/v1/{tenant}/flows/{id}/ctrl/flow/updated` (action=created)
- ✅ `PUT /flows/{id}` → MQTT `flowgent/v1/{tenant}/flows/{id}/ctrl/flow/updated` (action=updated, version++)
- ✅ `DELETE /flows/{id}` → MQTT `flowgent/v1/{tenant}/flows/{id}/ctrl/flow/deleted` (soft delete)
- ✅ `POST /runs` → MQTT `flowgent/v1/{tenant}/flows/{id}/runs/{rid}/ctrl/run/created`
- ✅ `PUT /runs/{id}` → MQTT `flowgent/v1/{tenant}/flows/{id}/runs/{rid}/ctrl/run/status` (status transitions)

**Command**: `python3 runner.py -s 02`

---

### 03 — A2A Protocol Module (L4)

**Purpose**: Validate A2A agent card discovery and task submission

**Checks**:
- ✅ `GET /.well-known/agent.json` returns valid agent card with skills
- ✅ `POST /a2a/tasks` submits a task and returns a task ID
- ✅ `GET /a2a/tasks` lists tasks (may not be supported)
- ✅ Graceful skip if A2A port is not reachable or not enabled

**Command**: `python3 runner.py -s 03`

**Note**: This scenario has a lightweight implementation. A2A is validated for connectivity; deep protocol compliance testing requires a registered flow.

---

### 04 — Controller Module (L5)

**Purpose**: Validate Application mode flow lifecycle management

**Scenarios**:
1. **Flow Created** → Controller creates JM Deployment
2. **Flow Updated** → Controller rolling updates JM pods
3. **Flow Deleted** → Controller garbage-collects JM/TM/Sandbox Deployments

**Command**: `python3 runner.py -s 04`

**Note**: This scenario focuses on K8s Deployment lifecycle verification. MQTT `ctrl/*` events are validated separately in scenario 07.

---

### 05 — Engine Module (L6)

**Purpose**: Validate DAG scheduling, node execution routing, and voting strategies

**Node Type Coverage** (12 types):
- `agent` — LLM agent execution
- `tool` — MCP tool invocation
- `skill` — Reusable skill execution
- `sandbox` — Isolated script execution
- `condition` — Expression evaluation
- `tribunal` — Multi-agent voting
- `supervisor` — Safety gate
- `human` — Async approval gate
- `map` — Parallel iteration
- `agentflow` — Subflow nesting
- `noop` — Pass-through
- (implicit) `join` — Fan-in aggregation (runtime task type, not a DAG node type)

**Command**: `python3 runner.py -s 05`

**Coverage Gap**: Scenario 05 validates DAG topologies at the REST API level
(create flow, trigger, wait for completion, check task order). It does **not**
validate the internal MQTT round-trip mechanism (subscribe-before-publish,
per-node channel routing, blocking Schedule()) or the iteration loop pattern
described in `docs/01-L1-Engine-Architecture.md` §3.4. These internals are
validated by Go unit tests (`pkg/core/pkg/engine/jobmanager/` and
`pkg/core/pkg/engine/resourcemanager/`) and are indirectly exercised by the
topology tests — if the iteration loop or result routing were broken, the
linear chain and fan-out tests would hang or produce wrong task ordering. A
future scenario could add MQTT-level introspection (subscribing to
`exec/plans` and `exec/results` topics during a test flow) to explicitly
verify the subscribe-before-publish ordering and per-node channel routing.

---

### 06 — Basic Nodes Module (L7)

**Purpose**: Validate simple DAG execution with agent, tribunal, and supervisor node types

**Covered Node Types**:
- `agent` — LLM agent execution (via `issue-detector`)
- `tribunal` — Majority vote aggregation
- `supervisor` — Safety gate with retry/injection/abort constraints

**Command**: `python3 runner.py -s 06`

**Note**: This scenario has a lightweight implementation covering the three most common node types. Full node-type coverage (12 types) is validated in scenario 05 (DAG topology tests). Additional node types (`tool`, `skill`, `sandbox`, `human`, `map`, `agentflow`) are exercised in combination during the E2E capstone (scenario 11).

---

### 07 — Messager Module (L8)

**Purpose**: Validate MQTT topic connectivity and message routing

**MQTT Topic Prefix**: All topics use the `flowgent/v1/` namespace with tenant/flow/run path segments. The table below shows topic suffixes for readability; full topic format is:
`flowgent/v1/{tenant}/flows/{flowId}/runs/{runId}/{suffix}` (or `flowgent/v1/heartbeat/{tmId}` for heartbeat, `flowgent/v1/notify/pod/{podId}/ws/{wsId}` for cross-pod WS).

**Topic Matrix** (14 topics):

| # | Topic Suffix | Publisher | Subscriber | Payload | Test |
|---|-------------|-----------|------------|---------|------|
| 1 | `exec/plans` | JM | TM ($share/tm-pool) | `ExecutionPlan` | Request dispatch |
| 2 | `exec/results` | TM | JM | `{plan_id, node_id, state}` | Status callback |
| 3 | `sandbox/trigger` | TM | Sandbox ($share/sandbox-pool) | `{plan_id, runtime, script}` | Script execution |
| 4 | `sandbox/result` | Sandbox | TM | `{plan_id, exit_code, stdout}` | Execution result |
| 5 | `notify/event` | Publisher | Notifier ($share/notify-pool) | `NotifyEvent` | Notification request |
| 6 | `notify/result` | Notifier | Publisher | `NotifyResult` | Delivery confirmation |
| 7 | `sign/request` | TM | Wallet ($share/wallet-pool) | `SignRequest` | Payment signing |
| 8 | `sign/response` | Wallet | TM | `SignResponse` | Signed transaction |
| 9 | `heartbeat/{tmId}` | TM | JM | `Heartbeat` | Liveness signal |
| 10 | `ctrl/flow/updated` | API Server | Controller ($share/ctrl-pool) | `FlowEvent` | Flow lifecycle |
| 11 | `ctrl/flow/deleted` | API Server | Controller ($share/ctrl-pool) | `FlowEvent` | Flow deletion |
| 12 | `ctrl/run/created` | API Server | Controller ($share/ctrl-pool) | `RunEvent` | Run creation |
| 13 | `ctrl/run/status` | API Server | Controller ($share/ctrl-pool) | `RunEvent` | Run status change |
| 14 | `notify/pod/{podId}/ws/{wsId}` | Notifier | Notifier | `WSMessage` | Cross-pod WS routing |

**Skill/Sandbox E2E Chain** (Topic 1→3→4→2):
```
JM → flowgent/v1/{tenant}/flows/{flowId}/runs/{runId}/exec/plans → TM
                   ├→ flowgent/v1/{tenant}/flows/{flowId}/runs/{runId}/sandbox/trigger → Sandbox
                   │                      ├→ seccomp execution
                   │                      └→ flowgent/v1/{tenant}/flows/{flowId}/runs/{runId}/sandbox/result → TM
                   ├→ PUT /tasks (persist output via REST)
                   └→ flowgent/v1/{tenant}/flows/{flowId}/runs/{runId}/exec/results (state only) → JM
```

**Command**: `python3 runner.py -s 07`

**Note**: This scenario self-publishes and self-subscribes to verify EMQX routing. It does NOT test that real JM/TM/Sandbox components publish to the correct topics — that requires a running flow (validated in scenario 11).

---

### 08 — Notifier Module (L9)

**Purpose**: Validate EMQX broker connectivity and notification topic subscription

**Checks**:
- ✅ EMQX dashboard API reachable
- ✅ MQTT client can connect and subscribe to `$share/notify-pool/flowgent/v1/+/flows/+/runs/+/notify/event`
- ✅ Messages received on the notification topic (opportunistic)

**Command**: `python3 runner.py -s 08`

**Note**: This scenario has a lightweight implementation. It verifies EMQX connectivity and topic subscription but does not trigger actual notification delivery. Full multi-channel delivery tests (Telegram, DingTalk, Slack, Email, Webhook, WebSocket SSE) require external service credentials and are validated manually or in the E2E capstone (scenario 11, which reaches the notify phase).

---

### 09 — Wallet Module (L10)

**Purpose**: Validate wallet key management and async payment signing.

The wallet service is intentionally a **signing boundary**, not an x402 runtime. TaskManager-side code parses HTTP 402 responses, builds unsigned payment payloads, evaluates policy, and calls the facilitator. Wallet only receives `SignRequest` messages, signs the opaque payload with the EOA private key from the configured secret store, and returns `SignResponse`.

**Command**: `python3 runner.py -s 09`

**Coverage**:
- `GET /health`
- `POST /api/v1/wallet/keys`, `GET /api/v1/wallet/keys`, `GET /api/v1/wallet/keys/{name}`, `DELETE /api/v1/wallet/keys/{name}`
- `POST /api/v1/wallet/sign`
- MQTT `sign/request` → `sign/response` using the real `InterMessage` envelope
- Boundary assertion: `sign/response` contains signature/error only, not x402 intent/policy/facilitator fields

---

### 10 — OTEL Tracing (L11)

**Purpose**: Validate complete distributed trace coverage for all 24 nodes

**Trace Requirements**:
- ✅ Every node execution produces at least 7 spans:
  1. `JM-DispatchPlan` (JobManager publishes ExecutionPlan)
  2. `TM-ConsumeExecPlan` (TaskManager consumes plan)
  3. `SlotWorker-Execute{Type}` (Agent/Tool/Sandbox/etc execution)
  4. Type-specific spans (LLM-Call, MCP-Call, Sandbox-Execute, etc)
  5. `API-PUT-/tasks` (TaskManager persists output)
  6. `MQTT-Publish-ExecResult` (TaskManager publishes state)
  7. `JM-ReceiveExecResult` (JobManager receives state)

- ✅ Parent-child span relationships correct
- ✅ All tags present (`flowgent.node_id`, `flowgent.task_type`, etc)
- ✅ Input/output data logged in span attributes

**Command**: `python3 runner.py -s 10`

**Validation Matrix**:

See complete 24-node span matrix in `scenarios/10_otel_verifier.py`

**Jaeger UI Visualization**:
- Access: `http://localhost:16686`
- Search: Service=`flowgent`, Tags=`flowgent.run_id={run_id}`
- Expected: Single trace with ~160-260 spans (24 nodes × ~7-10 spans each + MQTT/DB overhead)
- Gantt chart should show 11 distinct phases
- Parallel review nodes (review-security/quality/arch) should overlap in timeline

---

### 11 — Security Fixer E2E Capstone (L12)

**Purpose**: Capstone white-box run of the canonical `security-autonomy-fixer` flow (24 nodes).
Run **after** module scenarios `04-10`. It imports the real YAML, triggers a run, and checks
PG persistence + REST task APIs + opportunistic MQTT — it does **not** replace per-module
deep assertions in Controller/Engine/Messager/OTEL verifiers.

**Prerequisite — agent/MCP registration (handled automatically by the script)**:
`type: agent` nodes resolve against apiserver-registered `AgentInfo` (via
`client.GetAgent`, see `pkg/core/pkg/engine/executor/agent.go`) and `type: tool`
nodes resolve against MCP servers the TaskManager loaded from
`GET /api/v1/{tenant}/mcp` at startup (see `NewTaskManager` in
`pkg/core/pkg/engine/taskmanager/taskmanager.go`) — **not** directly from
`config/agents/*.yaml` / `config/mcps/README.md`, which are just documentation.

**MCP transport: HTTP-only (Streamable HTTP).** TM pods are backend K8s agents
with no human interaction — all MCP servers are accessed via HTTP, not stdio
subprocess. The `McpInfo` entity stores a `url` + optional `headers`, not a
command vector. See `docs/01-L1-Engine-Architecture.md` §6.2 for design rationale.

`seed_agents_and_mcps()` in `11_e2e_security_fixer.py` (and `10_otel_verifier.py`,
which triggers the same flow) POSTs every `config/agents/*.yaml` and registers
two MCP servers:

| MCP Server | URL | Type | Notes |
|------------|-----|------|-------|
| **sonarqube** | `http://172.29.235.101:18080/mcp` | `http` | Built with mcpfather (385 tools); Docker container on `:18080` → `:8080`; see `~/sonarqube-mcp/README.md` |
| **github** | `https://api.githubcopilot.com/mcp/` | `http` | Official GitHub MCP server (remote Streamable HTTP); header `Authorization: Bearer ${GITHUB_TOKEN}`; see `config/mcps/README.md` |

Both servers run as **independent services** — no MCP binaries are bundled into
the flowgent container image. The TM connects lazily via `McpManager.GetClient()`
→ `NewStreamableHttpClient(url, WithHTTPHeaders(headers))` on first tool call.
This requires real `GITHUB_TOKEN`/`SONARQUBE_TOKEN` credentials and a reachable
SonarQube server. LLM calls still require a real provider registered via
`POST /api/v1/{tenant}/llm/providers` (e.g. DeepSeek, matching
`config/agents/*.yaml`'s `model: deepseek/...`) — there is no LLM mock, so
`generate-fixes`/review/etc. nodes will legitimately FAIL without one; this is
tolerated (see step 9 below).

**Flow Phases** (11 total):
1. **DISCOVERY** (2 nodes) — `get-commit`, `scan-sonarqube`
2. **ANALYZE** (1 node) — `aggregate-issues`
3. **FIX** (1 node) — `generate-fixes`
4. **REVIEW** (3 nodes) — `review-security`, `review-quality`, `review-arch`
5. **VOTE** (1 node) — `tribunal`
6. **SUPERVISOR** (1 node) — `supervisor-check`
7. **CONDITION** (1 node) — `is-approved`
8. **HUMAN** (1 node) — `human-approval`
9. **COMMIT+PR** (3 nodes) — `create-branch`, `commit-fixes`, `create-pr`
10. **RE-SCAN** (5 nodes) — `trigger-rescan`, `wait-rescan`, `check-resolved`, `compare-results`, `fix-complete`
11. **REPORT** (5 nodes) — `summary-report`, `notify-pr`, `notify-email`, `notify-teams`, `end`

**Command**: `python3 runner.py -s 11`


---

## Troubleshooting

### Common Issues

| Symptom | Likely Cause | Resolution |
|---------|-------------|------------|
| `Connection refused :9999` | API Server not running | `kubectl logs deploy/flowgent-apiserver` |
| `MQTT publish timeout` | EMQX pod not ready | `kubectl get pods \| grep emqx` |
| `PG connection failed` | PostgreSQL credentials wrong | Check `config.py` PG_* vars |
| `ImagePullBackOff` | flowgent-core image missing | Re-run `docker save \| k3s ctr images import` |
| `JM Deployment not created` | Controller not running | Check Application mode enabled |
| `Jaeger trace not found` | OTEL not configured | Verify `OTEL_EXPORTER_OTLP_ENDPOINT` env |
| `Human approval timeout` | WebSocket not connected | Check notifier pod logs |

### Log Collection

```bash
# API Server logs
kubectl logs deploy/flowgent-apiserver -n default --tail=100

# JobManager logs (find pod first)
JM_POD=$(kubectl get pods -l app=flowgent-jobmanager -o name | head -1)
kubectl logs $JM_POD --tail=100

# TaskManager logs
kubectl logs deploy/flowgent-taskmanager -n default --tail=100

# EMQX dashboard
open http://localhost:18083  # admin / public

# Jaeger UI
open http://localhost:16686

# PostgreSQL query
kubectl exec -it deploy/flowgent-postgres -- psql -U flowgent -d flowgent -c "SELECT * FROM orh_flowrun ORDER BY created_at DESC LIMIT 5;"
```

---

## Performance Benchmarks

Expected execution times (K3s single-node, 4 CPU, 8GB RAM):

| Scenario | Duration | Bottleneck |
|----------|----------|------------|
| 01 — Preflight | 10-15s | K8s API queries |
| 02 — API Server CRUD | 20-30s | DB writes |
| 03 — A2A Protocol | 5-10s | HTTP roundtrips |
| 04 — Controller | 40-60s | K8s Deployment creation |
| 05 — Engine DAG | 60-90s | Agent LLM calls |
| 06 — Basic Nodes | 15-25s | Agent execution |
| 07 — Messager Topics | 30-45s | MQTT roundtrips |
| 08 — Notifier | 5-10s | MQTT connect + subscribe |
| 09 — Wallet | 15-20s | Key generation + MQTT signing |
| 10 — OTEL Tracing | 300-600s | Full flow execution |
| 11 — Security Fixer E2E | 300-600s | LLM calls + SonarQube API |

**Total suite runtime**: ~15-20 minutes

---

## Verification Checklist

### Pre-Execution Requirements

**Infrastructure (Scenario 01)**:
- [ ] K3s cluster accessible, namespace `default` ready
- [ ] PostgreSQL 15+ available with Flowgent schema
- [ ] EMQX broker on port 1883
- [ ] Redis on port 6379
- [ ] Jaeger on port 16686

**Deployment (Scenario 01)**:
- [ ] API Server pod ready
- [ ] JobManager pod ready  
- [ ] TaskManager pod ready
- [ ] Controller pod ready

### Critical Architecture Verifications

**Table Naming (Scenario 02)**:
- [ ] `orh_agentflow` exists and writable
- [ ] `orh_flowrun` exists and writable
- [ ] `task_runs` exists and writable
- [ ] `llm_agent`, `llm_mcp`, `llm_providers` exist

**REST API Paths (Scenario 02)**:
- [ ] `/api/v1/{tenant}/flows` CRUD working
- [ ] `/api/v1/{tenant}/agents` CRUD working
- [ ] `/api/v1/{tenant}/mcp` CRUD working
- [ ] `/api/v1/{tenant}/llm/providers` CRUD working
- [ ] `/api/v1/{tenant}/runs` query working

**MQTT Communication (Scenario 07)**:
- [ ] JM publishes to `flowgent/v1/{tenant}/flows/{flowId}/runs/{runId}/exec/plans` (TM consumes)
- [ ] TM publishes to `flowgent/v1/{tenant}/flows/{flowId}/runs/{runId}/sandbox/trigger` (Sandbox consumes)
- [ ] Sandbox publishes to `flowgent/v1/{tenant}/flows/{flowId}/runs/{runId}/sandbox/result` (TM consumes)
- [ ] TM publishes to `flowgent/v1/{tenant}/flows/{flowId}/runs/{runId}/exec/results` (JM consumes)
- [ ] **State-only callback**: `exec/results` contains only `{plan_id, node_id, state}`, NO `output`/`stdout`
- [ ] **Subscribe-before-publish**: K8sRM registers `exec/results` callback BEFORE publishing to `exec/plans`
- [ ] **Per-node result routing**: each node gets its own `chan execResult`, routed by `er.NodeID`

**DAG Dependency Coordination (Scenario 05 + Architecture §3.4 + §7.1.1)**:
- [ ] **Topological iteration loop**: JM outer-loop repeatedly calls `Ready()` to discover newly-unblocked nodes
- [ ] **Blocking Schedule()**: `K8sRM.Schedule()` blocks on a per-node Go channel until TM publishes `exec/results`
- [ ] **Sequential sibling dispatch**: sibling nodes are dispatched one-at-a-time (blocking), not concurrently
- [ ] **Dependency chain validation**: A→B→C — B only becomes Ready() after A is Done(); C only after B is Done()
- [ ] **Conditional edge handling**: dormant conditional edges (`condition` value on edge) do NOT block target in `depsDone()`
- [ ] **False-branch skip**: after condition node executes, children on false-match branch are `Skip()`ped
- [ ] **Deadlock detection**: if `Ready()` returns empty but `IsComplete()` is false, JM breaks (nodes left PENDING)
- [ ] **Node failure propagation**: when a node fails, its children stay blocked → `HasFailed()` → flow terminates

**Data Persistence (Scenario 02 + 07)**:
- [ ] TM calls `PUT /api/v1/{tenant}/runs/{run_id}/tasks/{task_id}` to persist output data
- [ ] API Server receives and saves to `task_runs.output` column
- [ ] Frontend queries `/api/v1/{tenant}/runs/{run_id}/tasks/{task_id}` to display results

**Lifecycle Events (Scenario 02)**:
- [ ] API Server publishes `ctrl/flow/updated` on POST `/flows`
- [ ] API Server publishes `ctrl/flow/updated` on PUT `/flows/{id}`
- [ ] API Server publishes `ctrl/flow/deleted` on DELETE `/flows/{id}` (soft delete)
- [ ] **Only API Server** publishes `ctrl/*` events (TM/JM do not)

**Application Mode (Scenario 04)**:
- [ ] Controller creates K8s Deployment for each tenant agentflow
- [ ] Deployment named `flowgent-jobmanager-{tenant}-{flow_id}` with labels `app=flowgent-jobmanager` and `flowgent.io/flow={flow_id}`
- [ ] Each pod has dedicated JM instance (1 replica)
- [ ] No Session mode resources created

**OTEL Tracing (Scenario 10)**:
- [ ] All 24 nodes generate spans
- [ ] Each node span contains minimum 7 child spans
- [ ] Spans have `input`, `output`, `state` attributes
- [ ] Jaeger UI shows complete trace tree

**Wallet Signing (Scenario 09)**:
- [ ] Wallet HTTP `/health` and key management endpoints work when wallet is enabled
- [ ] Wallet consumes `sign/request` and publishes `sign/response`
- [ ] Wallet signs opaque unsigned payloads only
- [ ] Wallet response does not contain x402 parsing, policy, facilitator, or PaymentIntent fields

### Scenario Execution Status

| ID | Scenario | Status | Notes |
|----|----------|--------|-------|
| 01 | Infrastructure | ⏸️ READY | Deployment checks |
| 02 | API Server | ⏸️ READY | CRUD + lifecycle events |
| 03 | A2A Protocol | ⏸️ READY | Agent card validation |
| 04 | Controller | ⏸️ READY | Application mode lifecycle |
| 05 | Engine | ⏸️ READY | DAG scheduling + voting |
| 06 | Basic Nodes | ⏸️ READY | Agent/tribunal/supervisor exec |
| 07 | Messager | ⏸️ READY | **State-only callback** |
| 08 | Notifier | ⏸️ READY | EMQX connectivity |
| 09 | Wallet | ⏸️ READY | x402 key mgmt + MQTT signing |
| 10 | OTEL | ⏸️ READY | 24-node span coverage |
| 11 | E2E Security Fixer | ⏸️ READY | Capstone 24-node pipeline |

**Legend**: ⏸️ READY (not executed), ✅ PASSED, ❌ FAILED, ⚠️ PARTIAL

---

## Architecture Compliance Summary

### Key Design Decisions (v3.0)

**1. TM → JM Communication**:
- ✅ State-only callback: `{plan_id, node_id, state}`
- ✅ NO output data in MQTT message
- ✅ Implemented in scenario 07

**2. TM → API Server Communication**:
- ✅ Data persistence via REST API `PUT /tasks`
- ✅ Output stored in `task_runs.output` column
- ✅ Frontend queries via REST API

**3. Lifecycle Event Publisher**:
- ✅ Only API Server publishes `ctrl/*` events
- ✅ Events: `ctrl/flow/updated`, `ctrl/flow/deleted`, `ctrl/run/created`, `ctrl/run/status`
- ✅ TM and JM do NOT publish lifecycle events

**4. Application Mode Only**:
- ✅ Session mode removed from all scenarios, and from the Go code itself:
  `entities.PriorityLow/Medium/Grade` and `Priority.IsApplication()` were
  deleted; `Controller.createSessionRun` / the `dispatchFlow` mode branch
  were deleted — every flow now unconditionally dispatches through
  `createApplicationRun` / `ensureApplicationInfra`. The `Priority` field
  itself is kept (reserved) on `FlowInfo`/`FlowRunInfo` for a possible
  future Session-mode reintroduction; `PriorityHigh` ("high") is the only
  value the API currently accepts (`handler.normalizePriority`, `Create`/
  `Update` return 400 for anything else).
- ✅ Controller creates K8s Deployment per flow
- ✅ Each tenant gets dedicated JM pods
- ✅ Controller dispatches (creates a run) at most once per flow-definition
  version ("on-new-definition" trigger, `pkg/controller/pkg/controller.go`
  `shouldDispatch`) — fixed a critical bug where every reconcile tick (10s)
  unconditionally created a brand-new `FlowRun` for every flow that existed
  in the system, forever, because the only prior gate (`c.running[flowID]`)
  is cleared almost immediately after the fast, synchronous dispatch call
  returns. See `TestShouldDispatchOnNewDefinitionOnly` in
  `pkg/controller/pkg/controller_test.go`.
- ✅ `POST /flows/trigger` (Path A) now unconditionally routes runs to
  the flow's tenant JM namespace (`pkg/api/pkg/handler/flow_def.go`
  `FlowDefHandler.applicationNamespace`, mirroring the Controller's own
  `applicationNamespace`) — fixed a critical bug where Trigger always
  created `namespace=""` runs, so flows triggered via the canonical
  `/trigger` endpoint (used by scenarios 05, 06, 10, 11, and the canonical
  `security-autonomy-fixer` flow) would sit PENDING forever in this
  Application-mode-only environment. See `TestApplicationNamespace` in
  `pkg/api/pkg/handler/flow_def_test.go`.
- ✅ Fixed a related double-dash bug: `tenant.namespace_prefix` already
  includes its trailing separator (default `"flowgent-"`), but the
  namespace was previously computed as `prefix + "-" + tenantID`
  (`"flowgent--{tenantID}"`), which would never match between Trigger and
  the Controller even after the routing fix above.
- ✅ Namespace is derived per-**tenant** (`{prefix}{tenant_id}`), not
  per-flow, per docs §1.3 ("each tenant gets its own namespace") /
  §4.3 ("namespace={tenant}") — every flow belonging to the same tenant
  shares one namespace, with each flow's dedicated JM Deployment
  disambiguated by name (`flowgent-jobmanager-{tenantId}-{flowId}`) rather
  than by a separate namespace per flow.

**5. Table Naming**:
- ✅ `orh_*` prefix: orchestration entities (`orh_agentflow`, `orh_flowrun`)
- ✅ `llm_*` prefix: AI entities (`llm_agent`, `llm_mcp`, `llm_providers`)
- ✅ No `agent_flows` or `flow_runs` tables

**6. REST API Paths**:
- ✅ AgentFlow path: `/api/v1/{tenant}/flows`
- ✅ MCP path: `/api/v1/{tenant}/mcp`
- ✅ Provider path: `/api/v1/{tenant}/llm/providers`
- ✅ Flow/run/task state path: `/api/v1/{tenant}/runs/...`
- ✅ Skill: no REST endpoint (import/console only; verified in scenario 02)

**7. DAG Dependency Coordination** (see `docs/01-L1-Engine-Architecture.md` §3.4, §5.2, §7.1.1):
- ✅ **Outer iteration loop**: `Execute()` runs `for iteration := 1; ; iteration++` — each iteration discovers newly-ready nodes via `Ready()`
- ✅ **Blocking Schedule()**: `K8sRM.Schedule()` subscribes to `exec/results`, registers per-node `chan execResult`, publishes to `exec/plans`, then **blocks on the channel** — NOT fire-and-forget
- ✅ **Subscribe-before-publish**: channel registered BEFORE publish to avoid missing TM response
- ✅ **Per-node result routing**: `runResults[runID][nodeID] = chan execResult` → callback matches `er.NodeID` to route results
- ✅ **`depsDone()` with dormant conditional edges**: edges with a `condition` value are skipped in dependency check until source node evaluates
- ✅ **Sequential sibling dispatch**: siblings (B1,B2,B3 all depending on A) are dispatched one-at-a-time, each blocking until TM result
- ✅ **Deadlock break**: if `Ready()` returns empty but `IsComplete()` is false → loop breaks, nodes left PENDING

**8. Wallet Boundary**:
- ✅ TM-side x402 client parses HTTP 402 responses and builds unsigned payloads
- ✅ Wallet receives only `SignRequest` and returns only `SignResponse`
- ✅ Wallet never evaluates policy or calls the facilitator


---

## References

- **Architecture**: `/docs/01-L1-Engine-Architecture.md`
- **Economic Layer**: `/docs/02-L1-x402-Economic-Support.md`
- **Use Cases**: `/docs/10-L2-USE-CASES.md`
- **Flow Definition**: `/examples/security-autonomy-fixer/config/flows/security-autonomy-fixer.yaml`
- **Agent Definitions**: `/examples/security-autonomy-fixer/config/agents/*.yaml`
- **MCP Servers**: `/examples/security-autonomy-fixer/config/mcps/*/`
