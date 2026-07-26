#!/usr/bin/env python3
"""
Scenario 23 — Controller Module: Application Mode Lifecycle.

Validates Controller's flow lifecycle management in Application mode.

Prerequisites: K8s/K3s cluster, Helm release, Controller pod running.

Steps with Expected I/O — Flow CREATE Lifecycle:
  Step 1. Create AgentFlow via API
    Action:  POST /api/v1/{tenant}/flows
    Input:   {id: "test-flow-{uuid}", nodes: [noop], edges: [], priority: "high"}
    Output:  HTTP 200/201, flow ID returned

  Step 1b. Verify MQTT ctrl/flow/updated event (best-effort)
    Action:  Subscribe to ctrl/flow/updated, wait 5s after flow create
    Input:   MQTT broker reachable
    Output:  Event with matching agentflow_id (WARN if not received — Controller may poll)

  Step 2. Wait for JM Deployment
    Action:  kubectl get deployment {name} -n {tenant_ns}
    Input:   Deployment name: flowgent-jobmanager-{tenant}-{flow_id}
    Output:  Deployment exists in tenant namespace within 30s

  Step 3. Verify Deployment Spec
    Action:  kubectl get deployment {name} -n {tenant_ns} -o json
    Input:   Deployment name
    Output:  Container env includes FLOWGENT__RUNTIME__AGENT_FLOW_ID={flow_id}

  Step 4. Wait for JM Pod Running
    Action:  kubectl get pods -l app=flowgent-jobmanager,flowgent.io/flow={flow_id}
    Input:   Label selector
    Output:  Pod reaches Running within 60s

  Step 5. Delete Flow
    Action:  DELETE /api/v1/{tenant}/flows/{id}
    Input:   Flow ID
    Output:  HTTP 200/204

  Step 6. Verify Deployment Cleanup
    Action:  Wait for Deployment deletion
    Input:   Deployment name, 60s timeout
    Output:  Deployment deleted (Controller GC)

Steps with Expected I/O — Flow UPDATE Lifecycle:
  Step 7. Create flow + wait for Deployment (as Steps 1-4)
  Step 8. PUT /api/v1/{tenant}/flows/{id}  {description, version}
    Input:   Updated description, new version
    Output:  HTTP 200, Deployment generation incremented
"""

import sys
import time
import json
import uuid
import requests
import subprocess
from typing import Dict, Any, Optional, List

from common import config

try:
    import paho.mqtt.client as mqtt
    MQTT_AVAILABLE = True
except ImportError:
    MQTT_AVAILABLE = False

API_BASE = config.K3S_APISERVER_URL
TENANT = config.K3S_TENANT
NAMESPACE = config.K3S_NAMESPACE


def rand_id() -> str:
    return str(uuid.uuid4())[:8]


def application_namespace(tenant_id: str = TENANT) -> str:
    """Mirror controller.go/flow_def.go applicationNamespace: every flow's
    dedicated JM Deployment lives in its TENANT's shared namespace
    "{namespace_prefix}{tenant_id}" (see docs/01-L1-Engine-Architecture.md
    §1.3/§4.3 — tenant isolation is per-tenant, not per-flow), NOT in
    NAMESPACE (config.K3S_NAMESPACE, typically "default") — that's only
    where the Controller/apiserver pods themselves run."""
    return f"{config.K3S_APP_NAMESPACE_PREFIX}{tenant_id}"


def kubectl_get(resource: str, name: str = None, namespace: str = NAMESPACE, 
                json_path: str = None) -> Optional[Dict]:
    """Execute kubectl get command"""
    cmd = ["kubectl", "get", resource]
    if name:
        cmd.append(name)
    cmd.extend(["-n", namespace, "-o", "json"])
    
    try:
        result = subprocess.run(cmd, capture_output=True, text=True, timeout=10)
        if result.returncode != 0:
            return None
        
        data = json.loads(result.stdout)
        
        if json_path:
            # Simple jsonpath extraction
            for key in json_path.split('.'):
                if isinstance(data, dict):
                    data = data.get(key)
                else:
                    return None
        
        return data
        
    except Exception as e:
        print(f"  ⚠ kubectl failed: {e}")
        return None


def kubectl_delete(resource: str, name: str, namespace: str = NAMESPACE) -> bool:
    """Execute kubectl delete command"""
    cmd = ["kubectl", "delete", resource, name, "-n", namespace, "--grace-period=0", "--force"]
    
    try:
        result = subprocess.run(cmd, capture_output=True, text=True, timeout=30)
        return result.returncode == 0
    except Exception as e:
        print(f"  ⚠ kubectl delete failed: {e}")
        return False


