"""Scenario 32 — Discovery & Analyze: commit fetch, SonarQube scan, issue aggregation."""

from __future__ import annotations

import os
import time

from util.report import VerificationResult

from verifier import context as common_api
from verifier.agentflow import SecurityAutonomyFixture as c
from verifier.context import (
    MAIN_LOG_PATH,
    RUN_ID_PATH,
    BaseVerifier,
    FlowgentRunEvidence,
    VerificationContext,
)

# ── Phase: DISCOVERY ──


class E2ES32DiscoveryAnalyzeVerifier(BaseVerifier):
    """Class-owned operations for s32 discovery analyze."""

    @staticmethod
    def verify_discovery(tasks_by_node):
        import re

        commit = c.require_output(tasks_by_node, "get-commit")
        sha = commit.get("commit_sha") or commit.get("sha")
        if not isinstance(sha, str) or not re.fullmatch(
            r"[a-fA-F0-9]{40}|[a-fA-F0-9]{64}", sha
        ):
            raise AssertionError("Discovery did not return a full commit SHA")
        output = c.require_output(tasks_by_node, "scan-sonarqube")
        issues = c.sonar_issues(output)
        if not isinstance(issues, list) or not issues:
            raise AssertionError(
                "This remediation scenario requires real source findings"
            )
        return 2, 2

    @staticmethod
    def verify_analyze(tasks_by_node):
        issues = c.require_output(tasks_by_node, "aggregate-issues").get("issues")
        if not isinstance(issues, list) or not issues:
            raise AssertionError("Issue aggregation did not produce findings")
        required = {"id", "repo", "severity", "type", "file", "description"}
        for issue in issues:
            if not isinstance(issue, dict) or required - issue.keys():
                raise AssertionError(
                    "An aggregated finding omits the documented normalized fields"
                )
        actual = c.sonar_issues(c.require_output(tasks_by_node, "scan-sonarqube"))
        source_ids = {item.get("key", item.get("id")) for item in actual}
        if not {item["id"] for item in issues} <= source_ids:
            raise AssertionError(
                "Aggregation invented findings that were not returned by SonarQube"
            )
        cloned = c.require_output(tasks_by_node, "git-clone").get("path")
        if not isinstance(cloned, str) or not cloned.startswith("/var/flowgent/"):
            raise AssertionError("Git clone did not persist a run workspace path")
        selected = c.require_output(tasks_by_node, "read-source-files")
        files = selected.get("files")
        if (
            not selected.get("issues")
            or not isinstance(files, list)
            or not files
            or not all(
                isinstance(item, dict)
                and item.get("path")
                and isinstance(item.get("content"), str)
                and item["content"]
                for item in files
            )
        ):
            raise AssertionError(
                "Selected findings are not linked to actual source-file contents"
            )
        return 4, 4

    @staticmethod
    def _verify_scenario():
        print("\n" + "=" * 60)
        print("  Scenario 32: Discovery & Analyze")
        print("=" * 60)

        # Load run_id from previous scenario's output
        if not os.path.isfile(RUN_ID_PATH):
            raise AssertionError(
                "No .last_run_id found — run Scenario 31 (Seed & Trigger) first"
            )
        with open(RUN_ID_PATH) as f:
            run_id = f.read().strip()
        print(f"  Using run_id: {run_id}")

        s = common_api.FlowgentE2EProject.session()

        print("\n-- [32 Runtime Pods] Verify JM-created TM/Sandbox pods --")
        c.ensure_global_mqtt_audit(run_id)
        try:
            c.wait_for_workload_components(c.FLOW_ID, run_id=run_id, timeout=240)

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
        finally:
            messages = c.stop_global_mqtt_audit(run_id)
        c.save_mqtt_audit(run_id, messages)

        # Fetch tasks
        print("\n-- Fetching task list from API --")
        tasks = c.get_tasks(s, run_id)
        tbn = c.tasks_by_node(tasks)
        print(f"  {len(tasks)} task(s) fetched, {len(tbn)} unique node(s)")

        if tasks:
            for t in tasks:
                nid = c.task_node_key(t) or "?"
                tstatus = t.get("status", "?")
                has_output = bool(t.get("output"))
                print(f"    {nid:<22} status={tstatus:<12} has_output={has_output}")

        if conn:
            conn.close()

        c.assert_persisted_attempts(
            run_id,
            [
                "get-commit",
                "scan-sonarqube",
                "aggregate-issues",
                "git-clone",
                "read-source-files",
            ],
        )

        # Run verifications
        results = []
        results.append(
            ("Discovery", *E2ES32DiscoveryAnalyzeVerifier.verify_discovery(tbn))
        )
        results.append(("Analyze", *E2ES32DiscoveryAnalyzeVerifier.verify_analyze(tbn)))

        print("\n-- [32 MQTT Audit] JM/TM/Sandbox message chain --")
        c.assert_runtime_execution_evidence(run_id, tasks, require_sandbox=True)

        total_checks = sum(r[2] for r in results)
        passed_checks = sum(r[1] for r in results)

        print(f"\n{'=' * 60}")
        print("  Scenario 32 Summary")
        for name, p, t in results:
            pct = int(100 * p / t) if t > 0 else 0
            print(f"  {name:<14} {p}/{t} ({pct}%)")
        print(f"  Total: {passed_checks}/{total_checks}")
        print(f"{'=' * 60}")

        if passed_checks < total_checks:
            raise AssertionError(
                f"Discovery/Analyze failed: {passed_checks}/{total_checks}"
            )

    scenario_id = "32"
    title = "E2E Fixer — Discovery & Analyze"

    def verifier(self, context: VerificationContext) -> VerificationResult:
        return self.execute(
            lambda: self.step(
                "verify discovery, analysis, and MQTT task evidence",
                self._verify_execution,
            )
        )

    def _verify_execution(self) -> None:
        import json
        import re

        E2ES32DiscoveryAnalyzeVerifier._verify_scenario()
        run_id = RUN_ID_PATH.read_text().strip()
        tasks = c.get_tasks(common_api.FlowgentE2EProject.session(), run_id)
        plans = [
            re.sub(r"-attempt-\d+$", "", task["execution_id"])
            for task in tasks
            if task.get("execution_id")
        ]
        scope = FlowgentRunEvidence.persisted(run_id, c.NAMESPACE, "get-commit")
        scope["plan_ids"] = plans
        scope["logs"] = FlowgentRunEvidence.logs(self.context, scope)
        scope["deployer"] = self.context.deployer
        MAIN_LOG_PATH.write_text(json.dumps(scope, indent=2) + "\n")

    # ── Phase: ANALYZE ──

    # ── Orchestration ──

    def deploy(self, context: VerificationContext) -> None:
        """Use the topology prepared by the runner; no scenario reset is needed."""

    def init(self, context: VerificationContext) -> None:
        MAIN_LOG_PATH.unlink(missing_ok=True)
