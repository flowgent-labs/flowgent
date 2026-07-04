# E2E Verification — Security Autonomy Fixer

**Status**: 📋 **TEST PLAN** (Implementation Ready)  
**Version**: v3.0 Final  
**Date**: 2026-07-04

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
python3 runner.py -s 04  # Controller -> JM pod lifecycle
python3 runner.py -s 05  # Engine DAG scheduling
python3 runner.py -s 06  # Basic node executors
python3 runner.py -s 07  # Messager topics + Sandbox chain
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

**Scenario Files** (numbered by functional module execution order):
- `01_infra_verifier.py` — Infrastructure & deployment checks
- `02_apiserver_verifier.py` — API Server CRUD + lifecycle events
- `03_a2a_protocol_verifier.py` — A2A protocol validation
- `04_controller_verifier.py` — Controller Application mode lifecycle (JM pods)
- `05_engine_verifier.py` — Engine DAG scheduling + voting
- `06_basic_nodes_verifier.py` — Basic node execution (after engine dispatch)
- `07_messager_verifier.py` — MQTT topics + Sandbox chain
- `08_notifier_verifier.py` — Multi-channel notifier
- `09_wallet_verifier.py` — x402 wallet key management + MQTT signing
- `10_otel_verifier.py` — OTEL / Jaeger 24-node tracing
- `11_e2e_security_fixer.py` — **Capstone**: full security fixer pipeline white-box

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
- ✅ Helm release `flowgent` deployed (Application mode)
- ✅ All pods Running (no CrashLoopBackOff)
- ✅ API Server `/healthz` returns 200
- ✅ JM/TM logs clean (no ERROR/FATAL/PANIC)

**Command**: `python3 runner.py -s 01`

**Manual Redeploy** (if infrastructure broken):
```bash
# Clean up
kubectl delete pod --field-selector=status.phase=Failed -n default --force
helm uninstall flowgent -n default

# Rebuild and import image
make build-image-core
docker save flowgent-core:latest | sudo k3s ctr images import -

# Install (Application mode)
helm install flowgent deploy/helm/flowgent \
  --set global.mode=application \
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
| Skill | `llm_skill` | `/api/v1/{tenant}/skills` | Tool bindings |
| MCP | `llm_mcp` | `/api/v1/{tenant}/mcp` | Enable/disable |
| Provider | `llm_providers` | `/api/v1/{tenant}/llm/providers` | Multi-model config |
| Approval | `human_approvals` | `/api/v1/human/approvals` | Token-based access |
| Channel | `nfy_channel` | `/api/v1/{tenant}/notifications/channels` | Multi-channel config |

**Lifecycle Event Validation**:
- ✅ `POST /agentflows` → MQTT `ctrl/flow/updated` (action=created)
- ✅ `PUT /agentflows/{id}` → MQTT `ctrl/flow/updated` (action=updated, version++)
- ✅ `DELETE /agentflows/{id}` → MQTT `ctrl/flow/deleted` (soft delete)
- ✅ `POST /runs` → MQTT `ctrl/run/created`
- ✅ `PUT /runs/{id}` → MQTT `ctrl/run/status` (status transitions)

**Command**: `python3 runner.py -s 02`

**Key Assertions**:
```python
# CREATE + PG persistence
resp = POST("/api/v1/default/agentflows", payload)
assert resp.status_code == 201
assert db.query("SELECT COUNT(*) FROM orh_agentflow WHERE id=$1", resp.id) == 1

# MQTT event published
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

### 07 — Messager Module (L8)

**Purpose**: Validate MQTT topic connectivity and message routing

**Topic Matrix** (15 topics):

| # | Topic Pattern | Publisher | Subscriber | Payload | Test |
|---|--------------|-----------|------------|---------|------|
| 1 | `exec/plans` | JM | TM ($share/tm-pool) | `ExecutionPlan` | Request dispatch |
| 2 | `exec/results` | TM | JM | `{plan_id, node_id, state}` | Status callback |
| 3 | `sandbox/trigger` | TM | Sandbox ($share/sandbox-pool) | `{plan_id, runtime, script}` | Script execution |
| 4 | `sandbox/result` | Sandbox | TM | `{plan_id, exit_code, stdout}` | Execution result |
| 5 | `notify/event` | Publisher | Notifier ($share/notify-pool) | `NotifyEvent` | Notification request |
| 6 | `notify/result` | Notifier | Publisher | `NotifyResult` | Delivery confirmation |
| 7 | `sign/request` | TM | Wallet ($share/wallet-pool) | `SignRequest` | Payment signing |
| 8 | `sign/response` | Wallet | TM | `SignResponse` | Signed transaction |
| 9 | `heartbeat/{tmId}` | TM | JM | `Heartbeat` | Liveness signal |
| 10 | `ctrl/flow/updated` | API Server | Controller | `FlowEvent` | Flow lifecycle |
| 11 | `ctrl/flow/deleted` | API Server | Controller | `FlowEvent` | Flow deletion |
| 12 | `ctrl/run/created` | API Server | Controller | `RunEvent` | Run creation |
| 13 | `ctrl/run/status` | API Server | Controller | `RunEvent` | Run status change |
| 14 | `notify/pod/{podId}/ws/{wsId}` | Notifier | Notifier | `WSMessage` | Cross-pod WS routing |

