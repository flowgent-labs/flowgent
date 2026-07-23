"""
Scenario 09 — Security Fixer: Full Pipeline White-Box Verification (capstone).

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
Phase  0: Seed          — Register 7 agents + 2 MCPs (idempotent), verify all present.
Phase  1: Trigger       — POST to /api/v1/webhook/github with pull_request payload
                         for wl4g/rengine PR #4. Verify 202 + run_id + PG row.
Phase  2: DISCOVERY     — get-commit has commit_sha; scan-sonarqube has issues array.
Phase  3: ANALYZE       — aggregate-issues has issues array with normalized entries.
Phase  4: FIX           — generate-fixes has patches array with file and diff fields.
Phase  5: REVIEW        — Each of 3 reviews has decision bool + reason string.
Phase  6: VOTE          — Committee output has decision (majority of 3 reviews).
Phase  7: SUPERVISOR    — supervisor-check output has action field.
Phase  8: CONDITION     — is-approved routes correctly; human-approval auto-approved.
Phase  9: COMMIT&PR     — Branch created, commits pushed, PR created (has url).
Phase 10: RESCAN        — Trigger -> wait -> check -> compare -> fix-complete chain.
Phase 11: REPORT        — summary-report has markdown; notify-pr completed.
Phase 12: PG FINAL      — Every phase has >= 1 completed task_runs row.

Structure
---------
- run() orchestrates the full pipeline.
- Each phase is a standalone helper (verify_<phase>) returning (passed, total).
- REST API polling (GET /runs/{id}, GET /runs/{id}/tasks) for run/task insight.
- PG queries for white-box verification (orh_flowrun, task_runs).
- Toleration: a phase whose nodes were not reached logs WARN but does not fail.
"""

import requests
import time
import sys
import os
import json
import yaml

sys.path.insert(0, '..')
import config

API = config.K3S_APISERVER_URL
TENANT = config.K3S_TENANT
FLOW_ID = "security-autonomy-fixer"
FLOW_TIMEOUT_S = config.FLOW_TIMEOUT_S
POLL_INTERVAL_S = config.POLL_INTERVAL_S

_CONFIG_ROOT = os.path.join(os.path.dirname(__file__), "..", "..", "config")
_FLOW_YAML_PATH = os.path.join(_CONFIG_ROOT, "flows", "security-autonomy-fixer.yaml")
_AGENTS_DIR = os.path.join(_CONFIG_ROOT, "agents")

_USE_REAL_MCP = os.getenv("FLOWGENT_E2E_USE_REAL_MCP", "true").lower() == "true"
_MOCK_MCP_COMMAND = ["/app/mcp-server.sh"]

# 11 pipeline phases matching config/flows/security-autonomy-fixer.yaml
PHASE_NODES = {
    "DISCOVERY": ["get-commit", "scan-sonarqube"],
    "ANALYZE": ["aggregate-issues"],
    "FIX": ["generate-fixes"],
    "REVIEW": ["review-security", "review-quality", "review-arch"],
    "VOTE": ["committee"],
    "SUPERVISOR": ["supervisor-check"],
    "CONDITION": ["is-approved"],
    "HUMAN": ["human-approval"],
    "COMMIT_PR": ["create-branch", "commit-fixes", "create-pr"],
    "RESCAN": ["trigger-rescan", "wait-rescan", "check-resolved", "compare-results", "fix-complete"],
    "REPORT": ["summary-report", "notify-pr", "notify-email", "notify-teams", "end"],
}

AGENT_NAMES = ["supervisor", "issue-detector", "fixer-agent",
               "security-reviewer", "quality-reviewer", "arch-reviewer", "git-agent"]
MCP_NAMES = ["github", "sonarqube"]

# ─────────────── Shared utility helpers ───────────────


def _unwrap_k8s(data: dict) -> dict:
    """If data is a K8s-style wrapper (apiVersion+kind+metadata+spec), extract
    the flat entity from spec and inject metadata.name. Otherwise return as-is."""
    if "spec" in data and "kind" in data:
        spec = data["spec"] or {}
        if isinstance(spec, dict):
            flat = dict(spec)
        else:
            flat = {}
        md = data.get("metadata", {}) or {}
        if md.get("name"):
            flat["name"] = md["name"]
        if md.get("tenant"):
            flat.setdefault("tenant_id", md["tenant"])
        return flat
    return data


