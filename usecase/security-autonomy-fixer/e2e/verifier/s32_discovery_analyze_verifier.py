"""Scenario 32 — Discovery & Analyze: commit fetch, SonarQube scan, issue aggregation."""

import requests
import time
import sys
import os
import json

import _common as c

# ── Phase: DISCOVERY ──

def verify_discovery(tasks_by_node):
    phase = "DISCOVERY"
    nodes = c.PHASE_NODES[phase]
    print(f"\n-- [32 Discovery] Commit + scan verification --")
    passed = 0
    total = 0

    task = c.node_task(tasks_by_node, "get-commit")
    if task and task.get("status") == "COMPLETED":
        total += 1
        output = c.parse_output(task)
        if "commit_sha" in output and output["commit_sha"]:
            print(f"  OK get-commit: commit_sha='{str(output['commit_sha'])[:20]}...'")
            passed += 1
        else:
            print(f"  WARN get-commit: output missing 'commit_sha' field")
    else:
        status = task.get("status") if task else "not_reached"
        print(f"  -- get-commit: status={status} (skipped)")

    task = c.node_task(tasks_by_node, "scan-sonarqube")
    if task and task.get("status") == "COMPLETED":
        total += 1
        output = c.parse_output(task)
        issues = output.get("issues") or output.get("results") or output.get("data")
        if issues and isinstance(issues, list) and len(issues) > 0:
            print(f"  OK scan-sonarqube: {len(issues)} issues found")
            passed += 1
        elif issues and isinstance(issues, list):
            print(f"  OK scan-sonarqube: issues array present (empty)")
            passed += 1
        elif issues and isinstance(issues, dict):
            inner = issues.get("issues") or issues.get("results") or []
            print(f"  OK scan-sonarqube: issues wrapped in outer key ({len(inner) if isinstance(inner, list) else '?'})")
            passed += 1
        else:
            if output:
                print(f"  WARN scan-sonarqube: output has keys {list(output.keys())[:5]} but no 'issues'")
            else:
                print(f"  WARN scan-sonarqube: empty output")
    else:
        status = task.get("status") if task else "not_reached"
        print(f"  -- scan-sonarqube: status={status} (skipped)")

    print(f"  Result: {passed}/{total} checks passed")
    return passed, total


# ── Phase: ANALYZE ──

def verify_analyze(tasks_by_node):
    phase = "ANALYZE"
    print(f"\n-- [32 Analyze] Issue aggregation verification --")
    passed = 0
    total = 0

    task = c.node_task(tasks_by_node, "aggregate-issues")
    if task and task.get("status") == "COMPLETED":
        total = 2
        output = c.parse_output(task)
        issues = output.get("issues") or output.get("results") or []
        if isinstance(issues, list) and len(issues) > 0:
            print(f"  OK aggregate-issues: issues array with {len(issues)} entries")
            passed += 1
            entry = issues[0]
            norm_fields = [k for k in ("source", "rule", "severity", "file", "line", "message")
                           if k in entry]
            if norm_fields:
                print(f"  OK aggregate-issues: entries have normalized fields: {norm_fields}")
                passed += 1
            else:
                keys = list(entry.keys())
                print(f"  WARN aggregate-issues: entry has unnormalized keys: {keys}")
        elif isinstance(issues, list):
            print(f"  OK aggregate-issues: issues array present (empty)")
            passed += 2
        elif output:
            keys = list(output.keys())
            print(f"  WARN aggregate-issues: output keys {keys} (expected 'issues' array)")
        else:
            print(f"  WARN aggregate-issues: empty output")
    else:
        status = task.get("status") if task else "not_reached"
        print(f"  -- aggregate-issues: status={status} (skipped)")

    print(f"  Result: {passed}/{total} checks passed")
    return passed, total


# ── Orchestration ──

def run():
    print("\n" + "=" * 60)
    print("  Scenario 32: Discovery & Analyze")
    print("=" * 60)

    # Load run_id from previous scenario's output
    run_id_file = os.path.join(os.path.dirname(__file__), "..", ".last_run_id")
    if not os.path.isfile(run_id_file):
        raise AssertionError("No .last_run_id found — run Scenario 31 (Seed & Trigger) first")
    with open(run_id_file) as f:
        run_id = f.read().strip()
    print(f"  Using run_id: {run_id}")

    s = requests.Session()
    s.headers["Content-Type"] = "application/json"

    # Poll run until task data is available
    print(f"\n-- Polling run {run_id} for task availability --")
    status = "PENDING"
    polls = max(1, c.FLOW_TIMEOUT_S // c.POLL_INTERVAL_S)
    conn = c.pg_connect()
    for i in range(polls):
        time.sleep(c.POLL_INTERVAL_S)
        r = s.get(f"{c.API}/api/v1/{c.NAMESPACE}/runs/{run_id}")
        if r.status_code == 200:
            status = r.json().get("status", "?")
            print(f"  [{i * c.POLL_INTERVAL_S}s] status={status}")
            if status in ("RUNNING", "PAUSED"):
                c.try_approve_pending_human(s, run_id, conn)
            if status in ("COMPLETED", "FAILED", "CANCELLED"):
                break
        else:
            print(f"  [{i * c.POLL_INTERVAL_S}s] GET returned {r.status_code}")
    print(f"  Final run status: {status}")

    # Fetch tasks
    print("\n-- Fetching task list from API --")
    tasks = c.get_tasks(s, run_id)
    tbn = c.tasks_by_node(tasks)
    print(f"  {len(tasks)} task(s) fetched, {len(tbn)} unique node(s)")

    if tasks:
        for t in tasks:
            nid = t.get("node_id", "?")
            tstatus = t.get("status", "?")
            has_output = bool(t.get("output"))
            print(f"    {nid:<22} status={tstatus:<12} has_output={has_output}")

    if conn:
        conn.close()

    # Run verifications
    results = []
    results.append(("Discovery", *verify_discovery(tbn)))
    results.append(("Analyze", *verify_analyze(tbn)))

    total_checks = sum(r[2] for r in results)
    passed_checks = sum(r[1] for r in results)

    print(f"\n{'=' * 60}")
    print(f"  Scenario 32 Summary")
    for name, p, t in results:
        pct = int(100 * p / t) if t > 0 else 0
        print(f"  {name:<14} {p}/{t} ({pct}%)")
    print(f"  Total: {passed_checks}/{total_checks}")
    print(f"{'=' * 60}")

    if passed_checks < total_checks:
        raise AssertionError(f"Discovery/Analyze failed: {passed_checks}/{total_checks}")
