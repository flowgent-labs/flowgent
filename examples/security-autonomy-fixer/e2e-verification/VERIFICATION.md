# E2E Verification — Security Autonomy Fixer

**Status**: 📋 **TEST PLAN** (Implementation Ready)  
**Version**: v3.1  
**Date**: 2026-07-05

This document describes the complete end-to-end verification strategy for the Security Autonomy Fixer use case, covering 11 phases, 24 nodes, and all inter-component communication paths.

**Note**: This is a comprehensive test plan. Scenario scripts are ready for execution but not yet run. All verification checkpoints are designed based on v3.0 architecture decisions.

---

## Quick Start

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
| AgentFlow | `orh_agentflow` | `/api/v1/{tenant}/agentflows` | CRUD, versioning, soft delete |
| FlowRun | `orh_flowrun` | `/api/v1/{tenant}/runs` | Trigger, status transitions |
| TaskRun | `task_runs` | `/api/v1/{tenant}/runs/{run_id}/tasks` | Nested CRUD, output persistence |
| Agent | `llm_agent` | `/api/v1/{tenant}/agents` | Name uniqueness |
| Skill | `llm_skill` | (no REST endpoint — import/console only) | Table existence verification |
| MCP | `llm_mcp` | `/api/v1/{tenant}/mcp` | Enable/disable |
| Provider | `llm_providers` | `/api/v1/{tenant}/llm/providers` | Multi-model config |
| Approval | `human_approvals` | `/api/v1/human/approvals` | Token-based access |
| Channel | `nfy_channel` | `/api/v1/{tenant}/notifications/channels` | Multi-channel config |

**Lifecycle Event Validation**:
- ✅ `POST /agentflows` → MQTT `flowgent/v1/{tenant}/flows/{id}/ctrl/flow/updated` (action=created)
- ✅ `PUT /agentflows/{id}` → MQTT `flowgent/v1/{tenant}/flows/{id}/ctrl/flow/updated` (action=updated, version++)
- ✅ `DELETE /agentflows/{id}` → MQTT `flowgent/v1/{tenant}/flows/{id}/ctrl/flow/deleted` (soft delete)
- ✅ `POST /runs` → MQTT `flowgent/v1/{tenant}/flows/{id}/runs/{rid}/ctrl/run/created`
- ✅ `PUT /runs/{id}` → MQTT `flowgent/v1/{tenant}/flows/{id}/runs/{rid}/ctrl/run/status` (status transitions)

**Command**: `python3 runner.py -s 02`

**Key Assertions**:
```python
# CREATE + PG persistence
resp = POST("/api/v1/default/agentflows", payload)
assert resp.status_code == 201
assert db.query("SELECT COUNT(*) FROM orh_agentflow WHERE id=$1", resp.id) == 1

# MQTT event published (full topic: flowgent/v1/{tenant}/flows/{id}/ctrl/flow/updated)
mqtt_msg = mqtt_client.wait_for_message("ctrl/flow/updated", timeout=3)
assert mqtt_msg["action"] == "created"
assert mqtt_msg["agentflow_id"] == resp.id

# UPDATE increments version
resp = PUT(f"/api/v1/default/agentflows/{flow_id}", {"description": "updated"})
assert resp.data["version"] == 2

# DELETE soft-deletes
DELETE(f"/api/v1/default/agentflows/{flow_id}")
assert db.query("SELECT del_flag FROM orh_agentflow WHERE id=$1", flow_id) == True
```

---

### 03 — A2A Protocol Module (L4)

**Purpose**: Validate A2A agent card discovery and task submission

**Checks**:
- ✅ `GET /.well-known/agent.json` returns valid agent card with skills
- ✅ `POST /a2a/tasks` submits a task and returns a task ID
- ✅ `GET /a2a/tasks` lists tasks (may not be supported)
- ✅ Graceful skip if A2A port is not reachable or not enabled

**Command**: `python3 runner.py -s 03`

**Key Assertions**:
```python
# Agent card discovery
r = GET(f"{A2A_URL}/.well-known/agent.json")
assert "skills" in r.json()

# Task submission (flow may not exist — non-critical)
r = POST(f"{A2A_URL}/a2a/tasks", {"agentflow_id": "vrf-a2a-02", "input": {"test": True}})
# 200 = success, 404 = flow not registered — both acceptable
```

