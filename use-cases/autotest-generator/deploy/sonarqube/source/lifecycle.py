"""Deploy the draft AutoTest SonarQube components through the shared image/runtime policy."""

import time

import requests

DEFAULT_TIMEOUT = 240
SONARQUBE_URL = "http://localhost:9000"
API_HEALTH = f"{SONARQUBE_URL}/api/system/health"
API_STATUS = f"{SONARQUBE_URL}/api/system/status"


class SonarQubeDeployment:
    @staticmethod
    def compose_up(compose_file=None):
        from pathlib import Path

        from util.constant import E2EOtherDefinitions as config
        from util.docker import E2EDockerDeployer

        files = (
            (Path(compose_file),)
            if compose_file
            else tuple(
                sorted(
                    config.USE_CASE_DIR.glob("deploy/sonarqube*/docker-compose.yaml")
                )
            )
        )
        environment = E2EDockerDeployer.detect()
        # Preserve the scaffold's explicit fresh database lifecycle.
        stopped = E2EDockerDeployer.compose(
            files,
            "e2e-autotest-sonarqube",
            "down",
            "--volumes",
            environment=environment,
            timeout=60,
        )
        if not stopped.passed:
            print("  Previous SonarQube topology could not be removed")
            return False
        return E2EDockerDeployer.compose(
            files,
            "e2e-autotest-sonarqube",
            "up",
            "-d",
            environment=environment,
            timeout=120,
        ).passed

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
                        resp2 = requests.get(API_HEALTH, timeout=10)
                        if resp2.status_code == 200:
                            health = resp2.json()
                            print(
                                f"  SonarQube health: {health.get('health', 'UNKNOWN')}"
                            )
                            print(f"  SonarQube ready at {SONARQUBE_URL}")
                            return True
                else:
                    print(f"  API returned {resp.status_code}, retrying...")
            except requests.ConnectionError:
                print("  Connection refused, waiting for SonarQube to start...")
            except requests.Timeout:
                print("  Request timed out, retrying...")
            except Exception as e:
                print(f"  Unexpected error: {e}")
            time.sleep(10)
        print(f"  ERROR: SonarQube did not become healthy within {timeout}s")
        return False

    @staticmethod
    def verify_credentials():
        print("\n-- Verifying SonarQube admin credentials --")
        try:
            resp = requests.get(
                f"{SONARQUBE_URL}/api/authentication/validate",
                auth=("admin", "Abcd1234@sonar"),
                timeout=10,
            )
            if resp.status_code == 200 and resp.json().get("valid"):
                print("  Admin credentials: OK")
                return True
            print(
                f"  WARN: Default admin credentials not valid (status={resp.status_code})"
            )
        except Exception as e:
            print(f"  WARN: Could not verify credentials: {e}")
        return True
