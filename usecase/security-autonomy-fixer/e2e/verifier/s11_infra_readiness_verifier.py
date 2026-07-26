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
      Action:  docker images flowgent-core
      Input:   Docker daemon running
      Output:  Image "flowgent-core" listed (WARN if missing)

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

    Step 3.3 Log Sanity
      Action:  kubectl logs -l app.kubernetes.io/component={c} --tail=20 (apiserver/controller/notifier)
      Input:   Pods running, logs accessible
      Output:  0 ERROR/FATAL/PANIC in recent log lines
"""

import subprocess
import sys
import os
import json
import time

# Config now in runner.py
from common import config

NAMESPACE = config.K8S_NAMESPACE


def kubectl(args, check=True):
    """Run kubectl and return stdout."""
    cmd = ["kubectl"] + args
    result = subprocess.run(cmd, capture_output=True, text=True)
    if check and result.returncode != 0:
        print(f"  WARN: {' '.join(cmd)} → rc={result.returncode}")
        print(f"  stderr: {result.stderr[:300]}")
    return result


def kubectl_json(args):
    """Run kubectl and parse JSON output."""
    result = kubectl(args + ["-o", "json"], check=False)
    if result.returncode != 0:
        return None
    try:
        return json.loads(result.stdout)
    except json.JSONDecodeError:
        return None


def run():
    # ── L1.1: K8S cluster health ───────────────────────────
    print("\n── L1: Pre-Deployment ──")
    result = kubectl(["get", "nodes"])
    if "Ready" in result.stdout:
        print("  [1.1] K8S nodes OK (Ready found)")
    else:
        print("  [1.1] WARN: no Ready nodes in output")

    # ── L1.2: System pods healthy ──────────────────────────
    result = kubectl(["get", "pods", "-n", "kube-system"])
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
    except Exception as e:
        print(f"  [1.5] EMQX check failed: {e}")

    # ── L1.7: Docker image built ───────────────────────────
    try:
        result = subprocess.run(["docker", "images", "flowgent-core"], capture_output=True, text=True)
        if "flowgent-core" in result.stdout:
            print("  [1.7] Docker image flowgent-core found")
        else:
            print("  [1.7] WARN: flowgent-core image not found — run make build-image-core")
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
            flowgent_releases = [r for r in releases if r.get("name") == "flowgent"]
            if flowgent_releases:
                rel = flowgent_releases[0]
                print(f"  [2.5] Helm release: {rel.get('name')} status={rel.get('status')} "
                      f"chart={rel.get('chart')} app_version={rel.get('app_version')}")
            else:
                print("  [2.5] WARN: no 'flowgent' Helm release found")
        else:
            print(f"  [2.5] helm list failed: {result.stderr[:200]}")
    except FileNotFoundError:
        print("  [2.5] SKIP: helm not installed")

    # ── L2.6: K8s resources ────────────────────────────────
    result = kubectl(["get", "deploy,svc,configmap", "-n", NAMESPACE,
                       "-l", "app.kubernetes.io/instance=flowgent"])
    output_lines = result.stdout.strip().split("\n")
    print(f"  [2.6] K8s resources: {max(0, len(output_lines) - 1)} items (deploy/svc/configmap)")

    # ── L3: Pod Readiness ──────────────────────────────────
    print("\n── L3: Pod Readiness ──")

    pods_data = kubectl_json(["get", "pods", "-n", NAMESPACE,
                               "-l", "app.kubernetes.io/instance=flowgent"])
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
    else:
        print("  [3.1] WARN: Could not get pod list")

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
    except Exception as e:
        print(f"  [3.3] Apiserver healthz: not reachable — {e}")

    # ── L3.9: Check logs for errors ────────────────────────
    # jobmanager/taskmanager are NOT Helm-deployed — Session mode (a shared,
    # Helm-managed JM/TM pool) is temporarily disabled (see VERIFICATION.md).
    # They only exist as dedicated, Controller-created per-flow Deployments
    # once any flow is created (label flowgent.io/mode=application, not
    # app.kubernetes.io/name=jobmanager). Only check components that are
    # actually always-on Helm Deployments here; scenario 23 checks dedicated
    # JM pod logs separately once a test flow exists.
    for component in ["apiserver", "controller", "notifier"]:
        # Deployments are labeled app.kubernetes.io/component={component}, not
        # app.kubernetes.io/name (which is always the chart name "flowgent" —
        # see deploy/helm/flowgent/templates/apiserver.yaml etc).
        result = kubectl(["logs", "-l", f"app.kubernetes.io/component={component}",
                           "-n", NAMESPACE, "--tail=20"], check=False)
        if not result.stdout.strip():
            print(f"  [3.9] {component} logs: no matching pods (may not be enabled) — SKIP")
            continue
        errors = sum(1 for line in result.stdout.splitlines()
                     if any(kw in line.lower() for kw in ("error", "fatal", "panic")))
        label = "WARN" if errors > 0 else "OK"
        print(f"  [3.9] {component} logs (last 20 lines): {errors} error/fatal/panic — {label}")

    # ── L3.10: Dedicated per-flow JM pods (Application mode) ───────
    jm_pods = kubectl_json(["get", "pods", "-n", NAMESPACE, "-A",
                            "-l", "flowgent.io/mode=application"])
    if jm_pods and jm_pods.get("items"):
        print(f"  [3.10] {len(jm_pods['items'])} dedicated per-flow JM pod(s) found "
              f"(leftover from a previous run? see Environment Reset in VERIFICATION.md)")
    else:
        print(f"  [3.10] No dedicated per-flow JM pods (expected — none created yet)")

    # ── Summary ────────────────────────────────────────────
    print(f"\n  Preflight check complete — verify items flagged WARN above.")
