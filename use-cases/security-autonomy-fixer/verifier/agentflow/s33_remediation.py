"""Scenario 33 — Remediation: fix generation, triple review, committee vote, supervisor gate, human approval."""

from __future__ import annotations

import os

from util.constant import E2EOtherDefinitions as config
from util.report import VerificationResult

from verifier import context as common_api
from verifier.agentflow import SecurityAutonomyFixture as c
from verifier.context import RUN_ID_PATH, BaseVerifier, VerificationContext

COMPLETED_SET = c.COMPLETED_STATUSES

# ── Phase: FIX ──


class E2ES33RemediationVerifier(BaseVerifier):
    """Class-owned operations for s33 remediation."""

    @staticmethod
    def verify_fix(tasks_by_node):
        output = c.require_output(tasks_by_node, "generate-fixes")
        files, patches = output.get("files"), output.get("patches")
        if (
            not isinstance(files, list)
            or not files
            or not all(
                isinstance(item, dict)
                and item.get("path")
                and isinstance(item.get("content"), str)
                and item["content"]
                for item in files
            )
        ):
            raise AssertionError("Fix generation has no complete file outputs")
        if (
            not isinstance(patches, list)
            or not patches
            or not all(
                isinstance(item, dict)
                and all(item.get(key) for key in ("file", "rule", "description"))
                for item in patches
            )
        ):
            raise AssertionError("Fix generation has no complete patch metadata")
        if not {patch["file"] for patch in patches} <= {item["path"] for item in files}:
            raise AssertionError(
                "Patch metadata refers to files that were not generated"
            )
        return 4, 4

    @staticmethod
    def verify_review(tasks_by_node):
        for node in ("review-security", "review-quality", "review-arch"):
            output = c.require_output(tasks_by_node, node)
            if (
                type(output.get("decision")) is not bool
                or not isinstance(output.get("reason"), str)
                or not output["reason"].strip()
            ):
                raise AssertionError(
                    f"{node} did not produce a Boolean decision and reason"
                )
            confidence = output.get("confidence")
            if (
                not isinstance(confidence, (int, float))
                or isinstance(confidence, bool)
                or not 0 <= confidence <= 1
            ):
                raise AssertionError(f"{node} did not produce confidence in [0,1]")
        return 6, 6

    @staticmethod
    def verify_vote(tasks_by_node):
        votes = [
            c.require_output(tasks_by_node, node)["decision"]
            for node in ("review-security", "review-quality", "review-arch")
        ]
        output = c.require_output(tasks_by_node, "committee")
        if type(output.get("decision")) is not bool or output["decision"] != (
            sum(votes) >= 2
        ):
            raise AssertionError(
                "Committee decision differs from the three persisted review votes"
            )
        if output.get("total") != 3 or output.get("approve") != sum(votes):
            raise AssertionError(
                "Committee vote counts differ from persisted review decisions"
            )
        return 1, 1

    @staticmethod
    def verify_supervisor(tasks_by_node):
        output = c.require_output(tasks_by_node, "supervisor-check")
        if (
            output.get("action") != "continue"
            or not isinstance(output.get("reason"), str)
            or not output["reason"].strip()
        ):
            raise AssertionError(
                "The successful delivery path requires an explicit continue decision and reason"
            )
        return 1, 1

    @staticmethod
    def verify_condition_human(tasks_by_node):
        decision = c.require_output(tasks_by_node, "committee")["decision"]
        condition = c.require_output(tasks_by_node, "is-approved")
        if type(decision) is not bool or condition.get("result") is not decision:
            raise AssertionError(
                "Routing differs from the persisted committee decision"
            )
        # Both manifest branches converge on human-approval; absence is not a valid skip.
        c.require_output(tasks_by_node, "human-approval")
        return 2, 2

    @staticmethod
    def _verify_scenario():
        print("\n" + "=" * 60)
        print("  Scenario 33: Remediation (Fix + Review + Vote + Gate)")
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

        c.assert_persisted_attempts(
            run_id,
            [
                "generate-fixes",
                "review-security",
                "review-quality",
                "review-arch",
                "committee",
                "supervisor-check",
                "is-approved",
                "human-approval",
            ],
        )
        from contextlib import closing

        from util.postgres import E2EPostgresChecker

        with closing(E2EPostgresChecker.connect(config.postgres_dsn())) as connection:
            approvals = E2EPostgresChecker.query(
                connection,
                "SELECT status,decided_by,decided_at FROM orh_approval WHERE namespace_id=%s AND run_id=%s AND node_run_id=%s AND type='human_gate'",
                (c.NAMESPACE, run_id, tbn["human-approval"]["id"]),
            )
            if not approvals or not all(
                status == "approved" and actor and at for status, actor, at in approvals
            ):
                raise AssertionError(
                    "Human gate completion has no matching durable approval decision"
                )
        results = []
        results.append(("Fix", *E2ES33RemediationVerifier.verify_fix(tbn)))
        results.append(("Review", *E2ES33RemediationVerifier.verify_review(tbn)))
        results.append(("Vote", *E2ES33RemediationVerifier.verify_vote(tbn)))
        results.append(
            ("Supervisor", *E2ES33RemediationVerifier.verify_supervisor(tbn))
        )
        results.append(("Gate", *E2ES33RemediationVerifier.verify_condition_human(tbn)))

        print("\n-- [33 MQTT Audit] Slot-worker status and dependency wave order --")
        expected_nodes = [
            "generate-fixes",
            "review-security",
            "review-quality",
            "review-arch",
            "committee",
        ]
        c.assert_runtime_execution_evidence(
            run_id, tasks, required_nodes=expected_nodes
        )

        total_checks = sum(r[2] for r in results)
        passed_checks = sum(r[1] for r in results)

        print(f"\n{'=' * 60}")
        print("  Scenario 33 Summary")
        for name, p, t in results:
            pct = int(100 * p / t) if t > 0 else 0
            print(f"  {name:<14} {p}/{t} ({pct}%)")
        print(f"  Total: {passed_checks}/{total_checks}")
        print(f"{'=' * 60}")

        if passed_checks < total_checks:
            raise AssertionError(f"Remediation failed: {passed_checks}/{total_checks}")

    scenario_id = "33"
    title = "E2E Fixer — Remediation"

    def verifier(self, context: VerificationContext) -> VerificationResult:
        return self.execute(
            lambda: self.step(
                "verify fix, reviews, voting, approval, and MQTT evidence",
                self._verify_execution,
            )
        )

    @staticmethod
    def _verify_execution() -> None:
        E2ES33RemediationVerifier._verify_scenario()

    # ── Phase: REVIEW ──

    # ── Phase: VOTE ──

    # ── Phase: SUPERVISOR ──

    # ── Phase: CONDITION + HUMAN ──

    # ── Orchestration ──

    def deploy(self, context: VerificationContext) -> None:
        """Use the topology prepared by the runner; no scenario reset is needed."""

    def init(self, context: VerificationContext) -> None:
        """Retain initialization ordering inside the existing scenario steps."""