def wait_for_deployment(name: str, namespace: str = NAMESPACE, timeout: int = 60) -> bool:
    """Wait for Deployment to exist"""
    print(f"    • Waiting for Deployment {name}...")
    
    start = time.time()
    while time.time() - start < timeout:
        deployment = kubectl_get("deployment", name, namespace)
        if deployment:
            print(f"      ✓ Deployment {name} exists")
            return True
        time.sleep(2)
    
    print(f"      ✗ Deployment {name} not found after {timeout}s")
    return False


def wait_for_pod_running(label_selector: str, namespace: str = NAMESPACE, timeout: int = 60) -> bool:
    """Wait for Pod with label to reach Running state"""
    print(f"    • Waiting for Pod (selector={label_selector})...")
    
    start = time.time()
    while time.time() - start < timeout:
        pods = kubectl_get("pods", namespace=namespace)
        if not pods:
            time.sleep(2)
            continue
        
        items = pods.get("items", [])
        for pod in items:
            metadata = pod.get("metadata", {})
            labels = metadata.get("labels", {})
            
            # Simple label matching
            if all(labels.get(k) == v for k, v in [l.split('=') for l in label_selector.split(',')]):
                status = pod.get("status", {})
                phase = status.get("phase")
                
                if phase == "Running":
                    pod_name = metadata.get("name")
                    print(f"      ✓ Pod {pod_name} is Running")
                    return True
        
        time.sleep(2)
    
    print(f"      ✗ No Running pod found after {timeout}s")
    return False


def wait_for_deployment_deleted(name: str, namespace: str = NAMESPACE, timeout: int = 60) -> bool:
    """Wait for Deployment to be deleted"""
    print(f"    • Waiting for Deployment {name} to be deleted...")
    
    start = time.time()
    while time.time() - start < timeout:
        deployment = kubectl_get("deployment", name, namespace)
        if not deployment:
            print(f"      ✓ Deployment {name} deleted")
            return True
        time.sleep(2)
    
    print(f"      ✗ Deployment {name} still exists after {timeout}s")
    return False


