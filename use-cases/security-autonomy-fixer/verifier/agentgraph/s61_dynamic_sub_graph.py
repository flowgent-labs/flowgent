"""Vertical SubGraph scenarios through the deployed API, JM, TM and SQL ledger.

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


class E2ES61DynamicSubGraphVerifier(BaseVerifier):
    scenario_id = "61"
    title = "SubGraph — Static, Adaptive, CAS and Seal"

    def deploy(self, context: VerificationContext) -> None:
        """Use the runner's deployment and explicit deterministic LLM provider."""

    def init(self, context: VerificationContext) -> None:
        self.prefix = f"vnext-{uuid.uuid4().hex[:10]}"
        self.base = f"/api/v1/{config.NAMESPACE_ID}"

    def verifier(self, context: VerificationContext) -> VerificationResult:
        return self.execute(self._verify)

    def _graph(self, suffix, nodes, edges=()):
        name = f"{self.prefix}-{suffix}"
        API.request("POST", f"{self.base}/agent-graphs", {
            "name": name, "kind": "graph", "runtime_mode": "session",
            "nodes": nodes, "edges": list(edges),
        }, expected=(201,))
        return name

    def _run(self, graph, status="PENDING"):
        return API.request("POST", f"{self.base}/graph-runs", {
            "agent_graph_name": graph, "runtime_mode": "session", "status": status,
        }, expected=(201,))["id"]

    def _wait(self, graph_run_id):
        deadline = time.monotonic() + self.context.timeout_seconds
        while time.monotonic() < deadline:
            run = API.request("GET", f"{self.base}/graph-runs/{graph_run_id}")
            if run["status"] == "COMPLETED":
                return run
            if run["status"] in {"FAILED", "CANCELLED", "PAUSED"}:
                raise AssertionError(f"run stopped: {run}")
            time.sleep(0.5)
        raise AssertionError(f"SubGraph run timed out: {graph_run_id}")

    def _verify(self):
        child = self._graph("child", [
            {"id": "seed", "kind": "noop"},
            {"id": "check", "kind": "noop"},
        ])
        static = self._graph("static", [
            {"id": "repair", "kind": "subgraph", "subgraph": {"ref": child}},
            {"id": "delivery", "kind": "noop"},
        ], [{"from": "repair", "to": "delivery"}])
        graph_run_id = self._run(static)
        self.step("Static SubGraph reference executes child before parent delivery",
                  lambda: self._wait(graph_run_id))
        tasks = API.request("GET", f"{self.base}/graph-runs/{graph_run_id}/node-runs")
        assert {"repair/seed", "repair/check", "delivery"} <= {
            t["node_instance_id"] for t in tasks if t["status"] == "SUCCESS"
        }, tasks

        planner = f"{self.prefix}-planner"
        API.request("POST", f"{self.base}/agents", {
            "name": planner, "model": "e2e-llm/e2e-deterministic",
            "soul": "Flowgent vNext deterministic subgraph planning fixture.",
            "instruction": "Return graph operations for the supplied graph_revision.",
            "max_tokens": 2048,
        }, expected=(201,))
        adaptive = self._graph("adaptive", [{
            "id": "repair", "kind": "subgraph", "subgraph": {
                "ref": child, "mode": "adaptive", "supervisor": planner,
                "mutation": {"allow": ["append", "update_pending_edges", "seal"]},
                "limits": {"max_created_nodes": 16, "max_parallelism": 4,
                           "max_patches": 4},
                "contract": {"required_nodes": ["seed", "check"]},
            },
        }, {"id": "delivery", "kind": "noop"}],
            [{"from": "repair", "to": "delivery"}])
        adaptive_run = self._run(adaptive, "PAUSED")
        API.request("PUT", f"{self.base}/agents/{planner}", {
            "name": planner, "model": "e2e-llm/e2e-deterministic",
            "soul": "A newer persona must not replace a pinned GraphRun agent.",
            "instruction": "Return unrelated new behavior.", "max_tokens": 2048,
        })
        API.request("PUT", f"{self.base}/graph-runs/{adaptive_run}", {"status": "PENDING"})
        completed = self.step("Supervisor expands graph and seals before delivery",
                              lambda: self._wait(adaptive_run))
        graph = completed["subgraphs"]["repair"]
        assert graph["sealed"] and graph["revision"] == 3, graph
        assert "review" in {n["id"] for n in graph["graph"]["nodes"]}, graph
        tasks = API.request("GET", f"{self.base}/graph-runs/{adaptive_run}/node-runs")
        assert any(t["node_instance_id"] == "repair/review" and t["status"] == "SUCCESS"
                   for t in tasks), tasks
        assert all(t.get("node_instance_id") for t in tasks), tasks

        paused = self._run(adaptive, "PAUSED")
        API.request("POST", f"{self.base}/graph-runs/{paused}/subgraphs/initialize",
                    {"instance_id": "repair"})
        patches = [{"id": f"race-{i}", "graph_run_id": paused,
                    "subgraph_instance_id": "repair", "expected_revision": 1,
                    "operations": [{"kind": "append", "node": {
                        "id": f"candidate-{i}", "kind": "noop"}}]}
                   for i in range(2)]

        def submit(patch):
            # HTTP status is the assertion; a conflict response has a text body.
            import requests
            from verifier.context import FlowgentE2EProject
            return requests.post(
                f"{API_BASE}{self.base}/graph-runs/{paused}/graph-patches",
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
            state = API.request("POST", f"{self.base}/graph-runs/{paused}/subgraphs/initialize",
                                {"instance_id": "repair"})
            assert state["revision"] == 2 and len(state["graph"]["nodes"]) == 3

        self.step("concurrent CAS conflict and committed decision replay", conflict)
        API.request("POST", f"{self.base}/graph-runs/{paused}/cancel", expected=(200,))
        self.details.append(f"Static run={graph_run_id}; Adaptive run={adaptive_run}; CAS run={paused}")
