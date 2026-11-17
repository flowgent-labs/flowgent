"""Match an actual AutoTest run and its durable attempts to Jaeger spans."""

import json

from util.constant import E2EOtherDefinitions as config
from verifier.context import BaseVerifier, VerificationContext


class E2ES13TracingVerifier(BaseVerifier):
    scenario_id = "13"
    title = "AutoTest — Persisted Run Tracing"

    def deploy(self, context: VerificationContext) -> None:
        """Use the configured real Jaeger query service."""

    def init(self, context: VerificationContext) -> None:
        self.execution = context.execution()

    def verifier(self, context: VerificationContext) -> None:
        import requests

        session = requests.Session()
        session.trust_env = False
        response = session.get(
            config.JAEGER_UI_URL.rstrip("/") + "/api/services", timeout=10
        )
        response.raise_for_status()
        observed = set()
        for service in response.json().get("data", []):
            if not service.startswith("flowgent"):
                continue
            response = session.get(
                config.JAEGER_UI_URL.rstrip("/") + "/api/traces",
                params={
                    "service": service,
                    "tags": json.dumps({"run.id": self.execution["run_id"]}),
                    "lookback": "24h",
                    "limit": 100,
                },
                timeout=15,
            )
            response.raise_for_status()
            for trace in response.json().get("data", []):
                for span in trace.get("spans", []):
                    tags = {
                        tag["key"]: tag.get("value") for tag in span.get("tags", [])
                    }
                    if tags.get("flowgent.task_id"):
                        observed.add(
                            (
                                tags.get("flowgent.task_id"),
                                tags.get("flowgent.node_id"),
                                tags.get("flowgent.attempt"),
                            )
                        )
        for task in self.execution["tasks"]:
            if (task["task_id"], task["node_id"], task["attempt"]) not in observed:
                raise AssertionError(
                    f"Jaeger lacks the exact persisted AutoTest attempt {task['task_id']}"
                )
        self.details.append(
            f"Matched every persisted attempt for AutoTest run {self.execution['run_id']}"
        )