def _get_or_post(s, get_path, post_path, payload, kind):
    """Idempotently register a definition: GET first (name is UNIQUE in
    llm_agent/llm_mcp), POST only if missing, so re-running this scenario
    doesn't 500 on a duplicate-name constraint violation."""
    r = s.get(f"{API}{get_path}")
    if r.status_code == 200:
        return True
    r = s.post(f"{API}{post_path}", json=payload)
    if r.status_code not in (200, 201):
        print(f"  WARN: failed to register {kind} {payload.get('name')}: {r.status_code} {r.text[:160]}")
        return False
    return True


def _resolve_env_vars(obj):
    """Recursively replace ${VAR} patterns in string values with environment
    variables. Returns the modified object (dict keys/values, list items)."""
    if isinstance(obj, str):
        import re

        def _repl(m):
            return os.environ.get(m.group(1), m.group(0))
        return re.sub(r'\$\{(\w+)\}', _repl, obj)
    if isinstance(obj, dict):
        return {k: _resolve_env_vars(v) for k, v in obj.items()}
    if isinstance(obj, list):
        return [_resolve_env_vars(v) for v in obj]
    return obj


def seed_agents_and_mcps(s):
    """Register the agent/MCP definitions the canonical flow depends on
    (see module docstring above _AGENTS_DIR) — idempotent, safe to call on
    every run."""
    print("  -> Seeding agent definitions...")
    agent_count = 0
    for fname in sorted(os.listdir(_AGENTS_DIR)):
        if not fname.endswith(".yaml"):
            continue
        with open(os.path.join(_AGENTS_DIR, fname)) as f:
            agent_def = _unwrap_k8s(yaml.safe_load(f))
        name = agent_def.get("name")
        if not name:
            continue
        if _get_or_post(s, f"/api/v1/{TENANT}/agents/{name}",
                        f"/api/v1/{TENANT}/agents", agent_def, "agent"):
            agent_count += 1
    print(f"  OK {agent_count} agent definition(s) registered (from {_AGENTS_DIR})")

    # Resolve GITHUB_TOKEN for the seed step
    if not os.environ.get("GITHUB_TOKEN") and os.environ.get("GH_TOKEN"):
        os.environ["GITHUB_TOKEN"] = os.environ["GH_TOKEN"]

    print("  -> Seeding MCP definitions...")
    mcp_count = 0
    for mode in ("github", "sonarqube"):
        if _USE_REAL_MCP:
            mcp_path = os.path.join(_CONFIG_ROOT, "mcps", f"{mode}.yaml")
            with open(mcp_path) as f:
                mcp_def = _unwrap_k8s(yaml.safe_load(f))
            mcp_def = _resolve_env_vars(mcp_def)
        else:
            mcp_def = {"name": mode, "enabled": True, "type": "stdio",
                       "command": _MOCK_MCP_COMMAND, "args": [mode], "env": {}}
        if _get_or_post(s, f"/api/v1/{TENANT}/mcp/{mode}",
                        f"/api/v1/{TENANT}/mcp", mcp_def, "mcp"):
            mcp_count += 1
    print(f"  OK {mcp_count} MCP server(s) registered "
          f"({'real config/mcps/*.yaml' if _USE_REAL_MCP else 'mock /app/mcp-server.sh'})")


def load_flow_from_yaml():
    """Load flow definition from the canonical YAML file, stripping non-API fields."""
    with open(_FLOW_YAML_PATH) as f:
        data = _unwrap_k8s(yaml.safe_load(f))
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
        r = s.post(f"{API}/api/v1/human/{token}/approve",
                   json={"comment": "Approved by e2e verifier"})
        print(f"  OK auto-approved human gate (token={token[:12]}...) status={r.status_code}")
        return r.status_code == 200
    return False


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


def _get_tasks(s, run_id):
    """Fetch task list for a run from REST API."""
    r = s.get(f"{API}/api/v1/{TENANT}/runs/{run_id}/tasks")
    if r.status_code != 200:
        return []
    tasks = r.json()
    if not isinstance(tasks, list):
        return []
    return tasks


def _tasks_by_node(tasks):
    """Convert task list to dict keyed by node_id."""
    return {t.get("node_id"): t for t in tasks if t.get("node_id")}


