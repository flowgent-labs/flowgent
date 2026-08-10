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


def _get_tasks_from_pg(run_id):
    """Query task_runs directly from PG as fallback when API endpoint fails."""
    try:
        from . import db
        conn = db.pg_connect()
        if not conn:
            return []
        cur = conn.cursor()
        cur.execute(
            "SELECT id, agentflow_run_id, node_id, status, input, output, error, "
            "retry_count, max_retries, exec_id, sequence, started_at, finished_at "
            "FROM task_runs WHERE agentflow_run_id=%s ORDER BY sequence ASC",
            (run_id,),
        )
        rows = cur.fetchall()
        cols = [d[0] for d in cur.description]
        tasks = []
        for row in rows:
            t = dict(zip(cols, row))
            for ts_col in ("started_at", "finished_at"):
                if t.get(ts_col):
                    t[ts_col] = t[ts_col].isoformat()
            tasks.append(t)
        conn.close()
        return tasks
    except Exception as e:
        print(f"  PG task fallback failed: {e}")
        return []


def get_tasks(s, api_base, namespace, run_id):
    """Fetch all tasks for a given flow run. Falls back to PG on API failure."""
    r = s.get(f"{api_base}/api/v1/{namespace}/runs/{run_id}/tasks")
    if r.status_code == 200:
        tasks = r.json()
        if isinstance(tasks, list):
            return tasks
    # Fallback: query PG directly
    return _get_tasks_from_pg(run_id)


def tasks_by_node(tasks):
    """Index task list by node_id."""
    return {t.get("node_id"): t for t in tasks if t.get("node_id")}


def parse_output(task):
    """Parse a task's output field (dict or JSON string) into a dict."""
    output = task.get("output") or {}
    if isinstance(output, str):
        try:
            output = json.loads(output)
        except (json.JSONDecodeError, TypeError):
            return {"_raw": output}
    if not isinstance(output, dict):
        return {}
    parsed = output.get("parsed")
    if isinstance(parsed, dict):
        return parsed
    text = output.get("text")
    if isinstance(text, str):
        try:
            decoded = json.loads(text)
            if isinstance(decoded, dict):
                return decoded
        except (json.JSONDecodeError, TypeError):
            pass
    return output


def node_task(tasks_by_node, node_id):
    """Get a task by node_id from a pre-indexed map."""
    return tasks_by_node.get(node_id)


def try_approve_pending_human(s, api_base, run_id, conn=None):
    """Auto-approve a pending human-approval gate for the given run.

    Checks the DB first (if conn is provided), falls back to the API.
    """
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
        r = s.get(f"{api_base}/api/v1/human/approvals")
        if r.status_code == 200:
            for item in r.json() or []:
                if item.get("agentflow_run_id") == run_id and item.get("status") == "PENDING":
                    token = item.get("token")
                    break
    if token:
        r = s.post(f"{api_base}/api/v1/human/{token}/approve",
                   json={"comment": "Approved by e2e verifier"})
        print(f"  OK auto-approved human gate (token={token[:12]}...) status={r.status_code}")
        return r.status_code == 200
    return False
