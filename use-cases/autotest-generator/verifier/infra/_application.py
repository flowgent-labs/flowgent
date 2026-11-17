"""
Deploy Script S11 — Flowgent Helm deployment and readiness check.

Deploys (or upgrades) the Flowgent Helm chart and waits for all pods to be
Ready before handing off to the verifier suite.

Usage:
  python3 s11_flowgent_deployer.py [--namespace NS] [--release NAME] [--timeout S]
"""

import json
import os
import time

from util.constant import E2EOtherDefinitions as config

HELM_CHART = config.HELM_CHART
from util.process import E2EProcessHelper

run_cmd = E2EProcessHelper.legacy
DEFAULT_NAMESPACE = config.K8S_NAMESPACE
DEFAULT_RELEASE = "flowgent"
DEFAULT_TIMEOUT = 300


class FlowgentDeployment:
    @staticmethod
    def helm_install_or_upgrade(release, namespace):
        from util.images import E2EImagesDefinitions
        from util.kubernetes import E2EKubernetesDeployer, KubernetesLocalImageLoader

        if not namespace.startswith(config.RESOURCE_PREFIX + "-"):
            raise ValueError(
                "AutoTest deployment requires its own e2e-autotest namespace"
            )

        KubernetesLocalImageLoader.detect()
        core_repository, core_tag = E2EImagesDefinitions.parts("flowgent")
        web_repository, web_tag = E2EImagesDefinitions.parts("flowgent-web")
        values = {
            "global": {"image": {"repository": core_repository, "tag": core_tag}},
            "web": {"image": {"repository": web_repository, "tag": web_tag}},
            "emqx": {
                "image": E2EImagesDefinitions.get("emqx"),
                "erlangFlags": os.getenv("E2E_EMQX_ERL_AFLAGS", ""),
            },
        }
        return E2EKubernetesDeployer.install(
            release, HELM_CHART, namespace, values=values
        ).passed

    @staticmethod
    def wait_for_pods(namespace, release, timeout):
        print(f"\n-- Waiting for pods (timeout={timeout}s) --")
        selector = f"app.kubernetes.io/instance={release}"
        deadline = time.time() + timeout
        while time.time() < deadline:
            rc, out = run_cmd(
                [
                    "kubectl",
                    "get",
                    "pods",
                    "-n",
                    namespace,
                    "-l",
                    selector,
                    "-o",
                    "json",
                ],
                timeout=30,
            )
            if rc != 0:
                print("  kubectl get pods failed, retrying...")
                time.sleep(10)
                continue
            try:
                pods = json.loads(out).get("items", [])
            except json.JSONDecodeError:
                print("  Failed to parse pod list, retrying...")
                time.sleep(10)
                continue
            if not pods:
                print(f"  No pods found yet with selector '{selector}'")
                time.sleep(10)
                continue
            all_ready = True
            for pod in pods:
                name = pod["metadata"]["name"]
                phase = pod.get("status", {}).get("phase", "Unknown")
                container_statuses = pod.get("status", {}).get("containerStatuses", [])
                ready = all((cs.get("ready", False) for cs in container_statuses))
                restarts = sum((cs.get("restartCount", 0) for cs in container_statuses))
                status = "OK" if ready else "NOT_READY"
                print(
                    f"    {name:<40} phase={phase:<12} ready={status:<10} restarts={restarts}"
                )
                if not ready and phase != "Succeeded":
                    all_ready = False
            if all_ready:
                print("  All pods Ready.")
                return True
            time.sleep(10)
        print(f"  ERROR: Timed out waiting for pods after {timeout}s")
        return False

    @staticmethod
    def health_check(namespace, release, timeout=60):
        print("\n-- Health check API server --")
        deadline = time.time() + timeout
        while time.time() < deadline:
            rc, out = run_cmd(
                [
                    "kubectl",
                    "get",
                    "pods",
                    "-n",
                    namespace,
                    "-l",
                    f"app.kubernetes.io/instance={release},app.kubernetes.io/component=apiserver",
                    "-o",
                    "jsonpath={.items[0].metadata.name}",
                ],
                timeout=15,
            )
            pod_name = out.strip()
            if not pod_name:
                print("  API server pod not found, retrying...")
                time.sleep(5)
                continue
            rc, _ = run_cmd(
                [
                    "kubectl",
                    "exec",
                    "-n",
                    namespace,
                    pod_name,
                    "--",
                    "wget",
                    "-q",
                    "-O",
                    "-",
                    "http://localhost:9999/_/healthz",
                ],
                timeout=15,
            )
            if rc == 0:
                print("  API server healthz: OK")
                return True
            print(f"  healthz returned {rc}, retrying...")
            time.sleep(5)
        print(f"  WARN: healthz check did not succeed within {timeout}s")
        return False
