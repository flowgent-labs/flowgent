"""Compare real Prometheus counters against the browser's persisted sandbox run."""

from __future__ import annotations

import json
import time

from verifier.infra._prometheus import PrometheusRuntime
from util.report import VerificationResult
from verifier.agentflow import SecurityAutonomyFixture
from verifier.context import BaseVerifier, FlowgentRunEvidence, VerificationContext


class E2ES51MetricsVerifier(BaseVerifier):
    scenario_id = "51"
    title = "OTEL — UI Run Prometheus Counters"

    def deploy(self, context: VerificationContext) -> None:
        self.execution = FlowgentRunEvidence.load_ui()
        persisted = FlowgentRunEvidence.persisted(
            self.execution["run_id"],
            self.execution["namespace"],
            self.execution["node_id"],
        )
        if persisted["task_id"] != self.execution["task_id"]:
            raise AssertionError(
                "Metrics expectations refer to a different persisted UI attempt"
            )
        self.prometheus = PrometheusRuntime(context)
        self.prometheus.deploy()

    def init(self, context: VerificationContext) -> None:
        """The UI scenario initialized and persisted the producing execution."""

    def verifier(self, context: VerificationContext) -> VerificationResult:
        try:
            return self.execute(
                lambda: self.step(
                    "match UI/PG execution counts through Prometheus query API",
                    self._verify_metrics,
                )
            )
        finally:
            self.prometheus.stop()

    def _verify_metrics(self) -> None:
        label = json.dumps(self.execution["flow_id"])
        expected = {
            f'flowgent_jm_runs_total{{agentflow_id={label},status="RUNNING"}}': 1,
            f'flowgent_jm_runs_total{{agentflow_id={label},status="COMPLETED"}}': 1,
            f"flowgent_jm_plans_dispatched_total{{agentflow_id={label}}}": 1,
            f'flowgent_jm_plans_completed_total{{agentflow_id={label},status="SUCCESS"}}': 1,
        }
        from verifier.context import RUN_ID_PATH, FlowgentE2EProject

        main_run = RUN_ID_PATH.read_text().strip()
        tasks = SecurityAutonomyFixture.tasks_by_node(
            SecurityAutonomyFixture.get_tasks(FlowgentE2EProject.session(), main_run)
        )
        active = SecurityAutonomyFixture.active_path(tasks)
        SecurityAutonomyFixture.assert_persisted_attempts(main_run, active)
        if self.context.deployer == "k8s":
            main_label = "run_id=" + json.dumps(main_run)
            expected.update(
                {
                    f'flowgent_jm_runs_total{{{main_label},status="COMPLETED"}}': 1,
                    f"flowgent_jm_plans_dispatched_total{{{main_label}}}": len(active),
                    f'flowgent_jm_plans_completed_total{{{main_label},status="SUCCESS"}}': len(
                        active
                    ),
                }
            )
        deadline = time.monotonic() + 30
        observed = {}
        up = []
        while time.monotonic() < deadline:
            up = self.prometheus.query('up{job="flowgent-e2e"}')
            if up and all(float(row["value"][1]) == 1 for row in up):
                observed = {
                    metric: self.prometheus.query(f"sum(last_over_time({metric}[1h]))")
                    for metric in expected
                }
                if all(
                    len(rows) == 1 and float(rows[0]["value"][1]) == expected[metric]
                    for metric, rows in observed.items()
                ):
                    break
            time.sleep(1)
        else:
            self.details.append(
                "Prometheus diagnostic: "
                + json.dumps(
                    {"up": up, "expected": expected, "observed": observed},
                    sort_keys=True,
                )
            )
            raise AssertionError(
                "Prometheus scrape/counters do not match the real UI execution; missing exporter/recording is documented in docs/development/e2e-runtime-gaps.md"
            )
        artifact = self.evidence_path(
            "51.metrics", "UI persisted run Prometheus counters", "json"
        )
        artifact.write_text(
            json.dumps(
                {
                    "run_id": self.execution["run_id"],
                    "expected": expected,
                    "observed": observed,
                },
                indent=2,
            )
        )
        self.record_evidence(
            "51.metrics",
            "UI → PG attempt → Prometheus exact counters",
            "application/json",
            artifact,
        )
