"""Correlate the browser-triggered sandbox execution with its persisted attempt."""

from __future__ import annotations

import json

from util.report import VerificationResult
from verifier.agentgraph import SecurityAutonomyFixture
from verifier.context import BaseVerifier, FlowgentRunEvidence, VerificationContext


class E2ES13TracingVerifier(BaseVerifier):
    scenario_id = "13"
    title = "OTEL — UI Run and Persisted Attempt Tracing"

    def deploy(self, context: VerificationContext) -> None:
        """Use the real Jaeger deployed with the application."""

    def init(self, context: VerificationContext) -> None:
        self.execution = FlowgentRunEvidence.load_ui()

    def verifier(self, context: VerificationContext) -> VerificationResult:
        return self.execute(self._verify_scenarios)

    def _verify_trace(self) -> None:
        evidence = self.execution
        persisted = FlowgentRunEvidence.persisted(
            evidence["graph_run_id"], evidence["namespace"], evidence["node_instance_id"]
        )
        for key in ("node_run_id", "attempt", "agent_graph_revision_id", "plan_id"):
            if persisted[key] != evidence[key]:
                raise AssertionError(f"Persisted execution identity changed: {key}")
        traces = FlowgentRunEvidence.traces(evidence["graph_run_id"])
        spans = [span for trace in traces for span in trace.get("spans", [])]
        attempts = [
            FlowgentRunEvidence.tags(span)
            for span in spans
            if FlowgentRunEvidence.tags(span).get("flowgent.node_run_id")
            == evidence["node_run_id"]
        ]
        if not attempts or not any(
            tags.get("flowgent.node_instance_id") == evidence["node_instance_id"]
            and int(tags.get("flowgent.attempt", 0)) == evidence["attempt"]
            for tags in attempts
        ):
            raise AssertionError(
                "Jaeger does not identify the UI-selected persisted node/attempt"
            )
        for span in spans:
            tags = FlowgentRunEvidence.tags(span)
            if any(key in tags for key in ("input", "output", "prompt", "response")):
                raise AssertionError("Trace attributes contain raw execution payloads")
        artifact = self.evidence_path(
            "13.trace", "UI run correlated Jaeger traces", "json"
        )
        artifact.write_text(
            json.dumps(
                {
                    "graph_run_id": evidence["graph_run_id"],
                    "node_run_id": evidence["node_run_id"],
                    "traces": traces,
                },
                indent=2,
            )
        )
        self.record_evidence(
            "13.trace", "UI → NodeRun → Jaeger", "application/json", artifact
        )
        self.details.append(
            f"Correlated run={evidence['graph_run_id']} task={evidence['node_run_id']} attempt={evidence['attempt']}"
        )

    def _verify_main_trace(self) -> None:
        from verifier.context import RUN_ID_PATH, FlowgentE2EProject

        graph_run_id = RUN_ID_PATH.read_text().strip()
        tasks = SecurityAutonomyFixture.tasks_by_node(
            SecurityAutonomyFixture.get_tasks(FlowgentE2EProject.session(), graph_run_id)
        )
        active = SecurityAutonomyFixture.active_path(tasks)
        SecurityAutonomyFixture.assert_persisted_attempts(graph_run_id, active)
        traces = FlowgentRunEvidence.traces(graph_run_id)
        tagged = [
            FlowgentRunEvidence.tags(span)
            for trace in traces
            for span in trace.get("spans", [])
        ]
        for node in active:
            task = tasks[node]
            if not any(
                tags.get("flowgent.node_instance_id") == node
                and tags.get("flowgent.node_run_id") == task["id"]
                and tags.get("flowgent.attempt") == int(task.get("attempt") or 1)
                for tags in tagged
            ):
                raise AssertionError(
                    f"No exact Jaeger attempt span for active remediation node {node}"
                )
        artifact = self.evidence_path(
            "13.main", "Main remediation path Jaeger coverage", "json"
        )
        artifact.write_text(
            json.dumps(
                {"graph_run_id": graph_run_id, "active_nodes": active, "traces": traces}, indent=2
            )
        )
        self.record_evidence(
            "13.main",
            "All active remediation nodes → PG → Jaeger",
            "application/json",
            artifact,
        )

    def _verify_scenarios(self) -> None:
        self.step("correlate UI, PG NodeRun and Jaeger spans", self._verify_trace)
        self.step(
            "verify every active remediation node against PG and Jaeger",
            self._verify_main_trace,
        )
