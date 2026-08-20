#!/usr/bin/env python3
"""
Flowgent E2E Runner — pipeline orchestration + scenario verification.

Orchestrates:
  1. SonarQube PG reset (docker compose)
  2. Flowgent PG public schema reset
  3. Build flowgent-core binary and image, then import the image into k3s
  4. Full Flowgent Helm redeploy (uninstall/install + wait for pods)
  5. Import config YAMLs into Flowgent DB (console import)
  6. Verification suite (scenario-based checks)

Usage:
  python3 runner.py                             # full pipeline + verify all
  python3 runner.py --skip-build                # skip binary build
  python3 runner.py --no-verify                 # pipeline only, skip verification
  python3 runner.py --skip-deploy --no-verify   # setup only (sonarqube + build + import)
  python3 runner.py --skip-sonarqube --skip-build --skip-import --skip-deploy
                                                # verify only (assumes infra is up)
  python3 runner.py --skip-sonarqube --skip-build --skip-import --skip-deploy -s 31
                                                # verify single scenario
  python3 runner.py -l                          # list available scenarios
  python3 runner.py --api http://10.0.0.1:9999 --pg postgres://u:p@h/db
                                                # override config

Reports are written to e2e/reports/ after every verification run.
Old reports are archived before each run.
"""

import sys
import os
import time
import argparse
import importlib
import io
import json
import glob
import shutil
import traceback
import contextlib
import subprocess as _sp
import socket
import urllib.request
from datetime import datetime

_SHELL_ENV_FILES = (
    "~/.bashrc",
    "~/.bash_profile",
    "~/.wl4gshrc.sec",
)

# Core builds are deliberately single-threaded below to protect the shared
# k3s host. A cold compiler/module cache therefore needs a wider budget than
# ordinary E2E subprocesses; keep this separate from runtime verifier limits.
_CORE_BUILD_TIMEOUT_SECONDS = 20 * 60


def _load_shell_env_files():
    """Merge exported variables from standard shell env files into this runner.

    The runner is often launched from a non-interactive shell where bash does
    not load the same files an SSH session loads. Keep this silent so secret
    values never appear in E2E logs.
    """
    files = " ".join(f'"{path}"' for path in _SHELL_ENV_FILES)
    script = f"""
set -a
for f in {files}; do
  f="${{f/#\\~/$HOME}}"
  [ -f "$f" ] && . "$f" >/dev/null 2>&1 || true
done
set +a
python3 - <<'PY'
import json
import os
print(json.dumps(dict(os.environ), separators=(",", ":")))
PY
"""
    try:
        result = _sp.run(
            ["bash", "-lc", script],
            capture_output=True,
            text=True,
            timeout=15,
        )
    except Exception:
        return
    if result.returncode != 0 or not result.stdout:
        return
    try:
        env = json.loads(result.stdout.strip().splitlines()[-1])
    except Exception:
        return
    for key, value in env.items():
        if isinstance(key, str) and isinstance(value, str):
            os.environ[key] = value


_load_shell_env_files()


def _usable_kubeconfig(path: str) -> bool:
    return bool(path) and os.path.isfile(path) and os.path.getsize(path) > 0


def _select_kubeconfig() -> str:
    candidates = (
        os.environ.get("KUBECONFIG", ""),
        os.path.expanduser("~/.kube/config"),
        "/etc/rancher/k3s/k3s.yaml",
    )
    for path in candidates:
        if _usable_kubeconfig(path):
            return path
    return ""


selected_kubeconfig = _select_kubeconfig()
if selected_kubeconfig:
    os.environ["KUBECONFIG"] = selected_kubeconfig
os.environ.setdefault("FLOWGENT_E2E_PROXY_ALLOWLIST_ENTRY", "github.com")

from common import config

E2E_DIR = os.path.dirname(os.path.abspath(__file__))

REPORTS_DIR = os.path.join(E2E_DIR, "reports")


