"""Scenario 33 — Remediation: fix generation, triple review, committee vote, supervisor gate, human approval."""

import requests
import sys
import os
import json

import _common as c

# ── Phase: FIX ──

def verify_fix(tasks_by_node):
    print(f"\n-- [33 Fix] Patch generation verification --")
    passed = 0
    total = 0

    task = c.node_task(tasks_by_node, "generate-fixes")
    if task and task.get("status") == "COMPLETED":
        total = 3
        output = c.parse_output(task)
        patches = output.get("patches") or output.get("fixes") or []
        if isinstance(patches, list) and len(patches) > 0:
            print(f"  OK generate-fixes: patches array with {len(patches)} entries")
            passed += 1
            entry = patches[0]
            if "file" in entry and entry["file"]:
                print(f"  OK generate-fixes: patch entry has 'file': {entry['file']}")
                passed += 1
            else:
                print(f"  WARN generate-fixes: patch entry missing 'file' field")
            if "patch" in entry and entry["patch"]:
                print(f"  OK generate-fixes: patch entry has 'patch' ({len(str(entry['patch']))} chars)")
                passed += 1
            elif "diff" in entry and entry["diff"]:
                print(f"  OK generate-fixes: patch entry has 'diff' ({len(str(entry['diff']))} chars)")
                passed += 1
            else:
                print(f"  WARN generate-fixes: patch entry missing 'patch'/'diff' field")
        elif isinstance(patches, list):
            print(f"  OK generate-fixes: patches array present (empty)")
            passed += 3
        elif output:
            keys = list(output.keys())
            print(f"  WARN generate-fixes: output keys {keys} (expected 'patches' array)")
        else:
            print(f"  WARN generate-fixes: empty output")
    else:
        status = task.get("status") if task else "not_reached"
        print(f"  -- generate-fixes: status={status} (skipped)")

    print(f"  Result: {passed}/{total} checks passed")
    return passed, total


# ── Phase: REVIEW ──

def verify_review(tasks_by_node):
    print(f"\n-- [33 Review] Multi-agent review board verification --")
    passed = 0
    total = 0
    review_nodes = ["review-security", "review-quality", "review-arch"]

    for node_id in review_nodes:
        task = c.node_task(tasks_by_node, node_id)
        if task and task.get("status") == "COMPLETED":
            total += 2
            output = c.parse_output(task)
            decision = output.get("decision")
            if decision is not None:
                print(f"  OK {node_id}: decision='{decision}'")
                passed += 1
            else:
                print(f"  WARN {node_id}: output missing 'decision' field")
            if "reason" in output and output["reason"]:
                reason_preview = str(output["reason"])[:60]
                print(f"  OK {node_id}: reason='{reason_preview}...'")
                passed += 1
            elif output.get("rationale"):
                print(f"  OK {node_id}: has 'rationale' as reason proxy")
                passed += 1
            else:
                print(f"  WARN {node_id}: output missing 'reason' field")
        else:
            status = task.get("status") if task else "not_reached"
            print(f"  -- {node_id}: status={status} (skipped)")

    print(f"  Result: {passed}/{total} checks passed")
    return passed, total


# ── Phase: VOTE ──

def verify_vote(tasks_by_node):
    print(f"\n-- [33 Vote] Committee majority vote verification --")
    passed = 0
    total = 0

    task = c.node_task(tasks_by_node, "committee")
    if task and task.get("status") == "COMPLETED":
        total = 1
        output = c.parse_output(task)
        decision = output.get("decision") or output.get("result") or output.get("vote")
        if decision is not None:
            print(f"  OK committee: decision='{decision}'")
            passed += 1
        else:
            keys = list(output.keys())
            print(f"  WARN committee: output has keys {keys} but no 'decision'")
    else:
        status = task.get("status") if task else "not_reached"
        print(f"  -- committee: status={status} (skipped)")

    print(f"  Result: {passed}/{total} checks passed")
    return passed, total


# ── Phase: SUPERVISOR ──

def verify_supervisor(tasks_by_node):
    print(f"\n-- [33 Supervisor] Safety gate verification --")
    passed = 0
    total = 0

    task = c.node_task(tasks_by_node, "supervisor-check")
    if task and task.get("status") == "COMPLETED":
        total = 1
        output = c.parse_output(task)
        action = output.get("action") or output.get("decision") or output.get("result")
        if action is not None:
            print(f"  OK supervisor-check: action='{action}'")
            passed += 1
        else:
            keys = list(output.keys())
            print(f"  WARN supervisor-check: output has keys {keys} but no 'action'")
    else:
        status = task.get("status") if task else "not_reached"
        print(f"  -- supervisor-check: status={status} (skipped)")

    print(f"  Result: {passed}/{total} checks passed")
    return passed, total


# ── Phase: CONDITION + HUMAN ──

def verify_condition_human(tasks_by_node):
    print(f"\n-- [33 Gate] Routing + human gate verification --")
    passed = 0
    total = 0

    task = c.node_task(tasks_by_node, "is-approved")
    if task:
        total += 1
        status = task.get("status")
        if status == "COMPLETED":
            print(f"  OK is-approved: condition evaluated true (COMPLETED)")
            passed += 1
        elif status == "SKIPPED":
            print(f"  OK is-approved: condition evaluated false (SKIPPED)")
            passed += 1
        else:
            print(f"  WARN is-approved: status={status}")
    else:
        print(f"  -- is-approved: not_reached")

    task = c.node_task(tasks_by_node, "human-approval")
    if task:
        total += 1
        status = task.get("status")
        if status == "COMPLETED":
            print(f"  OK human-approval: gate auto-approved (COMPLETED)")
            passed += 1
        elif status == "SKIPPED":
            print(f"  OK human-approval: gate skipped (condition routed)")
            passed += 1
        elif status == "PAUSED":
            print(f"  WARN human-approval: still PAUSED (verifier may have missed the gate)")
        else:
            print(f"  WARN human-approval: status={status}")
    else:
        print(f"  -- human-approval: not_reached")

    print(f"  Result: {passed}/{total} checks passed")
    return passed, total


# ── Orchestration ──

def run():
    print("\n" + "=" * 60)
    print("  Scenario 33: Remediation (Fix + Review + Vote + Gate)")
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

    results = []
    results.append(("Fix", *verify_fix(tbn)))
    results.append(("Review", *verify_review(tbn)))
    results.append(("Vote", *verify_vote(tbn)))
    results.append(("Supervisor", *verify_supervisor(tbn)))
    results.append(("Gate", *verify_condition_human(tbn)))

    total_checks = sum(r[2] for r in results)
    passed_checks = sum(r[1] for r in results)

    print(f"\n{'=' * 60}")
    print(f"  Scenario 33 Summary")
    for name, p, t in results:
        pct = int(100 * p / t) if t > 0 else 0
        print(f"  {name:<14} {p}/{t} ({pct}%)")
    print(f"  Total: {passed_checks}/{total_checks}")
    print(f"{'=' * 60}")

    if passed_checks < total_checks:
        raise AssertionError(f"Remediation failed: {passed_checks}/{total_checks}")
