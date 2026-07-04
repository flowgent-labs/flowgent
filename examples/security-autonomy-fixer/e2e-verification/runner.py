#!/usr/bin/env python3
"""
Flowgent E2E Verification Runner.

Scenarios are ordered by functional module execution time (runtime dependency):

  01 Infra        -> cluster / Helm / pods ready
  02 API Server   -> CRUD + lifecycle events
  03 A2A          -> agent card / task submit
  04 Controller   -> Application mode: create JM pods
  05 Engine       -> DAG scheduling / voting (after JM exists)
  06 Basic Nodes  -> node executors (after engine can dispatch)
  07 Messager     -> MQTT topics + sandbox chain
  08 Notifier     -> multi-channel delivery
  09 Wallet       -> x402 key mgmt + MQTT signing (optional)
  10 OTEL         -> Jaeger span coverage (after flow can run)
  11 E2E Fixer    -> capstone: full security-autonomy-fixer pipeline

Usage:
  python3 runner.py [-s N] [-l] [--api URL] [--pg DSN]

Examples:
  python3 runner.py                          # all scenarios
  python3 runner.py -s 01                    # infrastructure checks
  python3 runner.py -s 04                    # controller (JM lifecycle)
  python3 runner.py -s 05                    # engine DAG
  python3 runner.py -s 09                    # wallet signing
  python3 runner.py -s 10                    # OTEL / Jaeger
  python3 runner.py -s 11                    # E2E security fixer capstone
  python3 runner.py -l                       # list scenarios
  python3 runner.py --api http://10.0.0.1:9999 --pg postgres://u:p@h/db

All configuration via environment variables (FLOWGENT_* prefix).
"""

import sys
import os
import argparse
import importlib
import time
import config


SCENARIOS = {
    "01": ("Infrastructure — Pre-Deployment & Pod Readiness",  "scenarios.01_infra_verifier"),
    "02": ("API Server — REST CRUD + Lifecycle Events",        "scenarios.02_apiserver_verifier"),
    "03": ("A2A Protocol — Agent Card & Task Submit",          "scenarios.03_a2a_protocol_verifier"),
    "04": ("Controller — Application Mode Lifecycle",          "scenarios.04_controller_verifier"),
    "05": ("Engine — DAG Scheduling + Voting Strategies",      "scenarios.05_engine_verifier"),
    "06": ("Basic Nodes — Agent/Tribunal/Supervisor",          "scenarios.06_basic_nodes_verifier"),
    "07": ("Messager — MQTT Topics + Sandbox Chain",           "scenarios.07_messager_verifier"),
    "08": ("Notifier — Multi-Channel Delivery",                "scenarios.08_notifier_verifier"),
    "09": ("Wallet — x402 Key Management + MQTT Signing",      "scenarios.09_wallet_verifier"),
    "10": ("OTEL — Jaeger Span Coverage",                      "scenarios.10_otel_verifier"),
    "11": ("E2E — Security Fixer Full Pipeline (capstone)",    "scenarios.11_e2e_security_fixer"),
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