class _PortForwards:
    """Own the localhost tunnels required by black-box E2E verifiers."""

    _SPECS = (
        ("flowgent-apiserver", ("9999:9999",)),
        ("flowgent-a2a", ("9992:9992",)),
        ("flowgent-emqx", ("1883:1883", "18083:18083")),
        ("flowgent-jaeger", ("16687:16686",)),
    )

    def __init__(self, namespace):
        self.namespace = namespace
        self.processes = {}

    @staticmethod
    def _local_port(spec):
        return int(spec.split(":", 1)[0])

    @staticmethod
    def _reachable(port):
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as conn:
            conn.settimeout(0.2)
            return conn.connect_ex(("127.0.0.1", port)) == 0

    def _cleanup_stale_forwards(self):
        pattern = f"kubectl port-forward -n {self.namespace} service/flowgent-"
        _sp.run(["pkill", "-f", pattern], stdout=_sp.DEVNULL, stderr=_sp.DEVNULL)
        time.sleep(0.5)

    def _ports_reachable(self, ports):
        return all(self._reachable(self._local_port(port)) for port in ports)

    def _service_probe_ok(self):
        if not self._reachable(1883):
            return False
        try:
            with urllib.request.urlopen("http://127.0.0.1:9999/_/healthz", timeout=1) as resp:
                if resp.status != 200:
                    return False
        except Exception:
            return False
        try:
            with urllib.request.urlopen("http://127.0.0.1:9992/_/healthz", timeout=1) as resp:
                if resp.status != 200:
                    return False
        except Exception:
            return False
        try:
            with urllib.request.urlopen("http://127.0.0.1:16687/api/services", timeout=1) as resp:
                if resp.status != 200:
                    return False
        except Exception:
            return False
        return True

    def _wait_service_probes(self, timeout=30, stable_checks=3):
        deadline = time.time() + timeout
        ok_count = 0
        while time.time() < deadline:
            if self._service_probe_ok():
                ok_count += 1
                if ok_count >= stable_checks:
                    return
            else:
                ok_count = 0
            time.sleep(0.5)
        raise RuntimeError("local verification tunnels did not stay healthy")

    def _start_one(self, service, ports):
        if self._ports_reachable(ports):
            print(f"  Reusing existing local port(s) for {service}: {', '.join(ports)}")
            return
        old = self.processes.pop(service, None)
        if old and old.poll() is None:
            old.terminate()
            try:
                old.wait(timeout=3)
            except _sp.TimeoutExpired:
                old.kill()
        command = ["kubectl", "port-forward", "-n", self.namespace,
                   f"service/{service}", *ports]
        process = _sp.Popen(command, stdout=_sp.DEVNULL, stderr=_sp.PIPE, text=True)
        self.processes[service] = process
        deadline = time.time() + 20
        while time.time() < deadline:
            if self._ports_reachable(ports):
                time.sleep(0.5)
                if process.poll() is None:
                    return
                error = process.stderr.read().strip()
                raise RuntimeError(f"port-forward {service} exited after binding: {error}")
            if process.poll() is not None:
                error = process.stderr.read().strip()
                raise RuntimeError(f"port-forward {service} failed: {error}")
            time.sleep(0.2)
        raise RuntimeError(f"port-forward {service} did not become ready")

    def start(self):
        self._cleanup_stale_forwards()
        self.ensure()
        self._wait_service_probes()
        print("  Local verification tunnels ready: apiserver, A2A, EMQX, Jaeger")
        return self

    def ensure(self):
        for service, ports in self._SPECS:
            process = self.processes.get(service)
            if self._ports_reachable(ports) and (process is None or process.poll() is None):
                continue
            self._start_one(service, ports)
        self._wait_service_probes()

    def healthy(self):
        return all(
            self.processes.get(service) is not None
            and self.processes[service].poll() is None
            and self._ports_reachable(ports)
            for service, ports in self._SPECS
        ) and self._service_probe_ok()

    def stop(self):
        for process in reversed(list(self.processes.values())):
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=5)
                except _sp.TimeoutExpired:
                    process.kill()

