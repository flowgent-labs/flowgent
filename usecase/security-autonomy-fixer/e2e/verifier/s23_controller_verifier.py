#!/usr/bin/env python3
"""
Scenario 23 — Controller Module: Application Runtime Cluster Lifecycle.

Validates Controller's run-driven application JobManager and per-run runtime
cluster lifecycle.

Prerequisites: K8s/K8S cluster, Helm release, Controller pod running.

Steps with Expected I/O — Flow CREATE + TRIGGER Lifecycle:
  Step 1. Create an application-mode AgentFlow via API
    Action:  POST /api/v1/{namespace}/flows
    Input:   {id: "test-flow-{uuid}", nodes: [short sandbox], edges: [], runtime_mode: "application"}
    Output:  HTTP 200/201, flow ID returned

  Step 1b. Verify MQTT ctrl/flow/updated event (best-effort)
    Action:  Subscribe to ctrl/flow/updated, wait 5s after flow create
    Input:   MQTT broker reachable
    Output:  Event with matching agentflow_id (WARN if not received — Controller may poll)

  Step 2. Verify metadata-only creation does not create runtime Deployments
    Action:  kubectl get deployments with flow labels
    Output:  No JM/TM/Sandbox exists before a run is triggered

  Step 3. Trigger flow run
    Action:  POST /api/v1/{namespace}/flows/{id}/trigger
    Output:  run_id returned

  Step 4. Wait for FlowRun JM and cluster-owned TM/Sandbox Deployments
    Action:  kubectl get deployment {jm_name}; kubectl get deployment {tm_name}
    Output:  JM exists; runtime workers are scoped by runtime_cluster_id

  Step 5. Verify Deployment Spec
    Action:  kubectl get deployment {name} -n {namespace_ns} -o json
    Input:   Deployment name
    Output:  Container env includes FLOWGENT__RUNTIME__AGENT_FLOW_ID={flow_id}

  Step 6. Wait for FlowRun JM and runtime worker Pods Running
    Action:  kubectl get pods -l flowgent.io/runtime-cluster={cluster_id}
    Input:   Label selector
    Output:  JM pod and cluster-owned TM/Sandbox pods reach Running

  Step 7. Cancel Run and delete Flow
    Action:  POST .../runs/{run_id}/cancel; DELETE .../flows/{id}
    Output:  Run becomes CANCELLED and Flow DELETE returns HTTP 200/204

  Step 8. Verify application runtime cleanup
    Action:  wait for per-run Deployments and per-Flow configuration deletion
    Output:  Flow JM/config and application TM/Sandbox resources are deleted by Controller GC

Steps with Expected I/O — Flow UPDATE Lifecycle:
  Step 9. Create metadata-only flow
  Step 10. PUT /api/v1/{namespace}/flows/{id}  {description, version}
    Input:   Updated description, new version
    Output:  HTTP 200, still no idle JM/TM without active run
"""

import sys
import os
import time
import json
import uuid
import base64
import requests
import subprocess
import hashlib
import re
from typing import Dict, Any, Optional, List

from common import config
from common import api as common_api

try:
    import paho.mqtt.client as mqtt
    MQTT_AVAILABLE = True
except ImportError:
    MQTT_AVAILABLE = False

API_BASE = config.K8S_APISERVER_URL
NAMESPACE = config.NAMESPACE_ID
SYSTEM_NAMESPACE = config.SYSTEM_NAMESPACE
WORKLOAD_NAMESPACE = config.K8S_WORKLOAD_NAMESPACE
RUNTIME_CREDENTIAL_SECRET = os.getenv("FLOWGENT_E2E_RUNTIME_SECRET", "flowgent-e2e-runtime-env")
PROXY_SOURCE_KEYS = ("HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy", "ALL_PROXY", "all_proxy")
PROXY_SECRET_KEYS = ("HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY")
FLOWGENT_SESSION = common_api.flowgent_session()


def rand_id() -> str:
    return str(uuid.uuid4())[:8]


