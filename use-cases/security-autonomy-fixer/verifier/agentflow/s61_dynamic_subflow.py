"""Vertical Subflow scenarios through the deployed API, JM, TM and SQL ledger.

The deterministic LLM fixture plans from graph revision, never request count,
so retries and process restarts cannot change an already accepted decision.
"""
from __future__ import annotations

import concurrent.futures
import time
import uuid

from util.constant import E2EOtherDefinitions as config
from util.report import VerificationResult
from verifier.context import BaseVerifier, VerificationContext
from verifier.core.s21_apiserver import API_BASE, E2ES21ApiserverVerifier as API


class E2ES61DynamicSubflowVerifier(BaseVerifier):
    scenario_id = "61"
    title = "Subflow — Static, Adaptive, CAS and Seal"

    def deploy(self, context: VerificationContext) -> None:
        """Use the runner's deployment and explicit deterministic LLM provider."""

    def init(self, context: VerificationContext) -> None:
        self.prefix = f"vnext-{uuid.uuid4().hex[:10]}"
        self.base = f"/api/v1/{config.NAMESPACE_ID}"

    def verifier(self, context: VerificationContext) -> VerificationResult:
        return self.execute(self._verify)

    def _flow(self, suffix, nodes, edges=()):
        name = f"{self.prefix}-{suffix}"
        API.request("POST", f"{self.base}/flows", {
            "name": name, "kind": "flow", "runtime_mode": "session",
            "nodes": nodes, "edges": list(edges),
        }, expected=(201,))
        return name

    def _run(self, flow, status="PENDING"):
        return API.request("POST", f"{self.base}/runs", {
            "flow_name": flow, "runtime_mode": "session", "status": status,
        }, expected=(201,))["id"]

    def _wait(self, run_id):
        deadline = time.monotonic() + self.context.timeout_seconds
        while time.monotonic() < deadline:
            run = API.request("GET", f"{self.base}/runs/{run_id}")
            if run["status"] == "COMPLETED":
                return run
            if run["status"] in {"FAILED", "CANCELLED", "PAUSED"}:
                raise AssertionError(f"run stopped: {run}")
            time.sleep(0.5)
        raise AssertionError(f"Subflow run timed out: {run_id}")

    def _verify(self):
        child = self._flow("child", [
            {"id": "seed", "kind": "noop"},
            {"id": "check", "kind": "noop"},
        ])
        static = self._flow("static", [
            {"id": "repair", "kind": "agentflow", "agentflow": child},
            {"id": "delivery", "kind": "noop"},
        ], [{"from": "repair", "to": "delivery"}])
        run_id = self._run(static)
        self.step("legacy agentflow reference executes child before parent delivery",
                  lambda: self._wait(run_id))
        tasks = API.request("GET", f"{self.base}/runs/{run_id}/node-runs")
        assert {"repair/seed", "repair/check", "delivery"} <= {
            t["node_key"] for t in tasks if t["status"] == "SUCCESS"
        }, tasks

        planner = f"{self.prefix}-planner"
        API.request("POST", f"{self.base}/agents", {
            "name": planner, "model": "e2e-llm/e2e-deterministic",
            "soul": "Flowgent vNext deterministic subflow planning fixture.",
            "instruction": "Return graph operations for the supplied graph_revision.",
            "max_tokens": 2048,
        }, expected=(201,))
        adaptive = self._flow("adaptive", [{
            "id": "repair", "kind": "agentflow", "subflow": {
                "ref": child, "mode": "adaptive", "supervisor": planner,
                "mutation": {"allow": ["append", "update_pending_edges", "seal"]},
                "limits": {"max_created_nodes": 16, "max_parallelism": 4,
                           "max_patches": 4},
                "contract": {"required_nodes": ["seed", "check"]},
            },
        }, {"id": "delivery", "kind": "noop"}],
            [{"from": "repair", "to": "delivery"}])
        adaptive_run = self._run(adaptive)
        completed = self.step("Supervisor expands graph and seals before delivery",
                              lambda: self._wait(adaptive_run))
        graph = completed["metadata"]["subflows"]["repair"]
        assert graph["sealed"] and graph["revision"] == 3, graph
        assert "review" in {n["id"] for n in graph["graph"]["nodes"]}, graph
        tasks = API.request("GET", f"{self.base}/runs/{adaptive_run}/node-runs")
        assert any(t["node_key"] == "repair/review" and t["status"] == "SUCCESS"
                   for t in tasks), tasks
        assert all(t.get("node_instance_id") for t in tasks), tasks

        paused = self._run(adaptive, "PAUSED")
        API.request("POST", f"{self.base}/runs/{paused}/subflows/initialize",
                    {"instance_id": "repair"})
        patches = [{"id": f"race-{i}", "flow_run_id": paused,
                    "subflow_instance_id": "repair", "expected_revision": 1,
                    "operations": [{"kind": "append", "node": {
                        "id": f"candidate-{i}", "kind": "noop"}}]}
                   for i in range(2)]

        def submit(patch):
            # HTTP status is the assertion; a conflict response has a text body.
            import requests
            from verifier.context import FlowgentE2EProject
            return requests.post(
                f"{API_BASE}{self.base}/runs/{paused}/graph-patches",
                json=patch, headers=FlowgentE2EProject.headers(), timeout=20,
            )

        def conflict():
            with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
                responses = list(pool.map(submit, patches))
            assert sorted(r.status_code for r in responses) == [200, 409], [
                (r.status_code, r.text) for r in responses]
            winner = next(i for i, r in enumerate(responses) if r.status_code == 200)
            replay = submit(patches[winner])
            assert replay.status_code == 200 and replay.json()["revision"] == 2
            state = API.request("POST", f"{self.base}/runs/{paused}/subflows/initialize",
                                {"instance_id": "repair"})
            assert state["revision"] == 2 and len(state["graph"]["nodes"]) == 3

        self.step("concurrent CAS conflict and committed decision replay", conflict)
        API.request("POST", f"{self.base}/runs/{paused}/cancel", expected=(200,))
        self.details.append(f"Static run={run_id}; Adaptive run={adaptive_run}; CAS run={paused}")
