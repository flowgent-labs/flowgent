"""Scenario 31 — seed the fixer flow, trigger it, and capture the run baseline."""

from __future__ import annotations

import os

from common import project as common_api
from common.model import RunContext, VerificationResult
from common.project import RUN_ID_PATH
from verifier.agentflow.support import SecurityAutonomyFixture as flow
from verifier.agentflow.base import AgentFlowVerifier


class SeedTriggerVerifier(AgentFlowVerifier):
    """Own the resource seed and first real FlowRun lifecycle."""

    scenario_id = "31"
    title = "E2E Fixer — Seed & Trigger"

    def run(self) -> VerificationResult:
        return self.execute(
            lambda: self.step(
                "seed resources, trigger flow, and capture MQTT baseline",
                self._verify_scenario,
            )
        )

    @staticmethod
    def _verify_seed(session) -> tuple[int, int]:
        print("\n-- [31 Seed] Verify agents & MCPs registered --")
        total = len(flow.AGENT_NAMES) + len(flow.MCP_NAMES)
        passed = 0
        for name in flow.AGENT_NAMES:
            response = session.get(f"{flow.API}/api/v1/{flow.NAMESPACE}/agents/{name}")
            if response.status_code == 200:
                passed += 1
            else:
                print(f"  WARN agent '{name}' GET returned {response.status_code}")
        for name in flow.MCP_NAMES:
            response = session.get(f"{flow.API}/api/v1/{flow.NAMESPACE}/mcp/{name}")
            if response.status_code == 200:
                passed += 1
            else:
                print(f"  WARN MCP '{name}' GET returned {response.status_code}")
        print(f"  Result: {passed}/{total} definitions verified via API")
        return passed, total

    @staticmethod
    def _trigger(session, connection) -> str:
        print(f"\n-- [31 Trigger] POST /api/v1/{flow.NAMESPACE}/flows/{flow.FLOW_ID}/trigger --")
        run_id = None
        response = session.post(
            f"{flow.API}/api/v1/{flow.NAMESPACE}/flows/{flow.FLOW_ID}/trigger",
            json={"vars": {}},
        )
        if response.status_code in (200, 201, 202):
            payload = response.json() if response.text else {}
            run_id = payload.get("run_id") or payload.get("id")
            print(f"  OK manual trigger accepted (status={response.status_code}, run_id={run_id})")
        else:
            print(f"  WARN: Manual trigger returned {response.status_code}, trying webhook fallback...")
            payload = {
                "action": "opened",
                "number": 4,
                "pull_request": {
                    "head": {"ref": "fix/flowgent_sec_auto_fix", "sha": "trigger-e2e-abcdef1234567890", "repo": {"full_name": "wl4g/rengine"}},
                    "base": {"ref": "main", "sha": "base-main-abcdef1234567890", "repo": {"full_name": "wl4g/rengine"}},
                },
                "repository": {"full_name": "wl4g/rengine"},
            }
            response = session.post(
                f"{flow.API}/api/v1/webhook/github",
                json=payload,
                headers={"Content-Type": "application/json", "X-GitHub-Event": "pull_request"},
            )
            if response.status_code not in (200, 202):
                raise AssertionError(f"Both trigger methods failed. webhook returned {response.status_code}: {response.text[:200]}")
            payload = response.json() if response.text else {}
            run_id = payload.get("run_id") or payload.get("id")
            print(f"  OK webhook trigger accepted (status={response.status_code}, run_id={run_id})")
        if not run_id and connection:
            cursor = connection.cursor()
            cursor.execute("SELECT id FROM orh_flowrun WHERE agentflow_id=%s ORDER BY created_at DESC LIMIT 1", (flow.FLOW_ID,))
            row = cursor.fetchone()
            if row:
                run_id = row[0]
                print(f"  OK run_id from PG fallback: {run_id}")
        if not run_id:
            raise AssertionError("No run_id from trigger or PG fallback")
        if connection:
            cursor = connection.cursor()
            cursor.execute("SELECT id, status, agentflow_id, runtime_mode FROM orh_flowrun WHERE id=%s", (run_id,))
            row = cursor.fetchone()
            if row:
                print(f"  OK PG orh_flowrun: status={row[1]} flow={row[2]} runtime_mode={row[3]}")
            else:
                print("  WARN: run_id not yet visible in PG (may need persistence delay)")
        print(f"  OK run_id={run_id}")
        return run_id

    def _verify_scenario(self) -> None:
        print("\n" + "=" * 60)
        print("  Scenario 31: Seed & Trigger")
        print("=" * 60)
        session = common_api.FlowgentE2EProject.session()
        if os.path.exists(flow.MQTT_AUDIT_PATH):
            os.remove(flow.MQTT_AUDIT_PATH)
        connection = flow.pg_connect()
        try:
            flow.seed_agents_and_mcps(session)
            passed, total = self._verify_seed(session)
            definition = flow.load_flow_from_yaml()
            print(f"\n-- Flow definition: {flow.FLOW_ID} ({len(definition.get('nodes', []))} nodes, {len(definition.get('edges', []))} edges) --")
            response = flow.upsert_flow(session, definition)
            if response.status_code not in (200, 201):
                raise AssertionError(f"upsert flow returned {response.status_code}: {response.text[:200]}")
            print(f"  OK Flow definition ready (status={response.status_code})")
            baseline = flow.capture_pr_baseline()
            print(f"  OK PR #{flow.PR_NUMBER} baseline captured: commits={baseline.get('commit_count')} head={baseline.get('head_sha', '')[:8] or 'none'}")
            print("\n-- [31 MQTT Audit] Non-$share subscription before trigger --")
            audit = flow.start_global_mqtt_audit()
            run_id = self._trigger(session, connection)
            audit.set_run_id(run_id)
            flow.wait_for_workload_components(flow.FLOW_ID, run_id=run_id, timeout=240)
            audit.wait_for("ctrl/run/created", timeout=20)
            audit.wait_for(flow.execution_audit_wakeup_suffix(), timeout=45)
            flow.save_mqtt_audit(run_id, flow.snapshot_global_mqtt_audit(run_id))
            flow.assert_mqtt_suffixes(run_id, ["ctrl/run/created"])
            with open(RUN_ID_PATH, "w", encoding="utf-8") as output:
                output.write(run_id)
        finally:
            if connection:
                connection.close()
        print(f"\n{'=' * 60}\n  Scenario 31 Summary\n  Seed:  {passed}/{total}\n  Run ID: {run_id}\n  Stored in: .last_run_id\n{'=' * 60}")
        if passed < total:
            raise AssertionError(f"Seed verification failed: {passed}/{total}")

    @staticmethod
    def verify(context: RunContext) -> VerificationResult:
        """Create and run this scenario's class-owned verifier entrypoint."""
        return SeedTriggerVerifier(context).run()



VERIFIER_CLASS = SeedTriggerVerifier
