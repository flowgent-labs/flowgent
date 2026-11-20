"""
Flowgent E2E — shared API / K8s utilities.

Functions extracted from verifier/_common.py and available to all tests.
"""

import os
import re
import json


def unwrap_k8s(data: dict) -> dict:
    """Normalize core.flowgent.io/v1 structured resource into flat dict."""
    if "data" in data and "kind" in data:
        spec = data["data"] or {}
        flat = dict(spec) if isinstance(spec, dict) else {}
        md = data.get("metadata", {}) or {}
        if md.get("name"):
            flat["name"] = md["name"]
        if md.get("namespace"):
            flat.setdefault("namespace_id", md["namespace"])
        return flat
    return data


def resolve_env_vars(obj):
    """Recursively substitute ${VAR} placeholders from environment."""

    def _repl(m):
        return os.environ.get(m.group(1), m.group(0))

    if isinstance(obj, str):
        return re.sub(r'\$\{(\w+)\}', _repl, obj)
    if isinstance(obj, dict):
        return {k: resolve_env_vars(v) for k, v in obj.items()}
    if isinstance(obj, list):
        return [resolve_env_vars(v) for v in obj]
    return obj


def get_or_post(s, api_base, get_path, post_path, payload, kind):
    """Idempotent REST upsert: GET first, POST on 404."""
    r = s.get(f"{api_base}{get_path}")
    if r.status_code == 200:
        return True
    r = s.post(f"{api_base}{post_path}", json=payload)
    if r.status_code not in (200, 201):
        print(f"  WARN: failed to register {kind} {payload.get('name')}: {r.status_code} {r.text[:160]}")
        return False
    return True


def get_tasks(s, api_base, namespace, run_id):
    """Fetch all tasks for a given flow run."""
    r = s.get(f"{api_base}/api/v1/{namespace}/runs/{run_id}/node-runs")
    if r.status_code != 200:
        return []
    tasks = r.json()
    if not isinstance(tasks, list):
        return []
    return tasks


def tasks_by_node(tasks):
    """Index task list by canonical node_key."""
    return {t.get("node_key"): t for t in tasks if t.get("node_key")}


def parse_output(task):
    """Parse a task's output field (dict or JSON string) into a dict."""
    output = task.get("output") or {}
    if isinstance(output, str):
        try:
            return json.loads(output)
        except (json.JSONDecodeError, TypeError):
            return {"_raw": output}
    return output if isinstance(output, dict) else {}


def node_task(tasks_by_node, node_id):
    """Get a task by node_id from a pre-indexed map."""
    return tasks_by_node.get(node_id)


def try_approve_pending_human(s, api_base, run_id, conn=None):
    """Auto-approve a pending human-approval gate for the given run.

    Checks the DB first (if conn is provided), falls back to the API.
    """
    approval_id = None
    namespace = os.getenv("FLOWGENT_NAMESPACE_ID", "default")
    if conn:
        cur = conn.cursor()
        cur.execute(
            "SELECT id FROM orh_approval WHERE run_id=%s AND status='pending' LIMIT 1",
            (run_id,),
        )
        row = cur.fetchone()
        if row:
            approval_id = row[0]
    if not approval_id:
        r = s.get(f"{api_base}/api/v1/{namespace}/runs/{run_id}/approvals")
        if r.status_code == 200:
            for item in r.json() or []:
                if item.get("run_id") == run_id and item.get("status") == "pending":
                    approval_id = item.get("id")
                    break
    if approval_id:
        r = s.post(f"{api_base}/api/v1/{namespace}/runs/{run_id}/approvals/{approval_id}/approve",
                   json={"comment": "Approved by e2e verifier"})
        print(f"  OK auto-approved human gate (approval={approval_id[:12]}...) status={r.status_code}")
        return r.status_code == 200
    return False
