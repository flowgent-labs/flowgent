"""
Scenario 07 — Security Fixer V2: Full Pipeline White-Box Verification.

Creates and triggers the security-autonomy-fixer-v2 flow, then verifies
every step in PostgreSQL and EMQX.
"""

import requests
import time
import sys
import os
import json

sys.path.insert(0, os.path.dirname(os.path.dirname(__file__)))
import config

API = config.K3S_APISERVER_URL
TENANT = config.K3S_TENANT
FLOW_ID = "security-autonomy-fixer-v2"

# ── V2 flow definition (from examples/flows/01-security-autonomy-fix-v2.yaml) ──
V2_FLOW = {
    "id": FLOW_ID,
    "priority": "high",
    "tenant_id": TENANT,
    "namespace": "flowgent-rengine",
    "description": "Production security fixer with SonarQube MCP, .cyberbot stateful metadata. PR strategy: new-pr branch per run.",
    "vars": {"repo": "rengine", "repo_path": "/home/agent/rengine"},
    "nodes": [
        {"id": "fetch-issues", "type": "tool", "tool": "sonarqube",
         "input": {"action": "scan/get_issues", "project_key": "rengine", "severities": "BLOCKER"}},
        {"id": "analyze-issues", "type": "agent", "agent": "issue-detector",
         "input": {"repo": "${vars.repo}", "sonarqube_issues": "${fetch-issues}"}},
        {"id": "generate-fixes", "type": "agent", "agent": "fixer-agent",
         "input": {"issues": "${analyze-issues.issues}", "repo_path": "${vars.repo_path}"}},
        {"id": "review-security", "type": "agent", "agent": "security-reviewer",
         "input": {"patches": "${generate-fixes.patches}"}},
        {"id": "review-quality", "type": "agent", "agent": "quality-reviewer",
         "input": {"patches": "${generate-fixes.patches}"}},
        {"id": "review-arch", "type": "agent", "agent": "arch-reviewer",
         "input": {"patches": "${generate-fixes.patches}"}},
        {"id": "vote", "type": "tribunal", "strategy": {"type": "majority"},
         "input": {"votes": ["${review-security.decision}", "${review-quality.decision}", "${review-arch.decision}"]}},
        {"id": "supervisor", "type": "supervisor", "agent": "supervisor",
         "supervisor_config": {"max_retries": 3, "max_nodes": 50, "max_injections": 5,
                               "allowed_actions": ["continue", "retry", "inject", "abort"]}},
        {"id": "is-approved", "type": "condition", "expression": "${vote.decision == true}"},
        {"id": "write-cyberbot", "type": "agent", "agent": "git-agent",
         "input": {"repo": "${vars.repo}", "repo_path": "${vars.repo_path}",
                   "vote_decision": "${vote.decision}", "patches": "${generate-fixes.patches}",
                   "reviewers": [{"name": "security", "decision": "${review-security.decision}"},
                                 {"name": "quality", "decision": "${review-quality.decision}"},
                                 {"name": "arch", "decision": "${review-arch.decision}"}]}},
        {"id": "summary-report", "type": "agent", "agent": "issue-detector",
         "input": {"cyberbot_metadata": "${write-cyberbot}", "vote_outcome": "${vote.decision}",
                   "repo": "${vars.repo}"}},
        {"id": "end", "type": "noop"}
    ],
    "edges": [
        {"from": "fetch-issues", "to": "analyze-issues"},
        {"from": "analyze-issues", "to": "generate-fixes"},
        {"from": "generate-fixes", "to": "review-security"},
        {"from": "generate-fixes", "to": "review-quality"},
        {"from": "generate-fixes", "to": "review-arch"},
        {"from": "review-security", "to": "vote"},
        {"from": "review-quality", "to": "vote"},
        {"from": "review-arch", "to": "vote"},
        {"from": "vote", "to": "supervisor"},
        {"from": "supervisor", "to": "is-approved"},
        {"from": "is-approved", "to": "write-cyberbot", "condition": True},
        {"from": "is-approved", "to": "summary-report", "condition": False},
        {"from": "write-cyberbot", "to": "summary-report"},
        {"from": "summary-report", "to": "end"}
    ]
}


def pg_connect():
    """Connect to PG, trying port-forward first, then direct."""
    try:
        import psycopg2
    except ImportError:
        print("  SKIP: psycopg2 not installed")
        return None
    dsn = config.pg_dsn()
    try:
        conn = psycopg2.connect(dsn)
        conn.autocommit = True
        return conn
    except Exception:
        # Try alternate port
        try:
            alt = dsn.replace("port=5432", "port=5433")
            conn = psycopg2.connect(alt)
            conn.autocommit = True
            return conn
        except Exception:
            return None


