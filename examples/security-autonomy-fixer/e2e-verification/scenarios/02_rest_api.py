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

    # ── Seed MCP Servers ─────────────────────────────────────
    r = s.post(f"{API}/api/v1/{TENANT}/mcp", json={
        "name": "github", "enabled": True, "type": "local",
        "command": ["/app/mcp-server.sh", "github"], "args": [], "env": {},
    })
    assert r.status_code in (200, 201), f"create mcp github: {r.status_code}"
    print(f"  mcp github OK")
    r = s.post(f"{API}/api/v1/{TENANT}/mcp", json={
        "name": "sonarqube", "enabled": True, "type": "local",
        "command": ["/app/mcp-server.sh", "sonarqube"], "args": [], "env": {},
    })
    assert r.status_code in (200, 201), f"create mcp sonarqube: {r.status_code}"
    print("  mcp sonarqube OK")

    # ── Seed LLM Provider ────────────────────────────────────
    r = s.post(f"{API}/api/v1/{TENANT}/llm/providers", json={
        "type": "deepseek", "enabled": True, "provider": "deepseek",
        "endpoint": "https://api.deepseek.com", "apikey": "sk-test",
        "timeout_ms": 120000, "model": "deepseek-chat",
        "models": [
            {"name": "deepseek-chat", "temperature": 0.3, "topk": 0},
            {"name": "deepseek-reasoner", "temperature": 0.3, "topk": 0},
        ],
    })
    assert r.status_code in (200, 201), f"create llm provider: {r.status_code}"
    print(f"  llm provider OK: deepseek")

    # ── Seed Agents ──────────────────────────────────────────
    agents = [
        {"name": "supervisor", "model": "deepseek/deepseek-chat", "max_tokens": 16384,
         "temperature": 0.2, "soul": "You are a senior autonomous orchestration controller.",
         "instruction": 'Decide: {"action":"continue|retry|inject|abort"}'},
        {"name": "issue-detector", "model": "deepseek/deepseek-chat", "max_tokens": 16384,
         "temperature": 0.3, "soul": "You are a senior DevSecOps expert.",
         "instruction": 'Output JSON: {"issues":[...]}'},
        {"name": "fixer-agent", "model": "deepseek/deepseek-chat", "max_tokens": 16384,
         "temperature": 0.3, "soul": "You are a secure coding expert.",
         "instruction": 'Output JSON: {"patches":[...]}'},
        {"name": "security-reviewer", "model": "deepseek/deepseek-chat", "max_tokens": 16384,
         "temperature": 0.3, "soul": "You are a strict security reviewer.",
         "instruction": 'Output JSON: {"decision":true|false}'},
        {"name": "quality-reviewer", "model": "deepseek/deepseek-chat", "max_tokens": 16384,
         "temperature": 0.3, "soul": "You are a code quality reviewer.",
         "instruction": 'Output JSON: {"decision":true|false}'},
        {"name": "arch-reviewer", "model": "deepseek/deepseek-chat", "max_tokens": 16384,
         "temperature": 0.3, "soul": "You are an architecture reviewer.",
         "instruction": 'Output JSON: {"decision":true|false}'},
    ]
    for agent in agents:
        r = s.post(f"{API}/api/v1/{TENANT}/agents", json=agent)
        assert r.status_code in (200, 201), f"create agent {agent['name']}: {r.status_code}"
    print(f"  agents OK: {len(agents)} created")

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
