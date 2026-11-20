"""Verify lifecycle log events from the exact browser-triggered sandbox plan."""

from __future__ import annotations

import json
import re
import shlex

from util.report import VerificationResult
from verifier.agentgraph import SecurityAutonomyFixture
from verifier.context import BaseVerifier, FlowgentRunEvidence, VerificationContext


class E2ES52LogsVerifier(BaseVerifier):
    scenario_id = "52"
    title = "OTEL — UI Execution Lifecycle Logs"

    def deploy(self, context: VerificationContext) -> None:
        """Logs are captured while the producing workload still exists."""

    def init(self, context: VerificationContext) -> None:
        self.execution = FlowgentRunEvidence.load_ui()

    def verifier(self, context: VerificationContext) -> VerificationResult:
        return self.execute(self._verify_scenarios)

    @staticmethod
    def parse_event(line: str) -> dict | None:
        try:
            event = json.loads(line[line.index("{") :])
            return event if isinstance(event, dict) else None
        except (ValueError, json.JSONDecodeError):
            match = re.search(r"\b(DEBUG|INFO|WARN|ERROR|FATAL)\s+(.*)", line)
            if not match:
                return None
            event = {"level": match.group(1)}
            message = []
            for part in shlex.split(match.group(2)):
                if "=" in part:
                    key, value = part.split("=", 1)
                    event[key] = value
                elif len(event) == 1:
                    message.append(part)
            event["msg"] = " ".join(message)
            return event

    def _verify_logs(self) -> None:
        evidence = self.execution
        persisted = FlowgentRunEvidence.persisted(
            evidence["graph_run_id"], evidence["namespace"], evidence["node_instance_id"]
        )
        if persisted["plan_id"] != evidence["plan_id"]:
            raise AssertionError("Log evidence refers to another persisted plan")
        events = []
        for line in evidence.get("logs", []):
            event = self.parse_event(line)
            if event and event.get("plan_id", event.get("plan")) == evidence["plan_id"]:
                events.append(event)
        messages = {event.get("msg", event.get("message")) for event in events}
        required = {
            "sandbox trigger received",
            "sandbox result published",
            "slot worker task succeeded",
        }
        if required - messages:
            raise AssertionError(
                f"Missing correlated execution log events: {sorted(required - messages)}"
            )
        if any(
            event.get("level", "").upper() in {"ERROR", "FATAL"} for event in events
        ):
            raise AssertionError(
                "The successful UI execution emitted a correlated error event"
            )
        if any(
            event.get("msg", event.get("message")) == "sandbox result published"
            and str(event.get("has_error")).lower() != "false"
            for event in events
        ):
            raise AssertionError(
                "The correlated sandbox result reported an execution error"
            )
        artifact = self.evidence_path(
            "52.logs", "Persisted plan lifecycle log events", "json"
        )
        artifact.write_text(
            json.dumps(
                {
                    "graph_run_id": evidence["graph_run_id"],
                    "plan_id": evidence["plan_id"],
                    "events": events,
                },
                indent=2,
            )
        )
        self.record_evidence(
            "52.logs",
            "UI → persisted plan → runtime lifecycle logs",
            "application/json",
            artifact,
        )

    def _verify_main_logs(self) -> None:
        from verifier.context import MAIN_LOG_PATH, RUN_ID_PATH, FlowgentE2EProject

        scope = json.loads(MAIN_LOG_PATH.read_text())
        graph_run_id = RUN_ID_PATH.read_text().strip()
        if (
            scope.get("graph_run_id") != graph_run_id
            or scope.get("deployer") != self.context.deployer
        ):
            raise AssertionError(
                "Main execution log capture belongs to another run/backend"
            )
        tasks = SecurityAutonomyFixture.tasks_by_node(
            SecurityAutonomyFixture.get_tasks(FlowgentE2EProject.session(), graph_run_id)
        )
        active = SecurityAutonomyFixture.active_path(tasks)
        SecurityAutonomyFixture.assert_persisted_attempts(graph_run_id, active)
        events = {}
        for line in scope.get("logs", []):
            event = self.parse_event(line)
            if event is None:
                continue
            plan = event.get("plan_id", event.get("plan"))
            if plan:
                events.setdefault(plan, set()).add(
                    event.get("msg", event.get("message"))
                )
        for node in active:
            plan = re.sub(r"-attempt-\d+$", "", tasks[node]["execution_id"])
            if not {
                "slot worker task succeeded",
            } <= events.get(plan, set()):
                raise AssertionError(
                    f"Missing producing lifecycle events for remediation node {node}"
                )
        artifact = self.evidence_path(
            "52.main", "Main remediation lifecycle logs", "json"
        )
        artifact.write_text(json.dumps(scope, indent=2))
        self.record_evidence(
            "52.main",
            "All active remediation nodes → persisted plans → logs",
            "application/json",
            artifact,
        )

    def _verify_scenarios(self) -> None:
        self.step(
            "match runtime log events to UI and persisted execution", self._verify_logs
        )
        self.step("verify main remediation lifecycle events", self._verify_main_logs)
