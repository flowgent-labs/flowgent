"""
Scenario 01 — REST API: Flow CRUD + Trigger + Run Lifecycle.
"""

import requests
import time
import sys, os
sys.path.insert(0, os.path.dirname(os.path.dirname(__file__)))
import config

API = config.K3S_APISERVER_URL
TENANT = config.K3S_TENANT


def run():
    s = requests.Session()
    s.headers["Content-Type"] = "application/json"

    # ── Health ──────────────────────────────────────────────
    r = s.get(f"{API}/_/healthz")
    assert r.status_code == 200, f"healthz: {r.status_code}"
    print("  healthz OK")

    # ── Create Flow ─────────────────────────────────────────
    flow_id = "vrf-rest-01"
    r = s.post(f"{API}/api/v1/{TENANT}/agentflows", json={
        "id": flow_id, "nodes": [{"id": "n1", "type": "noop"}], "edges": []
    })
    assert r.status_code in (200, 201), f"create flow: {r.status_code} {r.text}"
    print(f"  create flow OK: {flow_id}")

    # ── Get Flow ────────────────────────────────────────────
    r = s.get(f"{API}/api/v1/{TENANT}/agentflows/{flow_id}")
    assert r.status_code == 200
    data = r.json()
    assert data["id"] == flow_id
    print(f"  get flow OK: nodes={len(data.get('nodes', []))}")

    # ── Update Flow ─────────────────────────────────────────
    r = s.put(f"{API}/api/v1/{TENANT}/agentflows/{flow_id}", json={
        "id": flow_id, "description": "updated", "nodes": [{"id": "n1", "type": "noop"}, {"id": "n2", "type": "noop"}],
        "edges": [{"from": "n1", "to": "n2"}]
    })
    assert r.status_code == 200, f"update: {r.status_code}"
    r = s.get(f"{API}/api/v1/{TENANT}/agentflows/{flow_id}")
    assert r.json()["description"] == "updated"
    print("  update flow OK")

    # ── Trigger Run ─────────────────────────────────────────
    r = s.post(f"{API}/api/v1/{TENANT}/agentflows/trigger", json={
        "agentflow_id": flow_id, "vars": {}, "trigger": {"type": "api"}
    })
    assert r.status_code == 200, f"trigger: {r.status_code} {r.text}"
    run_data = r.json()
    run_id = run_data["run_id"]
    assert run_data["status"] == "PENDING"
    print(f"  trigger OK: run_id={run_id[:20]}...")

    # ── Wait for Completion ─────────────────────────────────
    for i in range(config.FLOW_TIMEOUT_S // config.POLL_INTERVAL_S):
        time.sleep(config.POLL_INTERVAL_S)
        r = s.get(f"{API}/api/v1/{TENANT}/runs/{run_id}")
        status = r.json().get("status", "?")
        if status != "PENDING":
            break
    assert status in ("COMPLETED", "FAILED"), f"run stuck at {status}"
    print(f"  run finished: {status}")

    # ── List Runs ───────────────────────────────────────────
    r = s.get(f"{API}/api/v1/{TENANT}/runs")
    assert r.status_code == 200
    runs = r.json() or []
    assert len(runs) > 0
    print(f"  list runs OK: {len(runs)} total")

    # ── Delete Flow ─────────────────────────────────────────
    r = s.delete(f"{API}/api/v1/{TENANT}/agentflows/{flow_id}")
    assert r.status_code in (200, 204), f"delete: {r.status_code}"
    print("  delete flow OK")
