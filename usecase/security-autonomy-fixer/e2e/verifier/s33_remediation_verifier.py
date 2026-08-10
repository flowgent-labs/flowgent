"""Scenario 33 — Remediation: fix generation, triple review, committee vote, supervisor gate, human approval."""

import requests
import sys
import os
import json

from verifier import _common as c

COMPLETED_SET = c.COMPLETED_STATUSES

# ── Phase: FIX ──

def verify_fix(tasks_by_node):
    print(f"\n-- [33 Fix] Patch generation verification --")
    passed = 0
    total = 4

    task = c.node_task(tasks_by_node, "generate-fixes")
    if c.task_completed(task):
        output = c.parse_output(task)
        files = output.get("files") or []
        if isinstance(files, list) and len(files) > 0:
            print(f"  OK generate-fixes: files array with {len(files)} entries")
            passed += 1
            first_file = files[0]
            if first_file.get("path") and first_file.get("content"):
                print(f"  OK generate-fixes: file entry has path+content ({len(str(first_file.get('content')))} chars)")
                passed += 1
            else:
                print(f"  WARN generate-fixes: file entry missing path/content")
        elif isinstance(files, list):
            print(f"  WARN generate-fixes: files array present but empty")
        else:
            print(f"  WARN generate-fixes: output missing 'files' array")

        patches = output.get("patches") or output.get("fixes") or []
        if isinstance(patches, list) and len(patches) > 0:
            print(f"  OK generate-fixes: patches array with {len(patches)} entries")
            passed += 1
            entry = patches[0]
            if entry.get("file") and entry.get("rule") and entry.get("description"):
                print(f"  OK generate-fixes: patch metadata has file/rule/description")
                passed += 1
            else:
                print(f"  WARN generate-fixes: patch metadata incomplete: {list(entry.keys())}")
        elif isinstance(patches, list):
            print(f"  WARN generate-fixes: patches array present but empty")
        elif output:
            keys = list(output.keys())
            print(f"  WARN generate-fixes: output keys {keys} (expected files+patches arrays)")
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
    review_nodes = ["review-security", "review-quality", "review-arch"]
    total = len(review_nodes) * 2

    for node_id in review_nodes:
        task = c.node_task(tasks_by_node, node_id)
        if c.task_completed(task):
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
    total = 1

    task = c.node_task(tasks_by_node, "committee")
    if c.task_completed(task):
        output = c.parse_output(task)
        if "decision" in output:
            decision = output["decision"]
            print(f"  OK committee: decision='{decision}'")
            passed += 1
        elif "result" in output:
            decision = output["result"]
            print(f"  OK committee: result='{decision}'")
            passed += 1
        elif "vote" in output:
            decision = output["vote"]
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
    total = 1

    task = c.node_task(tasks_by_node, "supervisor-check")
    if c.task_completed(task):
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
    total = 2

    task = c.node_task(tasks_by_node, "is-approved")
    if task:
        status = task.get("status")
        if status in COMPLETED_SET:
            print(f"  OK is-approved: condition evaluated true ({status})")
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
        status = task.get("status")
        if status in COMPLETED_SET:
            print(f"  OK human-approval: gate auto-approved ({status})")
            passed += 1
        elif status == "SKIPPED":
            print(f"  OK human-approval: gate skipped (condition routed)")
            passed += 1
        elif status == "PAUSED":
            print(f"  WARN human-approval: still PAUSED (verifier may have missed the gate)")
        else:
            print(f"  WARN human-approval: status={status}")
    else:
        print(f"  OK human-approval: not reached on this conditional path")
        passed += 1

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

    print("\n-- [33 MQTT Audit] Slot-worker status and dependency wave order --")
    audit_messages = c.assert_mqtt_suffixes(run_id, ["exec/plans", "exec/results"])
    plan_nodes = [
        c.message_node_id(m)
        for m in audit_messages
        if m.get("topic", "").endswith("exec/plans")
    ]
    result_nodes = [
        c.message_node_id(m)
        for m in audit_messages
        if m.get("topic", "").endswith("exec/results")
    ]
    expected_nodes = ["generate-fixes", "review-security", "review-quality", "review-arch", "committee"]
    missing_plan = [n for n in expected_nodes if n not in plan_nodes]
    missing_result = [n for n in expected_nodes if n not in result_nodes]
    print(f"  exec/plans nodes observed: {len(set(plan_nodes))}")
    print(f"  exec/results nodes observed: {len(set(result_nodes))}")
    if missing_plan or missing_result:
        raise AssertionError(f"Missing remediation MQTT nodes: plans={missing_plan}, results={missing_result}")

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