def run():
    s = requests.Session()
    s.headers["Content-Type"] = "application/json"

    # ── 1. Create V2 flow definition ────────────────────────────
    print("\n── [1] Creating V2 flow definition ──")
    # Delete if already exists (clean slate)
    s.delete(f"{API}/api/v1/{TENANT}/agentflows/{FLOW_ID}")
    time.sleep(0.5)

    r = s.post(f"{API}/api/v1/{TENANT}/agentflows", json=V2_FLOW)
    if r.status_code not in (200, 201):
        print(f"  FAIL: create flow returned {r.status_code}: {r.text[:200]}")
        return
    print(f"  CREATE OK: {FLOW_ID} (status={r.status_code})")

    # ── 2. Verify flow persisted in PG ─────────────────────────
    print("\n── [2] PG: Verify flow definition persisted ──")
    conn = pg_connect()
    if not conn:
        print("  SKIP: cannot connect to PG")
        conn = None
    else:
        cur = conn.cursor()
        cur.execute(
            "SELECT agentflow_id, priority, tenant_id, namespace FROM agentflow_definitions WHERE agentflow_id=%s",
            (FLOW_ID,))
        row = cur.fetchone()
        if row:
            print(f"  PG OK: id={row[0]} priority={row[1]} tenant={row[2]} namespace={row[3]}")
        else:
            print(f"  FAIL: flow {FLOW_ID} not found in agentflow_definitions")

    # ── 3. Trigger the flow ────────────────────────────────────
    print("\n── [3] Triggering flow ──")
    r = s.post(f"{API}/api/v1/{TENANT}/agentflows/trigger", json={
        "agentflow_id": FLOW_ID, "vars": {}, "trigger": {"type": "api"}
    })
    if r.status_code != 200:
        print(f"  FAIL: trigger returned {r.status_code}: {r.text[:200]}")
        if conn:
            conn.close()
        return
    run_data = r.json()
    run_id = run_data.get("run_id", "")
    print(f"  TRIGGER OK: run_id={run_id[:30]}... status={run_data.get('status', '?')}")

    # ── 4. PG: Verify run persisted ────────────────────────────
    print("\n── [4] PG: Verify run persisted ──")
    if conn:
        cur.execute(
            "SELECT id, status, agentflow_id, priority FROM agentflow_runs WHERE id=%s",
            (run_id,))
        row = cur.fetchone()
        if row:
            print(f"  PG OK: run_id={row[0][:30]}... status={row[1]} flow={row[2]} priority={row[3]}")
        else:
            print(f"  INFO: run not yet in PG (may need commit delay)")
    else:
        print(f"  SKIP: no PG connection")

    # ── 5. Monitor execution ───────────────────────────────────
    print("\n── [5] Monitoring execution (up to 60s) ──")
    status = "PENDING"
    for i in range(30):
        time.sleep(2)
        r = s.get(f"{API}/api/v1/{TENANT}/runs/{run_id}")
        if r.status_code == 200:
            status = r.json().get("status", "?")
            print(f"  [{i*2}s] status={status}")
            if status != "PENDING":
                break
        else:
            print(f"  [{i*2}s] GET returned {r.status_code}")
    print(f"  Final status: {status}")

    # ── 6. PG: Verify task_runs ────────────────────────────────
    print("\n── [6] PG: Verify task_runs ──")
    if conn:
        cur.execute(
            "SELECT node_id, status, output IS NOT NULL AS has_output FROM task_runs WHERE agentflow_run_id=%s ORDER BY sequence",
            (run_id,))
        tasks = cur.fetchall()
        if tasks:
            for t in tasks:
                print(f"  task: {t[0]:<20} status={t[1]:<12} has_output={t[2]}")
        else:
            print("  INFO: no task_runs found (may use different table name)")

    # ── 7. PG: Check all related tables ────────────────────────
    print("\n── [7] PG: Schema discovery ──")
    if conn:
        cur.execute("""
            SELECT table_name FROM information_schema.tables
            WHERE table_schema='public' AND table_name LIKE '%agent%'
            ORDER BY table_name
        """)
        tables = [r[0] for r in cur.fetchall()]
        print(f"  agent tables: {tables}")

        for tbl in tables:
            try:
                cur.execute(f"SELECT column_name FROM information_schema.columns WHERE table_name=%s ORDER BY ordinal_position", (tbl,))
                cols = [r[0] for r in cur.fetchall()]
                cur.execute(f"SELECT COUNT(*) FROM {tbl}")
                count = cur.fetchone()[0]
                print(f"  {tbl}: {count} rows, cols={cols}")
            except Exception as e:
                print(f"  {tbl}: error={e}")

    # ── 8. EMQX: Verify notifications ─────────────────────────
    print("\n── [8] EMQX: Check for notification messages ──")
    try:
        import paho.mqtt.client as mqtt
        EMQX = config.EMQX_HOST
        topic = f"flowgent/notify/queue/{TENANT}/{FLOW_ID}/+"
        messages = []

        def on_message(client, userdata, msg):
            messages.append(msg.payload)

        client = mqtt.Client(mqtt.CallbackAPIVersion.VERSION2)
        client.on_message = on_message
        try:
            client.connect(EMQX, config.EMQX_PORT, 5)
            client.subscribe("flowgent/notify/queue/#")
            client.loop_start()
            time.sleep(2)
            client.loop_stop()
            client.disconnect()
            if messages:
                for m in messages:
                    print(f"  MQTT msg: {m[:200]}")
            else:
                print("  No MQTT messages captured (flow may not have completed)")
        except Exception as e:
            print(f"  MQTT connect failed: {e}")
    except ImportError:
        print("  SKIP: paho-mqtt not installed")

    # ── 9. API: Get tasks via REST ─────────────────────────────
    print("\n── [9] API: Get tasks via REST ──")
    r = s.get(f"{API}/api/v1/{TENANT}/runs/{run_id}/tasks")
    if r.status_code == 200:
        tasks = r.json()
        if isinstance(tasks, list):
            for t in tasks:
                node_id = t.get("node_id", "?")
                t_status = t.get("status", "?")
                has_output = bool(t.get("output"))
                print(f"  task: {node_id:<20} status={t_status:<12} has_output={has_output}")
        else:
            print(f"  tasks response: {tasks}")
    else:
        print(f"  GET tasks: {r.status_code}")

    # ── Cleanup ────────────────────────────────────────────────
    print("\n── Cleanup ──")
    if conn:
        conn.close()
    # Keep the flow for inspection; delete only on explicit request
    print(f"  Flow {FLOW_ID} left in place for manual inspection")
    print(f"  Run ID: {run_id}")
