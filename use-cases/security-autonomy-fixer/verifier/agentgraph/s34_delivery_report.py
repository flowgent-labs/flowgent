"""Scenario 34 — Delivery & Report: branch/commit/PR creation, SonarQube re-scan, compare, report, PG final."""

from __future__ import annotations

import json
import os

from util.report import VerificationResult

from verifier import context as common_api
from verifier.agentgraph import SecurityAutonomyFixture as c
from verifier.context import RUN_ID_PATH, BaseVerifier, VerificationContext

COMPLETED_SET = c.COMPLETED_STATUSES


class E2ES34DeliveryReportVerifier(BaseVerifier):
    """Class-owned operations for s34 delivery report."""

    @staticmethod
    def _is_completed(status):
        return status in COMPLETED_SET

    @staticmethod
    def _tool_task_ok(task, node_instance_id):
        if not task or not E2ES34DeliveryReportVerifier._is_completed(
            task.get("status", "")
        ):
            return False
        output = task.get("output") or {}
        parsed = c.parse_output(task)
        text_parts = []
        for source in (output, parsed):
            if isinstance(source, dict):
                for key in ("text", "error", "stderr", "stdout"):
                    value = source.get(key)
                    if isinstance(value, str) and value:
                        text_parts.append(value)
        text = "\n".join(text_parts).lower()
        failure_markers = ("failed to", "validation failed", "error:")
        if any(marker in text for marker in failure_markers):
            print(f"  WARN {node_instance_id}: tool output reports failure")
            return False
        return True

    @staticmethod
    def verify_commit_pr(tasks_by_node):
        c.require_output(tasks_by_node, "check-existing-pr")
        exists = c.require_output(tasks_by_node, "pr-exists").get("result")
        if type(exists) is not bool:
            raise AssertionError("PR routing has no Boolean condition output")
        selected = (
            ["commit-to-existing"]
            if exists
            else ["create-branch", "commit-fixes", "create-pr"]
        )
        rejected = (
            ["create-branch", "commit-fixes", "create-pr"]
            if exists
            else ["commit-to-existing"]
        )
        for node in selected:
            output = c.require_output(tasks_by_node, node)
            if not output or not E2ES34DeliveryReportVerifier._tool_task_ok(
                tasks_by_node[node], node
            ):
                raise AssertionError(
                    f"Selected delivery node {node} has no successful tool result"
                )
        for node in rejected:
            task = tasks_by_node.get(node)
            if task and task.get("status") != "SKIPPED":
                raise AssertionError(f"Unselected delivery node {node} was not skipped")
        return 4, 4

    @staticmethod
    def verify_rescan(tasks_by_node):
        for node in (
            "trigger-rescan",
            "wait-rescan",
            "check-resolved",
            "compare-results",
            "fix-complete",
        ):
            c.require_output(tasks_by_node, node)
            if not E2ES34DeliveryReportVerifier._tool_task_ok(
                tasks_by_node[node], node
            ):
                raise AssertionError(
                    f"Re-scan node {node} returned a failed tool result"
                )
        from urllib.parse import urlencode

        import requests
        from verifier.infra._sonar_scanner import SonarScannerRuntime
        from verifier.infra._sonarqube import SonarQubeRuntime

        initial = json.loads(SonarScannerRuntime.BASELINE.read_text())
        current_run = RUN_ID_PATH.read_text().strip()
        started = c.require_output(tasks_by_node, "trigger-rescan")
        completed = c.require_output(tasks_by_node, "wait-rescan")
        if initial.get("graph_run_id") != current_run or started.get(
            "job_id"
        ) != completed.get("job_id"):
            raise AssertionError(
                "Re-analysis evidence belongs to another exact job or GraphRun"
            )
        if (
            completed.get("status") != "SUCCESS"
            or completed.get("ce_status") != "SUCCESS"
        ):
            raise AssertionError("A successful real CE analysis is required")
        if (
            not completed.get("expected_commit")
            or completed.get("expected_commit") != completed.get("commit")
            or completed.get("revision") != completed.get("commit")
        ):
            raise AssertionError(
                "Sonar analysis revision differs from the delivered commit"
            )
        for field in ("ce_node_run_id", "analysis_id"):
            if not completed.get(field) or completed[field] == initial.get(field):
                raise AssertionError("Re-analysis reused an absent or initial " + field)
        print(
            f"  Real re-analysis: CE={completed['ce_node_run_id']} analysis={completed['analysis_id']} delivered_commit={completed['commit']}"
        )
        selected = c.require_output(tasks_by_node, "read-source-files").get("issues")
        remaining = c.sonar_issues(c.require_output(tasks_by_node, "check-resolved"))
        if not isinstance(selected, list) or not selected:
            raise AssertionError(
                "No source findings are associated with the delivered patch"
            )
        unresolved = {item.get("key", item.get("id")) for item in remaining}
        if any(item.get("id", item.get("key")) in unresolved for item in selected):
            raise AssertionError(
                "Delivered patches did not resolve the selected real Sonar findings"
            )
        selected_ids = [item.get("id", item.get("key")) for item in selected]
        with requests.Session() as sonar:
            sonar.trust_env = False
            response = sonar.get(
                SonarQubeRuntime.url()
                + "/api/issues/search?"
                + urlencode(
                    {"issues": ",".join(selected_ids), "resolved": "false", "ps": 500}
                ),
                auth=(os.environ["SONARQUBE_TOKEN"], ""),
                timeout=15,
            )
            response.raise_for_status()
            if response.json().get("issues"):
                raise AssertionError(
                    "Exact selected source findings remain unresolved; a top-page absence is not proof"
                )
        compared = c.require_output(tasks_by_node, "compare-results")
        if not compared:
            raise AssertionError("Re-scan comparison has no output evidence")
        complete = c.require_output(tasks_by_node, "fix-complete").get("result")
        if type(complete) is not bool or complete != (compared.get("resolution") == "complete"):
            raise AssertionError("Completion routing differs from the persisted re-scan comparison")
        return 5, 5

    @staticmethod
    def verify_report(tasks_by_node):
        output = c.require_output(tasks_by_node, "summary-report")
        report = output.get("report") or output.get("summary") or output.get("text")
        if not isinstance(report, str) or len(report.strip()) <= 20:
            raise AssertionError(
                "The selected delivery path has no substantive final report"
            )
        c.require_output(tasks_by_node, "notify-pr")
        c.require_output(tasks_by_node, "end")
        return 3, 3

    @staticmethod
    def verify_pg_final(conn, graph_run_id):
        if conn is None:
            raise AssertionError(
                "PostgreSQL evidence is required for delivery verification"
            )
        session = common_api.FlowgentE2EProject.session()
        tasks = c.tasks_by_node(c.get_tasks(session, graph_run_id))
        active = c.active_path(tasks)
        c.assert_persisted_attempts(graph_run_id, active)
        return len(active), len(active)

    @staticmethod
    def _verify_scenario():
        print("\n" + "=" * 60)
        print(
            "  Scenario 34: Delivery & Report (CommitPR + Rescan + Report + PG Final)"
        )
        print("=" * 60)

        if not os.path.isfile(RUN_ID_PATH):
            raise AssertionError("No .last_graph_run_id found — run Scenario 31 first")
        with open(RUN_ID_PATH) as f:
            graph_run_id = f.read().strip()
        print(f"  Using graph_run_id: {graph_run_id}")

        s = common_api.FlowgentE2EProject.session()

        print("\n-- Fetching task list from API --")
        tasks = c.get_tasks(s, graph_run_id)
        tbn = c.tasks_by_node(tasks)
        print(f"  {len(tasks)} task(s) fetched, {len(tbn)} unique node(s)")

        conn = c.pg_connect()

        results = []
        results.append(
            ("CommitPR", *E2ES34DeliveryReportVerifier.verify_commit_pr(tbn))
        )
        results.append(("Rescan", *E2ES34DeliveryReportVerifier.verify_rescan(tbn)))
        results.append(("Report", *E2ES34DeliveryReportVerifier.verify_report(tbn)))
        results.append(
            ("PG Final", *E2ES34DeliveryReportVerifier.verify_pg_final(conn, graph_run_id))
        )

        print("\n-- [34 MQTT Audit] Sandbox result and final execution callbacks --")
        c.assert_runtime_execution_evidence(
            graph_run_id,
            tasks,
            required_nodes=["summary-report", "notify-pr", "end"],
            require_sandbox=True,
        )

        r = s.get(f"{c.API}/api/v1/{c.NAMESPACE}/graph-runs/{graph_run_id}", timeout=10)
        if r.status_code != 200:
            raise AssertionError(
                f"Cannot fetch final run status: {r.status_code} {r.text[:160]}"
            )
        final_status = r.json().get("status")
        print(f"  Final API run status: {final_status}")
        if final_status != "COMPLETED":
            raise AssertionError(
                f"AgentGraph run did not complete successfully: {final_status}"
            )

        if conn:
            conn.close()

        total_checks = sum(r[2] for r in results)
        passed_checks = sum(r[1] for r in results)

        print(f"\n{'=' * 60}")
        print("  Scenario 34 Summary")
        for name, p, t in results:
            pct = int(100 * p / t) if t > 0 else 0
            print(f"  {name:<14} {p}/{t} ({pct}%)")
        print(f"  Total: {passed_checks}/{total_checks}")
        print(f"{'=' * 60}")

        if passed_checks < total_checks:
            raise AssertionError(
                f"Delivery/Report failed: {passed_checks}/{total_checks}"
            )

    scenario_id = "34"
    title = "E2E Fixer — Delivery & Report"

    def verifier(self, context: VerificationContext) -> VerificationResult:
        return self.execute(
            lambda: self.step(
                "verify PR delivery, rescan, final report, and persistence",
                self._verify_execution,
            )
        )

    @staticmethod
    def _verify_execution() -> None:
        E2ES34DeliveryReportVerifier._verify_scenario()

    # ── Phase: COMMIT & PR ──

    # ── Phase: RESCAN ──

    # ── Phase: REPORT ──

    # ── Phase: PG FINAL ──

    # ── Orchestration ──

    def deploy(self, context: VerificationContext) -> None:
        """Use the topology prepared by the runner; no scenario reset is needed."""

    def init(self, context: VerificationContext) -> None:
        """Retain initialization ordering inside the existing scenario steps."""