**Skill/Sandbox E2E Chain** (Topic 1→3→4→2):
```
JM → exec/plans → TM
                   ├→ sandbox/trigger → Sandbox
                   │                      ├→ seccomp execution
                   │                      └→ sandbox/result → TM
                   ├→ PUT /tasks (persist output)
                   └→ exec/results (state only) → JM
```

**Command**: `python3 runner.py -s 07`

**Key Assertions**:
```python
# Topic 1-2: JM → TM → JM (state callback)
plan = ExecutionPlan(task_type="agent", node_id="test-node")
mqtt.publish("exec/plans", plan)
result = mqtt.wait_for("exec/results", timeout=5)
assert result["node_id"] == "test-node"
assert result["state"] in ["COMPLETED", "FAILED"]

# Topic 3-4: TM → Sandbox → TM (full chain)
sandbox_req = {"plan_id": "p1", "runtime": "python3", "script": "print('ok')"}
mqtt.publish("sandbox/trigger", sandbox_req)
sandbox_res = mqtt.wait_for("sandbox/result", timeout=10)
assert sandbox_res["exit_code"] == 0
assert "ok" in sandbox_res["stdout"]

# Verify TM persisted output via REST
task = GET(f"/api/v1/default/runs/{run_id}/tasks/{task_id}")
assert task["output"]["stdout"] == "ok\n"
```

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
# 1. Create Flow via API
flow_id = "test-flow-" + uuid4()
POST("/api/v1/default/agentflows", {"agentflow_id": flow_id, ...})

# 2. Verify Controller received ctrl/flow/updated
mqtt_msg = mqtt.wait_for(f"ctrl/flow/updated", timeout=3)
assert mqtt_msg["agentflow_id"] == flow_id

# 3. Verify JM Deployment created
k8s.wait_for_deployment(f"flowgent-jobmanager-default-{flow_id}", timeout=30)
deployment = k8s.get_deployment(f"flowgent-jobmanager-default-{flow_id}")
assert deployment.spec.replicas == 1
assert deployment.spec.template.spec.containers[0].env["FLOWGENT_FLOW_ID"] == flow_id

# 4. Verify JM Pod running
pods = k8s.get_pods(label_selector=f"app=flowgent-jobmanager,flow_id={flow_id}")
assert len(pods) == 1
assert pods[0].status.phase == "Running"

# 5. Delete Flow
DELETE(f"/api/v1/default/agentflows/{flow_id}")

# 6. Verify Controller garbage-collects resources
k8s.wait_for_deployment_deleted(f"flowgent-jobmanager-default-{flow_id}", timeout=30)
```

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
- (implicit) `join` — Fan-in aggregation

**DAG Topology Tests**:
```python
# 1. Linear chain (A → B → C)
flow = {"nodes": [agent("A"), tool("B"), skill("C")], "edges": [("A","B"), ("B","C")]}
run_id = trigger_flow(flow)
wait_for_completion(run_id)
assert get_task_sequence(run_id) == ["A", "B", "C"]

# 2. Parallel fan-out (A → [B1, B2, B3] → C)
flow = {
    "nodes": [agent("A"), skill("B1"), tool("B2"), sandbox("B3"), join("C")],
    "edges": [("A","B1"), ("A","B2"), ("A","B3"), ("B1","C"), ("B2","C"), ("B3","C")]
}
run_id = trigger_flow(flow)
# Verify B1/B2/B3 start within 2s of each other
tasks = get_tasks(run_id)
start_times = [t["started_at"] for t in tasks if t["node_id"] in ["B1","B2","B3"]]
assert max(start_times) - min(start_times) < timedelta(seconds=2)

