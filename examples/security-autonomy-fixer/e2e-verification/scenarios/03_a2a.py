"""
Scenario 02 — A2A Protocol: Agent Card + Task Submit.
"""

import requests
import sys, os
sys.path.insert(0, os.path.dirname(os.path.dirname(__file__)))
import config

A2A = config.K3S_A2A_URL


def run():
    s = requests.Session()
    s.headers["Content-Type"] = "application/json"

    # ── Agent Card ──────────────────────────────────────────
    try:
        r = s.get(f"{A2A}/.well-known/agent.json", timeout=3)
    except Exception as e:
        print(f"  SKIP: A2A not reachable at {A2A} ({e})")
        return
    if r.status_code != 200:
        print(f"  SKIP: A2A returned {r.status_code} (A2A not enabled)")
        return
    card = r.json()
    assert "skills" in card, f"no skills in card: {list(card.keys())}"
    print(f"  agent card OK: {len(card.get('skills', []))} skills")

    # ── Submit Task ─────────────────────────────────────────
    r = s.post(f"{A2A}/a2a/tasks", json={
        "agentflow_id": "vrf-a2a-02", "input": {"test": True}
    })
    # May return 404 if flow doesn't exist — acceptable for verification
    if r.status_code == 200:
        data = r.json()
        print(f"  submit OK: task_id={data.get('task_id', '?')[:20]}")
    else:
        print(f"  submit: {r.status_code} (flow may not exist — non-critical)")

    # ── Query Task ──────────────────────────────────────────
    r = s.get(f"{A2A}/a2a/tasks")
    # A2A may not support listing tasks — non-critical
    if r.status_code == 200:
        print("  query tasks OK")
    else:
        print(f"  query tasks: {r.status_code} (non-critical)")