**Note**: This scenario has a lightweight implementation. A2A is validated for connectivity; deep protocol compliance testing requires a registered flow.

---

### 04 — Controller Module (L5)

**Purpose**: Validate Application mode flow lifecycle management

**Scenarios**:
1. **Flow Created** → Controller creates JM Deployment
2. **Flow Updated** → Controller rolling updates JM pods
3. **Flow Deleted** → Controller garbage-collects JM/TM/Sandbox Deployments

**Command**: `python3 runner.py -s 04`

**Test Flow**:
```python
# 1. Create Flow via API (flat AgentFlowInfo shape — "id" not "agentflow_id",
#    see pkg/api/pkg/handler/flow_def.go Create)
flow_id = "test-flow-" + uuid4()
POST("/api/v1/default/agentflows", {"id": flow_id, "nodes": [...], "edges": [...]})

# 2. Verify JM Deployment created by Controller. IMPORTANT: it lands in the
#    flow's TENANT namespace ("flowgent-default" by default —
#    tenant.namespace_prefix + tenant_id, per docs §1.3/§4.3 — every flow of
#    the same tenant shares one namespace, disambiguated by Deployment name —
#    see controller.go applicationNamespace), NOT in the "default" (K8s)
#    namespace where the Controller/apiserver pods run.
jm_ns = f"flowgent-default"  # {namespace_prefix}{tenant_id}, tenant_id="default" here
k8s.wait_for_deployment(f"flowgent-jobmanager-default-{flow_id}", namespace=jm_ns, timeout=30)
deployment = k8s.get_deployment(f"flowgent-jobmanager-default-{flow_id}", namespace=jm_ns)
assert deployment.spec.replicas == 1
assert deployment.spec.template.spec.containers[0].env["FLOWGENT__RUNTIME__AGENT_FLOW_ID"] == flow_id

# 3. Verify JM Pod running
pods = k8s.get_pods(namespace=jm_ns, label_selector=f"app=flowgent-jobmanager,flowgent.io/flow={flow_id}")
assert len(pods) == 1
assert pods[0].status.phase == "Running"

# 4. Update flow → verify deployment generation increments
PUT(f"/api/v1/default/agentflows/{flow_id}", {"description": "updated", "version": 2})
after = k8s.get_deployment(f"flowgent-jobmanager-default-{flow_id}", namespace=jm_ns)
assert after.metadata.generation >= before.metadata.generation

# 5. Delete Flow → verify Controller garbage-collects resources (the
#    Deployment only — the tenant namespace itself is left behind since other
#    flows of the same tenant may still use it, see Environment Reset step 2b)
DELETE(f"/api/v1/default/agentflows/{flow_id}")
k8s.wait_for_deployment_deleted(f"flowgent-jobmanager-default-{flow_id}", namespace=jm_ns, timeout=60)
```

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

**DAG Topology Tests**:
```python
# 1. Linear chain (A → B → C)
flow = {"nodes": [noop("A"), noop("B"), noop("C")], "edges": [("A","B"), ("B","C")]}
run_id = trigger_flow(flow)
wait_for_completion(run_id)
assert get_task_sequence(run_id) == ["A", "B", "C"]

# 2. Parallel fan-out (A → [B1, B2, B3] → C)
flow = {
    "nodes": [noop("A"), noop("B1"), noop("B2"), noop("B3"), noop("C")],
    "edges": [("A","B1"), ("A","B2"), ("A","B3"), ("B1","C"), ("B2","C"), ("B3","C")]
}
run_id = trigger_flow(flow)
tasks = get_tasks(run_id)
assert {"A", "B1", "B2", "B3", "C"} == {t["node_id"] for t in tasks if t["status"] == "COMPLETED"}

# 3. Condition routing (A → cond → [B (true), C (false)])
flow = {
    "nodes": [noop("A", input={"score": 0.9}), condition("cond", expr="${A.score} > 0.8"),
              noop("B"), noop("C")],
    "edges": [("A","cond"), ("cond","B",True), ("cond","C",False)]
}
run_id = trigger_flow(flow)
tasks = get_tasks(run_id)
assert find_task("B", tasks)["status"] == "COMPLETED"
assert find_task("C", tasks) is None  # skipped

# 4. Tribunal voting (3 voters → tribunal → result)
flow = {
    "nodes": [
        noop("vote1", input={"decision": True}),
        noop("vote2", input={"decision": True}),
        noop("vote3", input={"decision": False}),
        tribunal("tribunal", strategy="majority"),
        noop("result")
    ],
    "edges": [("vote1","tribunal"), ("vote2","tribunal"), ("vote3","tribunal"), ("tribunal","result")]
}
run_id = trigger_flow(flow)
vote_task = find_task("tribunal", get_tasks(run_id))
assert vote_task["output"]["decision"] == True
```

