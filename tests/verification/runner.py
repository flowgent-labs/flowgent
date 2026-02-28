#!/usr/bin/env python3
"""
Flowgent E2E Verification Runner.

Usage:
  python3 tests/verification/runner.py                    # run all scenarios
  python3 tests/verification/runner.py --scenario 01      # run specific scenario
  python3 tests/verification/runner.py --list             # list available scenarios

Environment overrides: see config.py — all settings can be overridden via FLOWGENT_* env vars.
"""

import sys
import os
import argparse
import importlib
import time

sys.path.insert(0, os.path.dirname(__file__))

import config

SCENARIOS = {
    "01": ("REST API CRUD + Trigger + Run Lifecycle",       "scenarios.01_rest_api"),
    "02": ("A2A Protocol — Agent Card + Task Submit",      "scenarios.02_a2a"),
    "03": ("Flow Execution — Agent / Tribunal / Supervisor / Human", "scenarios.03_flow_execution"),
    "04": ("PG Storage — Run & Definition Persistence",    "scenarios.04_pg_storage"),
    "05": ("Jaeger OTEL — Trace Export Verification",      "scenarios.05_jaeger_tracing"),
    "06": ("Notifier MQTT — EMQX Message Publishing",      "scenarios.06_notifier_mqtt"),
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
        print(f"  ✅ PASS ({elapsed:.1f}s)")
        return True
    except Exception as e:
        elapsed = time.time() - start
        print(f"  ❌ FAIL ({elapsed:.1f}s): {e}")
        return False


def main():
    parser = argparse.ArgumentParser(description="Flowgent E2E Verification Runner")
    parser.add_argument("--scenario", "-s", help="Run specific scenario (e.g. 01, 02)")
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
