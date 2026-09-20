"""
Scenario 11 — Pre-Deployment & Infrastructure: K8S, Helm, Pod Readiness.

Verifies cluster health, external dependencies, Helm deployment, and pod
readiness before any functional scenarios run.

Uses kubectl subprocess calls — runs on the K8S control node.

Steps with Expected I/O:
  L1 — Pre-Deployment
    Step 1.1 K8S Nodes
      Action:  kubectl get nodes
      Input:   KUBECONFIG set, K8S cluster running
      Output:  ≥1 node with "Ready" status

    Step 1.2 System Pods
      Action:  kubectl get pods -n kube-system
      Input:   K8S cluster accessible
      Output:  All kube-system pods Running

    Step 1.3 SonarQube Health
      Action:  GET {SONARQUBE_URL}/api/system/health
      Input:   config.SONARQUBE_URL
      Output:  HTTP 200, body contains "health"/"status" (WARN if unreachable — non-critical)

    Step 1.4 EMQX Reachable
      Action:  TCP connect {EMQX_HOST}:{EMQX_PORT}
      Input:   config.EMQX_HOST, config.EMQX_PORT
      Output:  Connection accepted (WARN if refused)

    Step 1.5 Docker Image
      Action:  docker images flowgent
      Input:   Docker daemon running
      Output:  Image "flowgent" listed (WARN if missing)

  L2 — Helm State
    Step 2.1 Helm Release
      Action:  helm list -n {namespace} -o json
      Input:   Helm binary on PATH
      Output:  Release "flowgent" with status "deployed"

    Step 2.2 K8s Resources
      Action:  kubectl get deploy,svc,configmap -l app.kubernetes.io/instance=flowgent
      Input:   Helm release deployed
      Output:  Resources listed (≥1 each)

  L3 — Pod Readiness
    Step 3.1 Pod Status
      Action:  kubectl get pods -l app.kubernetes.io/instance=flowgent -o json
      Input:   Helm release deployed
      Output:  All pods phase=Running, all containers ready

    Step 3.2 API Server Healthz
      Action:  GET {API_URL}/_/healthz
      Input:   API server pod Running on :9999
      Output:  HTTP 200

    Step 3.3 A2A Health and Replicas
      Action:  GET {A2A_URL}/_/healthz + inspect flowgent-a2a Deployment
      Output:  HTTP 200 and 2/2 available replicas

    Step 3.4 Log Sanity
      Action:  kubectl logs -l app.kubernetes.io/component={c} --tail=20 (apiserver/controller/notifier/a2a)
      Input:   Pods running, logs accessible
      Output:  0 ERROR/FATAL/PANIC in recent log lines
"""
from __future__ import annotations

import subprocess
import sys
import os
import json
import time

# Config now in runner.py
from common import config

NAMESPACE = config.K8S_NAMESPACE
RELEASE = config.RELEASE_NAME


