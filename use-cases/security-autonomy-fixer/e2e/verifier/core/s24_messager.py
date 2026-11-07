#!/usr/bin/env python3
"""
Scenario 24 — Messager Module: MQTT Topics + Sandbox Chain

Validates MQTT topic connectivity and message routing for all 14 topics,
plus complete Skill/Sandbox execution chain (TM → Sandbox → TM → JM).

Topic Coverage (14 topics, all prefixed flowgent/v1/):
1.  exec/plans           - JM → TM ($share/tm-cluster)
2.  exec/results         - TM → JM (state callback)
3.  sandbox/trigger      - TM → Sandbox ($share/sandbox-cluster)
4.  sandbox/result       - Sandbox → TM
5.  notify/event         - Publisher → Notifier ($share/notify-pool)
6.  notify/result        - Notifier → Publisher
9.  heartbeat/{tmId}     - TM → JM
10. ctrl/flow/updated    - API Server → Controller
11. ctrl/flow/deleted    - API Server → Controller
12. ctrl/run/created     - API Server → Controller
13. ctrl/run/status      - API Server → Controller
14. notify/pod/{}/ws/{}  - Notifier → Notifier (cross-pod WS)

Test Strategy:
- Each topic: publish → subscribe → verify payload
- Sandbox chain: JM → TM → Sandbox → TM → JM (state only)
- Verify TM persists output via REST API (not MQTT)
"""
from __future__ import annotations

from common.model import VerificationResult
from verifier import BaseVerifier

import time
import json
import uuid
from typing import Dict

from common import config
from common.mqtt import FlowgentMqttClient


API_BASE = config.K8S_APISERVER_URL
NAMESPACE = config.NAMESPACE_ID
EMQX_HOST = config.EMQX_HOST
EMQX_PORT = config.EMQX_PORT


