"""Flowgent-specific E2E project operations shared by all verifiers."""

from __future__ import annotations

import contextlib
import importlib
import io
import json
import os
import re
import sys
import time
import traceback

import requests

from .config import REPORTS_DIR, SCENARIOS
from deploy import E2EDeployerFactory
from .model import RunContext, VerificationResult
from .report import E2EReportWriter


STATE_DIR = REPORTS_DIR / ".state"
STATE_DIR.mkdir(parents=True, exist_ok=True)
RUN_ID_PATH = STATE_DIR / "last_run_id"
MQTT_AUDIT_PATH = STATE_DIR / "last_mqtt_audit.json"
PR_BASELINE_PATH = STATE_DIR / "last_pr_baseline.json"


class FlowgentE2EProject:
    """Own Flowgent API, database, and manifest operations for verifiers."""

    @staticmethod
    def pg_connect():
        """Connect to the isolated Flowgent database with a safe local fallback."""
        try:
            import psycopg2
        except ImportError:
            print("  SKIP: psycopg2 not installed")
            return None
        from . import config

        dsn = config.E2EConfiguration.postgres_dsn()
        for candidate in (dsn, dsn.replace("port=5432", "port=5433")):
            try:
                connection = psycopg2.connect(candidate)
                connection.autocommit = True
                return connection
            except Exception:
                continue
        return None

    @staticmethod
    def headers() -> dict:
        """Direct E2E control-plane calls carry no Flowgent-owned credentials."""
        return {}

    @classmethod
    def session(cls) -> requests.Session:
        """Create the canonical direct control-plane session for API/A2A tests."""
        session = requests.Session()
        session.headers.update(cls.headers())
        session.headers["Content-Type"] = "application/json"
        return session

    @staticmethod
    def unwrap_k8s(data: dict) -> dict:
        """Normalize core.flowgent.io/v1 structured resource into a flat dict."""
        if "data" in data and "kind" in data:
            spec = data["data"] or {}
            flat = dict(spec) if isinstance(spec, dict) else {}
            metadata = data.get("metadata", {}) or {}
            if metadata.get("name"):
                flat["name"] = metadata["name"]
            if metadata.get("namespace"):
                flat.setdefault("namespace_id", metadata["namespace"])
            return flat
        return data

    @classmethod
    def resolve_env_vars(cls, value):
        """Recursively substitute ${VAR} placeholders from the environment."""
        if isinstance(value, str):
            return re.sub(r"\$\{(\w+)\}", lambda match: os.environ.get(match.group(1), match.group(0)), value)
        if isinstance(value, dict):
            return {key: cls.resolve_env_vars(item) for key, item in value.items()}
        if isinstance(value, list):
            return [cls.resolve_env_vars(item) for item in value]
        return value

    @staticmethod
    def get_or_post(session, api_base, get_path, post_path, payload, kind):
        """Idempotently GET a resource, then POST it when genuinely absent."""
        response = session.get(f"{api_base}{get_path}")
        if response.status_code == 200:
            return True
        response = session.post(f"{api_base}{post_path}", json=payload)
        if response.status_code not in (200, 201):
            print(f"  WARN: failed to register {kind} {payload.get('name')}: {response.status_code} {response.text[:160]}")
            return False
        return True

    @classmethod
    def _get_tasks_from_pg(cls, run_id):
        """Query task_runs directly when the read API cannot return the task list."""
        try:
            connection = cls.pg_connect()
            if not connection:
                return []
            cursor = connection.cursor()
            cursor.execute(
                "SELECT id, agentflow_run_id, node_id, status, input, output, error, "
                "retry_count, max_retries, exec_id, sequence, started_at, finished_at "
                "FROM task_runs WHERE agentflow_run_id=%s ORDER BY sequence ASC",
                (run_id,),
            )
            rows = cursor.fetchall()
            columns = [description[0] for description in cursor.description]
            tasks = []
            for row in rows:
                task = dict(zip(columns, row))
                for column in ("started_at", "finished_at"):
                    if task.get(column):
                        task[column] = task[column].isoformat()
                tasks.append(task)
            connection.close()
            return tasks
        except Exception as error:
            print(f"  PG task fallback failed: {error}")
            return []

    @classmethod
    def get_tasks(cls, session, api_base, namespace, run_id):
        """Fetch a flow run's tasks, with a direct database fallback."""
        response = session.get(f"{api_base}/api/v1/{namespace}/runs/{run_id}/tasks")
        if response.status_code == 200:
            tasks = response.json()
            if isinstance(tasks, list):
                return tasks
        return cls._get_tasks_from_pg(run_id)

    @staticmethod
    def tasks_by_node(tasks):
        return {task.get("node_id"): task for task in tasks if task.get("node_id")}

    @staticmethod
    def parse_output(task):
        """Normalize a TaskRun output field into its structured representation."""
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

    @staticmethod
    def node_task(tasks_by_node, node_id):
        return tasks_by_node.get(node_id)

    @staticmethod
    def try_approve_pending_human(session, api_base, namespace, run_id, connection=None):
        """Approve a pending gate, preferring its direct database token lookup."""
        token = None
        if connection:
            cursor = connection.cursor()
            cursor.execute(
                "SELECT token FROM human_approvals WHERE agentflow_run_id=%s AND status='PENDING' LIMIT 1",
                (run_id,),
            )
            row = cursor.fetchone()
            if row:
                token = row[0]
        if not token:
            response = session.get(f"{api_base}/api/v1/{namespace}/runs/{run_id}/approvals")
            if response.status_code == 200:
                for item in response.json() or []:
                    if item.get("agentflow_run_id") == run_id and item.get("status") == "PENDING":
                        token = item.get("token")
                        break
        if token:
            response = session.post(
                f"{api_base}/api/v1/{namespace}/runs/{run_id}/approvals/{token}/approve",
                json={"comment": "Approved by e2e verifier"},
            )
            print(f"  OK auto-approved human gate (token={token[:12]}...) status={response.status_code}")
            return response.status_code == 200
        return False


