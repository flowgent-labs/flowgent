#!/usr/bin/env python3
"""Consecutive real-cluster UI E2E gate used by the unified runner.

Each successful round starts from a Flowgent DB reset and Helm redeploy, then
uses Playwright visible interactions to provision resources, trigger/approve a
run, inspect every persisted attempt, and verify real Jaeger traces. The full
scenario verifier suite consumes that same UI-triggered run; it may not import,
seed, or trigger a replacement run in UI provisioning mode.
"""

import json
import os
import shutil
import secrets
import subprocess
import sys
import time
from pathlib import Path
from types import SimpleNamespace

from common import config
from verifier import _common as verifier_common

E2E_DIR = Path(__file__).resolve().parent.parent
PROJECT_ROOT = E2E_DIR.parents[2]
UI_DIR = PROJECT_ROOT / "flowgent-ui"
ROUND_REPORTS = E2E_DIR / "reports" / "ui-rounds-phase6-runtime-clusters"
LAST_RUN_ID = E2E_DIR / ".last_run_id"
UI_EVIDENCE = E2E_DIR / ".last_ui_provision.json"
NOTIFICATION_URL = (
    f"http://{config.RESOURCE_PREFIX}-webhook.{config.K8S_NAMESPACE}.svc.cluster.local:8080/notify"
)

# Long-running subprocess output must remain observable in headless CI/agent
# sessions without relying on a TTY.
if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(line_buffering=True)


def host_health(label):
    """Fail early before a test workload can threaten the host."""
    usage = shutil.disk_usage("/")
    free_gib = usage.free / (1024 ** 3)
    load1 = os.getloadavg()[0]
    cpu_count = os.cpu_count() or 1
    available_kib = 0
    with open("/proc/meminfo") as meminfo:
        for line in meminfo:
            if line.startswith("MemAvailable:"):
                available_kib = int(line.split()[1])
                break
    available_gib = available_kib / (1024 ** 2)
    print(
        f"  Host health {label}: load1={load1:.2f}/{cpu_count} "
        f"mem_available={available_gib:.1f}GiB disk_free={free_gib:.1f}GiB"
    )
    # k3s uses a roughly 5 GiB nodefs eviction threshold on the E2E host. Keep
    # enough headroom for image unpacking and runtime workspaces instead of
    # accepting a host that kubelet is already about to taint.
    if free_gib < 8 or available_gib < 1 or load1 > cpu_count * 2:
        subprocess.run(
            ["ps", "-eo", "pid,ni,%cpu,%mem,comm,args", "--sort=-%cpu"],
            timeout=10,
            check=False,
        )
        subprocess.run(
            ["kubectl", "top", "pods", "-A", "--sort-by=cpu"],
            timeout=15,
            check=False,
        )
        # Stale browser/dev-server processes are recoverable and lower priority
        # than the deployed system. Do not target database or Kubernetes daemons.
        subprocess.run(
            ["pkill", "-9", "-f", "playwright.*security-autonomy-fixer.spec.ts"],
            check=False,
        )
        subprocess.run(["pkill", "-9", "-f", "vite.*24174"], check=False)
        raise RuntimeError(f"unsafe host resource pressure detected {label}")