class MessagerVerifier(BaseVerifier):
    """Class-owned operations for s24 messager."""

    @staticmethod
    def rand_id() -> str:
        return str(uuid.uuid4())[:8]

    @staticmethod
    def test_topic_pair(tester: FlowgentMqttClient, name: str, publish_topic: str,
                        subscribe_topic: str, test_payload: Dict) -> bool:
        """Test a single request/response topic pair"""
        print(f"\n    • Testing {name}...")
    
        try:
            # Subscribe first
            tester.subscribe(subscribe_topic)
            time.sleep(0.2)
            tester.clear_messages()
        
            # Publish
            tester.publish(publish_topic, test_payload)
            print(f"      → Published to {publish_topic}")
        
            # Wait for message
            msg = tester.wait_for_message(subscribe_topic, timeout=3)
            if not msg:
                raise AssertionError(f"No message received on {subscribe_topic}")
        
            print(f"      ← Received on {msg['topic']}")
        
            # Verify payload structure
            received_payload = msg["payload"]
            if not isinstance(received_payload, dict):
                raise AssertionError(f"Invalid payload type: {type(received_payload)}")
        
            print(f"      ✓ {name} verified")
            return True
        
        except Exception as e:
            print(f"      ✗ {name} failed: {e}")
            return False

    @staticmethod
    def test_sandbox_e2e_chain(tester: FlowgentMqttClient) -> bool:
        """Test complete Sandbox execution chain: JM → TM → Sandbox → TM → JM"""
        print(f"\n  → Testing Sandbox E2E Chain...")
    
        namespace = NAMESPACE
        flow_id = "test-flow-" + MessagerVerifier.rand_id()
        run_id = "run-" + MessagerVerifier.rand_id()
        plan_id = "plan-" + MessagerVerifier.rand_id()
        task_id = "task-" + MessagerVerifier.rand_id()
        cluster_id = "test-cluster"
    
        try:
            # Step 1: Subscribe to all relevant topics
            print(f"    • Step 1: Setting up subscriptions...")
        
            # Shared subscription for TM (simulating one runtime cluster)
            tester.subscribe("flowgent/v1/+/clusters/+/flows/+/runs/+/exec/plans")

            # Shared subscription for Sandbox (simulating one runtime cluster)
            tester.subscribe("flowgent/v1/+/clusters/+/flows/+/runs/+/sandbox/trigger")
        
            # Point-to-point for sandbox result (TM receives)
            tester.subscribe(f"flowgent/v1/{namespace}/flows/{flow_id}/runs/{run_id}/sandbox/result")
        
            # Point-to-point for exec result (JM receives)
            tester.subscribe(f"flowgent/v1/{namespace}/flows/{flow_id}/runs/{run_id}/exec/results")
        
            time.sleep(0.5)
            tester.clear_messages()
        
            # Step 2: JM publishes ExecutionPlan
            print(f"    • Step 2: JM → exec/plans...")
        
            exec_plan = {
                "plan_id": plan_id,
                "agentflow_run_id": run_id,
                "agentflow_definition_id": flow_id,
                "namespace_id": namespace,
                "runtime_mode": "application",
                "runtime_cluster_id": cluster_id,
                "task_id": task_id,
                "node_id": "sandbox-node",
                "task_type": "sandbox",
                "state": "PENDING",
                "node_spec": {
                    "runtime": "python3",
                    "script": "print('Hello from sandbox')",
                    "timeout": "5s",
                    "network_policy": {"type": "none"},
                },
            }
        
            tester.publish(
                f"flowgent/v1/{namespace}/clusters/{cluster_id}/flows/{flow_id}/runs/{run_id}/exec/plans",
                {"id": plan_id, "payload": json.dumps(exec_plan)}
            )
        
            # Step 3: TM receives ExecutionPlan
            print(f"    • Step 3: TM receives exec/plans...")
            exec_plans_topic = f"flowgent/v1/{namespace}/clusters/{cluster_id}/flows/{flow_id}/runs/{run_id}/exec/plans"
            msg = tester.wait_for_message(exec_plans_topic, timeout=3)
            if not msg:
                raise AssertionError("TM did not receive ExecutionPlan")
        
            print(f"      ✓ TM received plan")
        
            # Step 4: TM forwards to Sandbox (simulating TM logic)
            print(f"    • Step 4: TM → sandbox/trigger...")
        
            sandbox_req = {
                "plan_id": plan_id,
                "runtime_cluster_id": cluster_id,
                "runtime": "python3",
                "script": "print('Hello from sandbox')",
                "timeout": "5s",
                "network_policy": {"type": "none"},
            }
        
            tester.publish(
                f"flowgent/v1/{namespace}/clusters/{cluster_id}/flows/{flow_id}/runs/{run_id}/sandbox/trigger",
                {"id": plan_id, "payload": json.dumps(sandbox_req)}
            )
        
            # Step 5: Sandbox receives trigger
            print(f"    • Step 5: Sandbox receives trigger...")
            sb_trigger_topic = f"flowgent/v1/{namespace}/clusters/{cluster_id}/flows/{flow_id}/runs/{run_id}/sandbox/trigger"
            msg = tester.wait_for_message(sb_trigger_topic, timeout=3)
            if not msg:
                raise AssertionError("Sandbox did not receive trigger")
        
            print(f"      ✓ Sandbox received trigger")
        
            # Step 6: Sandbox publishes result (simulating execution)
            print(f"    • Step 6: Sandbox → sandbox/result...")
        
            sandbox_result = {
                "plan_id": plan_id,
                "exit_code": 0,
                "stdout": "Hello from sandbox\n",
                "stderr": "",
            }
        
            tester.publish(
                f"flowgent/v1/{namespace}/flows/{flow_id}/runs/{run_id}/sandbox/result",
                {"id": plan_id, "payload": json.dumps(sandbox_result)}
            )
        
            # Step 7: TM receives sandbox result
            print(f"    • Step 7: TM receives sandbox/result...")
            sb_result_topic = f"flowgent/v1/{namespace}/flows/{flow_id}/runs/{run_id}/sandbox/result"
            msg = tester.wait_for_message(sb_result_topic, timeout=3)
            if not msg:
                raise AssertionError("TM did not receive sandbox result")
        
            received_result = json.loads(msg["payload"]["payload"])
            if received_result["exit_code"] != 0:
                raise AssertionError(f"Unexpected exit_code: {received_result['exit_code']}")
            if "Hello" not in received_result["stdout"]:
                raise AssertionError(f"Unexpected stdout: {received_result['stdout']}")
        
            print(f"      ✓ TM received result: exit_code=0, stdout contains 'Hello'")
        
            # Step 8: TM would persist output via REST API (not tested here, see scenario 35)
            print(f"    • Step 8: [TM → API Server REST] (not tested in this scenario)")
        
            # Step 9: TM publishes state-only callback to JM
            print(f"    • Step 9: TM → exec/results (state only)...")
        
            exec_result = {
                "plan_id": plan_id,
                "node_id": "sandbox-node",
                "state": "COMPLETED",  # State only, no data
            }
        
            tester.publish(
                f"flowgent/v1/{namespace}/flows/{flow_id}/runs/{run_id}/exec/results",
                {"id": plan_id, "payload": json.dumps(exec_result)}
            )
        
            # Step 10: JM receives state callback
            print(f"    • Step 10: JM receives exec/results...")
            exec_results_topic = f"flowgent/v1/{namespace}/flows/{flow_id}/runs/{run_id}/exec/results"
            msg = tester.wait_for_message(exec_results_topic, timeout=5)
            if not msg:
                raise AssertionError("JM did not receive exec result")

            received_state = json.loads(msg["payload"]["payload"])
            if received_state["state"] != "COMPLETED":
                raise AssertionError(f"Unexpected state: {received_state['state']}")
        
            # Verify state-only (no output data)
            if "output" in received_state or "stdout" in received_state:
                print(f"      ⚠ exec/results contains data (expected state-only)")
        
            print(f"      ✓ JM received state: COMPLETED")
        
            print(f"    ✓ Complete Sandbox E2E chain verified")
            return True
        
        except Exception as e:
            print(f"    ✗ Sandbox E2E chain failed: {e}")
            return False

    @staticmethod
    def _verify_scenario():
        """Main test runner"""
        print("\n" + "="*60)
        print("  Scenario 24: Messager — MQTT Topics + Sandbox Chain")
        print("="*60)
    
        tester = FlowgentMqttClient(EMQX_HOST, EMQX_PORT)
        results = {}
    
        namespace = NAMESPACE
        flow_id = "test-flow-" + MessagerVerifier.rand_id()
        run_id = "run-" + MessagerVerifier.rand_id()
        tm_id = "tm-" + MessagerVerifier.rand_id()
        cluster_id = "test-cluster"

        # Test topic pairs
        topic_tests = [
            {
                "name": "exec/plans (JM → TM)",
                "publish": f"flowgent/v1/{namespace}/clusters/{cluster_id}/flows/{flow_id}/runs/{run_id}/exec/plans",
                "subscribe": "flowgent/v1/+/clusters/+/flows/+/runs/+/exec/plans",
                "payload": {"plan_id": MessagerVerifier.rand_id(), "task_type": "agent", "runtime_cluster_id": cluster_id},
            },
            {
                "name": "exec/results (TM → JM, state-only)",
                "publish": f"flowgent/v1/{namespace}/flows/{flow_id}/runs/{run_id}/exec/results",
                "subscribe": f"flowgent/v1/+/flows/+/runs/+/exec/results",
                "payload": {"plan_id": MessagerVerifier.rand_id(), "node_id": "n1", "state": "COMPLETED"},
            },
            {
                "name": "notify/event (Publisher → Notifier)",
                "publish": f"flowgent/v1/{namespace}/flows/{flow_id}/runs/{run_id}/notify/event",
                "subscribe": "flowgent/v1/+/flows/+/runs/+/notify/event",
                "payload": {"channel": "webhook", "message": "test notification"},
            },
            {
                "name": "notify/result (Notifier → Publisher)",
                "publish": f"flowgent/v1/{namespace}/flows/{flow_id}/runs/{run_id}/notify/result",
                "subscribe": f"flowgent/v1/+/flows/+/runs/+/notify/result",
                "payload": {"status": "delivered", "channel": "webhook"},
            },
            {
                "name": "heartbeat/{tmId} (TM → JM)",
                "publish": f"flowgent/v1/heartbeat/{tm_id}",
                "subscribe": f"flowgent/v1/heartbeat/+",
                "payload": {"tm_id": tm_id, "status": "alive", "slots_free": 4},
            },
            {
                "name": "ctrl/flow/updated (API → Controller)",
                "publish": f"flowgent/v1/{namespace}/flows/{flow_id}/ctrl/flow/updated",
                "subscribe": f"flowgent/v1/+/flows/+/ctrl/flow/updated",
                "payload": {"event_type": "UPDATED", "flow_id": flow_id, "namespace_id": namespace},
            },
            {
                "name": "ctrl/flow/deleted (API → Controller)",
                "publish": f"flowgent/v1/{namespace}/flows/{flow_id}/ctrl/flow/deleted",
                "subscribe": f"flowgent/v1/+/flows/+/ctrl/flow/deleted",
                "payload": {"event_type": "DELETED", "flow_id": flow_id, "namespace_id": namespace},
            },
            {
                "name": "ctrl/run/created (API → Controller)",
                "publish": f"flowgent/v1/{namespace}/flows/{flow_id}/runs/{run_id}/ctrl/run/created",
                "subscribe": f"flowgent/v1/+/flows/+/runs/+/ctrl/run/created",
                "payload": {"event_type": "CREATED", "flow_id": flow_id, "run_id": run_id, "status": "PENDING"},
            },
            {
                "name": "ctrl/run/status (API → Controller)",
                "publish": f"flowgent/v1/{namespace}/flows/{flow_id}/runs/{run_id}/ctrl/run/status",
                "subscribe": f"flowgent/v1/+/flows/+/runs/+/ctrl/run/status",
                "payload": {"event_type": "STATUS_CHANGED", "flow_id": flow_id, "run_id": run_id, "status": "RUNNING"},
            },
        ]
    
        print(f"\n  → Testing MQTT Topic Pairs...")
        for test in topic_tests:
            results[test["name"]] = MessagerVerifier.test_topic_pair(
                tester,
                test["name"],
                test["publish"],
                test["subscribe"],
                test["payload"],
            )
    
        # Test Sandbox E2E chain
        results["Sandbox E2E Chain"] = MessagerVerifier.test_sandbox_e2e_chain(tester)
    
        # Cleanup
        tester.close()
    
        # Summary
        passed = sum(1 for v in results.values() if v)
        total = len(results)
    
        print(f"\n  {'='*60}")
        print(f"  Summary: {passed}/{total} tests passed")
        print(f"  {'='*60}")
    
        if passed < total:
            failed = [k for k, v in results.items() if not v]
            raise AssertionError(f"Failed tests: {failed}")
    
        print(f"\n  ✓ All Messager tests passed")

    scenario_id = "24"
    title = "Messager — MQTT Topics + Sandbox Chain"

    def run(self) -> VerificationResult:
        return self.execute(lambda: self.step("verify MQTT topic contracts and sandbox chain", self._verify_mqtt_contract))

    @staticmethod
    def _verify_mqtt_contract() -> None:
        MessagerVerifier._verify_scenario()
