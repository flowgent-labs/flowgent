"""
Scenario 04 — PG Storage: Run & Definition Persistence.

Verifies that flow definitions and runs are correctly persisted to PostgreSQL
and retrievable after write. Uses psycopg2 for direct PG queries.
"""

import requests
import sys, os
sys.path.insert(0, os.path.dirname(os.path.dirname(__file__)))
import config

API = config.K3S_APISERVER_URL
TENANT = config.K3S_TENANT


def run():
    s = requests.Session()
    s.headers["Content-Type"] = "application/json"

    try:
        import psycopg2
    except ImportError:
        print("  SKIP: psycopg2 not installed")
        return

    dsn = config.pg_dsn()
    conn = psycopg2.connect(dsn)
    conn.autocommit = True
    cur = conn.cursor()

    # ── Pre-check: count existing rows ──────────────────────
    cur.execute("SELECT COUNT(*) FROM agentflow_definitions")
    defs_before = cur.fetchone()[0]
    cur.execute("SELECT COUNT(*) FROM agentflow_runs")
    runs_before = cur.fetchone()[0]
    print(f"  before: defs={defs_before} runs={runs_before}")

    # ── Create flow via API ─────────────────────────────────
    flow_id = "vrf-pg-04"
    r = s.post(f"{API}/api/v1/{TENANT}/agentflows", json={
        "id": flow_id, "nodes": [{"id": "n1", "type": "noop"}], "edges": []
    })
    assert r.status_code in (200, 201), f"create: {r.status_code}"

    # ── Trigger ─────────────────────────────────────────────
    r = s.post(f"{API}/api/v1/{TENANT}/agentflows/trigger", json={
        "agentflow_id": flow_id, "vars": {}, "trigger": {"type": "api"}
    })
    assert r.status_code == 200, f"trigger: {r.status_code} {r.text}"
    run_id = r.json()["run_id"]

    # ── Verify PG persistence ───────────────────────────────
    cur.execute("SELECT COUNT(*) FROM agentflow_definitions")
    defs_after = cur.fetchone()[0]
    assert defs_after > defs_before, f"defs not persisted: {defs_before} → {defs_after}"
    print(f"  defs persisted: {defs_before} → {defs_after}")

    cur.execute("SELECT COUNT(*) FROM agentflow_runs WHERE agentflow_id=%s", (flow_id,))
    runs_for_flow = cur.fetchone()[0]
    assert runs_for_flow > 0, f"run not persisted for {flow_id}"
    print(f"  runs persisted: {runs_for_flow} row(s) for {flow_id}")

    # ── Verify run detail ───────────────────────────────────
    cur.execute("SELECT id, status FROM agentflow_runs WHERE id=%s", (run_id,))
    row = cur.fetchone()
    assert row is not None, f"run {run_id} not found in PG"
    assert row[1] in ("PENDING", "RUNNING", "COMPLETED"), f"unexpected status: {row[1]}"
    print(f"  run detail: id={row[0][:20]}... status={row[1]}")

    # ── Cleanup ─────────────────────────────────────────────
    conn.close()
    s.delete(f"{API}/api/v1/{TENANT}/agentflows/{flow_id}")
    print("  cleanup OK")