def workload_namespace(namespace_id: str = NAMESPACE) -> str:
    """Mirror controller.go/flow_def.go workload namespace: every Flow's
    dedicated JM Deployment lives in its NAMESPACE's shared namespace
    "{namespace_prefix}{namespace_id}" (see docs/01-L1-Engine-Architecture.md
    §1.3/§4.3 — namespace isolation is per-namespace, not per-flow), NOT in
    NAMESPACE (tenant namespace ID, typically "default") — system pods run in
    config.SYSTEM_NAMESPACE."""
    return f"{config.K8S_WORKLOAD_NAMESPACE_PREFIX}{namespace_id}"


def kubernetes_name(*parts: str) -> str:
    """Mirror common/pkg/resourceid.KubernetesName for test-side assertions."""
    raw = "-".join(parts)
    lower = raw.lower()
    normalized = []
    previous_hyphen = False
    needs_hash = False
    for char in lower:
        if "a" <= char <= "z" or "0" <= char <= "9":
            normalized.append(char)
            previous_hyphen = False
            continue
        if char != "-":
            needs_hash = True
        if not previous_hyphen:
            normalized.append("-")
            previous_hyphen = True
    base = re.sub(r"^-+|-+$", "", "".join(normalized))
    if not base:
        base = "flowgent"
        needs_hash = True
    if len(base) > 63:
        needs_hash = True
    if not needs_hash:
        return base
    digest = hashlib.sha256(raw.lower().encode()).hexdigest()[:10]
    max_base = 63 - 1 - len(digest)
    return base[:max_base].rstrip("-") + "-" + digest


def application_runtime_cluster_id(run_id: str) -> str:
    return kubernetes_name("app", run_id)


def application_jobmanager_deployment_name(flow_id: str, run_id: str) -> str:
    return kubernetes_name("flowgent-jobmanager", NAMESPACE, flow_id, run_id)


def taskmanager_deployment_name(cluster_id: str) -> str:
    return kubernetes_name("flowgent-taskmanager", NAMESPACE, cluster_id)


def sandbox_deployment_name(cluster_id: str) -> str:
    return kubernetes_name("flowgent-sandbox", NAMESPACE, cluster_id)


def deployment_with_labels(namespace: str, required: Dict[str, str]) -> Optional[Dict]:
    deployments = kubectl_get("deployments", namespace=namespace)
    if not deployments:
        return None
    for item in deployments.get("items", []):
        labels = item.get("metadata", {}).get("labels", {})
        if all(labels.get(key) == value for key, value in required.items()):
            return item
    return None


