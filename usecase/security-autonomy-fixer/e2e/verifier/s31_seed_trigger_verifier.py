"""Scenario 31 — Seed & Trigger: agent/MCP registration, flow creation, trigger, run_id verification."""

import requests
import time
import sys
import os
import json

from verifier import _common as c

# ── Phase 0: Seed verification ──

def verify_seed(s, conn):
    print("\n-- [31 Seed] Verify agents & MCPs registered --")
    total = len(c.AGENT_NAMES) + len(c.MCP_NAMES)
    passed = 0

    for name in c.AGENT_NAMES:
        r = s.get(f"{c.API}/api/v1/{c.NAMESPACE}/agents/{name}")
        if r.status_code == 200:
            passed += 1
        else:
            print(f"  WARN agent '{name}' GET returned {r.status_code}")

    for name in c.MCP_NAMES:
        r = s.get(f"{c.API}/api/v1/{c.NAMESPACE}/mcp/{name}")
        if r.status_code == 200:
            passed += 1
        else:
            print(f"  WARN MCP '{name}' GET returned {r.status_code}")

    print(f"  Result: {passed}/{total} definitions verified via API")
    return passed, total


# ── Phase 1: Trigger ──

def verify_trigger(s, conn):
    """Trigger flow via manual endpoint (webhook fallback). Returns run_id."""
    print(f"\n-- [31 Trigger] POST /api/v1/{c.NAMESPACE}/flows/{c.FLOW_ID}/trigger --")
    run_id = None

    trigger_url = f"{c.API}/api/v1/{c.NAMESPACE}/flows/{c.FLOW_ID}/trigger"
    r = s.post(trigger_url, json={"vars": {}})
    if r.status_code in (200, 201, 202):
        resp_data = r.json() if r.text else {}
        run_id = resp_data.get("run_id") or resp_data.get("id")
        print(f"  OK manual trigger accepted (status={r.status_code}, run_id={run_id})")
    else:
        print(f"  WARN: Manual trigger returned {r.status_code}, trying webhook fallback...")
        payload = {
            "action": "opened",
            "number": 4,
            "pull_request": {
                "head": {"ref": "fix/flowgent_sec_auto_fix", "sha": "trigger-e2e-abcdef1234567890",
                         "repo": {"full_name": "wl4g/rengine"}},
                "base": {"ref": "main", "sha": "base-main-abcdef1234567890",
                         "repo": {"full_name": "wl4g/rengine"}},
            },
            "repository": {"full_name": "wl4g/rengine"},
        }
        headers = {"Content-Type": "application/json", "X-GitHub-Event": "pull_request"}
        r = s.post(f"{c.API}/api/v1/webhook/github", json=payload, headers=headers)
        if r.status_code not in (200, 202):
            raise AssertionError(f"Both trigger methods failed. webhook returned {r.status_code}: {r.text[:200]}")
        resp_data = r.json() if r.text else {}
        run_id = resp_data.get("run_id") or resp_data.get("id")
        print(f"  OK webhook trigger accepted (status={r.status_code}, run_id={run_id})")

    if not run_id and conn:
        cur = conn.cursor()
        cur.execute(
            "SELECT id FROM orh_flowrun WHERE agentflow_id=%s ORDER BY created_at DESC LIMIT 1",
            (c.FLOW_ID,),
        )
        row = cur.fetchone()
        if row:
            run_id = row[0]
            print(f"  OK run_id from PG fallback: {run_id}")

    if not run_id:
        raise AssertionError("No run_id from trigger or PG fallback")

    if conn:
        cur = conn.cursor()
        cur.execute(
            "SELECT id, status, agentflow_id, priority FROM orh_flowrun WHERE id=%s",
            (run_id,),
        )
        row = cur.fetchone()
        if row:
            print(f"  OK PG orh_flowrun: status={row[1]} flow={row[2]} priority={row[3]}")
        else:
            print("  WARN: run_id not yet visible in PG (may need persistence delay)")

    print(f"  OK run_id={run_id}")
    return run_id


# ── Orchestration ──

def run():
    print("\n" + "=" * 60)
    print("  Scenario 31: Seed & Trigger")
    print("=" * 60)

    s = requests.Session()
    s.headers["Content-Type"] = "application/json"
    if os.path.exists(c.MQTT_AUDIT_PATH):
        os.remove(c.MQTT_AUDIT_PATH)

    # Phase 0: Seed
    c.seed_agents_and_mcps(s)
    conn = c.pg_connect()
    p0_passed, p0_total = verify_seed(s, conn)

    # Create flow definition
    flow_def = c.load_flow_from_yaml()
    node_count = len(flow_def.get("nodes", []))
    edge_count = len(flow_def.get("edges", []))
    print(f"\n-- Flow definition: {c.FLOW_ID} ({node_count} nodes, {edge_count} edges) --")

    s.delete(f"{c.API}/api/v1/{c.NAMESPACE}/flows/{c.FLOW_ID}")
    time.sleep(0.5)
    r = s.post(f"{c.API}/api/v1/{c.NAMESPACE}/flows", json=flow_def)
    if r.status_code not in (200, 201):
        raise AssertionError(f"create flow returned {r.status_code}: {r.text[:200]}")
    print(f"  OK Flow definition created (status={r.status_code})")

    baseline = c.capture_pr_baseline()
    head = baseline.get("head_sha", "")[:8] or "none"
    print(f"  OK PR #{c.PR_NUMBER} baseline captured: commits={baseline.get('commit_count')} head={head}")

    # Phase 1: Trigger
    print("\n-- [31 MQTT Audit] Non-$share subscription before trigger --")
    audit = c.start_global_mqtt_audit()
    run_id = verify_trigger(s, conn)
    audit.set_run_id(run_id)
    c.wait_for_application_components(c.FLOW_ID, timeout=240)
    audit.wait_for("ctrl/run/created", timeout=20)
    audit.wait_for("exec/plans", timeout=45)
    c.save_mqtt_audit(run_id, c.snapshot_global_mqtt_audit(run_id))
    c.assert_mqtt_suffixes(run_id, ["ctrl/run/created"])

    if conn:
        conn.close()

    # Store run_id for downstream scenarios
    run_id_file = os.path.join(os.path.dirname(__file__), "..", ".last_run_id")
    with open(run_id_file, "w") as f:
        f.write(run_id)

    # Print summary
    print(f"\n{'=' * 60}")
    print(f"  Scenario 31 Summary")
    print(f"  Seed:  {p0_passed}/{p0_total}")
    print(f"  Run ID: {run_id}")
    print(f"  Stored in: .last_run_id")
    print(f"{'=' * 60}")

    if p0_passed < p0_total:
        raise AssertionError(f"Seed verification failed: {p0_passed}/{p0_total}")