# 3. Condition routing (A → cond → [B (true), C (false)])
flow = {
    "nodes": [agent("A"), condition("cond", expr="$.score>0.8"), tool("B"), noop("C")],
    "edges": [("A","cond"), ("cond","B",True), ("cond","C",False)]
}
# If A returns {"score": 0.9} → B executes, C skipped
run_id = trigger_flow(flow, input={"initial": "test"})
tasks = get_tasks(run_id)
assert find_task("B", tasks)["status"] == "COMPLETED"
assert find_task("C", tasks) is None  # skipped

# 4. Tribunal voting (3 agents → tribunal → result)
flow = {
    "nodes": [
        agent("reviewer1"), agent("reviewer2"), agent("reviewer3"),
        tribunal("vote", strategy="majority"),
        tool("action")
    ],
    "edges": [
        ("reviewer1","vote"), ("reviewer2","vote"), ("reviewer3","vote"),
        ("vote","action")
    ]
}
# Mock reviewers return [True, True, False] → majority=True
run_id = trigger_flow(flow)
vote_task = find_task("vote", get_tasks(run_id))
assert vote_task["output"]["decision"] == True
assert vote_task["output"]["vote_count"] == {"approve": 2, "reject": 1}
```

**Tribunal Strategy Tests**:
```python
# Majority (2/3)
assert evaluate_tribunal([True, True, False], "majority") == True
assert evaluate_tribunal([True, False, False], "majority") == False

# Unanimous (3/3)
assert evaluate_tribunal([True, True, True], "unanimous") == True
assert evaluate_tribunal([True, True, False], "unanimous") == False

# Veto (any False)
assert evaluate_tribunal([True, True, False], "veto") == False
assert evaluate_tribunal([True, True, True], "veto") == True

# Weighted ([0.5, 0.3, 0.2])
assert evaluate_tribunal([True, False, True], "weighted", weights=[0.5,0.3,0.2]) == True  # 0.7
assert evaluate_tribunal([False, True, True], "weighted", weights=[0.5,0.3,0.2]) == False  # 0.5
```

**Command**: `python3 runner.py -s 05`

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
for node_id in ["get-commit", "scan-sonarqube", "aggregate-issues", ...]:  # all 24
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
    # ... all 11 phases
}

for phase_name, node_ids in phase_spans.items():
    phase_start = min(find_span(trace, node_id).start_time for node_id in node_ids)
    phase_end = max(find_span(trace, node_id).end_time for node_id in node_ids)
    phase_duration = phase_end - phase_start
    print(f"Phase {phase_name}: {phase_duration.total_seconds():.1f}s")

# 5. Verify critical path (longest dependency chain)
critical_path = extract_critical_path(trace)
assert critical_path == [
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
- Expected: Single trace with ~180-260 spans (24 nodes × ~10 spans each + MQTT/DB overhead)
- Gantt chart should show 11 distinct phases
- Parallel review nodes (review-security/quality/arch) should overlap in timeline

---

### 11 — Security Fixer E2E Capstone (L12)

**Purpose**: Capstone white-box run of the canonical `security-autonomy-fixer` flow (24 nodes).
Run **after** module scenarios `04-10`. It imports the real YAML, triggers a run, and checks
PG persistence + REST task APIs + opportunistic MQTT — it does **not** replace per-module
deep assertions in Controller/Engine/Messager/OTEL verifiers.

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
10. **RE-SCAN** (4 nodes) — `trigger-rescan`, `wait-rescan`, `check-resolved`, `compare-results`, `fix-complete`
11. **REPORT** (4 nodes) — `summary-report`, `notify-pr`, `notify-email`, `notify-teams`, `end`

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
    POST(f"/api/v1/approvals/{approval_token}/approve", {"comment": "Approved by test"})
    
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
    
    # Phase 10: RE-SCAN
    rescan_task = wait_for_task(run_id, "trigger-rescan")
    wait_task = wait_for_task(run_id, "wait-rescan", timeout=120)  # polls for completion
    check_task = wait_for_task(run_id, "check-resolved")
    compare_task = wait_for_task(run_id, "compare-results")
    
    assert compare_task["output"]["resolution"] in ["complete", "partial", "max_iterations_reached"]
    assert compare_task["output"]["resolved_count"] >= 0
    
    # Phase 11: REPORT
    report_task = wait_for_task(run_id, "summary-report")
    assert "report" in report_task["output"]
    assert "status" in report_task["output"]
    
    notify_pr_task = wait_for_task(run_id, "notify-pr")
    notify_email_task = wait_for_task(run_id, "notify-email")
    notify_teams_task = wait_for_task(run_id, "notify-teams")
    
    for task in [notify_pr_task, notify_email_task, notify_teams_task]:
        assert task["status"] in ["COMPLETED", "FAILED"]  # delivery may fail in test env

# Final assertions
run = GET(f"/api/v1/default/runs/{run_id}")
assert run["status"] == "COMPLETED"
assert run["finished_at"] is not None

# Verify all 24 nodes executed (or skipped if condition=false)
tasks = GET(f"/api/v1/default/runs/{run_id}/tasks")
executed_nodes = [t["node_id"] for t in tasks if t["status"] in ["COMPLETED", "FAILED"]]
assert len(executed_nodes) >= 15  # minimum nodes even if loop short-circuits
```

