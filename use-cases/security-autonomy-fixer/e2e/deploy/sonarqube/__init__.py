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

Invoked only through ``runner.py`` by either E2E deployer backend.
"""
from __future__ import annotations

import os
import time

import requests

from deploy import BaseDeployer
from common.config import SONAR_COMPOSE
from common.model import RunContext
from common.process import CommandRunner

DEFAULT_TIMEOUT = 240
SONARQUBE_URL = os.getenv("SONARQUBE_URL", "http://localhost:9000").rstrip("/")
API_HEALTH = f"{SONARQUBE_URL}/api/system/health"
API_STATUS = f"{SONARQUBE_URL}/api/system/status"


class SonarQubeRuntime:
    """Class-owned operations for sonarqube."""

    @staticmethod
    def compose_up(compose_file=None, timeout=DEFAULT_TIMEOUT):
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

        rc, _ = CommandRunner.legacy(["docker", "compose", "-f", compose_file, "down", "-v"], timeout=60)
        if rc != 0:
            print(f"  WARN: docker compose down returned non-zero (may be first run)")

        rc, _ = CommandRunner.legacy(
            ["docker", "compose", "-f", compose_file, "up", "-d"],
            timeout=timeout,
        )
        if rc != 0:
            print(f"  ERROR: docker compose up failed")
            return False

        print(f"  Compose stack started (postgres + sonarqube + sonarqube-init).")
        return True

    @staticmethod
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
                        # /api/system/status is SonarQube's unauthenticated readiness
                        # contract and is also what the compose healthcheck uses. The
                        # richer /api/system/health endpoint requires admin permission
                        # on current releases, so treat it as diagnostic-only.
                        try:
                            health_resp = requests.get(API_HEALTH, timeout=10)
                            if health_resp.status_code == 200:
                                health = health_resp.json()
                                print(f"  SonarQube health: {health.get('health', 'UNKNOWN')}")
                            else:
                                print(
                                    "  SonarQube health details unavailable "
                                    f"(status={health_resp.status_code})"
                                )
                        except requests.RequestException as exc:
                            print(f"  SonarQube health details unavailable: {exc}")
                        print(f"  SonarQube ready at {SONARQUBE_URL}")
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

    @staticmethod
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






class SonarQubeDeployer(BaseDeployer):
    """Own the SonarQube fixture consumed by the security-fixer flow."""

    component = "sonarqube"

    def deploy(self) -> None:
        if not self.step(
            "start isolated SonarQube compose stack",
            lambda: SonarQubeRuntime.compose_up(timeout=self.context.timeout_seconds),
        ):
            raise RuntimeError("SonarQube compose deployment failed")

    def verify(self) -> None:
        if not self.step(
            "wait for SonarQube health",
            lambda: SonarQubeRuntime.wait_for_sonarqube(self.context.timeout_seconds),
        ):
            raise RuntimeError("SonarQube did not become healthy")
        if not self.step("verify SonarQube credentials", SonarQubeRuntime.verify_credentials):
            raise RuntimeError("SonarQube credential verification failed")
