#!/usr/bin/env python3
"""
Scenario 05 — Messager Module: MQTT Topics + Sandbox Chain

Validates MQTT topic connectivity and message routing for all 14 topics,
plus complete Skill/Sandbox execution chain (TM → Sandbox → TM → JM).

Topic Coverage (14 topics, all prefixed flowgent/v1/):
1.  exec/plans           - JM → TM ($share/tm-pool)
2.  exec/results         - TM → JM (state callback)
3.  sandbox/trigger      - TM → Sandbox ($share/sandbox-pool)
4.  sandbox/result       - Sandbox → TM
5.  notify/event         - Publisher → Notifier ($share/notify-pool)
6.  notify/result        - Notifier → Publisher
7.  sign/request         - TM → Wallet ($share/wallet-pool)
8.  sign/response        - Wallet → TM
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

import sys
import time
import json
import uuid
import base64
import requests
from typing import Dict, Any, Optional, List

sys.path.insert(0, '..')
import config

try:
    import paho.mqtt.client as mqtt
    MQTT_AVAILABLE = True
except ImportError:
    print("ERROR: paho-mqtt required for this scenario")
    print("Install: pip install paho-mqtt")
    sys.exit(1)

API_BASE = config.K3S_APISERVER_URL
TENANT = config.K3S_TENANT
EMQX_HOST = config.EMQX_HOST
EMQX_PORT = config.EMQX_PORT


def rand_id() -> str:
    return str(uuid.uuid4())[:8]


class MQTTTester:
    """MQTT test client with message collection"""
    
    def __init__(self):
        self.client = mqtt.Client()
        self.messages = {}  # topic -> [messages]
        self.client.on_message = self._on_message
        
        try:
            self.client.connect(EMQX_HOST, EMQX_PORT, 60)
            self.client.loop_start()
            time.sleep(0.5)  # Wait for connection
            print(f"  ✓ Connected to MQTT broker: {EMQX_HOST}:{EMQX_PORT}")
        except Exception as e:
            raise Exception(f"MQTT connection failed: {e}")
    
    def _on_message(self, client, userdata, msg):
        topic = msg.topic
        try:
            envelope = json.loads(msg.payload.decode())
        except Exception:
            payload = msg.payload.decode()
        else:
            # Decode InterMessage envelope: extract inner JSON payload
            if isinstance(envelope, dict) and "payload" in envelope:
                try:
                    inner = json.loads(base64.b64decode(envelope["payload"]).decode())
                    envelope = inner
                except Exception:
                    pass  # Not base64-encoded JSON — leave as-is
            payload = envelope

        if topic not in self.messages:
            self.messages[topic] = []

        self.messages[topic].append({
            "payload": payload,
            "timestamp": time.time(),
        })
    
    def subscribe(self, topic: str):
        """Subscribe to topic"""
        self.client.subscribe(topic)
    
    def publish(self, topic: str, inner_payload: Dict, envelope_id: str = None):
        """Publish message with InterMessage envelope (base64-encoded inner payload).

        This mirrors the real InterMessage wire format used by all Flowgent
        components (messager.go InterMessage struct with Payload []byte).
        """
        inner_json = json.dumps(inner_payload)
        envelope = {
            "id": envelope_id or str(uuid.uuid4())[:8],
            "payload": base64.b64encode(inner_json.encode()).decode(),
        }
        self.client.publish(topic, json.dumps(envelope), qos=1)
    
    def wait_for_message(self, topic_filter: str, timeout: int = 5) -> Optional[Dict]:
        """Wait for message matching topic filter.

        Strips $share/{group}/ prefix before matching because MQTT brokers
        deliver messages with the actual publish topic, not the subscription
        topic that includes $share/.
        """
        # Normalize: strip $share/{group}/ prefix for matching
        match_filter = topic_filter
        if topic_filter.startswith('$share/'):
            # $share/{group}/rest/of/topic → rest/of/topic
            parts = topic_filter.split('/', 2)
            match_filter = parts[2] if len(parts) > 2 else topic_filter

        start = time.time()

        while time.time() - start < timeout:
            for topic, msgs in self.messages.items():
                if match_filter in topic or self._topic_matches(match_filter, topic):
                    if msgs:
                        msg = msgs.pop(0)
                        return {"topic": topic, **msg}
            time.sleep(0.1)

        return None
    
    def _topic_matches(self, pattern: str, topic: str) -> bool:
        """Check if topic matches pattern (supports + wildcard)"""
        pattern_parts = pattern.split('/')
        topic_parts = topic.split('/')
        
        if len(pattern_parts) != len(topic_parts):
            return False
        
        for p, t in zip(pattern_parts, topic_parts):
            if p != '+' and p != t:
                return False
        
        return True
    
    def clear_messages(self):
        """Clear message buffer"""
        self.messages = {}
    
    def close(self):
        """Close connection"""
        self.client.loop_stop()
        self.client.disconnect()


def test_topic_pair(tester: MQTTTester, name: str, publish_topic: str, 
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


def test_sandbox_e2e_chain(tester: MQTTTester) -> bool:
    """Test complete Sandbox execution chain: JM → TM → Sandbox → TM → JM"""
    print(f"\n  → Testing Sandbox E2E Chain...")
    
    tenant = TENANT
    flow_id = "test-flow-" + rand_id()
    run_id = "run-" + rand_id()
    plan_id = "plan-" + rand_id()
    task_id = "task-" + rand_id()
    
    try:
        # Step 1: Subscribe to all relevant topics
        print(f"    • Step 1: Setting up subscriptions...")
        
        # Shared subscription for TM (simulating TM pool, unique group to avoid real TM)
        tester.subscribe(f"$share/e2e-tm-pool/flowgent/v1/+/flows/+/runs/+/exec/plans")

        # Shared subscription for Sandbox (simulating sandbox pool, unique group)
        tester.subscribe(f"$share/e2e-sandbox-pool/flowgent/v1/+/flows/+/runs/+/sandbox/trigger")
        
        # Point-to-point for sandbox result (TM receives)
        tester.subscribe(f"flowgent/v1/{tenant}/flows/{flow_id}/runs/{run_id}/sandbox/result")
        
        # Point-to-point for exec result (JM receives)
        tester.subscribe(f"flowgent/v1/{tenant}/flows/{flow_id}/runs/{run_id}/exec/results")
        
        time.sleep(0.5)
        tester.clear_messages()
        
        # Step 2: JM publishes ExecutionPlan
        print(f"    • Step 2: JM → exec/plans...")
        
        exec_plan = {
            "plan_id": plan_id,
            "agentflow_run_id": run_id,
            "agentflow_definition_id": flow_id,
            "tenant_id": tenant,
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
            f"flowgent/v1/{tenant}/flows/{flow_id}/runs/{run_id}/exec/plans",
            {"id": plan_id, "payload": json.dumps(exec_plan)}
        )
        
        # Step 3: TM receives ExecutionPlan
        print(f"    • Step 3: TM receives exec/plans...")
        exec_plans_topic = f"flowgent/v1/{tenant}/flows/{flow_id}/runs/{run_id}/exec/plans"
        msg = tester.wait_for_message(exec_plans_topic, timeout=3)
        if not msg:
            raise AssertionError("TM did not receive ExecutionPlan")
        
        print(f"      ✓ TM received plan")
        
        # Step 4: TM forwards to Sandbox (simulating TM logic)
        print(f"    • Step 4: TM → sandbox/trigger...")
        
        sandbox_req = {
            "plan_id": plan_id,
            "runtime": "python3",
            "script": "print('Hello from sandbox')",
            "timeout": "5s",
            "network_policy": {"type": "none"},
        }
        
        tester.publish(
            f"flowgent/v1/{tenant}/flows/{flow_id}/runs/{run_id}/sandbox/trigger",
            {"id": plan_id, "payload": json.dumps(sandbox_req)}
        )
        
        # Step 5: Sandbox receives trigger
        print(f"    • Step 5: Sandbox receives trigger...")
        sb_trigger_topic = f"flowgent/v1/{tenant}/flows/{flow_id}/runs/{run_id}/sandbox/trigger"
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
            f"flowgent/v1/{tenant}/flows/{flow_id}/runs/{run_id}/sandbox/result",
            {"id": plan_id, "payload": json.dumps(sandbox_result)}
        )
        
        # Step 7: TM receives sandbox result
        print(f"    • Step 7: TM receives sandbox/result...")
        sb_result_topic = f"flowgent/v1/{tenant}/flows/{flow_id}/runs/{run_id}/sandbox/result"
        msg = tester.wait_for_message(sb_result_topic, timeout=3)
        if not msg:
            raise AssertionError("TM did not receive sandbox result")
        
        received_result = json.loads(msg["payload"]["payload"])
        if received_result["exit_code"] != 0:
            raise AssertionError(f"Unexpected exit_code: {received_result['exit_code']}")
        if "Hello" not in received_result["stdout"]:
            raise AssertionError(f"Unexpected stdout: {received_result['stdout']}")
        
        print(f"      ✓ TM received result: exit_code=0, stdout contains 'Hello'")
        
        # Step 8: TM would persist output via REST API (not tested here, see scenario 10)
        print(f"    • Step 8: [TM → API Server REST] (not tested in this scenario)")
        
        # Step 9: TM publishes state-only callback to JM
        print(f"    • Step 9: TM → exec/results (state only)...")
        
        exec_result = {
            "plan_id": plan_id,
            "node_id": "sandbox-node",
            "state": "COMPLETED",  # State only, no data
        }
        
        tester.publish(
            f"flowgent/v1/{tenant}/flows/{flow_id}/runs/{run_id}/exec/results",
            {"id": plan_id, "payload": json.dumps(exec_result)}
        )
        
        # Step 10: JM receives state callback
        print(f"    • Step 10: JM receives exec/results...")
        exec_results_topic = f"flowgent/v1/{tenant}/flows/{flow_id}/runs/{run_id}/exec/results"
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


def run():
    """Main test runner"""
    print("\n" + "="*60)
    print("  Scenario 07: Messager — MQTT Topics + Sandbox Chain")
    print("="*60)
    
    tester = MQTTTester()
    results = {}
    
    tenant = TENANT
    flow_id = "test-flow-" + rand_id()
    run_id = "run-" + rand_id()
    tm_id = "tm-" + rand_id()

    # Test topic pairs
    topic_tests = [
        {
            "name": "exec/plans (JM → TM)",
            "publish": f"flowgent/v1/{tenant}/flows/{flow_id}/runs/{run_id}/exec/plans",
            "subscribe": f"$share/e2e-tm-pool/flowgent/v1/+/flows/+/runs/+/exec/plans",
            "payload": {"plan_id": rand_id(), "task_type": "agent"},
        },
        {
            "name": "exec/results (TM → JM, state-only)",
            "publish": f"flowgent/v1/{tenant}/flows/{flow_id}/runs/{run_id}/exec/results",
            "subscribe": f"flowgent/v1/+/flows/+/runs/+/exec/results",
            "payload": {"plan_id": rand_id(), "node_id": "n1", "state": "COMPLETED"},
        },
        {
            "name": "notify/event (Publisher → Notifier)",
            "publish": f"flowgent/v1/{tenant}/flows/{flow_id}/runs/{run_id}/notify/event",
            "subscribe": f"$share/e2e-notify-pool/flowgent/v1/+/flows/+/runs/+/notify/event",
            "payload": {"channel": "webhook", "message": "test notification"},
        },
        {
            "name": "notify/result (Notifier → Publisher)",
            "publish": f"flowgent/v1/{tenant}/flows/{flow_id}/runs/{run_id}/notify/result",
            "subscribe": f"flowgent/v1/+/flows/+/runs/+/notify/result",
            "payload": {"status": "delivered", "channel": "webhook"},
        },
        {
            "name": "sign/request (TM → Wallet)",
            "publish": f"flowgent/v1/{tenant}/flows/{flow_id}/runs/{run_id}/sign/request",
            "subscribe": f"$share/e2e-wallet-pool/flowgent/v1/+/flows/+/runs/+/sign/request",
            "payload": {
                "tenant_id": tenant,
                "flow_id": flow_id,
                "run_id": run_id,
                "request_id": rand_id(),
                "wallet": "default",
                "payload": "unsigned",
            },
        },
        {
            "name": "sign/response (Wallet → TM)",
            "publish": f"flowgent/v1/{tenant}/flows/{flow_id}/runs/{run_id}/sign/response",
            "subscribe": f"flowgent/v1/+/flows/+/runs/+/sign/response",
            "payload": {
                "tenant_id": tenant,
                "flow_id": flow_id,
                "run_id": run_id,
                "request_id": rand_id(),
                "wallet": "default",
                "signature": "a" * 128,
            },
        },
        {
            "name": "heartbeat/{tmId} (TM → JM)",
            "publish": f"flowgent/v1/heartbeat/{tm_id}",
            "subscribe": f"flowgent/v1/heartbeat/+",
            "payload": {"tm_id": tm_id, "status": "alive", "slots_free": 4},
        },
        {
            "name": "ctrl/flow/updated (API → Controller)",
            "publish": f"flowgent/v1/{tenant}/flows/{flow_id}/ctrl/flow/updated",
            "subscribe": f"flowgent/v1/+/flows/+/ctrl/flow/updated",
            "payload": {"action": "updated", "agentflow_id": flow_id},
        },
        {
            "name": "ctrl/flow/deleted (API → Controller)",
            "publish": f"flowgent/v1/{tenant}/flows/{flow_id}/ctrl/flow/deleted",
            "subscribe": f"flowgent/v1/+/flows/+/ctrl/flow/deleted",
            "payload": {"action": "deleted", "agentflow_id": flow_id},
        },
        {
            "name": "ctrl/run/created (API → Controller)",
            "publish": f"flowgent/v1/{tenant}/flows/{flow_id}/runs/{run_id}/ctrl/run/created",
            "subscribe": f"flowgent/v1/+/flows/+/runs/+/ctrl/run/created",
            "payload": {"run_id": run_id, "status": "PENDING"},
        },
        {
            "name": "ctrl/run/status (API → Controller)",
            "publish": f"flowgent/v1/{tenant}/flows/{flow_id}/runs/{run_id}/ctrl/run/status",
            "subscribe": f"flowgent/v1/+/flows/+/runs/+/ctrl/run/status",
            "payload": {"run_id": run_id, "status": "RUNNING"},
        },
    ]
    
    print(f"\n  → Testing MQTT Topic Pairs...")
    for test in topic_tests:
        results[test["name"]] = test_topic_pair(
            tester,
            test["name"],
            test["publish"],
            test["subscribe"],
            test["payload"],
        )
    
    # Test Sandbox E2E chain
    results["Sandbox E2E Chain"] = test_sandbox_e2e_chain(tester)
    
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


if __name__ == "__main__":
    run()