---

### 09 — Wallet Module (L10)

**Purpose**: Validate x402 wallet key management and async payment signing.

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
| 01 — Infrastructure | 10-15s | K8s API queries |
| 02 — API Server CRUD | 20-30s | DB writes |
| 07 — Messager Topics | 30-45s | MQTT roundtrips |
| 04 — Controller | 40-60s | K8s Deployment creation |
| 05 — Engine DAG | 60-90s | Agent LLM calls |
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
          helm install flowgent deploy/helm/flowgent --set global.mode=application -n default
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
- [ ] JM publishes to `exec/plans` (TM consumes)
- [ ] TM publishes to `sandbox/trigger` (Sandbox consumes)
- [ ] Sandbox publishes to `sandbox/result` (TM consumes)
- [ ] TM publishes to `exec/results` (JM consumes)
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
- [ ] Deployment has label `app=flowgent-jm-{tenant}-{flow_id}`
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
| 06 | Basic Nodes | ⏸️ READY | Simple execution test |
| 07 | Messager | ⏸️ READY | **State-only callback** |
| 08 | Notifier | ⏸️ READY | Multi-channel delivery |
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
- ✅ Implemented in scenario 07 (Step 9-10)

**2. TM → API Server Communication**:
- ✅ Data persistence via REST API `PUT /tasks`
- ✅ Output stored in `task_runs.output` column
- ✅ Frontend queries via REST API

**3. Lifecycle Event Publisher**:
- ✅ Only API Server publishes `ctrl/*` events
- ✅ Events: `ctrl/flow/created`, `ctrl/flow/updated`, `ctrl/flow/deleted`
- ✅ TM and JM do NOT publish lifecycle events

**4. Application Mode Only**:
- ✅ Session mode removed from all scenarios
- ✅ Controller creates K8s Deployment per flow
- ✅ Each tenant gets dedicated JM pods

**5. Table Naming**:
- ✅ `orh_*` prefix: orchestration entities (`orh_agentflow`, `orh_flowrun`)
- ✅ `llm_*` prefix: AI entities (`llm_agent`, `llm_mcp`, `llm_providers`)
- ✅ No `agent_flows` or `flow_runs` tables

**6. REST API Paths**:
- ✅ AgentFlow path: `/api/v1/{tenant}/agentflows`
- ✅ MCP path: `/api/v1/{tenant}/mcp`
- ✅ Provider path: `/api/v1/{tenant}/llm/providers`
- ✅ Flow/run/task state path: `/api/v1/{tenant}/runs/...`

**7. Wallet Boundary**:
- ✅ TM-side x402 client parses HTTP 402 responses and builds unsigned payloads
- ✅ Wallet receives only `SignRequest` and returns only `SignResponse`
- ✅ Wallet never evaluates policy or calls the facilitator

### Verification Code Highlights

**State-Only Callback** (`07_messager_verifier.py`):
```python
# Step 9: TM → JM state-only callback
exec_result = {
    "plan_id": plan_id,
    "node_id": "sandbox-node",
    "state": "COMPLETED",  # State only, no data
}
mqtt.publish("exec/results", json.dumps(exec_result))

# Step 10: Verification
received = json.loads(msg.payload)
if "output" in received or "stdout" in received:
    print("⚠️ exec/results contains data (expected state-only)")
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
expected_spans = {
    "github-webhook": ["http.request", "auth", "validate", ...],
    "pr-analyzer": ["llm.call", "prompt.build", "response.parse", ...],
    # ... 24 nodes × 7 spans each
}
```

**Wallet Signing Boundary** (`09_wallet_verifier.py`):
```python
resp = wait_for(".../sign/response")
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