**Tribunal Strategy Tests**:
```python
# Majority (2/3)
assert evaluate_tribunal([True, True, False], "majority") == True
assert evaluate_tribunal([True, False, False], "majority") == False

# Unanimous (3/3)
assert evaluate_tribunal([True, True, True], "unanimous") == True
assert evaluate_tribunal([True, True, False], "unanimous") == False

# Veto (any False → reject)
assert evaluate_tribunal([True, True, False], "veto") == False
assert evaluate_tribunal([True, True, True], "veto") == True

# Weighted ([0.5, 0.3, 0.2], threshold > 0.5)
assert evaluate_tribunal([True, False, True], "weighted", weights=[0.5,0.3,0.2]) == True  # 0.7 > 0.5
assert evaluate_tribunal([False, True, True], "weighted", weights=[0.5,0.3,0.2]) == False # 0.5 ≯ 0.5
```

**Command**: `python3 runner.py -s 05`

---

### 06 — Basic Nodes Module (L7)

**Purpose**: Validate simple DAG execution with agent, tribunal, and supervisor node types

**Covered Node Types**:
- `agent` — LLM agent execution (via `issue-detector`)
- `tribunal` — Majority vote aggregation
- `supervisor` — Safety gate with retry/injection/abort constraints

**Command**: `python3 runner.py -s 06`

**Test Flow** (4 nodes):
```python
flow = {
    "nodes": [
        {"id": "start", "type": "agent", "agent": "issue-detector"},
        {"id": "vote", "type": "tribunal", "strategy": {"type": "majority"}},
        {"id": "supervisor", "type": "supervisor", "agent": "supervisor",
         "supervisor_config": {"max_retries": 2, "max_nodes": 10, "max_injections": 2}},
        {"id": "end", "type": "noop"}
    ],
    "edges": [("start","vote"), ("vote","supervisor"), ("supervisor","end")]
}
run_id = trigger_flow(flow)
wait_for_completion(run_id)
tasks = get_tasks(run_id)
assert len(tasks) == 4
```

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

**Key Assertions**:
```python
# Topic 1-2: JM → TM → JM (state callback)
plan = ExecutionPlan(task_type="agent", node_id="test-node")
mqtt.publish("flowgent/v1/{tenant}/flows/{flowId}/runs/{runId}/exec/plans", wrap_envelope(plan))
result = mqtt.wait_for("flowgent/v1/{tenant}/flows/{flowId}/runs/{runId}/exec/results", timeout=5)
assert result["node_id"] == "test-node"
assert result["state"] in ["COMPLETED", "FAILED"]

# Topic 3-4: TM → Sandbox → TM (full chain)
sandbox_req = {"plan_id": "p1", "runtime": "python3", "script": "print('ok')"}
mqtt.publish("flowgent/v1/{tenant}/flows/{flowId}/runs/{runId}/sandbox/trigger", wrap_envelope(sandbox_req))
sandbox_res = mqtt.wait_for("flowgent/v1/{tenant}/flows/{flowId}/runs/{runId}/sandbox/result", timeout=10)
assert sandbox_res["exit_code"] == 0
assert "ok" in sandbox_res["stdout"]

# Verify state-only callback (no output data in exec/results)
received = unwrap_envelope(msg)
if "output" in received or "stdout" in received:
    print("FAIL: exec/results contains data (expected state-only)")
    return False
```