def kubectl_get(resource: str, name: str = None, namespace: str = WORKLOAD_NAMESPACE,
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


def kubectl_delete(resource: str, name: str, namespace: str = WORKLOAD_NAMESPACE) -> bool:
    """Execute kubectl delete command"""
    cmd = ["kubectl", "delete", resource, name, "-n", namespace, "--grace-period=0", "--force"]
    
    try:
        result = subprocess.run(cmd, capture_output=True, text=True, timeout=30)
        return result.returncode == 0
    except Exception as e:
        print(f"  ⚠ kubectl delete failed: {e}")
        return False


def runtime_proxy_required() -> bool:
    return any(os.environ.get(key) for key in PROXY_SOURCE_KEYS)


def validate_runtime_secret(secret: Dict, namespace: str) -> None:
    data = secret.get("data", {}) if secret else {}
    if runtime_proxy_required():
        missing = [key for key in PROXY_SECRET_KEYS if key not in data]
        if missing:
            raise AssertionError(f"Runtime Secret {namespace}/{RUNTIME_CREDENTIAL_SECRET} missing proxy keys: {missing}")
        for key in ("HTTP_PROXY", "HTTPS_PROXY"):
            raw = base64.b64decode(data[key]).decode(errors="replace")
            if "127.0.0.1" in raw or "localhost" in raw:
                raise AssertionError(
                    f"Runtime Secret {namespace}/{RUNTIME_CREDENTIAL_SECRET} has pod-unreachable {key}"
                )


def wait_for_deployment(name: str, namespace: str = WORKLOAD_NAMESPACE, timeout: int = 60) -> bool:
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


def wait_for_deployment_replicas(name: str, namespace: str, replicas: int, timeout: int = 60) -> bool:
    """Wait for Deployment spec replicas to match."""
    print(f"    • Waiting for Deployment {name} replicas={replicas}...")

    start = time.time()
    while time.time() - start < timeout:
        deployment = kubectl_get("deployment", name, namespace)
        if deployment:
            actual = deployment.get("spec", {}).get("replicas", 0)
            if actual == replicas:
                print(f"      ✓ Deployment {name} replicas={actual}")
                return True
        time.sleep(2)

    deployment = kubectl_get("deployment", name, namespace)
    actual = deployment.get("spec", {}).get("replicas", "?") if deployment else "missing"
    print(f"      ✗ Deployment {name} replicas={actual}, expected={replicas}")
    return False


def wait_for_pod_running(label_selector: str, namespace: str = WORKLOAD_NAMESPACE, timeout: int = 60) -> bool:
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


def wait_for_deployment_deleted(name: str, namespace: str = WORKLOAD_NAMESPACE, timeout: int = 60) -> bool:
    """Wait for Deployment to be deleted"""
    return wait_for_resource_deleted("deployment", name, namespace, timeout)


def wait_for_resource_deleted(resource: str, name: str, namespace: str = WORKLOAD_NAMESPACE,
                              timeout: int = 60) -> bool:
    """Wait for a namespaced Kubernetes resource to be deleted."""
    display = resource.replace("configmap", "ConfigMap").replace("secret", "Secret").replace("deployment", "Deployment")
    print(f"    • Waiting for {display} {name} to be deleted...")

    start = time.time()
    while time.time() - start < timeout:
        if not kubectl_get(resource, name, namespace):
            print(f"      ✓ {display} {name} deleted")
            return True
        time.sleep(2)

    print(f"      ✗ {display} {name} still exists after {timeout}s")
    return False


def test_flow_create_lifecycle() -> bool:
    """Test metadata-only Flow, trigger-driven application runtime, and cleanup."""
    print(f"\n  → Testing FlowRun JM and application runtime cluster lifecycle...")

    suffix = rand_id()
    flow_id = "test-flow-" + suffix
    runtime_config_map_name = f"flowgent-runtime-env-{NAMESPACE}-{flow_id}"
    runtime_secret_name = f"flowgent-runtime-secrets-{NAMESPACE}-{flow_id}"
    jm_namespace = workload_namespace()
    created_id = None
    run_id = None
    cluster_id = None
    jm_deployment_name = None
    tm_deployment_name = None
    sb_deployment_name = None

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
        # Step 1: Create an application-mode AgentFlow via API.
        print(f"    • Step 1: Creating application-mode AgentFlow ({flow_id})...")
        # POST /flows decodes the body directly into entities.FlowInfo —
        # a flat shape ("id"/"nodes"/"edges"/"runtime_mode" at top level), not a
        # nested "definition" object (see pkg/api/pkg/handler/flow_def.go Create).
        payload = {
            "id": flow_id,
            "kind": "flow",
            "runtime_mode": "application",
            "resources": {
                "jobmanager": {"cpu": "200m", "memory": "256Mi"},
                "taskmanager": {"cpu": "200m", "memory": "256Mi"},
                "sandbox": {"cpu": "200m", "memory": "256Mi"},
            },
            "nodes": [{
                "id": "hold",
                "kind": "sandbox",
                "runtime": "bash",
                "timeout": "45s",
                "script": "#!/bin/bash\nset -euo pipefail\nsleep 20\nprintf '{\"ok\":true}\\n'\n",
            }],
            "edges": [],
        }

        resp = FLOWGENT_SESSION.post(f"{API_BASE}/api/v1/{NAMESPACE}/flows", json=payload, timeout=10)
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

        # Step 2: Metadata creation must not allocate runtime components.
        print(f"    • Step 2: Verifying metadata-only import creates no JM/TM/Sandbox...")
        time.sleep(15)
        if deployment_with_labels(jm_namespace, {"flowgent.io/flow": flow_id}):
            raise AssertionError(f"Runtime Deployment created before trigger for flow={flow_id}")
        print(f"      ✓ No idle JM/TM/Sandbox created before trigger")

        # Step 3: Trigger a real run. Controller should create JM for the active run.
        print(f"    • Step 3: Triggering FlowRun...")
        resp = FLOWGENT_SESSION.post(f"{API_BASE}/api/v1/{NAMESPACE}/flows/{flow_id}/trigger", json={}, timeout=10)
        if resp.status_code not in [200, 201]:
            raise AssertionError(f"Flow trigger failed: {resp.status_code} {resp.text}")
        run_id = resp.json().get("run_id") or resp.json().get("id")
        if not run_id:
            raise AssertionError(f"Trigger response missing run_id: {resp.text}")
        print(f"      ✓ FlowRun triggered: run_id={run_id}")

        cluster_id = application_runtime_cluster_id(run_id)
        jm_deployment_name = application_jobmanager_deployment_name(flow_id, run_id)
        tm_deployment_name = taskmanager_deployment_name(cluster_id)
        sb_deployment_name = sandbox_deployment_name(cluster_id)

        # Step 4: Wait for Controller/JM/RM to realize the application runtime contract.
        print(f"    • Step 4: Waiting for active-run JM/TM/Sandbox Deployments...")
        if not wait_for_deployment(jm_deployment_name, namespace=jm_namespace, timeout=120):
            raise AssertionError(f"JM Deployment not created after trigger: {jm_namespace}/{jm_deployment_name}")
        if not wait_for_deployment(tm_deployment_name, namespace=jm_namespace, timeout=180):
            raise AssertionError(f"TM Deployment not created after trigger: {jm_namespace}/{tm_deployment_name}")
        if not wait_for_deployment_replicas(tm_deployment_name, namespace=jm_namespace, replicas=1, timeout=60):
            raise AssertionError(f"TM Deployment did not scale to exactly one replica for one pending plan")
        if not wait_for_deployment(sb_deployment_name, namespace=jm_namespace, timeout=180):
            raise AssertionError(f"Sandbox Deployment not created after sandbox task: {jm_namespace}/{sb_deployment_name}")
        if not wait_for_deployment_replicas(sb_deployment_name, namespace=jm_namespace, replicas=1, timeout=60):
            raise AssertionError("Sandbox Deployment did not scale to exactly one replica for one pending sandbox task")

        # Step 5: Verify JM Deployment spec
        print(f"    • Step 5: Verifying JM Deployment spec...")
        deployment = kubectl_get("deployment", jm_deployment_name, namespace=jm_namespace)
        if not deployment:
            raise AssertionError("Deployment disappeared")
        if not kubectl_get("configmap", runtime_config_map_name, namespace=jm_namespace):
            raise AssertionError(f"Flow runtime ConfigMap was not materialized: {jm_namespace}/{runtime_config_map_name}")
        if not kubectl_get("secret", runtime_secret_name, namespace=jm_namespace):
            raise AssertionError(f"Flow runtime Secret was not materialized: {jm_namespace}/{runtime_secret_name}")
        
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
            "FLOWGENT__RUNTIME__AGENT_FLOW_RUN_ID": run_id,
            "FLOWGENT__RUNTIME__MODE": "application",
            "FLOWGENT__RUNTIME__RUNTIME_CLUSTER_ID": cluster_id,
        }
        
        for key, expected_value in expected_env.items():
            actual_value = env_vars.get(key, "")
            if expected_value not in actual_value:
                raise AssertionError(f"Expected {key}={expected_value}, got {actual_value}")

        secret_refs = [
            src.get("secretRef", {}).get("name")
            for src in container.get("envFrom", [])
            if src.get("secretRef")
        ]
        if RUNTIME_CREDENTIAL_SECRET not in secret_refs:
            raise AssertionError(
                f"JM Deployment missing envFrom secretRef {RUNTIME_CREDENTIAL_SECRET}; refs={secret_refs}"
            )
        for ns in (SYSTEM_NAMESPACE, jm_namespace):
            secret = kubectl_get("secret", RUNTIME_CREDENTIAL_SECRET, namespace=ns)
            if not secret:
                raise AssertionError(f"Runtime credential Secret missing: {ns}/{RUNTIME_CREDENTIAL_SECRET}")
            validate_runtime_secret(secret, ns)
        
        print(f"      ✓ Deployment spec verified")
        
        # Step 6: Wait for Flow JM and cluster-owned worker Pods to reach Running.
        print(f"    • Step 6: Waiting for JM/TM/Sandbox Pods to reach Running...")
        
        label_selector = f"app=flowgent-jobmanager,flowgent.io/flow={flow_id},flowgent.io/run={run_id},flowgent.io/runtime-cluster={cluster_id}"
        if not wait_for_pod_running(label_selector, namespace=jm_namespace, timeout=120):
            raise AssertionError("JM Pod did not reach Running")
        tm_selector = f"flowgent/role=worker,flowgent.io/runtime-cluster={cluster_id}"
        if not wait_for_pod_running(tm_selector, namespace=jm_namespace, timeout=180):
            raise AssertionError("Application TM Pod did not reach Running")
        sandbox_selector = f"flowgent/role=sandbox-worker,flowgent.io/runtime-cluster={cluster_id}"
        if not wait_for_pod_running(sandbox_selector, namespace=jm_namespace, timeout=180):
            raise AssertionError("Application Sandbox Pod did not reach Running")
        
        # Step 7: Cancel the active Run before deleting the Flow.
        print(f"    • Step 7: Cancelling FlowRun and deleting AgentFlow...")
        resp = FLOWGENT_SESSION.post(
            f"{API_BASE}/api/v1/{NAMESPACE}/flows/{flow_id}/runs/{run_id}/cancel",
            json={}, timeout=10,
        )
        if resp.status_code not in [200, 204]:
            raise AssertionError(f"FlowRun cancellation failed: {resp.status_code} {resp.text}")
        
        resp = FLOWGENT_SESSION.delete(f"{API_BASE}/api/v1/{NAMESPACE}/flows/{created_id}", timeout=10)
        if resp.status_code not in [200, 204]:
            raise AssertionError(f"Flow deletion failed: {resp.status_code} {resp.text}")
        resp = FLOWGENT_SESSION.get(f"{API_BASE}/api/v1/{NAMESPACE}/flows/{created_id}", timeout=10)
        if resp.status_code != 404:
            raise AssertionError(f"Deleted flow still readable from API: HTTP {resp.status_code}")
        
        # Step 8: Controller GC removes per-run application runtime and deleted
        # Flow configuration. There is no second runtime ownership plane.
        print(f"    • Step 8: Verifying application runtime cleanup...")
        if not wait_for_deployment_deleted(jm_deployment_name, namespace=jm_namespace, timeout=90):
            raise AssertionError(f"Controller did not delete JM Deployment: {jm_namespace}/{jm_deployment_name}")
        if not wait_for_deployment_deleted(tm_deployment_name, namespace=jm_namespace, timeout=90):
            raise AssertionError(f"Controller did not delete TM Deployment: {jm_namespace}/{tm_deployment_name}")
        if not wait_for_deployment_deleted(sb_deployment_name, namespace=jm_namespace, timeout=90):
            raise AssertionError(f"Controller did not delete Sandbox Deployment: {jm_namespace}/{sb_deployment_name}")
        if not wait_for_resource_deleted("configmap", runtime_config_map_name, namespace=jm_namespace, timeout=90):
            raise AssertionError(f"Controller did not delete runtime ConfigMap: {jm_namespace}/{runtime_config_map_name}")
        if not wait_for_resource_deleted("secret", runtime_secret_name, namespace=jm_namespace, timeout=90):
            raise AssertionError(f"Controller did not delete runtime Secret: {jm_namespace}/{runtime_secret_name}")
        
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

        if run_id:
            try:
                FLOWGENT_SESSION.post(
                    f"{API_BASE}/api/v1/{NAMESPACE}/flows/{flow_id}/runs/{run_id}/cancel",
                    json={}, timeout=5,
                )
            except Exception:
                pass

        if created_id:
            try:
                FLOWGENT_SESSION.delete(f"{API_BASE}/api/v1/{NAMESPACE}/flows/{created_id}", timeout=5)
            except Exception:
                pass

        # Cleanup on failure
        try:
            if jm_deployment_name:
                kubectl_delete("deployment", jm_deployment_name, namespace=jm_namespace)
            if tm_deployment_name:
                kubectl_delete("deployment", tm_deployment_name, namespace=jm_namespace)
            if sb_deployment_name:
                kubectl_delete("deployment", sb_deployment_name, namespace=jm_namespace)
        except:
            pass
        
        return False


