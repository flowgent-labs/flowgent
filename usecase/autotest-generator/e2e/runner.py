#!/usr/bin/env python3
"""
Flowgent E2E Runner — pipeline orchestration + scenario verification.

Orchestrates:
  1. SonarQube PG reset (docker compose)
  2. Build flowgent-core binary (make build:core)
  3. Import config YAMLs into Flowgent DB (console import)
  4. Flowgent Helm deploy (helm install/upgrade + wait for pods)
  5. Verification suite (scenario-based checks)

Usage:
  python3 runner.py                             # full pipeline + verify all
  python3 runner.py --skip-build                # skip binary build
  python3 runner.py --no-verify                 # pipeline only, skip verification
  python3 runner.py --skip-deploy --no-verify   # setup only
  python3 runner.py --skip-sonarqube --skip-build --skip-import --skip-deploy
                                                # verify only (assumes infra is up)
  python3 runner.py --skip-sonarqube --skip-build --skip-import --skip-deploy -s 11
                                                # verify single scenario
  python3 runner.py -l                          # list available scenarios
  python3 runner.py --api http://10.0.0.1:9999 --pg postgres://u:p@h/db
                                                # override config

Reports are written to e2e/reports/ after every verification run.
"""

import sys
import os
import time
import argparse
import importlib
import io
import json
import shutil
import traceback
import contextlib
import subprocess as _sp
from datetime import datetime

from common import config

E2E_DIR = os.path.dirname(os.path.abspath(__file__))

REPORTS_DIR = os.path.join(E2E_DIR, "reports")

SCENARIOS = {
    # L0 — Infra Readiness
    # L1 — Flowgent Engine
    # L2 — Application (autotest-generator)
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


def _step_build():
    from common import PROJECT_ROOT, CONSOLE_BIN
    from common import run_cmd
    print("\n-- Step: Build flowgent-core --")
    rc, _ = run_cmd(["make", "-C", PROJECT_ROOT, "build:core"], timeout=120)
    if rc != 0:
        print("  ERROR: Build failed")
        return False
    if os.path.isfile(CONSOLE_BIN):
        print(f"  Binary: {CONSOLE_BIN}")
        return True
    print(f"  ERROR: Binary not found after build: {CONSOLE_BIN}")
    return False


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
    flowgent_health(namespace, release)
    return True


def run_pipeline(args):
    ok = True
    if not args.skip_sonarqube:
        if not _step_sonarqube():
            ok = False
    if not args.skip_build:
        if not _step_build():
            ok = False
    if not args.skip_import:
        if not _step_import():
            ok = False
    if not args.skip_deploy:
        if not _step_deploy(args.namespace, args.release):
            ok = False
    return ok


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
        elapsed = time.time() - start
        error_msg = f"{e}\n{traceback.format_exc()}"
        print(f"  FAIL ({elapsed:.1f}s): {e}")
        traceback.print_exc()
    return {"num": num, "name": name, "passed": passed, "elapsed": elapsed,
            "output": output, "error": error_msg}


def run_verification(args):
    if not SCENARIOS:
        print("  No scenarios defined yet.")
        return True
    _archive_old_reports()
    suite_start = time.time()
    results = {}
    if args.scenario:
        num = args.scenario
        if num not in SCENARIOS:
            print(f"Unknown scenario: {num}")
            return False
        name, mod = SCENARIOS[num]
        r = _run_scenario(num, name, mod)
        results[num] = r
        _write_report(num, name, r["passed"], r["elapsed"], r["output"], r["error"])
    else:
        for num, (name, mod) in SCENARIOS.items():
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
    parser.add_argument("--skip-sonarqube", action="store_true", help="Skip SonarQube PG reset")
    parser.add_argument("--skip-build", action="store_true", help="Skip binary build")
    parser.add_argument("--skip-import", action="store_true", help="Skip config YAML import")
    parser.add_argument("--skip-deploy", action="store_true", help="Skip Helm deploy")
    parser.add_argument("--no-verify", action="store_true", help="Skip verification suite")
    parser.add_argument("--namespace", "-n", default="default")
    parser.add_argument("--release", "-r", default="flowgent")
    parser.add_argument("--scenario", "-s", help="Run specific scenario (e.g. 11, 31)")
    parser.add_argument("--list", "-l", action="store_true", help="List available scenarios")
    parser.add_argument("--api", help=f"K3s API server URL (default: {config.K3S_APISERVER_URL})")
    parser.add_argument("--pg", help=f"PG DSN (default: {config.pg_dsn()})")
    args = parser.parse_args()

    if args.list:
        if SCENARIOS:
            for k, (name, _) in SCENARIOS.items():
                print(f"  {k}: {name}")
        else:
            print("  No scenarios defined yet.")
        return

    if args.api:
        config.K3S_APISERVER_URL = args.api
    if args.pg:
        config.apply_pg_override(args.pg)

    print("=" * 60)
    print("  Flowgent E2E Runner [autotest-generator]")
    print(f"  API:  {config.K3S_APISERVER_URL}")
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