SCENARIOS = {
    # L0 — Infra Readiness
    "11": ("Infrastructure — Pre-Deployment & Pod Readiness",             "verifier.s11_infra_readiness_verifier"),
    "12": ("Console Import — Binary, Import Command, DB Verification",    "verifier.s12_console_import_verifier"),
    "13": ("OTEL — Jaeger Span Coverage",                                 "verifier.s13_otel_verifier"),
    # L2 — Use case. Verify run-scoped pods/workspaces before the
    # JobManager's expected terminal-state garbage collection window closes.
    "31": ("E2E Fixer — Seed & Trigger",                                  "verifier.s31_seed_trigger_verifier"),
    "32": ("E2E Fixer — Discovery & Analyze",                             "verifier.s32_discovery_analyze_verifier"),
    "33": ("E2E Fixer — Remediation",                                     "verifier.s33_remediation_verifier"),
    "34": ("E2E Fixer — Delivery & Report",                               "verifier.s34_delivery_report_verifier"),
    "35": ("PR Commits — Verify Fix Commits on Target PR",                "verifier.s35_pr_commit_verifier"),
    "36": ("Knowledge — RAG Retrieval & Injection",                       "verifier.s36_knowledge_verifier"),
    "37": ("Volume Workspace — Pod-Container Mount, Git Clone Evidence (kubectl exec only)", "verifier.s37_volume_workspace_verifier"),
    # L1 — Flowgent Engine. These checks are independent of the UI run's
    # ephemeral Flow and runtime-cluster resources and can safely run afterward.
    "21": ("API Server — REST CRUD + Lifecycle Events",                   "verifier.s21_apiserver_verifier"),
    "22": ("Notifier — Multi-Channel Delivery",                           "verifier.s22_notifier_verifier"),
    "23": ("Controller — Application Runtime Cluster Lifecycle",           "verifier.s23_controller_verifier"),
    "24": ("Messager — MQTT Topics + Sandbox Chain",                      "verifier.s24_messager_verifier"),
    "25": ("A2A Protocol — Agent Card & Task Submit",                     "verifier.s25_a2a_protocol_verifier"),
}


# ═══════════════════════════════════════════════════════════════════════
# Pipeline steps
# ═══════════════════════════════════════════════════════════════════════

def _step_sonarqube():
    from deploy.s21_sonarqube_deployer import compose_up, wait_for_sonarqube, verify_credentials
    if not compose_up():
        return False
    if not wait_for_sonarqube():
        return False
    verify_credentials()
    return True


def _step_reset_flowgent_db():
    from common import run_cmd
    print("\n-- Step: Reset Flowgent PostgreSQL schema --")
    container = os.getenv("FLOWGENT_E2E_PG_CONTAINER", "sigbot_e2e_164364_postgres")
    pg_user = os.getenv("FLOWGENT_PG_USER", "test")
    pg_password = os.getenv("FLOWGENT_PG_PASSWORD", "test")
    pg_database = os.getenv("FLOWGENT_PG_DATABASE", "flowgent")
    sql = "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"
    rc, _ = run_cmd([
        "docker", "exec", container, "sh", "-c",
        f"PGPASSWORD={pg_password} psql -U {pg_user} -d {pg_database} -c '{sql}'",
    ], timeout=60)
    if rc != 0:
        print("  ERROR: Flowgent PG schema reset failed")
        return False
    print("  Flowgent PG schema reset complete.")
    return True


def _step_build():
    from common import PROJECT_ROOT, CONSOLE_BIN
    from common import run_cmd
    print("\n-- Step: Build flowgent-core --")
    build_env = {"GOFLAGS": "-p=1", "GOMAXPROCS": "1", "GOGC": "50"}
    rc, _ = run_cmd(
        ["make", "-C", PROJECT_ROOT, "build:core"],
        timeout=_CORE_BUILD_TIMEOUT_SECONDS,
        env=build_env,
    )
    if rc != 0:
        print("  ERROR: Build failed")
        return False
    if os.path.isfile(CONSOLE_BIN):
        print(f"  Binary: {CONSOLE_BIN}")
    else:
        print(f"  ERROR: Binary not found after build: {CONSOLE_BIN}")
        return False

    print("\n-- Step: Build flowgent-core image --")
    _ensure_disk_headroom("before image build")
    # The Makefile enables its proxy-aware image recipe only when both flags
    # are present.  Keep this scoped to image builds so ordinary local commands
    # do not acquire network policy implicitly.
    env = {
        **build_env,
        "HTTPS_PROXY": "http://127.0.0.1:8800",
        "IN_CN_GFW": "true",
    }
    rc, _ = run_cmd(["make", "-C", PROJECT_ROOT, "build:image:core"], timeout=1200, env=env)
    if rc != 0:
        print("  WARN: image build failed, retrying with HTTPS_PROXY=http://127.0.0.1:8800")
        rc, _ = run_cmd(["make", "-C", PROJECT_ROOT, "build:image:core"], timeout=1200, env=env)
    if rc != 0:
        print("  ERROR: Docker image build failed")
        return False

    rc, _ = run_cmd(["docker", "tag", "flowgent-core:latest", "localhost/flowgent-core:latest"], timeout=60)
    if rc != 0:
        print("  ERROR: docker tag failed")
        return False
    # Docker is provided by Podman in this environment. Its Docker-compatible
    # `builder prune -af` can delete the just-tagged final image, not only
    # intermediate cache. Never run a builder prune after producing the image
    # that the following deployment step must export into k3s.
    _ensure_disk_headroom("after image build", prune_builder_cache=False)
    return True


