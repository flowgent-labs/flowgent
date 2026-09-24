#!/usr/bin/env python3
"""
Deploy Script S11 — Flowgent Helm deployment and readiness check.

Deploys (or upgrades) the Flowgent Helm chart and waits for all pods to be
Ready before handing off to the verifier suite.

Usage:
  python3 s11_flowgent_deployer.py [--namespace NS] [--release NAME] [--timeout S]
"""

import sys
import os
import time
import argparse
import json

from common import HELM_CHART, run_cmd

DEFAULT_NAMESPACE = "default"
DEFAULT_RELEASE = "flowgent"
DEFAULT_TIMEOUT = 300


def helm_install_or_upgrade(release, namespace):
    print(f"\n-- Deploying Flowgent Helm chart --")
    print(f"  Release: {release}, Namespace: {namespace}")
    print(f"  Chart:   {HELM_CHART}")

    if not os.path.isdir(HELM_CHART):
        print(f"  ERROR: Helm chart not found: {HELM_CHART}")
        return False

    # Check if release already exists
    rc, out = run_cmd(["helm", "list", "-n", namespace, "-o", "json"], timeout=30)
    exists = False
    if rc == 0 and out:
        try:
            releases = json.loads(out)
            exists = any(r.get("name") == release for r in releases)
        except json.JSONDecodeError:
            pass

    if exists:
        print(f"  Release '{release}' exists — upgrading...")
        cmd = ["helm", "upgrade", release, HELM_CHART, "-n", namespace, "--wait", "--timeout", "300s"]
    else:
        print(f"  Release '{release}' not found — installing...")
        cmd = ["helm", "install", release, HELM_CHART, "-n", namespace, "--create-namespace", "--wait", "--timeout", "300s"]

    rc, _ = run_cmd(cmd, timeout=360)
    return rc == 0


def wait_for_pods(namespace, release, timeout):
    print(f"\n-- Waiting for pods (timeout={timeout}s) --")
    selector = f"app.kubernetes.io/instance={release}"
    deadline = time.time() + timeout

    while time.time() < deadline:
        rc, out = run_cmd(
            ["kubectl", "get", "pods", "-n", namespace, "-l", selector, "-o", "json"],
            timeout=30,
        )
        if rc != 0:
            print(f"  kubectl get pods failed, retrying...")
            time.sleep(10)
            continue

        try:
            pods = json.loads(out).get("items", [])
        except json.JSONDecodeError:
            print(f"  Failed to parse pod list, retrying...")
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
            ready = all(cs.get("ready", False) for cs in container_statuses)
            restarts = sum(cs.get("restartCount", 0) for cs in container_statuses)
            status = "OK" if ready else "NOT_READY"
            print(f"    {name:<40} phase={phase:<12} ready={status:<10} restarts={restarts}")
            if not ready and phase != "Succeeded":
                all_ready = False

        if all_ready:
            print(f"  All pods Ready.")
            return True

        time.sleep(10)

    print(f"  ERROR: Timed out waiting for pods after {timeout}s")
    return False


def health_check(namespace, release, timeout=60):
    print(f"\n-- Health check API server --")
    deadline = time.time() + timeout

    while time.time() < deadline:
        rc, out = run_cmd(
            ["kubectl", "get", "pods", "-n", namespace,
             "-l", f"app.kubernetes.io/instance={release},app.kubernetes.io/component=apiserver",
             "-o", "jsonpath={.items[0].metadata.name}"],
            timeout=15,
        )
        pod_name = out.strip()
        if not pod_name:
            print(f"  API server pod not found, retrying...")
            time.sleep(5)
            continue

        rc, _ = run_cmd(
            ["kubectl", "exec", "-n", namespace, pod_name, "--",
             "wget", "-q", "-O", "-", "http://localhost:9999/_/healthz"],
            timeout=15,
        )
        if rc == 0:
            print(f"  API server healthz: OK")
            return True
        print(f"  healthz returned {rc}, retrying...")
        time.sleep(5)

    print(f"  WARN: healthz check did not succeed within {timeout}s")
    return False


def main():
    parser = argparse.ArgumentParser(description="Deploy Flowgent via Helm")
    parser.add_argument("--namespace", "-n", default=DEFAULT_NAMESPACE)
    parser.add_argument("--release", "-r", default=DEFAULT_RELEASE)
    parser.add_argument("--timeout", "-t", type=int, default=DEFAULT_TIMEOUT)
    args = parser.parse_args()

    print("=" * 60)
    print("  Deploy S11: Flowgent Helm Deployment")
    print(f"  Chart:     {HELM_CHART}")
    print(f"  Release:   {args.release}")
    print(f"  Namespace: {args.namespace}")
    print("=" * 60)

    ok = True

    if not helm_install_or_upgrade(args.release, args.namespace):
        ok = False

    if ok and not wait_for_pods(args.namespace, args.release, args.timeout):
        ok = False

    if ok:
        health_check(args.namespace, args.release)

    print(f"\n{'=' * 60}")
    if ok:
        print("  Deploy S11 complete — Flowgent deployment ready.")
    else:
        print("  Deploy S11 finished with errors — check output above.")
    print(f"{'=' * 60}")

    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