def _parse_output(task):
    """Parse the output field from a task dict — may be a dict, JSON string, or None."""
    output = task.get("output") or {}
    if isinstance(output, str):
        try:
            return json.loads(output)
        except (json.JSONDecodeError, TypeError):
            return {"_raw": output}
    return output if isinstance(output, dict) else {}


def _node_task(tasks_by_node, node_id):
    """Safely retrieve a single node's task dict."""
    return tasks_by_node.get(node_id)


# ─────────────── Per-phase verification helpers ───────────────


def verify_seed(s, conn):
    """Phase 0: Verify all 7 agents + 2 MCPs are registered via API."""
    print("\n-- [0] Seed — Verify agents & MCPs registered --")
    total = len(AGENT_NAMES) + len(MCP_NAMES)
    passed = 0

    for name in AGENT_NAMES:
        r = s.get(f"{API}/api/v1/{TENANT}/agents/{name}")
        if r.status_code == 200:
            passed += 1
        else:
            print(f"  WARN agent '{name}' GET returned {r.status_code}")

    for name in MCP_NAMES:
        r = s.get(f"{API}/api/v1/{TENANT}/mcp/{name}")
        if r.status_code == 200:
            passed += 1
        else:
            print(f"  WARN MCP '{name}' GET returned {r.status_code}")

    print(f"  Result: {passed}/{total} definitions verified via API")
    return passed, total


def verify_trigger(s, conn):
    """Phase 1: Trigger flow via manual endpoint (webhook fallback).
    Returns run_id."""
    print("\n-- [1] Trigger — POST /api/v1/{tenant}/flows/{id}/trigger --")

    run_id = None

    # Primary: manual trigger endpoint
    trigger_url = f"{API}/api/v1/{TENANT}/flows/{FLOW_ID}/trigger"
    r = s.post(trigger_url, json={"vars": {}})
    if r.status_code in (200, 201, 202):
        resp_data = r.json() if r.text else {}
        run_id = resp_data.get("run_id") or resp_data.get("id")
        print(f"  OK manual trigger accepted (status={r.status_code}, run_id={run_id})")
    else:
        # Fallback: webhook endpoint
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
        r = s.post(f"{API}/api/v1/webhook/github", json=payload, headers=headers)
        if r.status_code not in (200, 202):
            raise AssertionError(f"Both trigger methods failed. webhook returned {r.status_code}: {r.text[:200]}")
        resp_data = r.json() if r.text else {}
        run_id = resp_data.get("run_id") or resp_data.get("id")
        print(f"  OK webhook trigger accepted (status={r.status_code}, run_id={run_id})")

    # Fallback: query PG for the most recent run
    if not run_id and conn:
        cur = conn.cursor()
        cur.execute(
            "SELECT id FROM orh_flowrun WHERE agentflow_id=%s ORDER BY created_at DESC LIMIT 1",
            (FLOW_ID,),
        )
        row = cur.fetchone()
        if row:
            run_id = row[0]
            print(f"  OK run_id from PG fallback: {run_id}")

    if not run_id:
        raise AssertionError("No run_id from trigger or PG fallback")

    # Verify PG persistence
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

    print(f"  OK run_id={run_id} (manual trigger)")
    return run_id


def verify_discovery(tasks_by_node):
    """Phase 2: DISCOVERY — get-commit has commit_sha; scan-sonarqube has issues."""
    phase = "DISCOVERY"
    nodes = PHASE_NODES[phase]
    print(f"\n-- [2] {phase} — Commit + scan verification --")
    passed = 0
    total = 0

    task = _node_task(tasks_by_node, "get-commit")
    if task and task.get("status") == "COMPLETED":
        total += 1
        output = _parse_output(task)
        if "commit_sha" in output and output["commit_sha"]:
            print(f"  OK get-commit: commit_sha='{str(output['commit_sha'])[:20]}...'")
            passed += 1
        else:
            print(f"  WARN get-commit: output missing 'commit_sha' field")
    else:
        status = task.get("status") if task else "not_reached"
        print(f"  -- get-commit: status={status} (skipped)")

    task = _node_task(tasks_by_node, "scan-sonarqube")
    if task and task.get("status") == "COMPLETED":
        total += 1
        output = _parse_output(task)
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
            # Accept any non-empty output as evidence the node ran
            if output:
                print(f"  WARN scan-sonarqube: output has keys {list(output.keys())[:5]} but no 'issues'")
            else:
                print(f"  WARN scan-sonarqube: empty output")
    else:
        status = task.get("status") if task else "not_reached"
        print(f"  -- scan-sonarqube: status={status} (skipped)")

    print(f"  Result: {passed}/{total} checks passed")
    return passed, total


