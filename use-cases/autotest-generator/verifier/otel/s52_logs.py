"""Read log events for the exact plan IDs persisted by an actual AutoTest run."""

import json
import re

from util.constant import E2EOtherDefinitions as config
from util.kubernetes import E2EKubernetesDeployer
from verifier.context import BaseVerifier, VerificationContext


class E2ES52LogsVerifier(BaseVerifier):
    scenario_id = "52"
    title = "AutoTest — Persisted Plan Lifecycle Logs"

    def deploy(self, context: VerificationContext) -> None:
        """Read producing workload logs; missing/collected pods cannot imply success."""

    def init(self, context: VerificationContext) -> None:
        self.execution = context.execution()

    def verifier(self, context: VerificationContext) -> None:
        result = E2EKubernetesDeployer.command(
            (
                "kubectl",
                "logs",
                "-n",
                config.K8S_APP_NAMESPACE_PREFIX + self.execution["namespace"],
                "-l",
                "flowgent.io/runtime-cluster",
                "--all-containers=true",
                "--prefix=true",
                "--tail=-1",
                "--max-log-requests=20",
            )
        )
        events = {}
        for line in result.output.splitlines():
            try:
                event = json.loads(line[line.index("{") :])
            except (ValueError, json.JSONDecodeError):
                continue
            plan = event.get("plan_id", event.get("plan"))
            if plan:
                events.setdefault(plan, set()).add(
                    event.get("msg", event.get("message"))
                )
        for task in self.execution["tasks"]:
            plan = re.sub(r"-attempt-\d+$", "", task["execution_id"])
            if not {
                "slot worker executing plan",
                "slot worker task succeeded",
            } <= events.get(plan, set()):
                raise AssertionError(
                    f"No producing lifecycle logs for AutoTest plan {plan}"
                )
        self.details.append(
            "Every persisted AutoTest attempt has correlated start/success events"
        )