def test_controller_pod_running() -> bool:
    """Verify Controller pod is running"""
    print(f"\n  → Verifying Controller pod...")
    
    try:
        pods = kubectl_get("pods", namespace=SYSTEM_NAMESPACE)
        if not pods:
            raise AssertionError("Failed to get pods")
        
        controller_pods = [
            pod for pod in pods.get("items", [])
            if "controller" in pod.get("metadata", {}).get("name", "")
            and pod.get("status", {}).get("phase") == "Running"
        ]

        if not controller_pods:
            raise AssertionError("No Running Controller pod found")

        controller_pod = controller_pods[0]

        print(f"      ✓ Controller pod is Running ({controller_pod['metadata']['name']})")
        return True
        
    except Exception as e:
        print(f"      ✗ Controller verification failed: {e}")
        return False


def test_flow_update_lifecycle() -> bool:
    """Test Flow UPDATE remains metadata-only without active runs."""
    print(f"\n  → Testing Flow UPDATE → no idle JM/TM allocation...")

    flow_id = "test-flow-" + rand_id()
    jm_namespace = workload_namespace()
    created_id = None

    try:
        payload = {
            "id": flow_id,
            "kind": "flow",
            "runtime_mode": "application",
            "nodes": [{"id": "n1", "kind": "noop"}],
            "edges": [],
        }
        resp = FLOWGENT_SESSION.post(f"{API_BASE}/api/v1/{NAMESPACE}/flows", json=payload, timeout=10)
        if resp.status_code not in [200, 201]:
            raise AssertionError(f"Flow creation failed: {resp.status_code}")
        created_id = resp.json().get("id")

        time.sleep(15)
        if deployment_with_labels(jm_namespace, {"flowgent.io/flow": flow_id}):
            raise AssertionError(f"Runtime Deployment created before trigger for flow={flow_id}")

        resp = FLOWGENT_SESSION.put(
            f"{API_BASE}/api/v1/{NAMESPACE}/flows/{created_id}",
            json={
                "kind": "flow",
                "description": "updated by e2e verifier",
                "runtime_mode": "application",
                "nodes": [{"id": "n1", "kind": "noop"}],
                "edges": [],
            },
            timeout=10,
        )
        if resp.status_code != 200:
            raise AssertionError(f"Flow update failed: {resp.status_code} {resp.text}")

        time.sleep(15)
        if deployment_with_labels(jm_namespace, {"flowgent.io/flow": flow_id}):
            raise AssertionError(f"Runtime Deployment created by metadata-only update for flow={flow_id}")
        print(f"      ✓ Flow update did not allocate an idle Flow JM")

        resp = FLOWGENT_SESSION.delete(f"{API_BASE}/api/v1/{NAMESPACE}/flows/{created_id}", timeout=10)
        if resp.status_code not in [200, 204]:
            raise AssertionError(f"Flow deletion failed: {resp.status_code} {resp.text}")
        resp = FLOWGENT_SESSION.get(f"{API_BASE}/api/v1/{NAMESPACE}/flows/{created_id}", timeout=10)
        if resp.status_code != 404:
            raise AssertionError(f"Deleted flow still readable from API: HTTP {resp.status_code}")

        print(f"    ✓ Flow UPDATE lifecycle verified")
        return True
    except Exception as e:
        print(f"    ✗ Flow UPDATE lifecycle failed: {e}")
        if created_id:
            try:
                FLOWGENT_SESSION.delete(f"{API_BASE}/api/v1/{NAMESPACE}/flows/{created_id}", timeout=5)
            except Exception:
                pass
        return False


def run():
    """Main test runner"""
    print("\n" + "="*60)
    print("  Scenario 23: Controller — Application Runtime Cluster Lifecycle")
    print("="*60)
    
    results = {}
    
    # Check Controller pod
    results["Controller Pod"] = test_controller_pod_running()
    
    # Test flow lifecycle
    results["Flow CREATE+TRIGGER Lifecycle"] = test_flow_create_lifecycle()
    results["Flow UPDATE Metadata Lifecycle"] = test_flow_update_lifecycle()
    
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
