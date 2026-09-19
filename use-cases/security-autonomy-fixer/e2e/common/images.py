"""Local image hand-off helpers for the real k3s E2E environment."""

from common.shell import run_cmd


CORE_IMAGE = "localhost/flowgent-core:latest"
UI_IMAGE = "localhost/flowgent-ui:latest"
AUTHGUARD_IMAGE = "localhost/e2e-flowgent-authguard:0.1.0"
AUTHGUARD_WEB_IMAGE = "localhost/e2e-flowgent-authguard-web:0.1.0"
AUTHGUARD_SOURCE_IMAGE = (
    "ghcr.io/wl4g/authguard"
    "@sha256:ffab9f32c4c472df1988da9474224d4e8603ad05fce9ede8d047020487da5a26"
)
AUTHGUARD_WEB_SOURCE_IMAGE = (
    "ghcr.io/wl4g/authguard-web"
    "@sha256:2496204877ac1fe37cef55749fb28ff09b28574cf31a9a6b481937989e8d9b3a"
)


def _import_image_to_k3s(image: str, label: str):
    """Refresh a local image after teardown and before Helm schedules pods.

    k3s may garbage-collect an unreferenced local image while the prior release is
    being removed. Importing at this exact lifecycle boundary makes every clean
    redeploy independent of containerd's previous cache state.
    """
    print(f"\n-- Refreshing {label} image in k3s containerd --")
    rc, _ = run_cmd(
        ["docker", "image", "inspect", "--format", "{{.Id}}", image],
        timeout=30,
    )
    if rc != 0:
        print(f"  ERROR: reusable Docker image not found: {image}")
        return False
    rc, _ = run_cmd(
        ["sh", "-c", f"docker save {image} | sudo k3s ctr images import -"],
        timeout=300,
    )
    if rc != 0:
        print(f"  ERROR: importing {label} image into k3s containerd failed")
        return False
    return True


def import_core_image_to_k3s():
    return _import_image_to_k3s(CORE_IMAGE, "flowgent-core")


def import_ui_image_to_k3s():
    return _import_image_to_k3s(UI_IMAGE, "flowgent-ui")


def import_authguard_image_to_k3s():
    """Pin and isolate the AuthGuard image used by this Flowgent deployment."""
    rc, _ = run_cmd(
        ["docker", "image", "inspect", "--format", "{{.Id}}", AUTHGUARD_SOURCE_IMAGE],
        timeout=30,
    )
    if rc != 0:
        rc, _ = run_cmd(
            ["docker", "pull", AUTHGUARD_SOURCE_IMAGE],
            timeout=300,
        )
        if rc != 0:
            print(f"  ERROR: pulling pinned AuthGuard image failed: {AUTHGUARD_SOURCE_IMAGE}")
            return False
    # Always refresh the isolated tag. Merely checking that it exists can keep
    # an older AuthZ-only image whose entrypoint is incompatible with the
    # unified `authguard authn|authz` Helm command contract.
    rc, _ = run_cmd(
        ["docker", "tag", AUTHGUARD_SOURCE_IMAGE, AUTHGUARD_IMAGE],
        timeout=30,
    )
    if rc != 0:
        print(f"  ERROR: tagging isolated AuthGuard image failed: {AUTHGUARD_IMAGE}")
        return False
    return _import_image_to_k3s(AUTHGUARD_IMAGE, "e2e-flowgent-authguard")


def import_authguard_web_image_to_k3s():
    """Pin and isolate the Hosted Login image used by this Flowgent deployment."""
    rc, _ = run_cmd(
        ["docker", "image", "inspect", "--format", "{{.Id}}", AUTHGUARD_WEB_SOURCE_IMAGE],
        timeout=30,
    )
    if rc != 0:
        rc, _ = run_cmd(
            ["docker", "pull", AUTHGUARD_WEB_SOURCE_IMAGE],
            timeout=300,
        )
        if rc != 0:
            print(f"  ERROR: pulling pinned AuthGuard Web image failed: {AUTHGUARD_WEB_SOURCE_IMAGE}")
            return False
    rc, _ = run_cmd(
        ["docker", "tag", AUTHGUARD_WEB_SOURCE_IMAGE, AUTHGUARD_WEB_IMAGE],
        timeout=30,
    )
    if rc != 0:
        print(f"  ERROR: tagging isolated AuthGuard Web image failed: {AUTHGUARD_WEB_IMAGE}")
        return False
    return _import_image_to_k3s(AUTHGUARD_WEB_IMAGE, "e2e-flowgent-authguard-web")
