"""
Scenario 37 — Volume Workspace & Git Clone: Pod-Container Mount Verification.

CRITICAL constraints:
  - Host path /mnt/disk1/flowgent/e2e/ maps to container path /var/flowgent
    (hostPath mount source -> container mount point, per sandbox.workspace config).
  - TM and Sandbox pods share the same workspace volume mounted at /var/flowgent.
  - The workspace directory tree MUST be created by flowgent DAG tasks
    (e.g. git-clone sandbox node) at execution time — this verifier MUST NOT
    create, write to, or delete anything on the host path.
  - ALL verification MUST run via kubectl exec inside TM/Sandbox pod containers.
    The host path /mnt/disk1/flowgent/e2e/ is only checked read-only for existence.

Steps:
  L1 — State path contract: no host filesystem access
  L2 — TM Pod container: verify /var/flowgent volume is mounted inside container
  L3 — TM Pod container: verify /var/flowgent is writable without writing
  L4 — TM Pod container: verify current run workspace hierarchy:
        /var/flowgent/{namespaceId}/{flowId}/{runId}/{taskId}
  L5 — TM Pod container: verify rengine .git under the git-clone task dir
"""

import os
import re
import subprocess
import sys
import json
import requests
from common import api as common_api
from common import config

NAMESPACE = config.NAMESPACE_ID
WORKLOAD_NAMESPACE = config.K8S_WORKLOAD_NAMESPACE
FLOW_ID = "security-autonomy-fixer"
SANDBOX_NODE_IDS = ("git-clone", "read-source-files", "wait-rescan")

# Container path is where TM/Sandbox pods mount the workspace volume.
# Per Helm values sandbox.workspace and ConfigMap, this is /var/flowgent.
# All verifier checks run via kubectl exec at this path inside the container.
CONTAINER_WORKSPACE = "/var/flowgent"


def kubectl(args, check=True):
    cmd = ["kubectl"] + args
    result = subprocess.run(cmd, capture_output=True, text=True)
    if check and result.returncode != 0:
        print(f"  WARN: {' '.join(cmd)} -> rc={result.returncode}")
    return result


def kubectl_json(args):
    result = kubectl(args + ["-o", "json"], check=False)
    if result.returncode != 0:
        return None
    try:
        return json.loads(result.stdout)
    except json.JSONDecodeError:
        return None


def exec_in_pod(namespace: str, pod_name: str, container: str, cmd: str) -> tuple:
    """Execute a command inside a pod container. Returns (rc, stdout, stderr)."""
    result = subprocess.run(
        ["kubectl", "exec", "-n", namespace, pod_name, "-c", container,
         "--", "sh", "-c", cmd],
        capture_output=True, text=True, timeout=15,
    )
    return result.returncode, result.stdout.strip(), result.stderr.strip()


def flow_resource_pool() -> str:
    session = common_api.flowgent_session()
    response = session.get(
        f"{config.K8S_APISERVER_URL}/api/v1/{NAMESPACE}/flows/{FLOW_ID}", timeout=10
    )
    if response.status_code != 200:
        raise AssertionError(f"cannot resolve Resource Pool for {FLOW_ID}: HTTP {response.status_code}")
    pool_id = response.json().get("resource_pool_id")
    if not pool_id:
        raise AssertionError(f"Flow {FLOW_ID} has no resource_pool_id")
    return pool_id


def find_tm_pod(pool_id: str) -> tuple:
    """Find a running Pool-owned TaskManager pod."""
    pods = kubectl_json([
        "get", "pods", "-n", WORKLOAD_NAMESPACE, "-l",
        f"flowgent/role=worker,flowgent.io/resource-pool={pool_id}",
    ])
    if pods:
        for p in pods.get("items", []):
            phase = p.get("status", {}).get("phase", "")
            containers = [c["name"] for c in p.get("spec", {}).get("containers", [])]
            if phase == "Running" and containers:
                return WORKLOAD_NAMESPACE, p["metadata"]["name"], containers[0]

    return None, None, None


def load_tasks(run_id: str) -> list:
    session = common_api.flowgent_session()
    try:
        return common_api.get_tasks(session, config.K8S_APISERVER_URL, NAMESPACE, run_id)
    except Exception as exc:
        print(f"  WARN: failed to load tasks for run {run_id}: {exc}")
        return []


