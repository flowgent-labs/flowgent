"""Compare Prometheus run-scoped counters with actual AutoTest attempts."""

import json

from util.constant import E2EOtherDefinitions as config
from verifier.context import BaseVerifier, VerificationContext


class E2ES51MetricsVerifier(BaseVerifier):
    scenario_id = "51"
    title = "AutoTest — Run-Scoped Prometheus Counters"

    def deploy(self, context: VerificationContext) -> None:
        """Use the configured Prometheus scraping the producing workload."""

    def init(self, context: VerificationContext) -> None:
        self.execution = context.execution()

    def verifier(self, context: VerificationContext) -> None:
        import requests

        # Application JM targets must retain their Kubernetes run label in Prometheus.
        labels = "run_id=" + json.dumps(self.execution["run_id"])
        expected = {
            f'flowgent_jm_runs_total{{{labels},status="COMPLETED"}}': 1,
            f"flowgent_jm_plans_dispatched_total{{{labels}}}": len(
                self.execution["tasks"]
            ),
        }
        for metric, count in expected.items():
            response = requests.get(
                config.PROMETHEUS_URL.rstrip("/") + "/api/v1/query",
                params={"query": f"sum({metric})"},
                timeout=15,
            )
            response.raise_for_status()
            document = response.json()
            rows = document.get("data", {}).get("result", [])
            if (
                document.get("status") != "success"
                or len(rows) != 1
                or float(rows[0]["value"][1]) != count
            ):
                raise AssertionError(
                    "Run-scoped Prometheus values do not match persisted AutoTest execution; see docs/development/e2e-runtime-gaps.md"
                )
        self.details.append(
            "Prometheus completed-run and dispatch counters match persisted attempts"
        )
