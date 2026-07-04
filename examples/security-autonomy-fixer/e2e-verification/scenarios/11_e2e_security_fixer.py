"""
Scenario 11 - Security Fixer: Full Pipeline White-Box Verification (capstone).

Purpose
-------
This is the **end-to-end capstone** scenario. Run it **after** module verifiers
01-10 have passed. It does NOT replace them:

  04 Controller  -> JM pod lifecycle (Application mode)
  05 Engine      -> DAG scheduling / voting
  06 Basic Nodes -> single-node execution types
  07 Messager    -> MQTT topic chains
  08 Notifier    -> multi-channel delivery
  09 Wallet      -> x402 signing boundary
  10 OTEL        -> Jaeger span coverage

What this script does
---------------------
1. Load the canonical `security-autonomy-fixer.yaml` (24 nodes, 11 phases).
2. POST flow definition via API Server (`/agentflows`).
3. Trigger a run and poll status via REST.
4. White-box checks:
   - PostgreSQL: `orh_agentflow`, `orh_flowrun`, `task_runs`
   - REST: `/runs/{id}/tasks` task list and outputs
   - EMQX: opportunistic capture of notify-related MQTT traffic

Why it looks PG-heavy
---------------------
Early versions focused on DB persistence because that is the fastest signal that
apiserver -> store -> run lifecycle works. REST task polling and MQTT checks are
included but shallow; deep per-node assertions live in 05/06/07/10. This scenario
answers: "did the full canonical flow get imported, triggered, and leave traces
in PG + API + bus?"
"""

import requests
import time
import sys
import os
import json
import yaml

import config

API = config.K3S_APISERVER_URL
TENANT = config.K3S_TENANT
FLOW_ID = "security-autonomy-fixer"
FLOW_TIMEOUT_S = config.FLOW_TIMEOUT_S
POLL_INTERVAL_S = config.POLL_INTERVAL_S

# Canonical flow YAML: scenarios/ -> security-autonomy-fixer/config/flows/
_FLOW_YAML_PATH = os.path.join(
    os.path.dirname(__file__), "..", "..", "config", "flows", "security-autonomy-fixer.yaml"
)

# 11 phases × key nodes (matches config/flows/security-autonomy-fixer.yaml)
PHASE_NODES = {
    "DISCOVERY": ["get-commit", "scan-sonarqube"],
    "ANALYZE": ["aggregate-issues"],
    "FIX": ["generate-fixes"],
    "REVIEW": ["review-security", "review-quality", "review-arch"],
    "VOTE": ["tribunal"],
    "SUPERVISOR": ["supervisor-check"],
    "CONDITION": ["is-approved"],
    "HUMAN": ["human-approval"],
    "COMMIT_PR": ["create-branch", "commit-fixes", "create-pr"],
    "RESCAN": ["trigger-rescan", "wait-rescan", "check-resolved", "compare-results", "fix-complete"],
    "REPORT": ["summary-report", "notify-pr", "notify-email", "notify-teams", "end"],
}
MIN_EXECUTED_NODES = 15


def load_flow_from_yaml():
    """Load flow definition from the canonical YAML file, stripping non-API fields."""
    with open(_FLOW_YAML_PATH) as f:
        data = yaml.safe_load(f)
    for key in ("triggers",):
        data.pop(key, None)
    return data


def try_approve_pending_human(s, run_id, conn):
    """Auto-approve human gate if the run is waiting (test env convenience)."""
    token = None
    if conn:
        cur = conn.cursor()
        cur.execute(
            "SELECT token FROM human_approvals WHERE agentflow_run_id=%s AND status='PENDING' LIMIT 1",
            (run_id,),
        )
        row = cur.fetchone()
        if row:
            token = row[0]
    if not token:
        r = s.get(f"{API}/api/v1/human/approvals")
        if r.status_code == 200:
            for item in r.json() or []:
                if item.get("agentflow_run_id") == run_id and item.get("status") == "PENDING":
                    token = item.get("token")
                    break
    if token:
        r = s.post(f"{API}/api/v1/human/{token}/approve", json={"comment": "Approved by e2e verifier"})
        print(f"  OK auto-approved human gate (token={token[:12]}...) status={r.status_code}")
        return r.status_code == 200
    return False