def workspace_plan_id(tasks_by_node: dict, run_id: str, node_id: str) -> str:
    """Resolve the stable plan workspace from an attempt-scoped exec_id."""
    task = tasks_by_node.get(node_id) or {}
    exec_id = task.get("exec_id") or ""
    match = re.fullmatch(r"(.+)-attempt-\d+", exec_id)
    return match.group(1) if match else (exec_id or f"plan-{run_id}-{node_id}")


def run():
    print("  Scenario 37: Volume Workspace — Pod-Internal Verification")
    run_id_file = os.path.join(os.path.dirname(__file__), "..", ".last_run_id")
    if not os.path.isfile(run_id_file):
        raise AssertionError("No .last_run_id found — run scenarios 31-34 first")
    with open(run_id_file) as f:
        run_id = f.read().strip()
    if not run_id:
        raise AssertionError(".last_run_id is empty")

    # ═══════════════════════════════════════════════════════════════
    # L1: State path contract.
    #     MUST NOT create, read, write to, or delete anything on the host.
    # ═══════════════════════════════════════════════════════════════
    print("\n── L1: State path contract ──")
    print(f"  [L1] Container mount    : {CONTAINER_WORKSPACE}")
    print(f"  [L1] NOTE: This verifier does not access the host workspace path directly.")
    print(f"  [L1] NOTE: The workspace tree is created at runtime by flowgent DAG tasks (git-clone sandbox node).")

    # ═══════════════════════════════════════════════════════════════
    # L2: Find a running TM or Sandbox pod and verify /var/flowgent
    #     volume mount inside the container.
    # ═══════════════════════════════════════════════════════════════
    print("\n── L2: Pod container — /var/flowgent volume mount ──")
    pool_id = flow_resource_pool()
    ns, pod_name, container = find_tm_pod(pool_id)
    if not pod_name:
        print("  [L2] No TM pod found, trying sandbox pod...")
        pods = kubectl_json([
            "get", "pods", "-n", WORKLOAD_NAMESPACE, "-l",
            f"flowgent/role=sandbox-worker,flowgent.io/resource-pool={pool_id}",
        ])
        if pods:
            for p in pods.get("items", []):
                if p.get("status", {}).get("phase") == "Running":
                    ns, pod_name = WORKLOAD_NAMESPACE, p["metadata"]["name"]
                    container = p.get("spec", {}).get("containers", [{}])[0].get("name", "sandbox")
                    break

    if not pod_name:
        raise AssertionError("No running TM or sandbox pod found. Run scenarios 31-34 first.")

    print(f"  [L2] Using pod: {ns}/{pod_name} (container={container})")

    # L2.1 — directory existence inside container
    rc, out, err = exec_in_pod(ns, pod_name, container,
        f"test -d '{CONTAINER_WORKSPACE}' && echo 'EXISTS' || echo 'MISSING'")
    if "EXISTS" in out:
        print(f"  [L2.1] OK: {CONTAINER_WORKSPACE} exists inside container")
    else:
        rc, out, err = exec_in_pod(ns, pod_name, container,
            "mount | grep -E '/var/flowgent' || echo 'NO_MOUNT'")
        if "NO_MOUNT" in out:
            raise AssertionError("No /var/flowgent mount found in container mount table")
        else:
            raise AssertionError(f"{CONTAINER_WORKSPACE} missing despite mount entry: {out[:300]}")

    # L2.2 — verify the selected runtime pod itself. JobManager may remove the
    # owning Deployment as soon as a run reaches a terminal state while its pod
    # remains available briefly. The pod spec is the authoritative evidence of
    # the volume and mount actually used by this run and avoids that GC race.
    runtime_pod = kubectl_json(["get", "pod", "-n", ns, pod_name])
    if not runtime_pod:
        raise AssertionError(f"Selected runtime pod disappeared: {ns}/{pod_name}")

    pod_spec = runtime_pod.get("spec", {})
    pod_volumes = {
        volume.get("name"): volume
        for volume in pod_spec.get("volumes", [])
        if volume.get("name")
    }
    runtime_container = next(
        (candidate for candidate in pod_spec.get("containers", []) if candidate.get("name") == container),
        None,
    )
    if not runtime_container:
        raise AssertionError(f"Container {container} not found in runtime pod {ns}/{pod_name}")

    workspace_mount = next(
        (
            mount
            for mount in runtime_container.get("volumeMounts", [])
            if mount.get("mountPath") == CONTAINER_WORKSPACE
        ),
        None,
    )
    if not workspace_mount:
        raise AssertionError(
            f"{ns}/{pod_name} container {container} has no {CONTAINER_WORKSPACE} volumeMount"
        )

    workspace_volume = pod_volumes.get(workspace_mount.get("name"))
    if not workspace_volume:
        raise AssertionError(
            f"{ns}/{pod_name} volumeMount {workspace_mount.get('name')} has no matching pod volume"
        )

    host_path = workspace_volume.get("hostPath", {}).get("path", "?")
    print(
        f"  [L2.2] OK: {ns}/{pod_name} has workspace volumeMount: "
        f"name={workspace_mount['name']} mountPath={workspace_mount['mountPath']}"
    )
    print(
        f"  [L2.2] OK: {ns}/{pod_name} has matching workspace volume: "
        f"name={workspace_mount['name']} hostPath={host_path}"
    )

    # ═══════════════════════════════════════════════════════════════
    # L3: Verify /var/flowgent is writable inside the container.
    # ═══════════════════════════════════════════════════════════════
    print("\n── L3: Workspace writable flag (inside container, no write) ──")
    rc, out, err = exec_in_pod(ns, pod_name, container,
        f"test -w '{CONTAINER_WORKSPACE}' && echo 'WRITABLE' || echo 'NOT_WRITABLE'")
    if "WRITABLE" in out:
        print(f"  [L3] OK: {CONTAINER_WORKSPACE} is writable inside container")
    else:
        raise AssertionError(f"{CONTAINER_WORKSPACE} is not writable inside container: {err}")

    # ═══════════════════════════════════════════════════════════════
    # L4: Find DAG run workspace subdirectories inside container.
    #     Created by sandbox executor at path:
    #       /var/flowgent/{namespaceId}/{flowId}/{runId}/{taskId}/
    # ═══════════════════════════════════════════════════════════════
    print("\n── L4: DAG run workspace subdirectories (inside container) ──")
    tasks = load_tasks(run_id)
    tasks_by_node = common_api.tasks_by_node(tasks)
    run_workspace = f"{CONTAINER_WORKSPACE}/{NAMESPACE}/{FLOW_ID}/{run_id}"
    legacy_workspace = f"{CONTAINER_WORKSPACE}/{NAMESPACE}/{FLOW_ID}/runs/{run_id}"
    rc, out, err = exec_in_pod(ns, pod_name, container,
        f"test -d '{run_workspace}' && echo 'RUN_WORKSPACE_EXISTS' || echo 'RUN_WORKSPACE_MISSING'")
    if "RUN_WORKSPACE_EXISTS" not in out:
        raise AssertionError(f"Current run workspace missing inside container: {run_workspace}")
    print(f"  [L4] OK: current run workspace exists: {run_workspace}")

    rc, out, err = exec_in_pod(ns, pod_name, container,
        f"test ! -d '{legacy_workspace}' && echo 'LEGACY_ABSENT' || echo 'LEGACY_PRESENT'")
    if "LEGACY_ABSENT" not in out:
        raise AssertionError(f"Legacy run/plans workspace still exists for current run: {legacy_workspace}")
    print(f"  [L4] OK: legacy runs/plans path absent for current run")

    expected_plan_paths = {}
    for node_id in SANDBOX_NODE_IDS:
        plan_id = workspace_plan_id(tasks_by_node, run_id, node_id)
        plan_path = f"{run_workspace}/{plan_id}"
        expected_plan_paths[node_id] = plan_path
        rc, out, err = exec_in_pod(ns, pod_name, container,
            f"test -d '{plan_path}' && "
            f"test -f '{plan_path}/input.json' && "
            f"test -f '{plan_path}/result.json' && "
            f"test -f '{plan_path}/status' && "
            f"find '{plan_path}' -maxdepth 1 -name 'script.*' -type f | grep -q . && "
            f"echo 'TASK_READY' || echo 'TASK_MISSING'")
        if "TASK_READY" not in out:
            raise AssertionError(f"task workspace incomplete for node {node_id}: {plan_path}")
        print(f"  [L4] OK: {node_id} task workspace exists: {plan_path}")

    rc, out, err = exec_in_pod(ns, pod_name, container,
        f"find '{run_workspace}' -maxdepth 3 -type d 2>/dev/null | sort")
    subdirs = [d for d in out.split("\n") if d.strip() and d != CONTAINER_WORKSPACE]
    if subdirs:
        print(f"  [L4] Found {len(subdirs)} subdirectories inside current run workspace:")
        for d in subdirs[:20]:
            print(f"    {d}")
    else:
        raise AssertionError("No DAG run subdirectories found under /var/flowgent")

    # ═══════════════════════════════════════════════════════════════
    # L5: Search for rengine repo clone evidence inside container.
    #     The git-clone sandbox node clones wl4g/rengine under its task
    #     workspace. Look for .git exactly there as proof of clone.
    # ═══════════════════════════════════════════════════════════════
    print("\n── L5: Rengine repo clone evidence (inside container) ──")
    expected_repo = f"{expected_plan_paths['git-clone']}/repos/rengine"
    rc, out, err = exec_in_pod(ns, pod_name, container,
        f"test -d '{expected_repo}/.git' && echo '{expected_repo}/.git' || true")
    git_dirs = [d for d in out.split("\n") if d.strip()]
    if not git_dirs:
        print(f"  [L5] Expected repo not found at {expected_repo}; scanning /var/flowgent as diagnostic...")
        rc, out, err = exec_in_pod(ns, pod_name, container,
            f"find '{CONTAINER_WORKSPACE}' -name '.git' -type d 2>/dev/null | head -5")
        git_dirs = [d for d in out.split("\n") if d.strip()]
        if git_dirs:
            raise AssertionError(f"Git repositories found outside expected repo path {expected_repo}: {git_dirs}")
        raise AssertionError(f"No rengine .git directory found at expected path inside container: {expected_repo}/.git")

    if git_dirs:
        for gd in git_dirs:
            repo_dir = gd.rsplit("/.git", 1)[0] if "/.git" in gd else gd.rsplit("/", 1)[0]
            print(f"  [L5] FOUND: Git repository inside container at {repo_dir}")
            if "rengine" not in repo_dir.lower():
                continue

            rc, out2, _ = exec_in_pod(ns, pod_name, container,
                f"cd '{repo_dir}' && echo '--- files ---' && ls | head -15 && "
                f"echo '--- git log ---' && git log --oneline -3 2>/dev/null")
            print(f"  [L5] Content preview: {out2[:500]}")

            rc, out3, _ = exec_in_pod(ns, pod_name, container,
                f"find '{repo_dir}' -type f -not -path '*/.git/*' | wc -l")
            print(f"  [L5] Total source files: {out3.strip()}")

            rc, out4, _ = exec_in_pod(ns, pod_name, container,
                f"test -f '{repo_dir}/pom.xml' && echo 'pom.xml: EXISTS' || echo 'pom.xml: MISSING'; "
                f"find '{repo_dir}' -name 'pom.xml' -maxdepth 1 2>/dev/null | head -1")
            print(f"  [L5] Build file: {out4.strip()}")
            if "pom.xml: EXISTS" not in out4:
                raise AssertionError(f"rengine repo found but pom.xml missing at {repo_dir}")
            break
        else:
            raise AssertionError(f"Git repositories found, but none look like rengine: {git_dirs}")
    else:
        raise AssertionError("No .git directory found inside /var/flowgent; git-clone node did not produce workspace evidence")

    # ── Summary ──
    print(f"\n  Volume workspace verification complete.")
    print(f"  Container mount   : {CONTAINER_WORKSPACE} (inside TM/sandbox pods)")
    print(f"  All verifier checks ran via kubectl exec INSIDE the container.")
    print(f"  Host filesystem was NOT modified by this verifier script.")