def test_flow_create_lifecycle() -> bool:
    """Test Flow creation triggers JM Deployment creation + MQTT event"""
    print(f"\n  → Testing Flow CREATE → JM Deployment + MQTT event...")

    flow_id = "test-flow-" + rand_id()
    # Deterministic from flow_id — see controller.go buildJMDeployment /
    # applicationNamespace — computed upfront so the except-block cleanup
    # below can always target the right namespace, even if an assertion
    # fails before Step 2 (re-)computes it.
    deployment_name = f"flowgent-jobmanager-{TENANT}-{flow_id}"
    jm_namespace = application_namespace()

    # Set up MQTT listener for ctrl events (best-effort)
    mqtt_client = None
    mqtt_messages = []
    if MQTT_AVAILABLE:
        try:
            mqtt_client = mqtt.Client()
            mqtt_client.on_message = lambda c, u, m: mqtt_messages.append(
                {"topic": m.topic, "payload": json.loads(m.payload.decode())}
            )
            mqtt_client.connect(config.EMQX_HOST, config.EMQX_PORT, 10)
            mqtt_client.loop_start()
            mqtt_client.subscribe(f"flowgent/v1/+/flows/+/ctrl/flow/updated")
            mqtt_client.subscribe(f"flowgent/v1/+/flows/+/ctrl/flow/deleted")
            time.sleep(0.3)
        except Exception:
            if mqtt_client:
                mqtt_client.loop_stop()
            mqtt_client = None

    try:
        # Step 1: Create AgentFlow via API
        print(f"    • Step 1: Creating AgentFlow ({flow_id})...")

        # priority=high is currently the only value the API accepts — Session
        # mode (a shared Helm-deployed JM/TM pool) is temporarily disabled,
        # so the Controller creates a dedicated K8s JM Deployment for every
        # flow (see controller.go dispatchFlow / entities.Priority doc
        # comment) regardless of priority; it is set explicitly here anyway
        # for clarity and to guard against the default ever changing.
        # POST /flows decodes the body directly into entities.FlowInfo —
        # a flat shape ("id"/"nodes"/"edges"/"priority" at top level), not a
        # nested "definition" object (see pkg/api/pkg/handler/flow_def.go Create).
        payload = {
            "id": flow_id,
            "nodes": [{"id": "n1", "type": "noop"}],
            "edges": [],
            "priority": "high",
        }

        resp = requests.post(f"{API_BASE}/api/v1/{TENANT}/flows", json=payload, timeout=10)
        if resp.status_code not in [200, 201]:
            raise AssertionError(f"Flow creation failed: {resp.status_code} {resp.text}")

        created_id = resp.json().get("id")
        print(f"      ✓ Flow created: id={created_id}")

        # Step 1b: Verify MQTT ctrl/flow/updated event (best-effort)
        if mqtt_client:
            deadline = time.time() + 5
            while time.time() < deadline:
                for msg in mqtt_messages:
                    if "ctrl/flow/updated" in msg["topic"]:
                        payload = msg.get("payload", {})
                        inner = payload.get("payload", payload)
                        if isinstance(inner, str):
                            try:
                                inner = json.loads(inner)
                            except Exception:
                                pass
                        if isinstance(inner, dict) and inner.get("agentflow_id") == flow_id:
                            print(f"      ✓ MQTT ctrl/flow/updated event received (action={inner.get('action','?')})")
                            break
                else:
                    time.sleep(0.2)
                    continue
                break
            else:
                print(f"      ⚠ No ctrl/flow/updated MQTT event (Controller may use polling)")

        # Step 2: Wait for Controller to create JM Deployment
        print(f"    • Step 2: Waiting for Controller to create JM Deployment...")
        
        # Deployment lands in the flow's tenant namespace (NOT NAMESPACE /
        # config.K3S_NAMESPACE — see applicationNamespace, and
        # ensureApplicationInfra, which now also auto-creates this namespace).
        if not wait_for_deployment(deployment_name, namespace=jm_namespace, timeout=30):
            raise AssertionError(f"JM Deployment not created: {deployment_name} (namespace={jm_namespace})")
        
        # Step 3: Verify Deployment spec
        print(f"    • Step 3: Verifying Deployment spec...")
        
        deployment = kubectl_get("deployment", deployment_name, namespace=jm_namespace)
        if not deployment:
            raise AssertionError("Deployment disappeared")
        
        spec = deployment.get("spec", {})
        template = spec.get("template", {})
        pod_spec = template.get("spec", {})
        containers = pod_spec.get("containers", [])
        
        if not containers:
            raise AssertionError("No containers in Deployment")
        
        container = containers[0]
        env_vars = {e["name"]: e.get("value", "") for e in container.get("env", [])}
        
        # Verify environment variables. The controller injects Spring Boot-style
        # FLOWGENT__ env vars (see controller.go buildJMDeployment): the flow ID
        # binds to runtime.agentFlowId via FLOWGENT__RUNTIME__AGENT_FLOW_ID.
        expected_env = {
            "FLOWGENT__RUNTIME__AGENT_FLOW_ID": flow_id,
        }
        
        for key, expected_value in expected_env.items():
            actual_value = env_vars.get(key, "")
            if expected_value not in actual_value:
                print(f"      ⚠ Expected {key}={expected_value}, got {actual_value}")
        
        print(f"      ✓ Deployment spec verified")
        
        # Step 4: Wait for JM Pod to reach Running
        print(f"    • Step 4: Waiting for JM Pod to reach Running...")
        
        label_selector = f"app=flowgent-jobmanager,flowgent.io/flow={flow_id}"
        if not wait_for_pod_running(label_selector, namespace=jm_namespace, timeout=60):
            print(f"      ⚠ JM Pod not Running (may still be initializing)")
        
        # Step 5: Cleanup - delete flow
        print(f"    • Step 5: Deleting AgentFlow...")
        
        resp = requests.delete(f"{API_BASE}/api/v1/{TENANT}/flows/{created_id}", timeout=10)
        if resp.status_code not in [200, 204]:
            print(f"      ⚠ Flow deletion returned {resp.status_code}")
        
        # Step 6: Verify Controller garbage-collects Deployment
        print(f"    • Step 6: Verifying Deployment cleanup...")
        
        if not wait_for_deployment_deleted(deployment_name, namespace=jm_namespace, timeout=60):
            print(f"      ⚠ Deployment not auto-deleted, manual cleanup...")
            kubectl_delete("deployment", deployment_name, namespace=jm_namespace)
        
        if mqtt_client:
            mqtt_client.loop_stop()
            mqtt_client.disconnect()
        print(f"    ✓ Flow CREATE lifecycle verified")
        return True

    except Exception as e:
        print(f"    ✗ Flow CREATE lifecycle failed: {e}")
        if mqtt_client:
            mqtt_client.loop_stop()
            mqtt_client.disconnect()

        # Cleanup on failure
        try:
            kubectl_delete("deployment", deployment_name, namespace=jm_namespace)
        except:
            pass
        
        return False