def verify_phase_checkpoints(tasks_by_node):
    """Report per-phase node execution coverage."""
    print("\n-- [9] Phase checkpoint coverage --")
    phases_ok = 0
    for phase, node_ids in PHASE_NODES.items():
        executed = [n for n in node_ids if tasks_by_node.get(n) in ("COMPLETED", "FAILED")]
        skipped = [n for n in node_ids if n not in tasks_by_node]
        if executed:
            phases_ok += 1
            print(f"  OK {phase:<12} executed={executed}")
        elif skipped:
            print(f"  -- {phase:<12} skipped/not reached: {skipped}")
        else:
            pending = [n for n in node_ids if tasks_by_node.get(n) not in ("COMPLETED", "FAILED")]
            print(f"  WARN {phase:<12} pending/incomplete: {pending}")
    return phases_ok


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
        try:
            alt = dsn.replace("port=5432", "port=5433")
            conn = psycopg2.connect(alt)
            conn.autocommit = True
            return conn
        except Exception:
            return None


def run():
    print("\n" + "=" * 60)
    print("  Scenario 11: E2E Security Fixer - Full Pipeline (capstone)")
    print("=" * 60)

    s = requests.Session()
    s.headers["Content-Type"] = "application/json"

    flow_def = load_flow_from_yaml()
    node_count = len(flow_def.get("nodes", []))
    edge_count = len(flow_def.get("edges", []))
    print(f"\n-- Loaded flow: {FLOW_ID} ({node_count} nodes, {edge_count} edges)")

    # -- 1. Create flow definition --
    print("\n-- [1] Creating flow definition (API) --")
    s.delete(f"{API}/api/v1/{TENANT}/agentflows/{FLOW_ID}")
    time.sleep(0.5)

    r = s.post(f"{API}/api/v1/{TENANT}/agentflows", json=flow_def)
    if r.status_code not in (200, 201):
        raise AssertionError(f"create flow returned {r.status_code}: {r.text[:200]}")
    print(f"  OK CREATE: {FLOW_ID} (status={r.status_code})")

    # -- 2. PG: flow definition persisted --
    print("\n-- [2] PG: flow definition persisted --")
    conn = pg_connect()
    if not conn:
        print("  SKIP: cannot connect to PG")
    else:
        cur = conn.cursor()
        cur.execute(
            "SELECT agentflow_id, priority, tenant_id, namespace FROM orh_agentflow WHERE agentflow_id=%s",
            (FLOW_ID,),
        )
        row = cur.fetchone()
        if row:
            print(f"  OK PG orh_agentflow: id={row[0]} priority={row[1]} tenant={row[2]}")
        else:
            raise AssertionError(f"flow {FLOW_ID} not found in orh_agentflow")

    # -- 3. Trigger run --
    print("\n-- [3] Triggering flow (API) --")
    r = s.post(
        f"{API}/api/v1/{TENANT}/agentflows/trigger",
        json={"agentflow_id": FLOW_ID, "vars": {}, "trigger": {"type": "api"}},
    )
    if r.status_code != 200:
        if conn:
            conn.close()
        raise AssertionError(f"trigger returned {r.status_code}: {r.text[:200]}")
    run_data = r.json()
    run_id = run_data.get("run_id") or run_data.get("id", "")
    if not run_id:
        raise AssertionError(f"no run_id in trigger response: {run_data}")
    print(f"  OK TRIGGER: run_id={run_id[:36]}... status={run_data.get('status', '?')}")

    # -- 4. PG: run persisted --
    print("\n-- [4] PG: run persisted --")
    if conn:
        cur.execute(
            "SELECT id, status, agentflow_id, priority FROM orh_flowrun WHERE id=%s",
            (run_id,),
        )
        row = cur.fetchone()
        if row:
            print(f"  OK PG orh_flowrun: status={row[1]} flow={row[2]}")
        else:
            print("  WARN: run not yet in PG (may need commit delay)")

    # -- 5. Poll run status via REST --
    print(f"\n-- [5] Monitoring execution (up to {FLOW_TIMEOUT_S}s) --")
    status = "PENDING"
    polls = max(1, FLOW_TIMEOUT_S // POLL_INTERVAL_S)
    for i in range(polls):
        time.sleep(POLL_INTERVAL_S)
        r = s.get(f"{API}/api/v1/{TENANT}/runs/{run_id}")
        if r.status_code == 200:
            status = r.json().get("status", "?")
            print(f"  [{i * POLL_INTERVAL_S}s] status={status}")
            if status in ("RUNNING", "PAUSED"):
                try_approve_pending_human(s, run_id, conn)
            if status in ("COMPLETED", "FAILED", "CANCELLED"):
                break
        else:
            print(f"  [{i * POLL_INTERVAL_S}s] GET returned {r.status_code}")
    print(f"  Final status: {status}")

    # -- 6. PG: task_runs --
    print("\n-- [6] PG: task_runs --")
    if conn:
        cur.execute(
            "SELECT node_id, status, output IS NOT NULL AS has_output "
            "FROM task_runs WHERE agentflow_run_id=%s ORDER BY sequence",
            (run_id,),
        )
        tasks = cur.fetchall()
        if tasks:
            for t in tasks:
                print(f"  task: {t[0]:<22} status={t[1]:<12} has_output={t[2]}")
        else:
            print("  WARN: no task_runs rows yet")

    # -- 7. REST: task list + phase checkpoints --
    print("\n-- [7] REST: /runs/{id}/tasks --")
    tasks_by_node = {}
    r = s.get(f"{API}/api/v1/{TENANT}/runs/{run_id}/tasks")
    if r.status_code == 200:
        tasks = r.json()
        if isinstance(tasks, list):
            for t in tasks:
                node_id = t.get("node_id", "?")
                t_status = t.get("status", "?")
                tasks_by_node[node_id] = t_status
                has_output = bool(t.get("output"))
                print(f"  task: {node_id:<22} status={t_status:<12} has_output={has_output}")
            if not tasks:
                print("  WARN: empty task list from REST")
            else:
                executed = [n for n, st in tasks_by_node.items() if st in ("COMPLETED", "FAILED")]
                print(f"  Executed nodes: {len(executed)}/{node_count}")
                if len(executed) < MIN_EXECUTED_NODES:
                    print(f"  WARN: expected >= {MIN_EXECUTED_NODES} executed nodes (flow may have short-circuited)")
                verify_phase_checkpoints(tasks_by_node)
                # Key output assertions (best-effort; nodes may fail in test env)
                for node_id, keys in (
                    ("get-commit", ("commit_sha",)),
                    ("scan-sonarqube", ("issues",)),
                    ("generate-fixes", ("patches",)),
                    ("tribunal", ("decision",)),
                ):
                    task = next((t for t in tasks if t.get("node_id") == node_id), None)
                    if task and task.get("status") == "COMPLETED" and task.get("output"):
                        missing = [k for k in keys if k not in task["output"]]
                        if missing:
                            print(f"  WARN {node_id}: output missing keys {missing}")
                        else:
                            print(f"  OK {node_id}: output keys present {keys}")
        else:
            print(f"  tasks response: {tasks}")
    else:
        print(f"  GET tasks: {r.status_code}")

    # -- 8. EMQX: opportunistic notify capture --
    print("\n-- [8] EMQX: notify traffic (opportunistic) --")
    try:
        import paho.mqtt.client as mqtt

        messages = []

        def on_message(client, userdata, msg):
            messages.append(msg.payload)

        client = mqtt.Client(mqtt.CallbackAPIVersion.VERSION2)
        client.on_message = on_message
        try:
            client.connect(config.EMQX_HOST, config.EMQX_PORT, 5)
            client.subscribe("flowgent/notify/queue/#")
            client.loop_start()
            time.sleep(2)
            client.loop_stop()
            client.disconnect()
            if messages:
                for m in messages[:5]:
                    print(f"  MQTT msg: {m[:200]}")
            else:
                print("  No MQTT notify messages captured (flow may not have reached notify phase)")
        except Exception as e:
            print(f"  MQTT connect failed: {e}")
    except ImportError:
        print("  SKIP: paho-mqtt not installed")

    if conn:
        conn.close()

    # Final run-level assertion (allow FAILED in test env lacking SonarQube/LLM)
    r = s.get(f"{API}/api/v1/{TENANT}/runs/{run_id}")
    if r.status_code == 200:
        run = r.json()
        if run.get("status") not in ("COMPLETED", "FAILED", "CANCELLED"):
            raise AssertionError(f"run still active: status={run.get('status')}")
        if run.get("status") == "COMPLETED":
            print(f"  OK run COMPLETED with {len(tasks_by_node)} task records")
        else:
            print(f"  WARN run ended with status={run.get('status')} (external deps may be unavailable)")

    print("\n-- Done --")
    print(f"  Flow {FLOW_ID} left in place for manual inspection")
    print(f"  Run ID: {run_id}")
    print("  For deep checks use scenarios 04-10 before relying on this capstone alone.")


if __name__ == "__main__":
    run()
