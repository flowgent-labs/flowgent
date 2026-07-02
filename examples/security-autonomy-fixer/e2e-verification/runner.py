#!/usr/bin/env python3
"""
Flowgent E2E Verification Runner.

Usage:
  python3 runner.py [-s N] [-l] [--api URL] [--pg DSN]

Examples:
  python3 runner.py                          # all scenarios
  python3 runner.py -s 01                    # preflight only
  python3 runner.py -s 08                    # security fixer only
  python3 runner.py -l                       # list scenarios
  python3 runner.py --api http://10.0.0.1:9999 --pg postgres://u:p@h/db
  FLOWGENT_K3S_APISERVER=http://k3s:9999 python3 runner.py

Scenarios:
  01 — Pre-Deployment & Infrastructure (L1-L3): K3s, Helm, Pod Readiness
  02 — REST API CRUD + Trigger + Run Lifecycle
  03 — A2A Protocol (Agent Card + Task Submit)
  04 — Flow Execution (Agent / Tribunal / Supervisor nodes)
  05 — PG Storage (Run & Definition Persistence)
  06 — Jaeger OTEL (Trace Export Verification)
  07 — Notifier MQTT (EMQX Message Publishing)
  08 — Security Fixer (Full Pipeline White-Box)

Config: see config.py for all FLOWGENT_* environment variables.
"""

import sys
import os
import argparse
import importlib
import time

sys.path.insert(0, os.path.dirname(__file__))

import config

SCENARIOS = {
    "01": ("Pre-Deployment & Infrastructure (L1-L3)",      "scenarios.01_preflight"),
    "02": ("REST API CRUD + Trigger + Run Lifecycle",       "scenarios.02_rest_api"),
    "03": ("A2A Protocol — Agent Card + Task Submit",      "scenarios.03_a2a"),
    "04": ("Flow Execution — Agent / Tribunal / Supervisor", "scenarios.04_flow_execution"),
    "05": ("PG Storage — Run & Definition Persistence",    "scenarios.05_pg_storage"),
    "06": ("Jaeger OTEL — Trace Export Verification",      "scenarios.06_jaeger_tracing"),
    "07": ("Notifier MQTT — EMQX Message Publishing",      "scenarios.07_notifier_mqtt"),
    "08": ("Security Fixer — Full Pipeline White-Box",     "scenarios.08_security_fixer"),
}


def run_scenario(num, name, module_path):
    print(f"\n{'='*60}")
    print(f"  Scenario {num}: {name}")
    print(f"{'='*60}")
    start = time.time()
    try:
        mod = importlib.import_module(module_path)
        mod.run()
        elapsed = time.time() - start
        print(f"  PASS ({elapsed:.1f}s)")
        return True
    except Exception as e:
        elapsed = time.time() - start
        print(f"  FAIL ({elapsed:.1f}s): {e}")
        import traceback
        traceback.print_exc()
        return False


def main():
    parser = argparse.ArgumentParser(description="Flowgent E2E Verification Runner")
    parser.add_argument("--scenario", "-s", help="Run specific scenario (e.g. 01, 08)")
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
        config.PG_PASSWORD = user_pass[1]
        config.PG_HOST = host_port[0]
        config.PG_PORT = int(host_port[1]) if len(host_port) > 1 else 5432
        config.PG_DATABASE = host_db[1].split("?")[0] if len(host_db) > 1 else "flowgent"

    print(f"API:  {config.K3S_APISERVER_URL}")
    print(f"PG:   {config.pg_dsn()}")
    print(f"EMQX: {config.EMQX_HOST}:{config.EMQX_PORT}")

    results = {}
    if args.scenario:
        num = args.scenario
        if num not in SCENARIOS:
            print(f"Unknown scenario: {num}")
            sys.exit(1)
        name, mod = SCENARIOS[num]
        results[num] = run_scenario(num, name, mod)
    else:
        for num, (name, mod) in SCENARIOS.items():
            results[num] = run_scenario(num, name, mod)

    passed = sum(1 for v in results.values() if v)
    total = len(results)
    print(f"\n{'='*60}")
    print(f"  Results: {passed}/{total} passed")
    print(f"{'='*60}")
    sys.exit(0 if passed == total else 1)


if __name__ == "__main__":
    main()
