"""Scenario 32 — Discovery & Analyze: commit fetch, SonarQube scan, issue aggregation."""
from __future__ import annotations

import requests
from common import project as common_api
import time
import sys
import os
import json

from verifier.agentflow.support import SecurityAutonomyFixture as c
from common.project import RUN_ID_PATH

# ── Phase: DISCOVERY ──

class DiscoveryAnalyzeChecks:
    """Class-owned operations for s32 discovery analyze."""

    @staticmethod
    def verify_discovery(tasks_by_node):
        phase = "DISCOVERY"
        nodes = c.PHASE_NODES[phase]
        print(f"\n-- [32 Discovery] Commit + scan verification --")
        passed = 0
        total = 2

        task = c.node_task(tasks_by_node, "get-commit")
        if c.task_completed(task):
            output = c.parse_output(task)
            commit_sha = output.get("commit_sha") or output.get("sha")
            if commit_sha:
                print(f"  OK get-commit: commit_sha='{str(commit_sha)[:20]}...'")
                passed += 1
            else:
                print(f"  WARN get-commit: output missing 'commit_sha'/'sha' field")
        else:
            status = task.get("status") if task else "not_reached"
            print(f"  -- get-commit: status={status} (skipped)")

        task = c.node_task(tasks_by_node, "scan-sonarqube")
        if c.task_completed(task):
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

    @staticmethod
    def verify_analyze(tasks_by_node):
        phase = "ANALYZE"
        print(f"\n-- [32 Analyze] Issue aggregation verification --")
        passed = 0
        total = 4

        task = c.node_task(tasks_by_node, "aggregate-issues")
        if c.task_completed(task):
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

        task = c.node_task(tasks_by_node, "git-clone")
        if c.task_completed(task):
            output = c.parse_output(task)
            repo_path = output.get("path")
            if isinstance(repo_path, str) and repo_path.startswith("/var/flowgent/"):
                print(f"  OK git-clone: repo path={repo_path}")
                passed += 1
            else:
                print(f"  WARN git-clone: missing /var/flowgent repo path in output")
        else:
            status = task.get("status") if task else "not_reached"
            print(f"  WARN git-clone: status={status}")

        task = c.node_task(tasks_by_node, "read-source-files")
        if c.task_completed(task):
            output = c.parse_output(task)
            issues = output.get("issues") or []
            files = output.get("files") or []
            first_file = files[0] if isinstance(files, list) and files else {}
            if (
                isinstance(issues, list) and issues
                and isinstance(files, list) and files
                and isinstance(first_file, dict)
                and first_file.get("path")
                and first_file.get("content")
            ):
                print(f"  OK read-source-files: selected {len(issues)} issue(s), {len(files)} file(s)")
                passed += 1
            else:
                print(f"  WARN read-source-files: no selected issue/file context")
        else:
            status = task.get("status") if task else "not_reached"
            print(f"  WARN read-source-files: status={status}")

        print(f"  Result: {passed}/{total} checks passed")
        return passed, total

    @staticmethod
    def _verify_scenario():
        print("\n" + "=" * 60)
        print("  Scenario 32: Discovery & Analyze")
        print("=" * 60)

        # Load run_id from previous scenario's output
        if not os.path.isfile(RUN_ID_PATH):
            raise AssertionError("No .last_run_id found — run Scenario 31 (Seed & Trigger) first")
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
                nid = t.get("node_id", "?")
                tstatus = t.get("status", "?")
                has_output = bool(t.get("output"))
                print(f"    {nid:<22} status={tstatus:<12} has_output={has_output}")

        if conn:
            conn.close()

        # Run verifications
        results = []
        results.append(("Discovery", *DiscoveryAnalyzeChecks.verify_discovery(tbn)))
        results.append(("Analyze", *DiscoveryAnalyzeChecks.verify_analyze(tbn)))

        print("\n-- [32 MQTT Audit] JM/TM/Sandbox message chain --")
        c.assert_runtime_execution_evidence(run_id, tasks, require_sandbox=True)

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



# ── Phase: ANALYZE ──



# ── Orchestration ──



from common.model import RunContext, VerificationResult
from verifier.agentflow.base import AgentFlowVerifier


class DiscoveryAnalyzeVerifier(AgentFlowVerifier):
    scenario_id = "32"
    title = "E2E Fixer — Discovery & Analyze"

    def run(self) -> VerificationResult:
        return self.execute(lambda: self.step("verify discovery, analysis, and MQTT task evidence", self._verify_execution))

    @staticmethod
    def _verify_execution() -> None:
        DiscoveryAnalyzeChecks._verify_scenario()

    @staticmethod
    def verify(context: RunContext) -> VerificationResult:
        """Create and run this scenario's class-owned verifier entrypoint."""
        return DiscoveryAnalyzeVerifier(context).run()

VERIFIER_CLASS = DiscoveryAnalyzeVerifier