class _Tee(io.StringIO):
    """Capture scenario output while preserving runner progress on stdout."""

    def __init__(self, stream: io.TextIOBase) -> None:
        super().__init__()
        self._stream = stream

    def write(self, value: str) -> int:
        self._stream.write(value)
        return super().write(value)

    def flush(self) -> None:
        self._stream.flush()
        super().flush()


class VerificationRunner:
    """Execute the ordered verifier matrix with one report/evidence contract."""

    @staticmethod
    def run_verifier(scenario_id: str, context: RunContext) -> VerificationResult:
        title, module_name = SCENARIOS[scenario_id]
        started = time.monotonic()
        capture = _Tee(sys.stdout)
        try:
            module = importlib.import_module(module_name)
            with contextlib.redirect_stdout(capture):
                entrypoint = getattr(module, "verifier", None)
                if not callable(entrypoint):
                    raise TypeError(f"{module_name} must export verifier(context)")
                result = entrypoint(context)
            if not isinstance(result, VerificationResult):
                raise TypeError(f"{module_name}.verifier(context) returned an invalid result")
            if result.scenario_id != scenario_id or result.title != title:
                raise ValueError(f"{module_name} metadata differs from common.config.SCENARIOS")
            result.output = capture.getvalue()
            return result
        except Exception:
            return VerificationResult(
                scenario_id=scenario_id,
                title=title,
                passed=False,
                duration_seconds=time.monotonic() - started,
                output=capture.getvalue(),
                error=traceback.format_exc(),
                details=[traceback.format_exc()],
            )

    @classmethod
    def run_matrix(
        cls,
        context: RunContext,
        scenario_ids: list[str],
        *,
        archive_existing: bool = True,
    ) -> tuple[bool, list[VerificationResult]]:
        if archive_existing:
            archive = E2EReportWriter.archive()
            if archive:
                print(f"Archived previous reports: {archive}")

        deployer = E2EDeployerFactory.create(context)
        tunnels = None
        if any(scenario_id != "01" for scenario_id in scenario_ids):
            tunnels = deployer.tunnels().start()
        results: list[VerificationResult] = []
        try:
            for scenario_id in scenario_ids:
                if tunnels is not None and scenario_id != "01":
                    tunnels.ensure()
                title = SCENARIOS[scenario_id][0]
                print(f"  [{scenario_id}] {title}", flush=True)
                result = cls.run_verifier(scenario_id, context)
                results.append(result)
                E2EReportWriter.write_round(context.round_number, result)
                status = "PASS" if result.passed else "FAIL"
                print(f"  [{scenario_id}] {status} ({result.duration_seconds:.2f}s)")
        finally:
            if tunnels is not None:
                tunnels.stop()
        summary = E2EReportWriter.write_summary([results])
        passed = all(result.passed for result in results)
        print(f"Summary: {summary} ({sum(result.passed for result in results)}/{len(results)} passed)")
        return passed, results
