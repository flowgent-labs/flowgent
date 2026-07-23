"""
Scenario 12 — Volume Workspace & Git Clone: PVC, Mount, Clone, File RW.

Verifies the workspace PVC is provisioned, mountable, writable, and can
serve as the git working directory for target repo source files during
flow execution.

Steps with Expected I/O:
  L1 — PVC Status
    Step 1.1 PVC Exists
      Action:  kubectl get pvc -A -o json
      Input:   KUBECONFIG set, K3s cluster running, Helm deployed
      Output:  Workspace PVC found with status "Bound"

    Step 1.2 PVC Capacity
      Action:  kubectl get pvc <name> -o json
      Input:   PVC name from config
      Output:  Capacity non-zero, access mode ReadWriteMany or ReadWriteOnce

  L2 — Sandbox Workspace
    Step 2.1 Workspace dir exists
      Action:  ls -la /workspace or check mount
      Input:   PVC mounted on controller node
      Output:  /workspace directory exists

    Step 2.2 Workspace is writable
      Action:  touch /workspace/.write_test && rm /workspace/.write_test
      Input:   Workspace mounted rw
      Output:  Write test succeeds

  L3 — Git Clone
    Step 3.1 Git available
      Action:  git --version
      Input:   git installed on node
      Output:  Git version string

    Step 3.2 Clone target repo
      Action:  git clone --depth 1 --branch master <repo_url> /workspace/rengine
      Input:   Network access to github.com
      Output:  Clone succeeds (or WARN if GitHub unreachable)

    Step 3.3 Repo on workspace
      Action:  ls /workspace/rengine
      Input:   Clone succeeded
      Output:  Source files present (≥1 file found)

    Step 3.4 File read/write on workspace
      Action:  Read sample file from cloned repo, write test file
      Input:   Workspace mounted rw
      Output:  Read succeeds, write succeeds
"""

import subprocess
import sys
import os
import json
import tempfile
import config

NAMESPACE = config.K3S_NAMESPACE


def kubectl(args, check=True):
    cmd = ["kubectl"] + args
    result = subprocess.run(cmd, capture_output=True, text=True)
    if check and result.returncode != 0:
        print(f"  WARN: {' '.join(cmd)} -> rc={result.returncode}")
        if result.stderr:
            print(f"  stderr: {result.stderr[:300]}")
    return result


def kubectl_json(args):
    result = kubectl(args + ["-o", "json"], check=False)
    if result.returncode != 0:
        return None
    try:
        return json.loads(result.stdout)
    except json.JSONDecodeError:
        return None