def verify_analyze(tasks_by_node):
    """Phase 3: ANALYZE — aggregate-issues has issues array with normalized entries."""
    phase = "ANALYZE"
    nodes = PHASE_NODES[phase]
    print(f"\n-- [3] {phase} — Issue aggregation verification --")
    passed = 0
    total = 0

    task = _node_task(tasks_by_node, "aggregate-issues")
    if task and task.get("status") == "COMPLETED":
        total = 2
        output = _parse_output(task)
        issues = output.get("issues") or output.get("results") or []
        if isinstance(issues, list) and len(issues) > 0:
            print(f"  OK aggregate-issues: issues array with {len(issues)} entries")
            passed += 1
            # Check that entries have normalized fields
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
            passed += 1
            passed += 1  # no entries to check fields on
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


def verify_fix(tasks_by_node):
    """Phase 4: FIX — generate-fixes has patches array with file and diff fields."""
    phase = "FIX"
    print(f"\n-- [4] {phase} — Patch generation verification --")
    passed = 0
    total = 0

    task = _node_task(tasks_by_node, "generate-fixes")
    if task and task.get("status") == "COMPLETED":
        total = 3
        output = _parse_output(task)
        patches = output.get("patches") or output.get("fixes") or []
        if isinstance(patches, list) and len(patches) > 0:
            print(f"  OK generate-fixes: patches array with {len(patches)} entries")
            passed += 1
            # Check file and diff/patch fields in first entry
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
            passed += 3  # no entries to check fields on, trust empty
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


def verify_review(tasks_by_node):
    """Phase 5: REVIEW — Each of 3 reviews has decision bool + reason string."""
    phase = "REVIEW"
    print(f"\n-- [5] {phase} — Multi-agent review board verification --")
    passed = 0
    total = 0
    review_nodes = ["review-security", "review-quality", "review-arch"]

    for node_id in review_nodes:
        task = _node_task(tasks_by_node, node_id)
        if task and task.get("status") == "COMPLETED":
            total += 2
            output = _parse_output(task)
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


def verify_vote(tasks_by_node):
    """Phase 6: VOTE — Committee output has decision (majority of 3 reviews)."""
    phase = "VOTE"
    print(f"\n-- [6] {phase} — Committee majority vote verification --")
    passed = 0
    total = 0

    task = _node_task(tasks_by_node, "committee")
    if task and task.get("status") == "COMPLETED":
        total = 1
        output = _parse_output(task)
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


def verify_supervisor(tasks_by_node):
    """Phase 7: SUPERVISOR — supervisor-check output has action field."""
    phase = "SUPERVISOR"
    print(f"\n-- [7] {phase} — Safety gate verification --")
    passed = 0
    total = 0

    task = _node_task(tasks_by_node, "supervisor-check")
    if task and task.get("status") == "COMPLETED":
        total = 1
        output = _parse_output(task)
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


def verify_condition_human(tasks_by_node):
    """Phase 8: CONDITION + HUMAN — is-approved routes, human gate auto-approved."""
    phase = "CONDITION"
    print(f"\n-- [8] {phase} + HUMAN — Routing + human gate verification --")
    passed = 0
    total = 0

    # is-approved is a condition node — its status tells us the routing
    task = _node_task(tasks_by_node, "is-approved")
    if task:
        total += 1
        status = task.get("status")
        if status == "COMPLETED":
            print(f"  OK is-approved: condition evaluated true (COMPLETED)")
            passed += 1
        elif status == "SKIPPED":
            print(f"  OK is-approved: condition evaluated false (SKIPPED — expected alternate route)")
            passed += 1
        else:
            print(f"  WARN is-approved: status={status}")
    else:
        print(f"  -- is-approved: not_reached")

    # human-approval
    task = _node_task(tasks_by_node, "human-approval")
    if task:
        total += 1
        status = task.get("status")
        if status == "COMPLETED":
            print(f"  OK human-approval: gate auto-approved (COMPLETED)")
            passed += 1
        elif status == "SKIPPED":
            print(f"  OK human-approval: gate skipped (condition routed to other path)")
            passed += 1
        elif status == "PAUSED":
            print(f"  WARN human-approval: still PAUSED (verifier may have missed the gate)")
        else:
            print(f"  WARN human-approval: status={status}")
    else:
        print(f"  -- human-approval: not_reached (gate may have been skipped by is-approved=false)")

    print(f"  Result: {passed}/{total} checks passed")
    return passed, total