**Note**: This scenario self-publishes and self-subscribes to verify EMQX routing. It does NOT test that real JM/TM/Sandbox components publish to the correct topics — that requires a running flow (validated in scenario 11).

---

### 08 — Notifier Module (L9)

**Purpose**: Validate EMQX broker connectivity and notification topic subscription

**Checks**:
- ✅ EMQX dashboard API reachable
- ✅ MQTT client can connect and subscribe to `$share/notify-pool/flowgent/v1/+/flows/+/runs/+/notify/event`
- ✅ Messages received on the notification topic (opportunistic)

**Command**: `python3 runner.py -s 08`

**Key Assertions**:
```python
# EMQX status check
r = GET(f"http://{EMQX_HOST}:{EMQX_DASHBOARD}/api/v5/status")
assert r.json()["status"] == "running"

# Subscribe to notification wildcard topic
client.subscribe("$share/notify-pool/flowgent/v1/+/flows/+/runs/+/notify/event")
# Wait briefly for any notification traffic
time.sleep(3)
# Report messages captured
```

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

**Key Assertions**:
```python
sign_req = {
    "request_id": req_id,
    "wallet": wallet_name,
    "payload": "unsigned-x402-payment-payload",
    "tenant_id": tenant,
    "flow_id": flow_id,
    "run_id": run_id,
}
mqtt.publish("flowgent/v1/{tenant}/flows/{flow}/runs/{run}/sign/request", wrap_envelope(sign_req))
resp = wait_for("flowgent/v1/{tenant}/flows/{flow}/runs/{run}/sign/response")
assert len(resp["signature"]) == 128  # Ed25519 hex signature
assert not any(k in resp for k in ["intent", "policy", "facilitator", "amount", "asset"])
```

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

