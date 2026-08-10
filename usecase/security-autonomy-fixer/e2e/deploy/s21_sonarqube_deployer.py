#!/usr/bin/env python3
"""
Deploy Script S21 — SonarQube Middleware deployment and health check.

Starts (or restarts) the SonarQube + PostgreSQL docker compose stack and
waits for the SonarQube API to respond healthy before handing off.

Stack components (from deploy/docker/sonarqube/docker-compose.yml):
  - sonarqube-postgres  — PostgreSQL 18.3 (health check: pg_isready)
  - sonarqube           — SonarQube 26.4.0 Community (health: /api/system/health)
  - sonarqube-init      — One-shot container that resets admin password on first boot

Auto Password Reset:
  The sonarqube-init container waits for SonarQube to be healthy, then calls:
    POST /api/users/change_password?login=admin&password=Abcd1234@sonar&previousPassword=admin
  This ensures the admin password is always Abcd1234@sonar after deployment,
  even if the SonarQube data volume is recreated.

Credentials:
  +-------------+------------------------------------+
  | URL         | http://localhost:9000              |
  | Admin user  | admin                             |
  | Password    | Abcd1234@sonar                    |
  +-------------+------------------------------------+

Post-Deploy CI Integration:
  After deploy, a SonarQube API token is needed for CI (GitHub Actions, Jenkins, etc.):
    1. Log in to SonarQube → My Account → Security → Generate Token
    2. Name: flowgent-ci
    3. Set as CI secret: SONAR_TOKEN + SONAR_HOST_URL
  GitHub Actions PR workflow runs incremental scan (sonar.pullrequest.key/branch/base),
  push-to-main workflow runs full scan without PR parameters.

SonarQube verification checklist:
  +------------------------+----------------------------------------------------------+
  | Check                  | Command                                                  |
  +------------------------+----------------------------------------------------------+
  | Status                 | curl -u admin:Abcd1234@sonar http://localhost:9000/api/  |
  |                        |   system/status                                          |
  | Health                 | curl -u admin:Abcd1234@sonar http://localhost:9000/api/  |
  |                        |   system/health                                          |
  | Validate credentials   | curl -u admin:Abcd1234@sonar http://localhost:9000/api/  |
  |                        |   authentication/validate                                |
  +------------------------+----------------------------------------------------------+

Usage:
  python3 s21_sonarqube_deployer.py [--compose PATH] [--timeout S]
"""

import sys
import os
import time
import argparse

import requests

from common import SONAR_COMPOSE, run_cmd

DEFAULT_TIMEOUT = 240
SONARQUBE_URL = os.getenv("SONARQUBE_URL", "http://localhost:9000").rstrip("/")
API_HEALTH = f"{SONARQUBE_URL}/api/system/health"
API_STATUS = f"{SONARQUBE_URL}/api/system/status"


def compose_up(compose_file=None):
    """
    Bring up the SonarQube + PostgreSQL stack via docker compose.

    Performs a full teardown (down -v to wipe PG data) then up -d. The
    sonarqube-init container automatically resets the admin password to
    Abcd1234@sonar once SonarQube becomes healthy.
    """
    compose_file = compose_file or SONAR_COMPOSE
    if SONARQUBE_URL != "http://localhost:9000":
        print(f"  Using externally managed SonarQube: {SONARQUBE_URL}")
        return True
    print(f"\n-- Starting SonarQube middleware --")
    print(f"  Compose file: {compose_file}")

    if not os.path.isfile(compose_file):
        print(f"  ERROR: Compose file not found: {compose_file}")
        return False

    rc, _ = run_cmd(["docker", "compose", "-f", compose_file, "down", "-v"], timeout=60)
    if rc != 0:
        print(f"  WARN: docker compose down returned non-zero (may be first run)")

    rc, _ = run_cmd(["docker", "compose", "-f", compose_file, "up", "-d"], timeout=120)
    if rc != 0:
        print(f"  ERROR: docker compose up failed")
        return False

    print(f"  Compose stack started (postgres + sonarqube + sonarqube-init).")
    return True


def wait_for_sonarqube(timeout=DEFAULT_TIMEOUT):
    print(f"\n-- Waiting for SonarQube API (timeout={timeout}s) --")
    deadline = time.time() + timeout

    while time.time() < deadline:
        try:
            resp = requests.get(API_STATUS, timeout=10)
            if resp.status_code == 200:
                data = resp.json()
                status = data.get("status", "UNKNOWN")
                print(f"  SonarQube status: {status}")
                if status == "UP":
                    resp2 = requests.get(API_HEALTH, timeout=10)
                    if resp2.status_code == 200:
                        health = resp2.json()
                        print(f"  SonarQube health: {health.get('health', 'UNKNOWN')}")
                        print(f"  SonarQube ready at {SONARQUBE_URL}")
                        return True
                    if SONARQUBE_URL != "http://localhost:9000" and resp2.status_code == 403:
                        # The public status endpoint already proved the service is UP;
                        # external deployments may restrict the diagnostic health API.
                        print(f"  SonarQube ready at {SONARQUBE_URL} (health API access restricted)")
                        return True
            else:
                print(f"  API returned {resp.status_code}, retrying...")
        except requests.ConnectionError:
            print(f"  Connection refused, waiting for SonarQube to start...")
        except requests.Timeout:
            print(f"  Request timed out, retrying...")
        except Exception as e:
            print(f"  Unexpected error: {e}")

        time.sleep(10)

    print(f"  ERROR: SonarQube did not become healthy within {timeout}s")
    return False


def verify_credentials():
    print(f"\n-- Verifying SonarQube admin credentials --")
    try:
        resp = requests.get(
            f"{SONARQUBE_URL}/api/authentication/validate",
            auth=("admin", "Abcd1234@sonar"),
            timeout=10,
        )
        if resp.status_code == 200 and resp.json().get("valid"):
            print(f"  Admin credentials: OK")
            return True
        print(f"  WARN: Default admin credentials not valid (status={resp.status_code})")
    except Exception as e:
        print(f"  WARN: Could not verify credentials: {e}")
    return True  # non-fatal


def main():
    parser = argparse.ArgumentParser(description="Deploy SonarQube middleware via docker compose")
    parser.add_argument("--compose", "-c", default=SONAR_COMPOSE)
    parser.add_argument("--timeout", "-t", type=int, default=DEFAULT_TIMEOUT)
    args = parser.parse_args()

    print("=" * 60)
    print("  Deploy S21: SonarQube Middleware")
    print(f"  Compose: {args.compose}")
    print(f"  URL:     {SONARQUBE_URL}")
    print("=" * 60)

    ok = True

    if not compose_up(args.compose):
        ok = False

    if ok and not wait_for_sonarqube(args.timeout):
        ok = False

    if ok:
        verify_credentials()

    print(f"\n{'=' * 60}")
    if ok:
        print("  Deploy S21 complete — SonarQube middleware ready.")
        print(f"  Admin: admin / Abcd1234@sonar")
        print()
        print("  Next: generate an API token for CI:")
        print("    SonarQube → My Account → Security → Generate Token")
        print("    gh secret set SONAR_TOKEN --body \"squ_...\" -R <owner/repo>")
        print("    gh secret set SONAR_HOST_URL --body \"http://localhost:9000\" -R <owner/repo>")
    else:
        print("  Deploy S21 finished with errors — check output above.")
    print(f"{'=' * 60}")

    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