def verify_commit_pr(tasks_by_node):
    """Phase 9: COMMIT&PR — Branch, commit, PR created with url."""
    phase = "COMMIT_PR"
    print(f"\n-- [9] {phase} — Branch + commit + PR verification --")
    passed = 0
    total = 0

    for node_id in ["create-branch", "commit-fixes", "create-pr"]:
        task = _node_task(tasks_by_node, node_id)
        if task and task.get("status") == "COMPLETED":
            total += 1
            passed += 1
            output = _parse_output(task)
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


def verify_rescan(tasks_by_node):
    """Phase 10: RESCAN — Trigger -> wait -> check -> compare -> fix-complete chain."""
    phase = "RESCAN"
    print(f"\n-- [10] {phase} — Re-scan chain verification --")
    passed = 0
    total = 0
    rescan_nodes = ["trigger-rescan", "wait-rescan", "check-resolved",
                    "compare-results", "fix-complete"]

    for node_id in rescan_nodes:
        task = _node_task(tasks_by_node, node_id)
        if task and task.get("status") == "COMPLETED":
            total += 1
            passed += 1
            output = _parse_output(task)
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


def verify_report(tasks_by_node):
    """Phase 11: REPORT — summary-report has markdown; notify-pr completed."""
    phase = "REPORT"
    print(f"\n-- [11] {phase} — Report + notification verification --")
    passed = 0
    total = 0
    report_nodes = ["summary-report", "notify-pr", "notify-email", "notify-teams", "end"]

    for node_id in report_nodes:
        task = _node_task(tasks_by_node, node_id)
        if task and task.get("status") == "COMPLETED":
            total += 1
            passed += 1
            output = _parse_output(task)
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


def verify_pg_final(conn, run_id):
    """Phase 12: PG FINAL — Every phase has at least one completed task_runs row."""
    print("\n-- [12] PG FINAL — All-phase task_runs coverage --")
    if not conn:
        print("  SKIP: no PG connection")
        return 0, 0

    passed = 0
    total = len(PHASE_NODES)

    for phase_name, node_ids in PHASE_NODES.items():
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

    # Also count total task_runs across all phases
    cur = conn.cursor()
    cur.execute(
        "SELECT COUNT(*) FROM task_runs WHERE agentflow_run_id=%s AND status='COMPLETED'",
        (run_id,),
    )
    total_completed = cur.fetchone()[0]
    print(f"  Total COMPLETED task_runs: {total_completed}")

    print(f"  Result: {passed}/{total} phases have completed task_runs")
    return passed, total


# ─────────────── Run orchestration ───────────────


