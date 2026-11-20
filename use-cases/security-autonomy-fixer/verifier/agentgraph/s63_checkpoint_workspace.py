"""Vertical checkpoint recovery through real API/JM/TM/Sandbox execution.

Attempt one leaves a partial file and is killed with SIGKILL. Attempt two must restore the
committed baseline before executing. The final GraphCheckpoint and subsequent
ledger are then read through the same recovery endpoint used by JobManager.
"""
from __future__ import annotations

import time
import uuid

from util.constant import E2EOtherDefinitions as config
from util.report import VerificationResult
from verifier.context import BaseVerifier, VerificationContext
from verifier.core.s21_apiserver import E2ES21ApiserverVerifier as API


class E2ES63CheckpointWorkspaceVerifier(BaseVerifier):
    scenario_id = "63"
    title = "Checkpoint — Workspace rollback, retry and committed-state recovery"

    def deploy(self, context: VerificationContext) -> None:
        """Use the runner's canonical runtime and durable workspace volume."""

    def init(self, context: VerificationContext) -> None:
        self.base = f"/api/v1/{config.NAMESPACE_ID}"
        self.name = f"checkpoint-{uuid.uuid4().hex[:10]}"

    def verifier(self, context: VerificationContext) -> VerificationResult:
        return self.execute(self._verify)

    def _verify(self):
        script = r'''set -eu
if [ "${FLOWGENT_NODE_ATTEMPT:-1}" = "1" ]; then
  printf 'incomplete' > partial.txt
  printf '{"stage":"partial"}\n'
  kill -KILL $$
fi
test ! -e partial.txt
printf 'verified artifact' > artifact.txt
printf '{"stage":"verified","workspace_restored":true}\n'
'''
        API.request("POST", self.base + "/agent-graphs", {
            "name": self.name, "kind": "graph", "runtime_mode": "session",
            "nodes": [{"id": "mutate", "kind": "sandbox", "runtime": "bash",
                       "script": script, "network_policy": {"mode": "none"},
                       "retry": {"max": 1, "initial": "10ms", "max_delay": "10ms", "factor": 1}}],
            "edges": [],
        }, expected=(201,))
        run = API.request("POST", self.base + "/graph-runs", {
            "agent_graph_name": self.name, "runtime_mode": "session",
        }, expected=(201,))
        graph_run_id = run["id"]
        path = f"{self.base}/graph-runs/{graph_run_id}"

        def wait_complete():
            deadline = time.monotonic() + self.context.timeout_seconds
            while time.monotonic() < deadline:
                state = API.request("GET", path)
                if state["status"] == "COMPLETED":
                    return state
                if state["status"] in {"FAILED", "CANCELLED", "PAUSED"}:
                    raise AssertionError(f"recovery run stopped: {state.get('error')}")
                time.sleep(0.5)
            raise AssertionError(f"workspace retry timed out: {graph_run_id}")

        completed = self.step("SIGKILL during mutation restores baseline before retry", wait_complete)
        nodes = sorted(completed["node_runs"], key=lambda item: item["attempt"])
        assert len(nodes) == 2, nodes
        assert [node["status"] for node in nodes] == ["FAILED", "SUCCESS"], nodes
        assert nodes[0]["node_instance_id"] == nodes[1]["node_instance_id"]
        assert nodes[0]["id"] != nodes[1]["id"]
        assert nodes[1]["output"].get("workspace_restored") is True, nodes[1]
        baseline = nodes[0]["checkpoint"]["workspace_snapshot"]
        final = nodes[1]["workspace_snapshot"]
        assert baseline["sha256"] and final["sha256"] and baseline["id"] != final["id"]

        def consistent_recovery():
            checkpoint = API.request("POST", path + "/checkpoints", expected=(201,))
            recovered = API.request("GET", path + "/recovery")
            assert recovered["checkpoint"]["id"] == checkpoint["id"]
            assert recovered["snapshot"]["id"] == run["agent_graph_id"]
            assert checkpoint["sha256"] and checkpoint["event_seq"] > 0
            assert len(recovered["node_runs"]) == 2
            latest = max(recovered["node_runs"], key=lambda node: node["attempt"])
            assert latest["status"] == "SUCCESS" and latest["workspace_snapshot"] == final
            # The published definition can change while this run's snapshot stays fixed.
            graph = API.request("GET", f"{self.base}/agent-graphs/{self.name}")
            graph["nodes"] = [{"id": "different", "kind": "noop"}]
            API.request("PUT", f"{self.base}/agent-graphs/{self.name}", graph)
            locked = API.request("GET", path + "/snapshot")
            assert locked["nodes"][0]["id"] == "mutate", locked

        self.step("consistent GraphCheckpoint preserves completed attempts and pinned graph", consistent_recovery)
        self.details.append(f"workspace and checkpoint recovery run={graph_run_id}")
