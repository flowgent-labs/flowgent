"""
Scenario 03 — Flow Execution: Agent / Tribunal / Supervisor / Human nodes.
Creates a flow with all node types and verifies execution completes.
"""

import requests
import time
import sys, os
sys.path.insert(0, os.path.dirname(os.path.dirname(__file__)))
import config

API = config.K3S_APISERVER_URL
TENANT = config.K3S_TENANT
FLOW_ID = "vrf-exec-03"


def run():
    s = requests.Session()
    s.headers["Content-Type"] = "application/json"

    # ── Create Flow with all node types ────────────────────
    flow = {
        "id": FLOW_ID,
        "priority": "high",
        "tenant_id": TENANT,
        "description": "Verification flow: agent + tribunal + supervisor",
        "nodes": [
            {"id": "start", "type": "agent", "agent": "issue-detector", "input": {"test": True}},
            {"id": "vote", "type": "tribunal", "strategy": {"type": "majority"},
             "input": {"votes": ["${start.decision}", "${start.decision}", "${start.decision}"]}},
            {"id": "supervisor", "type": "supervisor", "agent": "supervisor",
             "supervisor_config": {"max_retries": 2, "max_nodes": 10, "max_injections": 2,
                                   "allowed_actions": ["continue", "retry", "abort"]}},
            {"id": "end", "type": "noop"}
        ],
        "edges": [
            {"from": "start", "to": "vote"},
            {"from": "vote", "to": "supervisor"},
            {"from": "supervisor", "to": "end"}
        ]
    }

    r = s.post(f"{API}/api/v1/{TENANT}/agentflows", json=flow)
    assert r.status_code in (200, 201), f"create: {r.status_code} {r.text}"
    print(f"  create flow OK: {FLOW_ID}")

    # ── Trigger ─────────────────────────────────────────────
    r = s.post(f"{API}/api/v1/{TENANT}/agentflows/trigger", json={
        "agentflow_id": FLOW_ID, "vars": {}, "trigger": {"type": "api"}
    })
    assert r.status_code == 200, f"trigger: {r.status_code} {r.text}"
    run_data = r.json()
    run_id = run_data["run_id"]
    print(f"  trigger OK: run_id={run_id[:20]}...")

    # ── Monitor ─────────────────────────────────────────────
    status = "PENDING"
    for i in range(config.FLOW_TIMEOUT_S // config.POLL_INTERVAL_S):
        time.sleep(config.POLL_INTERVAL_S)
        r = s.get(f"{API}/api/v1/{TENANT}/runs/{run_id}")
        status = r.json().get("status", "?")
        if status != "PENDING":
            break
    assert status == "COMPLETED", f"flow did not complete: status={status}"
    print(f"  execution OK: {status}")

    # ── Check Tasks ─────────────────────────────────────────
    r = s.get(f"{API}/api/v1/{TENANT}/runs/{run_id}/tasks")
    tasks = r.json() or []
    task_statuses = {t["node_id"]: t["status"] for t in tasks}
    print(f"  tasks: {len(tasks)} nodes — {task_statuses}")

    # ── Cleanup ─────────────────────────────────────────────
    s.delete(f"{API}/api/v1/{TENANT}/agentflows/{FLOW_ID}")
    print("  cleanup OK")
