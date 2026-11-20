"""Scenario 34 — Delivery & Report: branch/commit/PR creation, SonarQube re-scan, compare, report, PG final."""
from __future__ import annotations

from common.model import VerificationResult
from verifier import BaseVerifier

import requests
from common import project as common_api
import sys
import os
import json

from common.agentflow import SecurityAutonomyFixture as c
from common.project import RUN_ID_PATH

COMPLETED_SET = c.COMPLETED_STATUSES

class DeliveryReportVerifier(BaseVerifier):
    """Class-owned operations for s34 delivery report."""

    @staticmethod
    def _is_completed(status):
        return status in COMPLETED_SET

    @staticmethod
    def _tool_task_ok(task, node_id):
        if not task or not DeliveryReportVerifier._is_completed(task.get("status", "")):
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
            print(f"  WARN {node_id}: tool output reports failure")
            return False
        return True

    @staticmethod
    def verify_commit_pr(tasks_by_node):
        print(f"\n-- [34 CommitPR] Branch + commit + PR verification --")
        passed = 0
        total = 4

        for node_id in ["check-existing-pr", "pr-exists"]:
            task = c.node_task(tasks_by_node, node_id)
            if task and DeliveryReportVerifier._is_completed(task.get("status", "")):
                passed += 1
                print(f"  OK {node_id}: {task.get('status')}")
            else:
                status = task.get("status") if task else "not_reached"
                print(f"  -- {node_id}: status={status} (skipped)")

        branch_nodes = {
            "new-pr": ["create-branch", "commit-fixes", "create-pr"],
            "existing-pr": ["commit-to-existing"],
        }
        branch_passed = 0
        for branch_name, node_ids in branch_nodes.items():
            completed = []
            failed = []
            for node_id in node_ids:
                task = c.node_task(tasks_by_node, node_id)
                if DeliveryReportVerifier._tool_task_ok(task, node_id):
                    completed.append(node_id)
                elif task and DeliveryReportVerifier._is_completed(task.get("status", "")):
                    failed.append(node_id)
            print(f"  Branch {branch_name}: completed={completed}/{node_ids}")
            if failed:
                print(f"  Branch {branch_name}: failed-output={failed}")
            if len(completed) == len(node_ids):
                branch_passed = 1
        if branch_passed:
            passed += 2

        print(f"  Result: {passed}/{total} checks passed")
        return passed, total

    @staticmethod
    def verify_rescan(tasks_by_node):
        print(f"\n-- [34 Rescan] Re-scan chain verification --")
        passed = 0
        rescan_nodes = ["trigger-rescan", "wait-rescan", "check-resolved",
                        "compare-results", "fix-complete"]
        total = len(rescan_nodes)

        for node_id in rescan_nodes:
            task = c.node_task(tasks_by_node, node_id)
            if task and DeliveryReportVerifier._is_completed(task.get("status", "")):
                passed += 1
                output = c.parse_output(task)
                extra = ""
                if node_id == "compare-results":
                    resolution = output.get("resolution") or output.get("status")
                    if resolution:
                        extra = f" (resolution={resolution})"
                print(f"  OK {node_id}: {task.get('status')}{extra}")
            else:
                status = task.get("status") if task else "not_reached"
                print(f"  -- {node_id}: status={status} (skipped)")

        print(f"  Result: {passed}/{total} checks passed")
        return passed, total

    @staticmethod
    def verify_report(tasks_by_node):
        print(f"\n-- [34 Report] Report + notification verification --")
        passed = 0
        total = 3

        summary = c.node_task(tasks_by_node, "summary-report")
        if summary and DeliveryReportVerifier._is_completed(summary.get("status", "")):
            output = c.parse_output(summary)
            report = output.get("report") or output.get("summary") or str(output)[:100]
            if isinstance(report, str) and len(report) > 20:
                print(f"  OK summary-report: {summary.get('status')} (report: {len(report)} chars)")
                passed += 1
            else:
                print(f"  WARN summary-report: report missing/short")
        else:
            status = summary.get("status") if summary else "not_reached"
            print(f"  OK summary-report: status={status} (path-dependent)")
            passed += 1

        for node_id in ["notify-pr", "end"]:
            task = c.node_task(tasks_by_node, node_id)
            if task and DeliveryReportVerifier._is_completed(task.get("status", "")):
                print(f"  OK {node_id}: {task.get('status')}")
                passed += 1
            else:
                status = task.get("status") if task else "not_reached"
                print(f"  -- {node_id}: status={status} (skipped)")

        print(f"  Result: {passed}/{total} checks passed")
        return passed, total

    @staticmethod
    def verify_pg_final(conn, run_id):
        print("\n-- [34 PG Final] All-phase NodeRun coverage --")
        if not conn:
            print("  SKIP: no PG connection")
            return 0, 0

        passed = 0
        optional_phases = {"HUMAN"}
        total = len(c.PHASE_NODES) - len(optional_phases)

        for phase_name, node_ids in c.PHASE_NODES.items():
            if phase_name in optional_phases:
                continue
            placeholders = ", ".join("%s" for _ in node_ids)
            cur = conn.cursor()
            cur.execute(
                f"SELECT node_key,status FROM orh_node_run "
                f"WHERE run_id=%s AND node_key IN ({placeholders}) AND status<>'DELETED' "
                f"ORDER BY node_key,attempt",
                (run_id, *node_ids),
            )
            rows = cur.fetchall()
            if rows:
                completed = [r for r in rows if DeliveryReportVerifier._is_completed(r[1])]
                if completed:
                    print(f"  OK {phase_name:<12}: {len(completed)} completed task(s)")
                    passed += 1
                else:
                    statuses = [f"{r[0]}={r[1]}" for r in rows]
                    print(f"  -- {phase_name:<12}: no completed tasks — {statuses}")
            else:
                print(f"  -- {phase_name:<12}: no NodeRun entries (phase not reached)")

        cur = conn.cursor()
        cur.execute(
            "SELECT COUNT(*) FROM orh_node_run WHERE run_id=%s AND status IN ('COMPLETED','SUCCESS')",
            (run_id,),
        )
        total_completed = cur.fetchone()[0]
        print(f"  Total completed NodeRuns: {total_completed}")

        print(f"  Result: {passed}/{total} phases have completed NodeRuns")
        return passed, total

    @staticmethod
    def _verify_scenario():
        print("\n" + "=" * 60)
        print("  Scenario 34: Delivery & Report (CommitPR + Rescan + Report + PG Final)")
        print("=" * 60)

        if not os.path.isfile(RUN_ID_PATH):
            raise AssertionError("No .last_run_id found — run Scenario 31 first")
        with open(RUN_ID_PATH) as f:
            run_id = f.read().strip()
        print(f"  Using run_id: {run_id}")

        s = common_api.FlowgentE2EProject.session()

        print("\n-- Fetching task list from API --")
        tasks = c.get_tasks(s, run_id)
        tbn = c.tasks_by_node(tasks)
        print(f"  {len(tasks)} task(s) fetched, {len(tbn)} unique node(s)")

        conn = c.pg_connect()

        results = []
        results.append(("CommitPR", *DeliveryReportVerifier.verify_commit_pr(tbn)))
        results.append(("Rescan", *DeliveryReportVerifier.verify_rescan(tbn)))
        results.append(("Report", *DeliveryReportVerifier.verify_report(tbn)))
        results.append(("PG Final", *DeliveryReportVerifier.verify_pg_final(conn, run_id)))

        print("\n-- [34 MQTT Audit] Sandbox result and final execution callbacks --")
        c.assert_runtime_execution_evidence(
            run_id,
            tasks,
            required_nodes=["summary-report", "notify-pr", "end"],
            require_sandbox=True,
        )

        r = s.get(f"{c.API}/api/v1/{c.NAMESPACE}/runs/{run_id}", timeout=10)
        if r.status_code != 200:
            raise AssertionError(f"Cannot fetch final run status: {r.status_code} {r.text[:160]}")
        final_status = r.json().get("status")
        print(f"  Final API run status: {final_status}")
        if final_status != "COMPLETED":
            raise AssertionError(f"Flow run did not complete successfully: {final_status}")

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

    scenario_id = "34"
    title = "E2E Fixer — Delivery & Report"

    def run(self) -> VerificationResult:
        return self.execute(lambda: self.step("verify PR delivery, rescan, final report, and persistence", self._verify_execution))

    @staticmethod
    def _verify_execution() -> None:
        DeliveryReportVerifier._verify_scenario()




# ── Phase: COMMIT & PR ──



# ── Phase: RESCAN ──



# ── Phase: REPORT ──



# ── Phase: PG FINAL ──



# ── Orchestration ──
