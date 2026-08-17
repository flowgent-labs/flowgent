"""Local image hand-off helpers for the real k3s E2E environment."""

from common.shell import run_cmd


CORE_IMAGE = "localhost/flowgent-core:latest"


def import_core_image_to_k3s():
    """Refresh the local core image after teardown and before Helm schedules pods.

    k3s may garbage-collect an unreferenced local image while the prior release is
    being removed. Importing at this exact lifecycle boundary makes every clean
    redeploy independent of containerd's previous cache state.
    """
    print("\n-- Refreshing flowgent-core image in k3s containerd --")
    rc, _ = run_cmd(
        ["docker", "image", "inspect", "--format", "{{.Id}}", CORE_IMAGE],
        timeout=30,
    )
    if rc != 0:
        print(f"  ERROR: reusable Docker image not found: {CORE_IMAGE}")
        return False
    rc, _ = run_cmd(
        ["sh", "-c", f"docker save {CORE_IMAGE} | sudo k3s ctr images import -"],
        timeout=300,
    )
    if rc != 0:
        print("  ERROR: importing image into k3s containerd failed")
        return False
    return True
