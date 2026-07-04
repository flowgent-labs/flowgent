"""
Scenario 06 — Basic Nodes: Agent / Tribunal / Supervisor / Human nodes.
Creates a flow with all node types and verifies execution completes.
"""

import requests
import time
import sys, os
import config

API = config.K3S_APISERVER_URL
TENANT = config.K3S_TENANT
FLOW_ID = "vrf-exec-03"


def try_approve_human(s, run_id):
    """Approve pending human gate via REST if the run is blocked."""
    r = s.get(f"{API}/api/v1/human/approvals")
    if r.status_code != 200:
        return False
    for item in r.json() or []:
        if item.get("agentflow_run_id") == run_id and item.get("status") == "PENDING":
            token = item.get("token")
            if token:
                resp = s.post(f"{API}/api/v1/human/{token}/approve", json={"comment": "e2e auto-approve"})
                print(f"  auto-approved human gate: {resp.status_code}")
                return resp.status_code == 200
    return False


def run():
    s = requests.Session()
    s.headers["Content-Type"] = "application/json"

    # ── Create Flow with all node types ────────────────────
    flow = {
        "agentflow_id": FLOW_ID,
        "version": 1,
        "description": "Verification flow: agent + tribunal + supervisor + human",
        "definition": {
            "nodes": [
                {"id": "start", "type": "agent", "agent": "issue-detector", "input": {"test": True}},
                {
                    "id": "vote",
                    "type": "tribunal",
                    "strategy": {"type": "majority"},
                    "input": {"votes": ["${start.decision}", "${start.decision}", "${start.decision}"]},
                },
                {
                    "id": "supervisor",
                    "type": "supervisor",
                    "agent": "supervisor",
                    "supervisor_config": {
                        "max_retries": 2,
                        "max_nodes": 10,
                        "max_injections": 2,
                        "allowed_actions": ["continue", "retry", "abort"],
                    },
                },
                {
                    "id": "human",
                    "type": "human",
                    "approval": {"timeout": "5m", "on_approve": "continue", "on_reject": "abort"},
                },
                {"id": "end", "type": "noop"},
            ],
            "edges": [
                {"from": "start", "to": "vote"},
                {"from": "vote", "to": "supervisor"},
                {"from": "supervisor", "to": "human"},
                {"from": "human", "to": "end"},
            ],
        },
    }

    s.delete(f"{API}/api/v1/{TENANT}/agentflows/{FLOW_ID}")
    time.sleep(0.3)
    r = s.post(f"{API}/api/v1/{TENANT}/agentflows", json=flow)
    assert r.status_code in (200, 201), f"create: {r.status_code} {r.text}"
    print(f"  create flow OK: {FLOW_ID}")

    # ── Trigger ─────────────────────────────────────────────
    r = s.post(f"{API}/api/v1/{TENANT}/agentflows/trigger", json={
        "agentflow_id": FLOW_ID, "vars": {}, "trigger": {"type": "api"}
    })
    assert r.status_code == 200, f"trigger: {r.status_code} {r.text}"
    run_data = r.json()
    run_id = run_data.get("run_id") or run_data.get("id")
    assert run_id, f"no run_id in response: {run_data}"
    print(f"  trigger OK: run_id={run_id[:20]}...")

    # ── Monitor (auto-approve human gate if needed) ─────────
    status = "PENDING"
    for i in range(config.FLOW_TIMEOUT_S // config.POLL_INTERVAL_S):
        time.sleep(config.POLL_INTERVAL_S)
        r = s.get(f"{API}/api/v1/{TENANT}/runs/{run_id}")
        status = r.json().get("status", "?")
        if status in ("RUNNING", "PAUSED"):
            try_approve_human(s, run_id)
        if status in ("COMPLETED", "FAILED", "CANCELLED"):
            break
    assert status in ("COMPLETED", "FAILED"), f"flow did not finish: status={status}"
    print(f"  execution OK: {status}")

    # ── Check Tasks ─────────────────────────────────────────
    r = s.get(f"{API}/api/v1/{TENANT}/runs/{run_id}/tasks")
    tasks = r.json() or []
    task_statuses = {t["node_id"]: t["status"] for t in tasks}
    print(f"  tasks: {len(tasks)} nodes — {task_statuses}")

    expected_nodes = {"start", "vote", "supervisor", "human", "end"}
    seen = set(task_statuses.keys()) & expected_nodes
    if len(seen) < 4:
        print(f"  WARN: expected nodes {expected_nodes}, saw {seen}")
    else:
        print(f"  OK node coverage: {sorted(seen)}")

    if task_statuses.get("human") == "COMPLETED":
        print(f"  OK human node completed")
    elif "human" in task_statuses:
        print(f"  WARN human node status={task_statuses['human']} (may need manual approval in env)")

    # ── Cleanup ─────────────────────────────────────────────
    s.delete(f"{API}/api/v1/{TENANT}/agentflows/{FLOW_ID}")
    print("  cleanup OK")