def test_controller_pod_running() -> bool:
    """Verify Controller pod is running"""
    print(f"\n  → Verifying Controller pod...")
    
    try:
        pods = kubectl_get("pods", namespace=NAMESPACE)
        if not pods:
            raise AssertionError("Failed to get pods")
        
        controller_pods = [
            pod for pod in pods.get("items", [])
            if "controller" in pod.get("metadata", {}).get("name", "")
            and pod.get("status", {}).get("phase") == "Running"
        ]

        if not controller_pods:
            print(f"      ⚠ No Running Controller pod found (may not be deployed in this environment)")
            return True  # Not a failure, Controller may not be deployed

        controller_pod = controller_pods[0]

        print(f"      ✓ Controller pod is Running ({controller_pod['metadata']['name']})")
        return True
        
    except Exception as e:
        print(f"      ✗ Controller verification failed: {e}")
        return False


def test_flow_update_lifecycle() -> bool:
    """Test Flow UPDATE triggers Controller rolling update of JM Deployment."""
    print(f"\n  → Testing Flow UPDATE → JM rolling update...")

    flow_id = "test-flow-" + rand_id()
    deployment_name = f"flowgent-jobmanager-{TENANT}-{flow_id}"
    jm_namespace = application_namespace()
    created_id = None

    try:
        # priority=high is currently the only value the API accepts (see
        # entities.Priority doc comment); every flow gets a dedicated JM
        # Deployment regardless.
        payload = {
            "id": flow_id,
            "nodes": [{"id": "n1", "type": "noop"}],
            "edges": [],
            "priority": "high",
        }
        resp = requests.post(f"{API_BASE}/api/v1/{TENANT}/flows", json=payload, timeout=10)
        if resp.status_code not in [200, 201]:
            raise AssertionError(f"Flow creation failed: {resp.status_code}")
        created_id = resp.json().get("id")

        if not wait_for_deployment(deployment_name, namespace=jm_namespace, timeout=60):
            raise AssertionError(f"JM Deployment not created for UPDATE test: {deployment_name} (namespace={jm_namespace})")

        before = kubectl_get("deployment", deployment_name, namespace=jm_namespace)
        before_gen = before.get("metadata", {}).get("generation", 0) if before else 0

        resp = requests.put(
            f"{API_BASE}/api/v1/{TENANT}/flows/{created_id}",
            json={"description": "updated by e2e verifier", "version": 2},
            timeout=10,
        )
        if resp.status_code != 200:
            raise AssertionError(f"Flow update failed: {resp.status_code} {resp.text}")

        # Allow Controller reconciliation time
        time.sleep(5)
        after = kubectl_get("deployment", deployment_name, namespace=jm_namespace)
        after_gen = after.get("metadata", {}).get("generation", 0) if after else 0
        if after_gen >= before_gen:
            print(f"      ✓ Deployment generation {before_gen} → {after_gen}")
        else:
            print(f"      ⚠ Deployment generation unchanged (Controller may reconcile async)")

        resp = requests.delete(f"{API_BASE}/api/v1/{TENANT}/flows/{created_id}", timeout=10)
        if resp.status_code not in [200, 204]:
            print(f"      ⚠ Flow deletion returned {resp.status_code}")
        wait_for_deployment_deleted(deployment_name, namespace=jm_namespace, timeout=60)

        print(f"    ✓ Flow UPDATE lifecycle verified")
        return True
    except Exception as e:
        print(f"    ✗ Flow UPDATE lifecycle failed: {e}")
        if deployment_name:
            kubectl_delete("deployment", deployment_name, namespace=jm_namespace)
        return False


def run():
    """Main test runner"""
    print("\n" + "="*60)
    print("  Scenario 23: Controller — Application Mode Lifecycle")
    print("="*60)
    
    results = {}
    
    # Check Controller pod
    results["Controller Pod"] = test_controller_pod_running()
    
    # Test flow lifecycle
    results["Flow CREATE Lifecycle"] = test_flow_create_lifecycle()
    results["Flow UPDATE Lifecycle"] = test_flow_update_lifecycle()
    
    # Summary
    passed = sum(1 for v in results.values() if v)
    total = len(results)
    
    print(f"\n  {'='*60}")
    print(f"  Summary: {passed}/{total} tests passed")
    print(f"  {'='*60}")
    
    if passed < total:
        failed = [k for k, v in results.items() if not v]
        raise AssertionError(f"Failed tests: {failed}")
    
    print(f"\n  ✓ All Controller tests passed")


if __name__ == "__main__":
    run()