def _ensure_disk_headroom(
    label: str,
    min_free_gib: int = 12,
    max_used_percent: int = 88,
    prune_builder_cache: bool = True,
):
    """Clean transient build cache only when root disk is near kubelet eviction."""
    usage = shutil.disk_usage("/")
    free_gib = usage.free / (1024 ** 3)
    used_percent = int((usage.used / usage.total) * 100)
    if free_gib >= min_free_gib and used_percent <= max_used_percent:
        return

    from common import run_cmd

    print(
        f"  WARN: low disk headroom {label}: free={free_gib:.1f}GiB used={used_percent}% "
        f"(target free>={min_free_gib}GiB used<={max_used_percent}%)"
    )
    if prune_builder_cache:
        rc, _ = run_cmd(["docker", "builder", "prune", "-af"], timeout=120)
        if rc != 0:
            run_cmd(["podman", "builder", "prune", "-af"], timeout=120)

    removed = 0
    for path in glob.glob("/tmp/go-build*"):
        if os.path.isdir(path):
            shutil.rmtree(path, ignore_errors=True)
            removed += 1
    if removed:
        print(f"  Removed {removed} /tmp/go-build* temporary directorie(s)")

    usage = shutil.disk_usage("/")
    print(f"  Disk after cleanup: free={usage.free / (1024 ** 3):.1f}GiB used={int((usage.used / usage.total) * 100)}%")


def _step_import():
    from common import PROJECT_ROOT, CONSOLE_BIN, CONSOLE_CFG, CONFIG_DIR
    from common import run_cmd
    print("\n-- Step: Import Config YAMLs --")
    if not os.path.isfile(CONSOLE_BIN):
        print(f"  ERROR: Binary not found: {CONSOLE_BIN}")
        print(f"  Build first: make -C {PROJECT_ROOT} build:core")
        return False
    if not os.path.isfile(CONSOLE_CFG):
        print(f"  ERROR: Config file not found: {CONSOLE_CFG}")
        return False
    if not os.path.isdir(CONFIG_DIR):
        print(f"  ERROR: Config dir not found: {CONFIG_DIR}")
        return False
    print(f"  Binary:     {CONSOLE_BIN}")
    print(f"  Config:     {CONSOLE_CFG}")
    print(f"  Config dir: {CONFIG_DIR}")
    rc, _ = run_cmd([CONSOLE_BIN, "--config", CONSOLE_CFG, "console", "import", CONFIG_DIR], timeout=60)
    if rc != 0:
        print("  ERROR: Console import returned non-zero exit code")
        return False
    print("  Config import complete.")
    return True


def _step_deploy(namespace, release):
    from deploy.s11_flowgent_deployer import (
        helm_install_or_upgrade, wait_for_pods, health_check as flowgent_health,
    )
    if not helm_install_or_upgrade(release, namespace):
        return False
    if not wait_for_pods(namespace, release, 300):
        return False
    return flowgent_health(namespace, release)


def run_pipeline(args):
    """Execute pipeline steps. Returns True if all requested steps succeeded."""
    if not args.skip_sonarqube:
        if not _step_sonarqube():
            return False

    if not all([args.skip_deploy, args.skip_import]):
        if not _step_reset_flowgent_db():
            return False

    if not args.skip_build:
        if not _step_build():
            return False

    if not args.skip_deploy:
        if not _step_deploy(args.namespace, args.release):
            return False

    if not args.skip_import:
        if not _step_import():
            return False

    return True


