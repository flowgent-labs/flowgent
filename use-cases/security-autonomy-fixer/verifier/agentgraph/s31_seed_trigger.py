"""Scenario 31 — seed the fixer graph, trigger it, and capture the run baseline."""

from __future__ import annotations

import os

from verifier.infra._sonar_scanner import SonarScannerRuntime
from util.report import VerificationResult

from verifier import context as common_api
from verifier.agentgraph import SecurityAutonomyFixture as graph
from verifier.context import RUN_ID_PATH, BaseVerifier, VerificationContext


class E2ES31SeedTriggerVerifier(BaseVerifier):
    """Own the resource seed and first real GraphRun lifecycle."""

    scenario_id = "31"
    title = "E2E Fixer — Seed & Trigger"

    def verifier(self, context: VerificationContext) -> VerificationResult:
        return self.execute(
            lambda: self.step(
                "seed resources, trigger graph, and capture MQTT baseline",
                self._verify_scenario,
            )
        )

    @staticmethod
    def _verify_seed(session) -> tuple[int, int]:
        print("\n-- [31 Seed] Verify agents & MCPs registered --")
        total = len(graph.AGENT_NAMES) + len(graph.MCP_NAMES)
        passed = 0
        for name in graph.AGENT_NAMES:
            response = session.get(f"{graph.API}/api/v1/{graph.NAMESPACE}/agents/{name}")
            if response.status_code == 200:
                expected_model = os.environ["FLOWGENT_E2E_AGENT_MODEL"]
                if response.json().get("model") != expected_model:
                    raise AssertionError(
                        f"Agent {name} did not retain the selected provider/model"
                    )
                passed += 1
            else:
                print(f"  WARN agent '{name}' GET returned {response.status_code}")
        for name in graph.MCP_NAMES:
            response = session.get(f"{graph.API}/api/v1/{graph.NAMESPACE}/mcp/{name}")
            if response.status_code == 200:
                passed += 1
            else:
                print(f"  WARN MCP '{name}' GET returned {response.status_code}")
        print(f"  Result: {passed}/{total} definitions verified via API")
        return passed, total

    @staticmethod
    def _trigger(session, connection) -> str:
        print(
            f"\n-- [31 Trigger] POST /api/v1/{graph.NAMESPACE}/agent-graphs/{graph.AGENT_GRAPH_NAME}/trigger --"
        )
        graph_run_id = None
        response = session.post(
            f"{graph.API}/api/v1/{graph.NAMESPACE}/agent-graphs/{graph.AGENT_GRAPH_NAME}/trigger",
            json={"input": {}},
        )
        if response.status_code in (200, 201, 202):
            payload = response.json() if response.text else {}
            graph_run_id = payload.get("graph_run_id") or payload.get("id")
            print(
                f"  OK manual trigger accepted (status={response.status_code}, graph_run_id={graph_run_id})"
            )
        else:
            print(
                f"  WARN: Manual trigger returned {response.status_code}, trying webhook fallback..."
            )
            payload = {
                "action": "opened",
                "number": 4,
                "pull_request": {
                    "head": {
                        "ref": "fix/flowgent_sec_auto_fix",
                        "sha": "trigger-e2e-abcdef1234567890",
                        "repo": {"full_name": "wl4g/rengine"},
                    },
                    "base": {
                        "ref": "main",
                        "sha": "base-main-abcdef1234567890",
                        "repo": {"full_name": "wl4g/rengine"},
                    },
                },
                "repository": {"full_name": "wl4g/rengine"},
            }
            response = session.post(
                f"{graph.API}/api/v1/webhook/github",
                json=payload,
                headers={
                    "Content-Type": "application/json",
                    "X-GitHub-Event": "pull_request",
                },
            )
            if response.status_code not in (200, 202):
                raise AssertionError(
                    f"Both trigger methods failed. webhook returned {response.status_code}: {response.text[:200]}"
                )
            payload = response.json() if response.text else {}
            graph_run_id = payload.get("graph_run_id") or payload.get("id")
            print(
                f"  OK webhook trigger accepted (status={response.status_code}, graph_run_id={graph_run_id})"
            )
        if not graph_run_id and connection:
            cursor = connection.cursor()
            cursor.execute(
                "SELECT r.id FROM orh_graph_run r JOIN orh_agent_graph f ON f.id=r.agent_graph_id "
                "WHERE f.name=%s AND r.namespace_id=%s ORDER BY r.created_at DESC LIMIT 1",
                (graph.AGENT_GRAPH_NAME, graph.NAMESPACE),
            )
            row = cursor.fetchone()
            if row:
                graph_run_id = row[0]
                print(f"  OK graph_run_id from PG fallback: {graph_run_id}")
        if not graph_run_id:
            raise AssertionError("No graph_run_id from trigger or PG fallback")
        if connection:
            cursor = connection.cursor()
            cursor.execute(
                "SELECT r.id,r.status,f.name,r.runtime_mode FROM orh_graph_run r "
                "JOIN orh_agent_graph f ON f.id=r.agent_graph_id WHERE r.id=%s",
                (graph_run_id,),
            )
            row = cursor.fetchone()
            if row:
                print(
                    f"  OK PG orh_graph_run: status={row[1]} graph={row[2]} runtime_mode={row[3]}"
                )
            else:
                print(
                    "  WARN: graph_run_id not yet visible in PG (may need persistence delay)"
                )
        print(f"  OK graph_run_id={graph_run_id}")
        return graph_run_id

    def _verify_scenario(self) -> None:
        print("\n" + "=" * 60)
        print("  Scenario 31: Seed & Trigger")
        print("=" * 60)
        session = common_api.FlowgentE2EProject.session()
        connection = graph.pg_connect()
        try:
            passed, total = self._verify_seed(session)
            print("\n-- [31 MQTT Audit] Non-$share subscription before trigger --")
            audit = graph.start_global_mqtt_audit()
            graph_run_id = self._trigger(session, connection)
            audit.set_graph_run_id(graph_run_id)
            graph.wait_for_workload_components(graph.AGENT_GRAPH_NAME, graph_run_id=graph_run_id, timeout=240)
            audit.wait_for("ctrl/run/created", timeout=20)
            audit.wait_for(graph.execution_audit_wakeup_suffix(), timeout=45)
            graph.save_mqtt_audit(graph_run_id, graph.snapshot_global_mqtt_audit(graph_run_id))
            graph.assert_mqtt_suffixes(graph_run_id, ["ctrl/run/created"])
            with open(RUN_ID_PATH, "w", encoding="utf-8") as output:
                output.write(graph_run_id)
            SonarScannerRuntime.bind_run(graph_run_id)
        finally:
            if connection:
                connection.close()
        print(
            f"\n{'=' * 60}\n  Scenario 31 Summary\n  Seed:  {passed}/{total}\n  Run ID: {graph_run_id}\n  Stored in: .last_graph_run_id\n{'=' * 60}"
        )
        if passed < total:
            raise AssertionError(f"Seed verification failed: {passed}/{total}")

    def deploy(self, context: VerificationContext) -> None:
        RUN_ID_PATH.unlink(missing_ok=True)
        if context.deployer == "k8s":
            from util.constant import E2EOtherDefinitions as config

            from verifier.infra._application import FlowgentRuntime

            if not FlowgentRuntime._ensure_runtime_credential_secret(
                [context.namespace, config.K8S_WORKLOAD_NAMESPACE]
            ):
                raise AssertionError("Scanner credential references are unavailable")
        SonarScannerRuntime.deploy(context)

    def init(self, context: VerificationContext) -> None:
        """Seed scenario resources and capture the pre-trigger delivery baseline."""
        RUN_ID_PATH.unlink(missing_ok=True)
        baseline = SonarScannerRuntime.initialize(context)
        artifact = self.evidence_path(
            "31.initial-scan", "Official scanner initial analysis", "json"
        )
        import json

        artifact.write_text(json.dumps(baseline, indent=2) + "\n")
        self.record_evidence(
            "31.initial-scan",
            "Repository commit → scanner → CE task → real findings",
            "application/json",
            artifact,
        )
        if os.path.exists(graph.MQTT_AUDIT_PATH):
            os.remove(graph.MQTT_AUDIT_PATH)
        session = common_api.FlowgentE2EProject.session()
        graph.seed_agents_and_mcps(session)
        definition = graph.load_graph_from_yaml()
        print(
            f"\n-- AgentGraph definition: {graph.AGENT_GRAPH_NAME} ({len(definition.get('nodes', []))} nodes, {len(definition.get('edges', []))} edges) --"
        )
        response = graph.upsert_graph(session, definition)
        if response.status_code not in (200, 201):
            raise AssertionError(
                f"upsert graph returned {response.status_code}: {response.text[:200]}"
            )
        print(f"  OK AgentGraph definition ready (status={response.status_code})")
        baseline = graph.capture_pr_baseline()
        print(
            f"  OK PR #{graph.PR_NUMBER} baseline captured: commits={baseline.get('commit_count')} head={baseline.get('head_sha', '')[:8] or 'none'}"
        )