**Key Assertions**:
```python
# 1. Trigger complete Security Fixer flow
run_id = POST("/api/v1/default/agentflows/trigger", {"agentflow_id": "security-autonomy-fixer"})
wait_for_completion(run_id, timeout=600)

# 2. Query Jaeger for trace
traces = jaeger.find_traces(tags={"flowgent.run_id": run_id})
assert len(traces) == 1
trace = traces[0]

# 3. Verify all 24 nodes have complete span coverage
all_nodes = [node for nodes in FLOW_PHASES.values() for node in nodes]
for node_id in all_nodes:
    node_spans = [s for s in trace.spans if s.tags.get("flowgent.node_id") == node_id]
    assert len(node_spans) >= 7, f"Node {node_id} missing spans"
    
    # Verify required spans exist
    assert find_span(node_spans, "JM-DispatchPlan")
    assert find_span(node_spans, "TM-ConsumeExecPlan")
    assert find_span(node_spans, "SlotWorker-Execute*")
    assert find_span(node_spans, "API-PUT-/tasks")
    assert find_span(node_spans, "MQTT-Publish-ExecResult")
    assert find_span(node_spans, "JM-ReceiveExecResult")
    
    # Verify parent-child relationships
    dispatch_span = find_span(node_spans, "JM-DispatchPlan")
    consume_span = find_span(node_spans, "TM-ConsumeExecPlan")
    assert consume_span.parent_span_id == dispatch_span.span_id

# 4. Verify phase-level grouping
phase_spans = {
    "DISCOVERY": ["get-commit", "scan-sonarqube"],
    "ANALYZE": ["aggregate-issues"],
    "FIX": ["generate-fixes"],
    "REVIEW": ["review-security", "review-quality", "review-arch"],
    "VOTE": ["tribunal"],
    "SUPERVISOR": ["supervisor-check"],
    "CONDITION": ["is-approved"],
    "HUMAN": ["human-approval"],
    "COMMIT_PR": ["create-branch", "commit-fixes", "create-pr"],
    "RESCAN": ["trigger-rescan", "wait-rescan", "check-resolved", "compare-results", "fix-complete"],
    "REPORT": ["summary-report", "notify-pr", "notify-email", "notify-teams", "end"],
}

for phase_name, node_ids in phase_spans.items():
    phase_start = min(find_span(trace, node_id).start_time for node_id in node_ids)
    phase_end = max(find_span(trace, node_id).end_time for node_id in node_ids)
    phase_duration = phase_end - phase_start
    print(f"Phase {phase_name}: {phase_duration.total_seconds():.1f}s")

# 5. Verify critical path (longest dependency chain through the DAG)
critical_path = [
    "get-commit", "scan-sonarqube", "aggregate-issues", "generate-fixes",
    "review-security", "tribunal", "supervisor-check", "is-approved",
    "human-approval", "create-branch", "commit-fixes", "create-pr",
    "trigger-rescan", "wait-rescan", "check-resolved", "compare-results",
    "fix-complete", "summary-report", "notify-pr", "end"
]
```

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
`config/agents/*.yaml` / `config/mcps/*/*.yaml`, which are just templates.
`seed_agents_and_mcps()` in `11_e2e_security_fixer.py` (and `10_otel_verifier.py`,
which triggers the same flow) POSTs every `config/agents/*.yaml` and, by
default, registers the production `github`/`sonarqube` MCPs from
`config/mcps/{github,sonarqube}/*.yaml` — matching how this flow actually runs
in production. This **requires** the real `github-mcp`/`sonarqube-mcp`
binaries to be present in the JM/TM container image, plus real
`GITHUB_TOKEN`/`SONARQUBE_TOKEN` credentials and a reachable SonarQube server
(see those YAML files' `env:` blocks) — without these the tool nodes register
fine but FAIL at call time. Set `FLOWGENT_E2E_USE_REAL_MCP=false` to instead
point both MCPs at the mock stdio JSON-RPC server baked into the image
(`deploy/docker/Dockerfile.core` copies `config/mcps/mock-server.sh` to
`/app/mcp-server.sh`) for a credential-free smoke run. LLM calls still
require a real provider registered via `POST /api/v1/{tenant}/llm/providers`
(e.g. DeepSeek, matching `config/agents/*.yaml`'s `model: deepseek/...`) — there
is no LLM mock, so `generate-fixes`/review/etc. nodes will legitimately FAIL
without one; this is tolerated (see step 9 below).

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

**Validation Checkpoints**:

```python
# Phase 1: DISCOVERY
run_id = trigger_security_fixer()

# Verify get-commit output
commit_task = wait_for_task(run_id, "get-commit")
assert commit_task["status"] == "COMPLETED"
assert "commit_sha" in commit_task["output"]

# Verify scan-sonarqube issues fetched
scan_task = wait_for_task(run_id, "scan-sonarqube")
assert scan_task["status"] == "COMPLETED"
assert len(scan_task["output"]["issues"]) > 0

# Phase 2: ANALYZE
analyze_task = wait_for_task(run_id, "aggregate-issues")
assert analyze_task["output"]["issues"] is list
assert len(analyze_task["output"]["issues"]) <= 10  # top 10 prioritized

# Phase 3: FIX
fix_task = wait_for_task(run_id, "generate-fixes")
assert "patches" in fix_task["output"]
assert len(fix_task["output"]["patches"]) > 0

# Phase 4: REVIEW (parallel)
review_tasks = [
    wait_for_task(run_id, "review-security"),
    wait_for_task(run_id, "review-quality"),
    wait_for_task(run_id, "review-arch")
]
for task in review_tasks:
    assert task["status"] == "COMPLETED"
    assert "decision" in task["output"]
    assert "confidence" in task["output"]

# Phase 5: VOTE
vote_task = wait_for_task(run_id, "tribunal")
assert vote_task["output"]["decision"] in [True, False]
assert vote_task["output"]["vote_count"]["total"] == 3

# Phase 6: SUPERVISOR
supervisor_task = wait_for_task(run_id, "supervisor-check")
assert supervisor_task["status"] == "COMPLETED"
# Verify supervisor_log entry created
supervisor_logs = db.query("SELECT * FROM supervisor_log WHERE agentflow_run_id=$1", run_id)
assert len(supervisor_logs) >= 1

# Phase 7: CONDITION
condition_task = wait_for_task(run_id, "is-approved")
approved = condition_task["output"]["result"]

if approved:
    # Phase 8: HUMAN (mock approval)
    approval_task = wait_for_task(run_id, "human-approval", timeout=5)
    approval_token = db.query("SELECT token FROM human_approvals WHERE task_run_id=$1", approval_task["id"])
    POST(f"/api/v1/human/{approval_token}/approve", {"comment": "Approved by test"})
    
    # Verify flow resumed
    wait_for_task_status(run_id, "human-approval", "COMPLETED", timeout=10)
    
    # Phase 9: COMMIT+PR
    branch_task = wait_for_task(run_id, "create-branch")
    assert "branch_name" in branch_task["output"]
    
    commit_task = wait_for_task(run_id, "commit-fixes")
    assert commit_task["status"] == "COMPLETED"
    
    pr_task = wait_for_task(run_id, "create-pr")
    assert "pr_url" in pr_task["output"]
    assert "pr_number" in pr_task["output"]
    
    # Phase 10: RE-SCAN (5 nodes)
    rescan_task = wait_for_task(run_id, "trigger-rescan")
    wait_task = wait_for_task(run_id, "wait-rescan", timeout=120)  # polls for completion
    check_task = wait_for_task(run_id, "check-resolved")
    compare_task = wait_for_task(run_id, "compare-results")
    fix_complete_task = wait_for_task(run_id, "fix-complete")
    
    assert compare_task["output"]["resolution"] in ["complete", "partial", "max_iterations_reached"]
    assert compare_task["output"]["resolved_count"] >= 0
    
    # Phase 11: REPORT (5 nodes)
    report_task = wait_for_task(run_id, "summary-report")
    assert "report" in report_task["output"]
    assert "status" in report_task["output"]
    
    # notify-pr, notify-email, notify-teams run in parallel from summary-report
    notify_pr_task = wait_for_task(run_id, "notify-pr")
    notify_email_task = wait_for_task(run_id, "notify-email")
    notify_teams_task = wait_for_task(run_id, "notify-teams")
    
    for task in [notify_pr_task, notify_email_task, notify_teams_task]:
        assert task["status"] in ["COMPLETED", "FAILED"]  # delivery may fail in test env

# Final assertions
run = GET(f"/api/v1/default/runs/{run_id}")
assert run["status"] == "COMPLETED"
assert run["finished_at"] is not None

# Verify execution coverage
tasks = GET(f"/api/v1/default/runs/{run_id}/tasks")
executed_nodes = [t["node_id"] for t in tasks if t["status"] in ["COMPLETED", "FAILED"]]
assert len(executed_nodes) >= 15  # minimum nodes even if loop short-circuits
```

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

## CI/CD Integration

### GitHub Actions Example

```yaml
name: E2E Verification
on: [push, pull_request]

jobs:
  e2e:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Setup K3s
        run: |
          curl -sfL https://get.k3s.io | sh -
          sudo k3s kubectl wait --for=condition=Ready node --all --timeout=60s
      
      - name: Deploy Flowgent
        run: |
          make build-image-core
          docker save flowgent-core:latest | sudo k3s ctr images import -
          helm install flowgent deploy/helm/flowgent -n default
          kubectl wait --for=condition=Ready pod -l app=flowgent-apiserver --timeout=60s
      
      - name: Run E2E Tests
        run: |
          cd examples/security-autonomy-fixer/e2e-verification
          python3 runner.py
      
      - name: Collect Logs
        if: failure()
        run: |
          kubectl logs deploy/flowgent-apiserver > apiserver.log
          kubectl logs deploy/flowgent-taskmanager > taskmanager.log
      
      - uses: actions/upload-artifact@v3
        if: failure()
        with:
          name: logs
          path: "*.log"
```

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
- [ ] `/api/v1/{tenant}/agentflows` CRUD working
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

**Data Persistence (Scenario 02 + 07)**:
- [ ] TM calls `PUT /api/v1/{tenant}/runs/{run_id}/tasks/{task_id}` to persist output data
- [ ] API Server receives and saves to `task_runs.output` column
- [ ] Frontend queries `/api/v1/{tenant}/runs/{run_id}/tasks/{task_id}` to display results

**Lifecycle Events (Scenario 02)**:
- [ ] API Server publishes `ctrl/flow/updated` on POST `/agentflows`
- [ ] API Server publishes `ctrl/flow/updated` on PUT `/agentflows/{id}`
- [ ] API Server publishes `ctrl/flow/deleted` on DELETE `/agentflows/{id}` (soft delete)
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
  itself is kept (reserved) on `AgentFlowInfo`/`FlowRunInfo` for a possible
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
- ✅ `POST /agentflows/trigger` (Path A) now unconditionally routes runs to
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
- ✅ AgentFlow path: `/api/v1/{tenant}/agentflows`
- ✅ MCP path: `/api/v1/{tenant}/mcp`
- ✅ Provider path: `/api/v1/{tenant}/llm/providers`
- ✅ Flow/run/task state path: `/api/v1/{tenant}/runs/...`
- ✅ Skill: no REST endpoint (import/console only; verified in scenario 02)

**7. Wallet Boundary**:
- ✅ TM-side x402 client parses HTTP 402 responses and builds unsigned payloads
- ✅ Wallet receives only `SignRequest` and returns only `SignResponse`
- ✅ Wallet never evaluates policy or calls the facilitator

### Verification Code Highlights

**State-Only Callback** (`07_messager_verifier.py`):
```python
# Step 9: TM → JM state-only callback (full topic: flowgent/v1/{tenant}/flows/{flowId}/runs/{runId}/exec/results)
exec_result = {
    "plan_id": plan_id,
    "node_id": "sandbox-node",
    "state": "COMPLETED",  # State only, no data
}
mqtt.publish("flowgent/v1/{tenant}/flows/{flowId}/runs/{runId}/exec/results", wrap_envelope(exec_result))

# Step 10: Verification
received = unwrap_envelope(msg.payload)
if "output" in received or "stdout" in received:
    print("FAIL: exec/results contains data (expected state-only)")
    return False
```

**Table Names** (`02_apiserver_verifier.py`):
```python
tables = [
    {"table": "orh_agentflow", "base_path": f"/api/v1/{TENANT}/agentflows"},
    {"table": "orh_flowrun",   "base_path": f"/api/v1/{TENANT}/runs"},
    {"table": "llm_agent",     "base_path": f"/api/v1/{TENANT}/agents"},
    {"table": "llm_mcp",       "base_path": f"/api/v1/{TENANT}/mcp"},
    {"table": "llm_providers", "base_path": f"/api/v1/{TENANT}/llm/providers"},
]
```

**OTEL Span Matrix** (`10_otel_verifier.py`):
```python
FLOW_PHASES = {
    "DISCOVERY": ["get-commit", "scan-sonarqube"],
    "ANALYZE": ["aggregate-issues"],
    "FIX": ["generate-fixes"],
    "REVIEW": ["review-security", "review-quality", "review-arch"],
    "VOTE": ["tribunal"],
    "SUPERVISOR": ["supervisor-check"],
    "CONDITION": ["is-approved"],
    "HUMAN": ["human-approval"],
    "COMMIT_PR": ["create-branch", "commit-fixes", "create-pr"],
    "RESCAN": ["trigger-rescan", "wait-rescan", "check-resolved", "compare-results", "fix-complete"],
    "REPORT": ["summary-report", "notify-pr", "notify-email", "notify-teams", "end"],
}
```

**Wallet Signing Boundary** (`09_wallet_verifier.py`):
```python
resp = wait_for("flowgent/v1/{tenant}/flows/{flow}/runs/{run}/sign/response")
assert "signature" in resp
assert not any(k in resp for k in ["intent", "policy", "facilitator", "amount", "asset"])
```

---

## References

- **Architecture**: `/docs/01-L1-Engine-Architecture.md`
- **Economic Layer**: `/docs/02-L1-x402-Economic-Support.md`
- **Use Cases**: `/docs/10-L2-USE-CASES.md`
- **Flow Definition**: `/examples/security-autonomy-fixer/config/flows/security-autonomy-fixer.yaml`
- **Agent Definitions**: `/examples/security-autonomy-fixer/config/agents/*.yaml`
- **MCP Servers**: `/examples/security-autonomy-fixer/config/mcps/*/`