# ═══════════════════════════════════════════════════════════════════════
# Verification
# ═══════════════════════════════════════════════════════════════════════

def _archive_old_reports():
    if not os.path.isdir(REPORTS_DIR):
        return
    existing = [f for f in os.listdir(REPORTS_DIR)
                if f.endswith(".md") and os.path.isfile(os.path.join(REPORTS_DIR, f))]
    if not existing:
        return
    ts = datetime.now().strftime("%Y%m%d-%H%M%S")
    archive_dir = os.path.join(REPORTS_DIR, f"archived-{ts}")
    os.makedirs(archive_dir, exist_ok=True)
    for f in existing:
        shutil.move(os.path.join(REPORTS_DIR, f), os.path.join(archive_dir, f))
    print(f"  Archived {len(existing)} old report(s) -> {archive_dir}")


def _write_report(num, name, passed, elapsed, output, error=None):
    os.makedirs(REPORTS_DIR, exist_ok=True)
    status = "PASS" if passed else "FAIL"
    ts = datetime.now().strftime("%Y-%m-%d %H:%M:%S")
    lines = [
        f"# Scenario {num}: {name}",
        "",
        f"**Status**: {status}  ",
        f"**Duration**: {elapsed:.1f}s  ",
        f"**Timestamp**: {ts}  ",
        "",
        "## Output",
        "```",
    ]
    for line in output.getvalue().split("\n")[:200]:
        lines.append(line)
    lines.append("```")
    if error:
        lines.extend(["", "## Error", "```", str(error), "```"])
    report_path = os.path.join(REPORTS_DIR, f"{num}_{name.split('—')[0].strip().lower().replace(' ', '_')}.md")
    with open(report_path, "w") as f:
        f.write("\n".join(lines) + "\n")
    return report_path


def _write_summary(results, total_elapsed):
    passed = sum(1 for v in results.values() if v.get("passed"))
    total = len(results)
    ts = datetime.now().strftime("%Y-%m-%d %H:%M:%S")
    lines = [
        "# E2E Verification Summary",
        "",
        f"**Result**: {passed}/{total} passed  ",
        f"**Duration**: {total_elapsed:.1f}s  ",
        f"**Timestamp**: {ts}  ",
        "",
        "| # | Scenario | Status | Duration |",
        "|---|----------|--------|----------|",
    ]
    for num in sorted(results.keys()):
        r = results[num]
        status = "PASS" if r["passed"] else "FAIL"
        name_short = r["name"].split("—")[0].strip()
        lines.append(f"| {num} | {name_short} | {status} | {r['elapsed']:.1f}s |")
    lines.append("")
    failures = [(n, r) for n, r in results.items() if not r["passed"]]
    if failures:
        lines.append("## Failures")
        lines.append("")
        for num, r in failures:
            lines.append(f"### Scenario {num}: {r['name']}")
            lines.append("```")
            lines.append(r.get("error", "unknown error"))
            lines.append("```")
            lines.append("")
    else:
        lines.append("All scenarios passed.")
        lines.append("")
    report_path = os.path.join(REPORTS_DIR, "00_summary.md")
    with open(report_path, "w") as f:
        f.write("\n".join(lines) + "\n")
    return report_path


def _run_scenario(num, name, module_path):
    print(f"\n{'='*60}")
    print(f"  Scenario {num}: {name}")
    print(f"{'='*60}")

    output = io.StringIO()
    start = time.time()
    passed = False
    error_msg = None
    tee = None

    try:
        mod = importlib.import_module(module_path)
        class Tee(io.StringIO):
            def __init__(self, *args, **kwargs):
                super().__init__(*args, **kwargs)
                self.terminal = sys.stdout
            def write(self, s):
                self.terminal.write(s)
                super().write(s)
            def flush(self):
                self.terminal.flush()
                super().flush()
        tee = Tee()
        with contextlib.redirect_stdout(tee):
            mod.run()
        output = io.StringIO(tee.getvalue())
        elapsed = time.time() - start
        print(f"  PASS ({elapsed:.1f}s)")
        passed = True
    except Exception as e:
        if tee is not None:
            output = io.StringIO(tee.getvalue())
        elapsed = time.time() - start
        error_msg = f"{e}\n{traceback.format_exc()}"
        print(f"  FAIL ({elapsed:.1f}s): {e}")
        traceback.print_exc()

    return {"num": num, "name": name, "passed": passed, "elapsed": elapsed,
            "output": output, "error": error_msg}