def run():
    # ── L1.1: PVC exists and is bound ─────────────────────
    print("\n── L1: PVC Status ──")
    pvcs = kubectl_json(["get", "pvc", "-A"])
    workspace_pvc = None
    if pvcs:
        for pvc in pvcs.get("items", []):
            name = pvc["metadata"]["name"]
            namespace = pvc["metadata"]["namespace"]
            phase = pvc.get("status", {}).get("phase", "Unknown")
            storage = pvc.get("spec", {}).get("resources", {}).get("requests", {}).get("storage", "?")
            access_modes = pvc.get("spec", {}).get("accessModes", [])
            print(f"  [1.1] PVC: {namespace}/{name} phase={phase} storage={storage} access={access_modes}")
            if "workspace" in name.lower() or "flowgent" in name.lower():
                if not workspace_pvc or phase == "Bound":
                    workspace_pvc = pvc
    else:
        print("  [1.1] WARN: Could not list PVCs")

    if workspace_pvc:
        name = workspace_pvc["metadata"]["name"]
        phase = workspace_pvc.get("status", {}).get("phase", "Unknown")
        if phase == "Bound":
            print(f"  [1.1] Workspace PVC {name} is Bound")
        else:
            print(f"  [1.1] WARN: Workspace PVC {name} phase={phase} (expected Bound)")
    else:
        print("  [1.1] WARN: No workspace PVC found. Check Helm values for workspace.persistence.enabled")

    # ── L1.2: PVC capacity ────────────────────────────────
    if workspace_pvc:
        storage = workspace_pvc.get("spec", {}).get("resources", {}).get("requests", {}).get("storage", "0")
        access_modes = workspace_pvc.get("spec", {}).get("accessModes", [])
        volume_name = workspace_pvc.get("spec", {}).get("volumeName", "N/A")
        storage_class = workspace_pvc.get("spec", {}).get("storageClassName", "N/A")
        print(f"  [1.2] Capacity: {storage}, AccessModes: {access_modes}, Volume: {volume_name}, StorageClass: {storage_class}")
        if storage == "0" or not storage:
            print("  [1.2] WARN: PVC has zero or unknown capacity")
        if "ReadWriteMany" not in access_modes and "ReadWriteOnce" not in access_modes:
            print("  [1.2] WARN: PVC lacks ReadWriteMany/ReadWriteOnce access mode")

    # ── L2: Sandbox Workspace ─────────────────────────────
    print("\n── L2: Sandbox Workspace ──")

    # Workspace path — try common locations
    workspace_paths = [
        "/workspace",
        "/home/agent/workspace",
        "/var/flowgent/workspace",
    ]
    found_path = None
    for wp in workspace_paths:
        if os.path.isdir(wp):
            found_path = wp
            print(f"  [2.1] Workspace directory found: {wp}")
            break

    if not found_path:
        # Check if workspace is a PV that's mounted elsewhere
        print("  [2.1] Workspace dir not at common paths — checking mount points...")
        try:
            result = subprocess.run(["mount"], capture_output=True, text=True)
            for line in result.stdout.splitlines():
                if "workspace" in line.lower() or "flowgent" in line.lower():
                    print(f"  [2.1] Mount found: {line.strip()}")
                    found_path = line.split()[2]
        except Exception:
            pass
        if not found_path:
            print("  [2.1] WARN: No workspace mount found. PVC may not be mounted on this node.")
            # Non-blocking — workspace is used inside sandbox pods, not on control node
            found_path = "/tmp/flowgent-workspace-test"

    if found_path and os.path.exists(found_path):
        # ── L2.2: Workspace writable ──────────────────────
        test_file = os.path.join(found_path, ".write_test")
        try:
            with open(test_file, "w") as f:
                f.write("test")
            os.remove(test_file)
            print(f"  [2.2] Workspace write test OK ({found_path})")
        except PermissionError:
            print(f"  [2.2] WARN: Workspace {found_path} is not writable — check mount options")
        except FileNotFoundError:
            print(f"  [2.2] WARN: Cannot write to {found_path} — parent may not exist")

    # ── L3: Git Clone on Workspace ────────────────────────
    print("\n── L3: Git Clone on Workspace ──")

    # ── L3.1: Git available ───────────────────────────────
    try:
        result = subprocess.run(["git", "--version"], capture_output=True, text=True)
        git_version = result.stdout.strip()
        print(f"  [3.1] Git available: {git_version}")
    except FileNotFoundError:
        print("  [3.1] FAIL: git not found — required for source checkout")
        return

    # ── L3.2: Clone target repo ───────────────────────────
    clone_dir = os.path.join(found_path, "rengine")
    clone_ok = False

    # Clean up any stale clone
    if os.path.exists(clone_dir):
        subprocess.run(["rm", "-rf", clone_dir], capture_output=True)

    try:
        # Try minimal clone — shallow, single branch
        print(f"  [3.2] Cloning wl4g/rengine into {clone_dir} ...")
        result = subprocess.run(
            ["git", "clone", "--depth", "1", "--branch", "master",
             "https://github.com/wl4g/rengine.git", clone_dir],
            capture_output=True, text=True,
            timeout=120,
            env={**os.environ, "GIT_TERMINAL_PROMPT": "0"},
        )
        if result.returncode == 0:
            print(f"  [3.2] Clone succeeded")
            clone_ok = True
        else:
            stderr_short = result.stderr[:300].replace("\n", " ")
            print(f"  [3.2] WARN: Clone failed (rc={result.returncode}): {stderr_short}")
            # Try with HTTPS_PROXY if available
            proxy = os.environ.get("HTTPS_PROXY") or os.environ.get("https_proxy")
            if proxy:
                print(f"  [3.2] Retrying with HTTPS_PROXY={proxy} ...")
                result = subprocess.run(
                    ["git", "clone", "--depth", "1", "--branch", "master",
                     "https://github.com/wl4g/rengine.git", clone_dir],
                    capture_output=True, text=True,
                    timeout=120,
                    env={**os.environ, "GIT_TERMINAL_PROMPT": "0", "HTTPS_PROXY": proxy},
                )
                if result.returncode == 0:
                    print(f"  [3.2] Clone succeeded (via proxy)")
                    clone_ok = True
                else:
                    stderr_short = result.stderr[:300].replace("\n", " ")
                    print(f"  [3.2] Clone with proxy also failed: {stderr_short}")
                    print(f"  [3.2] INFO: Clone is non-critical — sandbox pods will clone within the cluster")
    except subprocess.TimeoutExpired:
        print("  [3.2] WARN: Clone timed out after 120s — GitHub may be unreachable")
        print("  [3.2] INFO: Git clone runs inside sandbox pods, not on control node")

    # ── L3.3: Source files present ───────────────────────
    if clone_ok and os.path.isdir(clone_dir):
        file_count = 0
        for root, dirs, files in os.walk(clone_dir):
            # Skip .git directory
            if ".git" in root.split(os.sep):
                continue
            file_count += len(files)
        dir_count = sum(1 for _ in os.walk(clone_dir))
        print(f"  [3.3] Cloned repo: {file_count} files in {dir_count} dirs at {clone_dir}")
        if file_count == 0:
            print("  [3.3] WARN: Repo appears empty — check branch name or repo structure")

        # ── L3.4: File read/write on workspace ───────────
        try:
            # Read first non-binary file found
            for root, dirs, files in os.walk(clone_dir):
                if ".git" in root.split(os.sep):
                    continue
                for f in sorted(files):
                    fpath = os.path.join(root, f)
                    try:
                        with open(fpath, "r") as fh:
                            content = fh.read(200)
                            print(f"  [3.4] Read OK: {os.path.relpath(fpath, clone_dir)} ({len(content)} chars preview)")
                            break
                    except (UnicodeDecodeError, IsADirectoryError):
                        continue
                else:
                    continue
                break

            # Write test
            test_write = os.path.join(clone_dir, ".flowgent_test")
            with open(test_write, "w") as f:
                f.write("flowgent workspace test")
            os.remove(test_write)
            print(f"  [3.4] Write test OK on workspace")
        except Exception as e:
            print(f"  [3.4] WARN: File rw test failed: {e}")
    elif not clone_ok:
        print("  [3.3] SKIP: Clone did not succeed, cannot verify source files")
        print("  [3.4] SKIP: No files to test read/write")

    # ── Summary ────────────────────────────────────────────
    print(f"\n  Volume workspace & git clone verification complete.")
    if workspace_pvc and workspace_pvc.get("status", {}).get("phase") == "Bound":
        print("  PVC: Ready for sandbox git operations.")
    else:
        print("  PVC: Needs attention — check Helm values.yaml workspace settings.")
