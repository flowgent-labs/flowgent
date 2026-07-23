#!/usr/bin/env python3
"""
Flowgent E2E Verification — scenario-based checks for the security-autonomy-fixer use case.

Run AFTER runner.py has completed setup (DB reset + config import + Helm deploy).
Runner:     use-cases/security-autonomy-fixer/runner.py
Verification: use-cases/security-autonomy-fixer/e2e-verification/verification.py

Scenarios are ordered by functional module execution time (runtime dependency):

  01 Infra        -> cluster / Helm / pods ready
  02 API Server   -> CRUD + lifecycle events
  03 A2A          -> agent card / task submit
  04 Controller   -> Application mode: create JM pods
  05 Messager     -> MQTT topics + sandbox chain
  06 Notifier     -> multi-channel delivery
  07 Wallet       -> x402 key mgmt + MQTT signing (optional)
  08 OTEL         -> OTEL infrastructure + Jaeger span coverage
  09 E2E Fixer    -> capstone: full security-autonomy-fixer pipeline
  10 PR Commits   -> verify actual fix commits on target PR
  11 Knowledge    -> RAG retrieval & knowledge injection
  12 Volume       -> PVC, mount, git clone & file RW
  13 Console      -> binary, import command, DB verification

Usage:
  python3 verification.py [-s N] [-l] [--api URL] [--pg DSN]

Examples:
  python3 verification.py                          # all scenarios
  python3 verification.py -s 01                    # infrastructure checks
  python3 verification.py -s 09                    # E2E security fixer capstone
  python3 verification.py -s 10                    # PR commit verification
  python3 verification.py -l                       # list scenarios
  python3 verification.py --api http://10.0.0.1:9999 --pg postgres://u:p@h/db

Reports are written to use-cases/security-autonomy-fixer/reports/ after every run.
Old reports are archived to reports/archived-YYYYMMDD-HHMMSS/ before each run.
"""

import sys
import os
import argparse
import importlib
import time
import io
import json
import shutil
import traceback
import contextlib
from datetime import datetime

import config

REPORTS_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "reports")

SCENARIOS = {
    "01": ("Infrastructure — Pre-Deployment & Pod Readiness",  "scenarios.01_infra_verifier"),
    "02": ("API Server — REST CRUD + Lifecycle Events",        "scenarios.02_apiserver_verifier"),
    "03": ("A2A Protocol — Agent Card & Task Submit",          "scenarios.03_a2a_protocol_verifier"),
    "04": ("Controller — Application Mode Lifecycle",          "scenarios.04_controller_verifier"),
    "05": ("Messager — MQTT Topics + Sandbox Chain",           "scenarios.05_messager_verifier"),
    "06": ("Notifier — Multi-Channel Delivery",                "scenarios.06_notifier_verifier"),
    "07": ("Wallet — x402 Key Management + MQTT Signing",      "scenarios.07_wallet_verifier"),
    "08": ("OTEL — Jaeger Span Coverage",                      "scenarios.08_otel_verifier"),
    "09": ("E2E — Security Fixer Full Pipeline (capstone)",    "scenarios.09_e2e_security_fixer"),
    "10": ("PR Commits — Verify Fix Commits on Target PR",     "scenarios.10_pr_commit_verifier"),
    "11": ("Knowledge — RAG Retrieval & Injection",            "scenarios.11_knowledge_verifier"),
    "12": ("Volume Workspace — PVC, Mount, Git Clone & File RW", "scenarios.12_volume_workspace_verifier"),
    "13": ("Console Import — Binary, Import Command, DB Verification", "scenarios.13_console_import_verifier"),
}


def archive_old_reports():
    """Move existing reports/*.md to reports/archived-YYYYMMDD-HHMMSS/."""
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


def write_report(num, name, passed, elapsed, output, error=None):
    """Write an individual scenario report .md file."""
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


def write_summary(results, total_elapsed):
    """Write 00_summary.md with overall results."""
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
    # Failures detail
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


def run_scenario(num, name, module_path):
    print(f"\n{'='*60}")
    print(f"  Scenario {num}: {name}")
    print(f"{'='*60}")

    output = io.StringIO()
    start = time.time()
    passed = False
    error_msg = None

    try:
        mod = importlib.import_module(module_path)
        # Tee stdout: capture to buffer AND print to terminal
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

    return {
        "num": num,
        "name": name,
        "passed": passed,
        "elapsed": elapsed,
        "output": output,
        "error": error_msg,
    }


def main():
    parser = argparse.ArgumentParser(description="Flowgent E2E Verification")
    parser.add_argument("--scenario", "-s", help="Run specific scenario (e.g. 01, 04, 11)")
    parser.add_argument("--list", "-l", action="store_true", help="List available scenarios")
    parser.add_argument("--api", help=f"K3s API server URL (default: {config.K3S_APISERVER_URL})")
    parser.add_argument("--pg", help=f"PG DSN (default: {config.pg_dsn()})")
    args = parser.parse_args()

    if args.list:
        for k, (name, _) in SCENARIOS.items():
            print(f"  {k}: {name}")
        return

    if args.api:
        config.K3S_APISERVER_URL = args.api
    if args.pg:
        parts = args.pg.replace("postgres://", "").split("@")
        user_pass = parts[0].split(":")
        host_db = parts[1].split("/")
        host_port = host_db[0].split(":")
        config.PG_USER = user_pass[0]
        if len(user_pass) > 1:
            config.PG_PASSWORD = user_pass[1]
        config.PG_HOST = host_port[0]
        if len(host_port) > 1:
            config.PG_PORT = int(host_port[1])
        config.PG_DATABASE = host_db[1].split("?")[0]

    print(f"API:  {config.K3S_APISERVER_URL}")
    print(f"PG:   {config.pg_dsn()}")
    print(f"EMQX: {config.EMQX_HOST}:{config.EMQX_PORT}")

    archive_old_reports()

    suite_start = time.time()
    results = {}

    if args.scenario:
        num = args.scenario
        if num not in SCENARIOS:
            print(f"Unknown scenario: {num}")
            sys.exit(1)
        name, mod = SCENARIOS[num]
        r = run_scenario(num, name, mod)
        results[num] = r
        write_report(num, name, r["passed"], r["elapsed"], r["output"], r["error"])
    else:
        for num, (name, mod) in SCENARIOS.items():
            r = run_scenario(num, name, mod)
            results[num] = r
            write_report(num, name, r["passed"], r["elapsed"], r["output"], r["error"])

    total_elapsed = time.time() - suite_start
    passed = sum(1 for v in results.values() if v["passed"])
    total = len(results)

    # Summary report
    summary_path = write_summary(results, total_elapsed)
    print(f"\n{'='*60}")
    print(f"  Results: {passed}/{total} passed ({total_elapsed:.1f}s)")
    print(f"  Reports: {summary_path}")
    print(f"{'='*60}")

    sys.exit(0 if passed == total else 1)


if __name__ == "__main__":
    main()
