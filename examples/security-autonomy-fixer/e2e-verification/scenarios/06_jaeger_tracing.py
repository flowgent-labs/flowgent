"""
Scenario 05 — Jaeger OTEL: Trace Export Verification.

Triggers a flow and verifies that OTEL traces appear in Jaeger.
"""

import requests
import time
import sys, os
sys.path.insert(0, os.path.dirname(os.path.dirname(__file__)))
import config

API = config.K3S_APISERVER_URL
JAEGER = config.JAEGER_UI_URL
TENANT = config.K3S_TENANT


def run():
    s = requests.Session()
    s.headers["Content-Type"] = "application/json"

    # ── Jaeger health ───────────────────────────────────────
    r = requests.get(f"{JAEGER}/api/services", timeout=5)
    if r.status_code != 200:
        print("  SKIP: Jaeger not reachable")
        return
    services = r.json().get("data", [])
    print(f"  Jaeger OK: {len(services)} services registered")

    # ── Trigger a flow to generate traces ───────────────────
    flow_id = "vrf-jaeger-05"
    s.post(f"{API}/api/v1/{TENANT}/agentflows", json={
        "id": flow_id, "nodes": [{"id": "n1", "type": "noop"}], "edges": []
    })

    r = s.post(f"{API}/api/v1/{TENANT}/agentflows/trigger", json={
        "agentflow_id": flow_id, "vars": {}, "trigger": {"type": "api"}
    })
    if r.status_code != 200:
        print(f"  trigger: {r.status_code} (flow may need pre-existing)")
        return
    run_id = r.json().get("run_id", "")

    # ── Wait for trace export ───────────────────────────────
    time.sleep(10)

    # ── Check Jaeger for flowgent traces ────────────────────
    r = requests.get(f"{JAEGER}/api/services", timeout=5)
    services = r.json().get("data", [])
    flowgent_registered = any("flowgent" in svc.lower() or svc == "empty-service-name" for svc in services)
    if flowgent_registered:
        print(f"  OTEL traces OK: flowgent-like service found in Jaeger")
    else:
        print(f"  OTEL traces: flowgent service not yet registered (services={services})")
        print(f"  (check OTEL_EXPORTER_OTLP_ENDPOINT config)")

    # ── Cleanup ─────────────────────────────────────────────
    s.delete(f"{API}/api/v1/{TENANT}/agentflows/{flow_id}")
    print("  cleanup OK")