def run_verification(args):
    """Run the verification suite. Returns True if all scenarios passed."""
    _archive_old_reports()

    forwards = _PortForwards(args.namespace).start()
    try:
        return _run_verification_suite(args, forwards)
    finally:
        forwards.stop()


def _run_verification_suite(args, forwards):
    """Run the suite after its required localhost service tunnels are ready."""

    suite_start = time.time()
    results = {}

    if args.scenario:
        num = args.scenario
        if num not in SCENARIOS:
            print(f"Unknown scenario: {num}")
            return False
        name, mod = SCENARIOS[num]
        forwards.ensure()
        r = _run_scenario(num, name, mod)
        results[num] = r
        _write_report(num, name, r["passed"], r["elapsed"], r["output"], r["error"])
    else:
        for num, (name, mod) in SCENARIOS.items():
            forwards.ensure()
            r = _run_scenario(num, name, mod)
            results[num] = r
            _write_report(num, name, r["passed"], r["elapsed"], r["output"], r["error"])

    total_elapsed = time.time() - suite_start
    passed = sum(1 for v in results.values() if v["passed"])
    total = len(results)

    summary_path = _write_summary(results, total_elapsed)
    print(f"\n{'='*60}")
    print(f"  Results: {passed}/{total} passed ({total_elapsed:.1f}s)")
    print(f"  Reports: {summary_path}")
    print(f"{'='*60}")

    return passed == total


# ═══════════════════════════════════════════════════════════════════════
# Main
# ═══════════════════════════════════════════════════════════════════════

def main():
    parser = argparse.ArgumentParser(description="Flowgent E2E Runner — pipeline + verification")
    # Pipeline flags
    parser.add_argument("--skip-sonarqube", action="store_true", help="Skip SonarQube PG reset")
    parser.add_argument("--skip-build", action="store_true", help="Skip binary build")
    parser.add_argument("--skip-import", action="store_true", help="Skip config YAML import")
    parser.add_argument("--skip-deploy", action="store_true", help="Skip Helm deploy")
    parser.add_argument("--no-verify", action="store_true", help="Skip verification suite")
    parser.add_argument("--namespace", "-n", default=config.K8S_NAMESPACE)
    parser.add_argument("--release", "-r", default="flowgent")
    # Verification flags
    parser.add_argument("--scenario", "-s", help="Run specific scenario (e.g. 11, 31)")
    parser.add_argument("--list", "-l", action="store_true", help="List available scenarios")
    # Config overrides
    parser.add_argument("--api", help=f"K8S API server URL (default: {config.K8S_APISERVER_URL})")
    parser.add_argument("--pg", help=f"PG DSN (default: {config.pg_dsn()})")
    args = parser.parse_args()

    if args.list:
        for k, (name, _) in SCENARIOS.items():
            print(f"  {k}: {name}")
        return

    if args.api:
        config.K8S_APISERVER_URL = args.api
    if args.pg:
        config.apply_pg_override(args.pg)

    print("=" * 60)
    print("  Flowgent E2E Runner")
    print(f"  API:  {config.K8S_APISERVER_URL}")
    print(f"  PG:   {config.pg_dsn()}")
    print("=" * 60)

    all_steps_skipped = args.skip_sonarqube and args.skip_build and args.skip_import and args.skip_deploy
    if all_steps_skipped:
        print("  All pipeline steps skipped — verify-only mode.")
    elif not all_steps_skipped:
        ok = run_pipeline(args)
        print(f"\n{'=' * 60}")
        if ok:
            print("  Pipeline complete — all steps OK.")
        else:
            print("  Pipeline finished with errors — check output above.")
            if not args.no_verify:
                print("  Skipping verification due to pipeline errors.")
                sys.exit(1)
        print(f"{'=' * 60}")

    if args.no_verify:
        print("\n  Verification skipped (--no-verify).")
        sys.exit(0)

    all_passed = run_verification(args)
    sys.exit(0 if all_passed else 1)


if __name__ == "__main__":
    main()
