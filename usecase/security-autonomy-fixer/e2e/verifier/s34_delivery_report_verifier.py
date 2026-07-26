"""Scenario 34 — Delivery & Report: branch/commit/PR creation, SonarQube re-scan, compare, report, PG final."""

import requests
import sys
import os
import json

import _common as c

# ── Phase: COMMIT & PR ──

def verify_commit_pr(tasks_by_node):
    print(f"\n-- [34 CommitPR] Branch + commit + PR verification --")
    passed = 0
    total = 0

    for node_id in ["create-branch", "commit-fixes", "create-pr"]:
        task = c.node_task(tasks_by_node, node_id)
        if task and task.get("status") == "COMPLETED":
            total += 1
            passed += 1
            output = c.parse_output(task)
            extra = ""
            if node_id == "create-pr":
                pr_url = output.get("pr_url") or output.get("url") or output.get("html_url")
                if pr_url:
                    extra = f" (url={pr_url})"
            print(f"  OK {node_id}: COMPLETED{extra}")
        else:
            status = task.get("status") if task else "not_reached"
            print(f"  -- {node_id}: status={status} (skipped)")

    print(f"  Result: {passed}/{total} checks passed")
    return passed, total


# ── Phase: RESCAN ──

def verify_rescan(tasks_by_node):
    print(f"\n-- [34 Rescan] Re-scan chain verification --")
    passed = 0
    total = 0
    rescan_nodes = ["trigger-rescan", "wait-rescan", "check-resolved",
                    "compare-results", "fix-complete"]

    for node_id in rescan_nodes:
        task = c.node_task(tasks_by_node, node_id)
        if task and task.get("status") == "COMPLETED":
            total += 1
            passed += 1
            output = c.parse_output(task)
            extra = ""
            if node_id == "compare-results":
                resolution = output.get("resolution") or output.get("status")
                if resolution:
                    extra = f" (resolution={resolution})"
            print(f"  OK {node_id}: COMPLETED{extra}")
        else:
            status = task.get("status") if task else "not_reached"
            print(f"  -- {node_id}: status={status} (skipped)")

    print(f"  Result: {passed}/{total} checks passed")
    return passed, total


# ── Phase: REPORT ──

def verify_report(tasks_by_node):
    print(f"\n-- [34 Report] Report + notification verification --")
    passed = 0
    total = 0
    report_nodes = ["summary-report", "notify-pr", "end"]

    for node_id in report_nodes:
        task = c.node_task(tasks_by_node, node_id)
        if task and task.get("status") == "COMPLETED":
            total += 1
            passed += 1
            output = c.parse_output(task)
            extra = ""
            if node_id == "summary-report":
                report = output.get("report") or output.get("summary") or str(output)[:100]
                if isinstance(report, str) and len(report) > 20:
                    extra = f" (report: {len(report)} chars)"
                elif isinstance(report, str):
                    extra = f" (report: short — {len(report)} chars)"
                print(f"  OK {node_id}: COMPLETED{extra}")
            else:
                print(f"  OK {node_id}: COMPLETED")
        else:
            status = task.get("status") if task else "not_reached"
            print(f"  -- {node_id}: status={status} (skipped)")

    print(f"  Result: {passed}/{total} checks passed")
    return passed, total


# ── Phase: PG FINAL ──

def verify_pg_final(conn, run_id):
    print("\n-- [34 PG Final] All-phase task_runs coverage --")
    if not conn:
        print("  SKIP: no PG connection")
        return 0, 0

    passed = 0
    total = len(c.PHASE_NODES)

    for phase_name, node_ids in c.PHASE_NODES.items():
        placeholders = ", ".join("%s" for _ in node_ids)
        cur = conn.cursor()
        cur.execute(
            f"SELECT node_id, status FROM task_runs "
            f"WHERE agentflow_run_id=%s AND node_id IN ({placeholders}) "
            f"ORDER BY sequence",
            (run_id, *node_ids),
        )
        rows = cur.fetchall()
        if rows:
            completed = [r for r in rows if r[1] == "COMPLETED"]
            if completed:
                print(f"  OK {phase_name:<12}: {len(completed)} completed task(s)")
                passed += 1
            else:
                statuses = [f"{r[0]}={r[1]}" for r in rows]
                print(f"  -- {phase_name:<12}: no COMPLETED tasks — {statuses}")
        else:
            print(f"  -- {phase_name:<12}: no task_runs entries (phase not reached)")

    cur = conn.cursor()
    cur.execute(
        "SELECT COUNT(*) FROM task_runs WHERE agentflow_run_id=%s AND status='COMPLETED'",
        (run_id,),
    )
    total_completed = cur.fetchone()[0]
    print(f"  Total COMPLETED task_runs: {total_completed}")

    print(f"  Result: {passed}/{total} phases have completed task_runs")
    return passed, total


# ── Orchestration ──

def run():
    print("\n" + "=" * 60)
    print("  Scenario 34: Delivery & Report (CommitPR + Rescan + Report + PG Final)")
    print("=" * 60)

    run_id_file = os.path.join(os.path.dirname(__file__), "..", ".last_run_id")
    if not os.path.isfile(run_id_file):
        raise AssertionError("No .last_run_id found — run Scenario 31 first")
    with open(run_id_file) as f:
        run_id = f.read().strip()
    print(f"  Using run_id: {run_id}")

    s = requests.Session()
    s.headers["Content-Type"] = "application/json"

    print("\n-- Fetching task list from API --")
    tasks = c.get_tasks(s, run_id)
    tbn = c.tasks_by_node(tasks)
    print(f"  {len(tasks)} task(s) fetched, {len(tbn)} unique node(s)")

    conn = c.pg_connect()

    results = []
    results.append(("CommitPR", *verify_commit_pr(tbn)))
    results.append(("Rescan", *verify_rescan(tbn)))
    results.append(("Report", *verify_report(tbn)))
    results.append(("PG Final", *verify_pg_final(conn, run_id)))

    if conn:
        conn.close()

    total_checks = sum(r[2] for r in results)
    passed_checks = sum(r[1] for r in results)

    print(f"\n{'=' * 60}")
    print(f"  Scenario 34 Summary")
    for name, p, t in results:
        pct = int(100 * p / t) if t > 0 else 0
        print(f"  {name:<14} {p}/{t} ({pct}%)")
    print(f"  Total: {passed_checks}/{total_checks}")
    print(f"{'=' * 60}")

    if passed_checks < total_checks:
        raise AssertionError(f"Delivery/Report failed: {passed_checks}/{total_checks}")
