#!/usr/bin/env python3
"""
Security Autonomy Fixer — Use-Case Runner.

Master entry point for the real demo. Runs "from zero" setup before every
Helm deploy / test cycle:

  1. Clean SonarQube PostgreSQL (docker compose down -v + up -d)
  2. Build the flowgent-core binary (make build:core)
  3. Import all config YAMLs into the Flowgent database (console import)

After this runner completes, run the verification suite to check each step:
    python3 e2e-verification/verification.py

Usage:
  python3 runner.py [--skip-sonarqube] [--skip-build] [--skip-import]

Examples:
  python3 runner.py                              # full reset
  python3 runner.py --skip-sonarqube             # build + import only
  python3 runner.py --skip-build                 # sonarqube reset + import (binary must exist)
"""

import subprocess
import sys
import os
import argparse
import time

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
PROJECT_ROOT = os.path.abspath(os.path.join(SCRIPT_DIR, "..", ".."))

SONAR_COMPOSE = os.path.join(PROJECT_ROOT, "deploy", "docker", "sonarqube", "docker-compose.yml")
CONFIG_DIR = os.path.join(SCRIPT_DIR, "config")
CONSOLE_BIN = os.path.join(PROJECT_ROOT, "bin", "flowgent-core")
CONSOLE_CFG = os.path.join(PROJECT_ROOT, "etc", "flowgent.yaml")

SONAR_STARTUP_WAIT = 15


def run_cmd(cmd, cwd=None, timeout=120, env=None):
    """Run a command and stream output. Returns exit code."""
    print(f"  $ {' '.join(cmd)}")
    merged_env = {**os.environ, **(env or {})}
    try:
        result = subprocess.run(
            cmd, cwd=cwd, timeout=timeout, env=merged_env,
            capture_output=True, text=True,
        )
        if result.stdout:
            for line in result.stdout.splitlines():
                print(f"    {line}")
        if result.stderr:
            for line in result.stderr.splitlines():
                print(f"    [stderr] {line}")
        return result.returncode
    except subprocess.TimeoutExpired:
        print(f"    ERROR: timed out after {timeout}s")
        return 1
    except FileNotFoundError:
        print(f"    ERROR: command not found: {cmd[0]}")
        return 1


def reset_sonarqube():
    """Nuke SonarQube PG volume and restart fresh."""
    print("\n── Step 1: Reset SonarQube PostgreSQL ──")
    compose_file = SONAR_COMPOSE
    if not os.path.isfile(compose_file):
        print(f"  ERROR: compose file not found: {compose_file}")
        return False

    print(f"  Compose file: {compose_file}")
    rc = run_cmd(["docker", "compose", "-f", compose_file, "down", "-v"])
    if rc != 0:
        print("  WARN: docker compose down returned non-zero (may be first run)")

    rc = run_cmd(["docker", "compose", "-f", compose_file, "up", "-d"], timeout=60)
    if rc != 0:
        print("  ERROR: docker compose up failed")
        return False

    print(f"  Waiting {SONAR_STARTUP_WAIT}s for SonarQube startup...")
    time.sleep(SONAR_STARTUP_WAIT)
    print("  SonarQube should be ready at http://localhost:9000")
    print("  Admin credentials: admin / Abcd1234@sonar")
    return True


def build_core():
    """Build the flowgent-core binary."""
    print("\n── Step 2: Build flowgent-core ──")
    rc = run_cmd(["make", "-C", PROJECT_ROOT, "build:core"], timeout=120)
    if rc != 0:
        print("  ERROR: Build failed")
        return False
    if os.path.isfile(CONSOLE_BIN):
        print(f"  Binary: {CONSOLE_BIN}")
        return True
    print(f"  ERROR: Binary not found after build: {CONSOLE_BIN}")
    return False


def import_config():
    """Import all config YAMLs into Flowgent PG."""
    print("\n── Step 3: Import Config YAMLs ──")
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

    rc = run_cmd(
        [CONSOLE_BIN, "--config", CONSOLE_CFG, "console", "import", CONFIG_DIR],
        timeout=60,
    )
    if rc != 0:
        print("  ERROR: Console import returned non-zero exit code")
        return False

    print("  Config import complete.")
    return True


def main():
    parser = argparse.ArgumentParser(
        description="Security Autonomy Fixer — Use-Case Runner (from-zero setup)"
    )
    parser.add_argument("--skip-sonarqube", action="store_true",
                        help="Skip SonarQube PG reset")
    parser.add_argument("--skip-build", action="store_true",
                        help="Skip binary build")
    parser.add_argument("--skip-import", action="store_true",
                        help="Skip config YAML import")
    args = parser.parse_args()

    print("=" * 60)
    print("  Security Autonomy Fixer — Runner")
    print(f"  Project root: {PROJECT_ROOT}")
    print(f"  Use-case dir: {SCRIPT_DIR}")
    print("=" * 60)

    ok = True

    if not args.skip_sonarqube:
        if not reset_sonarqube():
            ok = False

    if not args.skip_build:
        if not build_core():
            ok = False

    if not args.skip_import:
        if not import_config():
            ok = False

    print(f"\n{'=' * 60}")
    if ok:
        print("  Runner complete — all steps OK.")
        print()
        print("  Next: run the verification suite:")
        print(f"    cd {SCRIPT_DIR}/e2e-verification")
        print("    python3 verification.py")
    else:
        print("  Runner finished with errors — check output above.")
    print(f"{'=' * 60}")

    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
