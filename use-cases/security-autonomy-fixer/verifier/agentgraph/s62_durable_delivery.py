"""Verify durable dispatch/result contracts using real REST and PostgreSQL.

This scenario verifies transaction boundaries and duplicate/late delivery.
It does not claim a JM/TM/EMQX simultaneous process crash or workspace recovery.
"""
from __future__ import annotations

import uuid

import requests
from util.constant import E2EOtherDefinitions as config
from util.report import VerificationResult
from verifier.context import BaseVerifier, FlowgentE2EProject, VerificationContext
from verifier.core.s21_apiserver import API_BASE, E2ES21ApiserverVerifier as API


class E2ES62DurableDeliveryVerifier(BaseVerifier):
    scenario_id = "62"
    title = "Durable execution — Claims, immutable results and Outbox"

    def deploy(self, context: VerificationContext) -> None:
        """Reuse the runner's API, PostgreSQL and EMQX deployment."""

    def init(self, context: VerificationContext) -> None:
        self.base = f"/api/v1/{config.NAMESPACE_ID}"
        self.prefix = f"vnext-delivery-{uuid.uuid4().hex[:8]}"

    def verifier(self, context: VerificationContext) -> VerificationResult:
        return self.execute(self._verify)

    def _raw(self, method, path, body):
        return requests.request(
            method, API_BASE + path, json=body,
            headers=FlowgentE2EProject.headers(), timeout=20,
        )

    def _verify(self):
        API.request("POST", f"{self.base}/agent-graphs", {
            "name": self.prefix, "kind": "graph", "runtime_mode": "session",
            "nodes": [{"id": f"agent{i}", "kind": "noop"} for i in range(10)],
            "edges": [],
        }, expected=(201,))
        run = API.request("POST", f"{self.base}/graph-runs", {
            "agent_graph_name": self.prefix, "runtime_mode": "session", "status": "PAUSED",
        }, expected=(201,))["id"]
        path = f"{self.base}/graph-runs/{run}"
        ids = []
        for i in range(10):
            node_run_id = f"{self.prefix}-{i}"
            ids.append(node_run_id)
            API.request("POST", path + "/node-runs", {
                "id": node_run_id, "node_instance_id": f"agent{i}",
                "node_instance_id": f"agent{i}", "attempt": 1,
                "dispatch_epoch": 1, "fencing_token": 1,
                "execution_id": f"plan-{node_run_id}-attempt-1", "status": "PENDING",
            }, expected=(201,))

        def claims():
            for i, node_run_id in enumerate(ids):
                claim = {"node_instance_id": f"agent{i}", "dispatch_epoch": 1,
                         "owner": "e2e-recovery/slot"}
                assert API.request("POST", f"{path}/node-runs/{node_run_id}/claim", claim)["claimed"]
                assert not API.request("POST", f"{path}/node-runs/{node_run_id}/claim", claim)["claimed"]
            tasks = API.request("GET", path + "/node-runs")
            assert len(tasks) == 10 and all(t["status"] == "RUNNING" for t in tasks)
        self.step("10 persistent claims reject duplicate MQTT delivery identities", claims)

        def results():
            for i, node_run_id in enumerate(ids):
                body = {"node_instance_id": f"agent{i}", "node_instance_id": f"agent{i}",
                        "attempt": 1, "dispatch_epoch": 1, "fencing_token": 1,
                        "execution_id": f"plan-{node_run_id}-attempt-1",
                        "status": "SUCCESS", "output": {"receipt": node_run_id}}
                API.request("PUT", f"{path}/node-runs/{node_run_id}", body)
                API.request("PUT", f"{path}/node-runs/{node_run_id}", body)
                body["output"] = {"receipt": "different"}
                rejected = self._raw("PUT", f"{path}/node-runs/{node_run_id}", body)
                assert rejected.status_code == 409, rejected.text
            connection = API.pg_connect()
            try:
                count = API.scalar(connection,
                    "SELECT COUNT(*) FROM orh_outbox WHERE aggregate_id=ANY(%s) "
                    "AND event_type='node_result'", (ids,))
                assert count == 10, count
                tasks = API.request("GET", path + "/node-runs")
                assert all(t["output"]["receipt"] == t["id"] for t in tasks), tasks
            finally:
                connection.close()
        self.step("result + Outbox commit together; duplicate completion is immutable", results)

        def obsolete():
            node = "attempt-race"
            old_id = self.prefix + "-old"
            new_id = self.prefix + "-new"
            for attempt, node_run_id in ((1, old_id), (2, new_id)):
                API.request("POST", path + "/node-runs", {
                    "id": node_run_id, "node_instance_id": node, "node_instance_id": node,
                    "attempt": attempt, "dispatch_epoch": attempt,
                    "fencing_token": attempt, "execution_id": node_run_id,
                    "status": "PENDING",
                }, expected=(201,))
            response = self._raw("PUT", f"{path}/node-runs/{old_id}", {
                "node_instance_id": node, "node_instance_id": node, "attempt": 1,
                "dispatch_epoch": 1, "fencing_token": 1, "execution_id": old_id,
                "status": "SUCCESS", "output": {"late": True},
            })
            assert response.status_code == 409, response.text
            current = API.request("GET", f"{path}/node-runs/{new_id}")
            assert current["status"] == "PENDING", current
        self.step("late Attempt cannot advance its superseding logical instance", obsolete)
        API.request("POST", path + "/cancel", expected=(200,))
        self.details.append(f"Durable result/duplicate/late-message run={run}")