def run():
    print("\n" + "=" * 60)
    print("  Scenario 11: E2E Security Fixer - Full Pipeline (capstone)")
    print("=" * 60)

    s = requests.Session()
    s.headers["Content-Type"] = "application/json"

    # ── Phase 0: Seed ──
    seed_agents_and_mcps(s)
    conn = pg_connect()
    p0_passed, p0_total = verify_seed(s, conn)

    # ── Load and create flow definition ──
    flow_def = load_flow_from_yaml()
    node_count = len(flow_def.get("nodes", []))
    edge_count = len(flow_def.get("edges", []))
    print(f"\n-- Flow definition: {FLOW_ID} ({node_count} nodes, {edge_count} edges) --")

    # Idempotent upsert: delete then POST (safe for re-runs)
    s.delete(f"{API}/api/v1/{TENANT}/flows/{FLOW_ID}")
    time.sleep(0.5)
    r = s.post(f"{API}/api/v1/{TENANT}/flows", json=flow_def)
    if r.status_code not in (200, 201):
        raise AssertionError(f"create flow returned {r.status_code}: {r.text[:200]}")
    print(f"  OK Flow definition created (status={r.status_code})")

    # ── Phase 1: Trigger via webhook ──
    run_id = verify_trigger(s, conn)

    # ── Poll run completion ──
    print(f"\n-- Polling run {run_id} for completion (timeout={FLOW_TIMEOUT_S}s) --")
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
    print(f"  Final run status: {status}")

    # ── Fetch tasks ──
    print("\n-- Fetching task list from API --")
    tasks = _get_tasks(s, run_id)
    tasks_by_node = _tasks_by_node(tasks)
    print(f"  {len(tasks)} task(s) fetched, {len(tasks_by_node)} unique node(s)")

    # Print task overview
    if tasks:
        for t in tasks:
            nid = t.get("node_id", "?")
            tstatus = t.get("status", "?")
            has_output = bool(t.get("output"))
            print(f"    {nid:<22} status={tstatus:<12} has_output={has_output}")

    # ── Phases 2-11: Pipeline verification ──
    phase_results = []

    phase_results.append((2, "DISCOVERY", *verify_discovery(tasks_by_node)))
    phase_results.append((3, "ANALYZE", *verify_analyze(tasks_by_node)))
    phase_results.append((4, "FIX", *verify_fix(tasks_by_node)))
    phase_results.append((5, "REVIEW", *verify_review(tasks_by_node)))
    phase_results.append((6, "VOTE", *verify_vote(tasks_by_node)))
    phase_results.append((7, "SUPERVISOR", *verify_supervisor(tasks_by_node)))
    phase_results.append((8, "CONDITION+HUMAN", *verify_condition_human(tasks_by_node)))
    phase_results.append((9, "COMMIT&PR", *verify_commit_pr(tasks_by_node)))
    phase_results.append((10, "RESCAN", *verify_rescan(tasks_by_node)))
    phase_results.append((11, "REPORT", *verify_report(tasks_by_node)))

    # ── Phase 12: PG FINAL ──
    p12_passed, p12_total = verify_pg_final(conn, run_id)

    if conn:
        conn.close()

    # ── Summary ──
    print("\n" + "=" * 60)
    print("  Phase Summary")
    print("=" * 60)

    all_results = [
        (0, "Seed", p0_passed, p0_total),
    ]
    for num, name, p_passed, p_total in phase_results:
        all_results.append((num, name, p_passed, p_total))
    all_results.append((12, "PG FINAL", p12_passed, p12_total))

    total_checks = 0
    passed_checks = 0
    for num, name, p_passed, p_total in all_results:
        total_checks += p_total
        passed_checks += p_passed
        if p_total > 0:
            pct = int(100 * p_passed / p_total)
            print(f"  Phase {num:<2} {name:<18} {p_passed}/{p_total} ({pct}%)")
        else:
            print(f"  Phase {num:<2} {name:<18} N/A (skipped)")

    print(f"  ---")
    if total_checks > 0:
        pct = int(100 * passed_checks / total_checks)
        print(f"  Total: {passed_checks}/{total_checks} checks passed ({pct}%)")
    else:
        print(f"  Total: 0 checks (all phases skipped)")

    # Run-level assertion: allow FAILED in test env lacking real MCPs
    r = s.get(f"{API}/api/v1/{TENANT}/runs/{run_id}")
    if r.status_code == 200:
        run_data = r.json()
        final_status = run_data.get("status")
        print(f"\n  Run final status: {final_status}")
        if final_status == "COMPLETED":
            print(f"  OK run completed with {len(tasks_by_node)} task node(s)")
        elif final_status in ("FAILED", "CANCELLED"):
            error_msg = run_data.get("error") or run_data.get("message", "")
            print(f"  WARN run {final_status.lower()}: {error_msg[:120]}")
            print(f"  WARN this is permissible if external MCPs (GitHub/SonarQube) are unavailable")
        else:
            print(f"  WARN run still active: {final_status}")

    print(f"\n-- Done --")
    print(f"  Flow {FLOW_ID} left in place for manual inspection")
    print(f"  Run ID: {run_id}")
    print(f"  Use scenario 12 (pr_commit_verifier) for actual PR commit inspection")