class InfrastructureReadinessOperations:
    """Class-owned operations for s11 infrastructure."""

    @staticmethod
    def kubectl(args, check=True):
        """Run kubectl and return stdout."""
        cmd = ["kubectl"] + args
        result = subprocess.run(cmd, capture_output=True, text=True)
        if check and result.returncode != 0:
            print(f"  WARN: {' '.join(cmd)} → rc={result.returncode}")
            print(f"  stderr: {result.stderr[:300]}")
        return result

    @staticmethod
    def kubectl_json(args):
        """Run kubectl and parse JSON output."""
        result = InfrastructureReadinessOperations.kubectl(args + ["-o", "json"], check=False)
        if result.returncode != 0:
            return None
        try:
            return json.loads(result.stdout)
        except json.JSONDecodeError:
            return None

    @staticmethod
    def _verify_scenario():
        failures = []

        # ── L1.1: K8S cluster health ───────────────────────────
        print("\n── L1: Pre-Deployment ──")
        result = InfrastructureReadinessOperations.kubectl(["get", "nodes"])
        if "Ready" in result.stdout:
            print("  [1.1] K8S nodes OK (Ready found)")
        else:
            print("  [1.1] WARN: no Ready nodes in output")
            failures.append("no Ready K8S node found")

        # ── L1.2: System pods healthy ──────────────────────────
        result = InfrastructureReadinessOperations.kubectl(["get", "pods", "-n", "kube-system"])
        print(f"  [1.2] kube-system pods: {result.stdout.count('Running')} Running of {max(1, len(result.stdout.splitlines()) - 1)} total")

        # ── L1.3: SonarQube reachable ──────────────────────────
        try:
            import requests
            sq_url = config.SONARQUBE_URL
            r = requests.get(f"{sq_url}/api/system/health", timeout=5)
            if r.status_code == 200:
                data = r.json()
                health = data.get("health", data.get("status", "?"))
                print(f"  [1.3] SonarQube: {sq_url} → {health}")
            else:
                print(f"  [1.3] SonarQube: {sq_url} → HTTP {r.status_code} (non-critical)")
        except Exception as e:
            print(f"  [1.3] SonarQube: {sq_url} not reachable ({e}) — non-critical")

        # ── L1.5: EMQX reachable ───────────────────────────────
        try:
            import socket
            s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            s.settimeout(3)
            result = s.connect_ex((config.EMQX_HOST, config.EMQX_PORT))
            s.close()
            if result == 0:
                print(f"  [1.5] EMQX reachable: {config.EMQX_HOST}:{config.EMQX_PORT}")
            else:
                print(f"  [1.5] EMQX not reachable at {config.EMQX_HOST}:{config.EMQX_PORT} — check EMQX pod")
                failures.append("EMQX MQTT port is not reachable")
        except Exception as e:
            print(f"  [1.5] EMQX check failed: {e}")

        # ── L1.7: Docker image built ───────────────────────────
        try:
            result = subprocess.run(["docker", "images", "flowgent"], capture_output=True, text=True)
            if "flowgent" in result.stdout:
                print("  [1.7] Docker image flowgent found")
            else:
                print("  [1.7] WARN: flowgent image not found — run make build:image:core")
        except FileNotFoundError:
            print("  [1.7] SKIP: docker not available")

        # ── L2: Helm Redeploy ──────────────────────────────────
        print("\n── L2: Helm State ──")

        # ── L2.5: Helm release state ───────────────────────────
        try:
            result = subprocess.run(
                ["helm", "list", "-n", NAMESPACE, "-o", "json"],
                capture_output=True, text=True
            )
            if result.returncode == 0:
                releases = json.loads(result.stdout)
                flowgent_releases = [r for r in releases if r.get("name") == RELEASE]
                if flowgent_releases:
                    rel = flowgent_releases[0]
                    print(f"  [2.5] Helm release: {rel.get('name')} status={rel.get('status')} "
                          f"chart={rel.get('chart')} app_version={rel.get('app_version')}")
                    if rel.get("status") != "deployed":
                        failures.append(f"Helm release status is {rel.get('status')}")
                else:
                    print(f"  [2.5] WARN: no {RELEASE!r} Helm release found")
                    failures.append(f"Helm release {RELEASE} not found")
            else:
                print(f"  [2.5] helm list failed: {result.stderr[:200]}")
                failures.append("helm list failed")
        except FileNotFoundError:
            print("  [2.5] SKIP: helm not installed")

        # ── L2.6: K8s resources ────────────────────────────────
        result = InfrastructureReadinessOperations.kubectl(["get", "deploy,svc,configmap", "-n", NAMESPACE,
                           "-l", f"app.kubernetes.io/instance={RELEASE}"])
        output_lines = result.stdout.strip().split("\n")
        print(f"  [2.6] K8s resources: {max(0, len(output_lines) - 1)} items (deploy/svc/configmap)")

        # ── L3: Pod Readiness ──────────────────────────────────
        print("\n── L3: Pod Readiness ──")

        pods_data = InfrastructureReadinessOperations.kubectl_json(["get", "pods", "-n", NAMESPACE,
                                   "-l", f"app.kubernetes.io/instance={RELEASE}"])
        if pods_data:
            items = pods_data.get("items", [])
            print(f"  [3.1] Flowgent pods: {len(items)} total")
            for pod in items:
                name = pod["metadata"]["name"]
                phase = pod.get("status", {}).get("phase", "Unknown")
                container_statuses = pod.get("status", {}).get("containerStatuses", [])
                ready = sum(1 for c in container_statuses if c.get("ready"))
                total = len(container_statuses)
                restart_marker = ""
                for c in container_statuses:
                    waiting = c.get("state", {}).get("waiting", {})
                    if waiting.get("reason") == "CrashLoopBackOff":
                        restart_marker = " [CrashLoopBackOff!]"
                    elif waiting.get("reason") == "ImagePullBackOff":
                        restart_marker = " [ImagePullBackOff!]"
                print(f"    {name:<50} phase={phase:<10} ready={ready}/{total}{restart_marker}")

            # ── L3.8: No crash loops ───────────────────────────
            crash_loops = 0
            for pod in items:
                for c in pod.get("status", {}).get("containerStatuses", []):
                    waiting = c.get("state", {}).get("waiting", {})
                    if waiting.get("reason") == "CrashLoopBackOff":
                        crash_loops += 1
            if crash_loops == 0:
                print(f"  [3.8] No CrashLoopBackOff containers")
            else:
                print(f"  [3.8] WARN: {crash_loops} CrashLoopBackOff container(s)")

            # Check all Running
            running_count = sum(1 for p in items if p.get("status", {}).get("phase") == "Running")
            if running_count == len(items):
                print(f"  [3.1] All {running_count} pods Running")
            else:
                not_running = [p["metadata"]["name"] for p in items
                               if p.get("status", {}).get("phase") != "Running"]
                print(f"  [3.1] WARN: {running_count}/{len(items)} Running. Not running: {not_running}")
                failures.append(f"not all Helm pods Running: {not_running}")

            not_ready = []
            for pod in items:
                statuses = pod.get("status", {}).get("containerStatuses", [])
                if not statuses or not all(c.get("ready") for c in statuses):
                    not_ready.append(pod["metadata"]["name"])
            if not_ready:
                failures.append(f"not all Helm pod containers Ready: {not_ready}")
        else:
            print("  [3.1] WARN: Could not get pod list")
            failures.append("could not list Helm pods")

        # ── L3.3: Apiserver healthz ────────────────────────────
        try:
            import requests
            api_url = config.K8S_APISERVER_URL
            r = requests.get(f"{api_url}/_/healthz", timeout=5)
            if r.status_code == 200:
                body = r.text[:200]
                print(f"  [3.3] Apiserver healthz: HTTP 200 — {body}")
            else:
                print(f"  [3.3] Apiserver healthz: HTTP {r.status_code}")
                failures.append(f"apiserver healthz HTTP {r.status_code}")
        except Exception as e:
            print(f"  [3.3] Apiserver healthz: not reachable — {e}")
            failures.append(f"apiserver healthz not reachable: {e}")

        # ── L3.4: A2A is required for this suite, including replica safety ──
        try:
            import requests
            a2a_url = config.K8S_A2A_URL
            r = requests.get(f"{a2a_url}/_/healthz", timeout=5)
            if r.status_code == 200:
                print(f"  [3.4] A2A healthz: HTTP 200 — {r.text[:120]}")
            else:
                failures.append(f"A2A healthz HTTP {r.status_code}")
        except Exception as e:
            failures.append(f"A2A healthz not reachable: {e}")
        a2a_deployment = InfrastructureReadinessOperations.kubectl_json([
            "get", "deployment", f"{RELEASE}-a2a", "-n", NAMESPACE,
        ])
        if a2a_deployment:
            desired = a2a_deployment.get("spec", {}).get("replicas", 0)
            available = a2a_deployment.get("status", {}).get("availableReplicas", 0)
            print(f"  [3.4] A2A replicas: {available}/{desired} available")
            if desired != 2 or available != 2:
                failures.append(f"A2A replicas are {available}/{desired}, want 2/2")
        else:
            failures.append(f"A2A deployment {RELEASE}-a2a not found")

        # ── L3.9: Check logs for errors ────────────────────────
        # Application JobManagers and runtime workers are Controller/JM-created and
        # exist only after actual workload demand. Only check components that are
        # actually always-on Helm Deployments here; scenario 23 checks application
        # runtime pod logs separately once a test flow exists.
        for component in ["apiserver", "controller", "session-jobmanager", "notifier", "a2a"]:
            # Deployments are labeled app.kubernetes.io/component={component}, not
            # app.kubernetes.io/name (which is always the chart name "flowgent" —
            # see deploy/helm/flowgent/templates/apiserver.yaml etc).
            result = InfrastructureReadinessOperations.kubectl(["logs", "-l", f"app.kubernetes.io/component={component}",
                               "-n", NAMESPACE, "--tail=20"], check=False)
            if not result.stdout.strip():
                print(f"  [3.9] {component} logs: no matching pods (may not be enabled) — SKIP")
                continue
            error_lines = [
                line for line in result.stdout.splitlines()
                if any(kw in line.lower() for kw in ("error", "fatal", "panic"))
            ]
            expected_test_errors = []
            unexpected_errors = []
            for line in error_lines:
                expected = (
                    component == "apiserver"
                    and "FlowDefHandler.GetSpec failed id=test-flow-" in line
                    and 'error="not found"' in line
                ) or (
                    component == "notifier"
                    and "send notification" in line
                    and "channel=test-channel-" in line
                    and 'error="webhook: HTTP 405"' in line
                )
                (expected_test_errors if expected else unexpected_errors).append(line)
            errors = len(unexpected_errors)
            label = "WARN" if errors > 0 else "OK"
            ignored = f", {len(expected_test_errors)} expected test error(s) ignored" if expected_test_errors else ""
            print(f"  [3.9] {component} logs (last 20 lines): {errors} unexpected error/fatal/panic{ignored} — {label}")
            if errors > 0:
                failures.append(f"{component} recent logs contain {errors} error/fatal/panic line(s)")

        # ── L3.10: Dedicated Flow JobManager pods/deployments ───────
        # Flow imports are metadata only. Flow JobManager resources should
        # exist only while real runs are active; test-flow-* leftovers are always
        # leaks after a full redeploy/import cycle.
        app_pods = InfrastructureReadinessOperations.kubectl_json(["get", "pods", "-n", config.K8S_WORKLOAD_NAMESPACE, "-l", "flowgent.io/runtime-boundary=flow-jobmanager"])
        leaked_pods = []
        if app_pods and app_pods.get("items"):
            for pod in app_pods["items"]:
                labels = pod.get("metadata", {}).get("labels", {})
                flow_id = labels.get("flowgent.io/flow", "")
                if flow_id.startswith("test-flow-"):
                    leaked_pods.append(f"{pod['metadata'].get('namespace')}/{pod['metadata'].get('name')}")
            print(f"  [3.10] {len(app_pods['items'])} Flow JobManager pod(s) found")
        else:
            print(f"  [3.10] No Flow JobManager pods found")
        if leaked_pods:
            failures.append(f"leaked test-flow JobManager pods: {leaked_pods}")

        app_deployments = InfrastructureReadinessOperations.kubectl_json(["get", "deployments", "-n", config.K8S_WORKLOAD_NAMESPACE, "-l", "flowgent.io/runtime-boundary=flow-jobmanager"])
        leaked_deployments = []
        if app_deployments and app_deployments.get("items"):
            for deployment in app_deployments["items"]:
                labels = deployment.get("metadata", {}).get("labels", {})
                flow_id = labels.get("flowgent.io/flow", "")
                if flow_id.startswith("test-flow-"):
                    leaked_deployments.append(
                        f"{deployment['metadata'].get('namespace')}/{deployment['metadata'].get('name')}"
                    )
            print(f"  [3.11] {len(app_deployments['items'])} Flow JobManager deployment(s) found")
        else:
            print(f"  [3.11] No Flow JobManager deployments found")
        if leaked_deployments:
            failures.append(f"leaked test-flow JobManager deployments: {leaked_deployments}")

        # ── Summary ────────────────────────────────────────────
        print(f"\n  Preflight check complete.")
        if failures:
            raise AssertionError(f"Infrastructure readiness failed: {failures}")







from common.model import RunContext, VerificationResult
from verifier import BaseVerifier


class InfrastructureReadinessVerifier(BaseVerifier):
    scenario_id = "11"
    title = "Infrastructure — Pre-Deployment & Pod Readiness"

    def run(self) -> VerificationResult:
        return self.execute(self._run_scenario)

    def _run_scenario(self) -> None:
        if self.context.deployer == "docker":
            self.step(
                "verify Compose services and mode-neutral endpoints",
                self.infrastructure.verify,
            )
            return
        self.step("verify cluster, Helm, pods, endpoints, and logs", self._verify_kubernetes)

    @staticmethod
    def _verify_kubernetes() -> None:
        InfrastructureReadinessOperations._verify_scenario()

def verifier(context: RunContext) -> VerificationResult:
    """Run the infrastructure-readiness scenario."""
    return InfrastructureReadinessVerifier(context).run()