def wait_for_cluster_capacity(timeout_seconds=360):
    """Wait until at least one schedulable node is ready and pressure-free."""
    deadline = time.time() + timeout_seconds
    last_log = 0
    last_reason = "no Kubernetes node status returned"
    while time.time() < deadline:
        result = subprocess.run(
            ["kubectl", "get", "nodes", "-o", "json"],
            capture_output=True,
            text=True,
            timeout=15,
            check=False,
        )
        if result.returncode == 0:
            nodes = json.loads(result.stdout).get("items", [])
            reasons = []
            for node in nodes:
                name = node.get("metadata", {}).get("name", "unknown")
                conditions = {
                    condition.get("type"): condition.get("status")
                    for condition in node.get("status", {}).get("conditions", [])
                }
                taints = {
                    taint.get("key")
                    for taint in node.get("spec", {}).get("taints", [])
                }
                disk_tainted = "node.kubernetes.io/disk-pressure" in taints
                if (
                    not node.get("spec", {}).get("unschedulable", False)
                    and conditions.get("Ready") == "True"
                    and conditions.get("DiskPressure") != "True"
                    and not disk_tainted
                ):
                    print(f"  Cluster capacity ready: schedulable node={name}")
                    return
                reasons.append(
                    f"{name}(ready={conditions.get('Ready')}, "
                    f"disk_pressure={conditions.get('DiskPressure')}, "
                    f"disk_taint={disk_tainted})"
                )
            last_reason = ", ".join(reasons) or last_reason
        else:
            last_reason = result.stderr.strip() or "kubectl get nodes failed"

        now = time.time()
        if now - last_log >= 30:
            print(f"  Waiting for cluster capacity: {last_reason}")
            last_log = now
        time.sleep(5)
    raise RuntimeError(f"cluster capacity did not recover: {last_reason}")


def configure_verifier_postgres():
    """Keep host-side verifiers aligned with the database reset/deployment."""
    defaults = {
        "FLOWGENT_PG_HOST": "localhost",
        "FLOWGENT_PG_PORT": "5432",
        "FLOWGENT_PG_USER": "test",
        "FLOWGENT_PG_PASSWORD": "test",
        "FLOWGENT_PG_DATABASE": "flowgent",
    }
    for name, value in defaults.items():
        os.environ.setdefault(name, value)

    # common.config is imported before main(), so synchronize its cached values
    # explicitly instead of relying on late environment mutation.
    config.PG_HOST = os.environ["FLOWGENT_PG_HOST"]
    config.PG_PORT = int(os.environ["FLOWGENT_PG_PORT"])
    config.PG_USER = os.environ["FLOWGENT_PG_USER"]
    config.PG_PASSWORD = os.environ["FLOWGENT_PG_PASSWORD"]
    config.PG_DATABASE = os.environ["FLOWGENT_PG_DATABASE"]


def run_playwright(forwards):
    env = dict(os.environ)
    env.update(
        {
            "FLOWGENT_UI_E2E_API_URL": config.K8S_APISERVER_URL,
            "FLOWGENT_E2E_PROVISION_MODE": "ui",
            "HTTPS_PROXY": "http://127.0.0.1:8800",
            "FLOWGENT_E2E_NOTIFICATION_URL": NOTIFICATION_URL,
        }
    )
    process = subprocess.Popen(
        ["npm", "run", "test:system"],
        cwd=UI_DIR,
        env=env,
    )
    deadline = time.time() + 25 * 60
    while process.poll() is None:
        if time.time() >= deadline:
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
            print("  ERROR: Playwright exceeded its 25-minute infrastructure timeout")
            return False
        if not forwards.healthy():
            print("  WARN: local verification tunnel dropped; restarting before UI polling continues")
            try:
                forwards.ensure()
            except Exception as exc:
                print(f"  WARN: local tunnel recovery pending: {exc}")
        time.sleep(2)
    return process.returncode == 0


def archive_round(round_number, passed, include_verifier_reports=False):
    destination = ROUND_REPORTS / f"round-{round_number:02d}"
    if destination.exists():
        shutil.rmtree(destination)
    destination.mkdir(parents=True)
    # A browser/provisioning failure happens before the verifier suite rotates
    # and rewrites its reports. Do not attach stale verifier evidence to that
    # failed round; it can otherwise make a skipped, older deployment look like
    # part of the current acceptance run.
    if include_verifier_reports:
        for report in (E2E_DIR / "reports").glob("*.md"):
            shutil.copy2(report, destination / report.name)
    for source_name in ("playwright-report-system", "test-results/system"):
        source = UI_DIR / source_name
        if source.exists():
            shutil.copytree(source, destination / source_name)
    if UI_EVIDENCE.exists():
        shutil.copy2(UI_EVIDENCE, destination / "ui-provision.json")
    (destination / "round.json").write_text(
        json.dumps(
            {"round": round_number, "passed": passed, "completed_at": time.time()},
            indent=2,
        )
    )


def execute_round(round_number, build):
    import runner

    print("\n" + "=" * 72)
    print(f"  UI SYSTEM E2E ROUND {round_number} (build={'yes' if build else 'reuse'})")
    print("=" * 72)
    host_health("before redeploy")
    wait_for_cluster_capacity()
    pipeline_args = SimpleNamespace(
        skip_sonarqube=False,
        skip_build=not build,
        skip_import=True,
        skip_deploy=False,
        namespace=config.K8S_NAMESPACE,
        release=config.RELEASE_NAME,
    )
    if not runner.run_pipeline(pipeline_args):
        archive_round(round_number, False)
        return False

    host_health("after redeploy")
    for artifact in (LAST_RUN_ID, UI_EVIDENCE, Path(verifier_common.MQTT_AUDIT_PATH)):
        artifact.unlink(missing_ok=True)

    baseline = verifier_common.capture_pr_baseline()
    print(
        f"  PR baseline captured: commits={baseline.get('commit_count')} "
        f"head={(baseline.get('head_sha') or '')[:8] or 'none'}"
    )

    forwards = runner._PortForwards(config.K8S_NAMESPACE, config.RELEASE_NAME).start()
    audit = None
    browser_ok = False
    run_id = ""
    try:
        audit = verifier_common.MQTTAudit().start()
        browser_ok = run_playwright(forwards)
        if LAST_RUN_ID.is_file():
            run_id = LAST_RUN_ID.read_text().strip()
            audit.set_run_id(run_id)
        messages = audit.stop()
        audit = None
        if run_id:
            verifier_common.save_mqtt_audit(run_id, messages)
            try:
                verifier_common.assert_mqtt_suffixes(run_id, ["ctrl/run/created", "exec/plans"])
            except AssertionError as exc:
                browser_ok = False
                print(f"  ERROR: {exc}")
        else:
            browser_ok = False
            print("  ERROR: browser journey did not persist .last_run_id")
    finally:
        if audit is not None:
            audit.stop()
        forwards.stop()

    if not browser_ok:
        archive_round(round_number, False)
        return False

    host_health("before verifier suite")
    os.environ["FLOWGENT_E2E_PROVISION_MODE"] = "ui"
    verify_args = SimpleNamespace(
        scenario=None,
        namespace=config.K8S_NAMESPACE,
        release=config.RELEASE_NAME,
    )
    verified = runner.run_verification(verify_args)
    host_health("after verifier suite")
    archive_round(round_number, verified, include_verifier_reports=True)
    return verified


def run(rounds=10, skip_first_build=False):
    if rounds < 5:
        raise SystemExit("The runtime-cluster acceptance gate requires at least 5 successful rounds")

    os.environ.setdefault("FLOWGENT_E2E_NOTIFICATION_TOKEN", secrets.token_urlsafe(32))
    os.environ.setdefault("FLOWGENT_E2E_NOTIFICATION_URL", NOTIFICATION_URL)
    configure_verifier_postgres()

    ROUND_REPORTS.mkdir(parents=True, exist_ok=True)
    completed = 0
    for round_number in range(1, rounds + 1):
        if not execute_round(round_number, build=round_number == 1 and not skip_first_build):
            print(
                f"\nFAIL: round {round_number} failed; consecutive success streak resets "
                "and no failed round is counted."
            )
            return 1
        completed += 1
        print(f"\nPASS: {completed}/{rounds} consecutive real UI system rounds")

    (ROUND_REPORTS / "summary.json").write_text(
        json.dumps(
            {"result": "PASS", "consecutive_successes": completed, "required": rounds},
            indent=2,
        )
    )
    print(f"\nPASS: {completed} consecutive real UI system E2E rounds completed")
    return 0
