#!/usr/bin/env python3
"""Deploy and verify isolated SonarQube, PostgreSQL and MCP components for either backend."""

from __future__ import annotations

import atexit
import hashlib
import json
import os
import subprocess
import time
from pathlib import Path

import requests
from util.constant import E2EOtherDefinitions as config
from util.images import E2EImagesDefinitions
from util.kubernetes import E2EKubernetesDeployer
from verifier.context import BaseDeployer

DEFAULT_TIMEOUT = 240
REQUIRED_MCP_TOOL = "GetIssuesSearch"
SONARQUBE_NAME = "sonarqube"
SONARQUBE_POSTGRES_NAME = f"{SONARQUBE_NAME}-postgres"
SONARQUBE_MCP_NAME = f"{SONARQUBE_NAME}-mcp"
SONARQUBE_IMAGE = E2EImagesDefinitions.get("sonarqube")
SONARQUBE_POSTGRES_IMAGE = E2EImagesDefinitions.get("postgres18")
SONARQUBE_MCP_IMAGE = E2EImagesDefinitions.get("sonarqube-mcp")
SONARQUBE_PASSWORD = "Abcd1234@sonar"


class SonarQubeRuntime:
    """Class-owned operations for sonarqube."""

    JVM_OPTIONS = "-Xms128m -Xmx256m"
    _clients = {}

    @classmethod
    def _forward_client(cls, service: str, port: int, environment_key: str) -> None:
        if service not in cls._clients:
            forward = E2EKubernetesDeployer.port_forward(
                config.SONARQUBE_NAMESPACE, f"service/{service}", port
            )
            local_port = forward.__enter__()
            cls._clients[service] = (forward, local_port)
            atexit.register(forward.__exit__, None, None, None)
        os.environ[environment_key] = f"http://127.0.0.1:{cls._clients[service][1]}"
        if service == SONARQUBE_NAME:
            config.SONARQUBE_URL = os.environ[environment_key]
        if service == SONARQUBE_MCP_NAME:
            os.environ[environment_key] += "/mcp"

    @staticmethod
    def url() -> str:
        return os.getenv("FLOWGENT_SONARQUBE_URL", config.SONARQUBE_URL).rstrip("/")

    @staticmethod
    def mcp_url() -> str:
        return os.getenv(
            "FLOWGENT_E2E_SONARQUBE_MCP_URL",
            os.getenv("SONARQUBE_MCP_URL", "http://localhost:18080/mcp"),
        ).rstrip("/")

    @staticmethod
    def _apply(manifest: dict) -> None:
        E2EKubernetesDeployer.apply(manifest)

    @staticmethod
    def _labels(component: str) -> dict[str, str]:
        return {
            "app.kubernetes.io/name": "sonarqube",
            "app.kubernetes.io/instance": SONARQUBE_NAME,
            "app.kubernetes.io/component": component,
            "app.kubernetes.io/part-of": config.RESOURCE_PREFIX,
            "flowgent.io/e2e-owner": config.RESOURCE_PREFIX,
        }

    @staticmethod
    def _wait_rollout(resource: str, timeout: int) -> None:
        result = subprocess.run(
            [
                "kubectl",
                "rollout",
                "status",
                resource,
                "-n",
                config.SONARQUBE_NAMESPACE,
                f"--timeout={timeout}s",
            ],
            capture_output=True,
            text=True,
            timeout=timeout + 30,
        )
        if result.returncode != 0:
            detail = (result.stdout + result.stderr).strip()[-800:]
            raise RuntimeError(f"SonarQube resource not ready: {resource}: {detail}")

    @staticmethod
    def kubernetes_up(timeout: int = DEFAULT_TIMEOUT) -> bool:
        """Reconcile the complete SonarQube fixture inside its owned namespace."""
        namespace = config.SONARQUBE_NAMESPACE
        postgres_labels = SonarQubeRuntime._labels("postgresql")
        sonar_labels = SonarQubeRuntime._labels("server")
        SonarQubeRuntime._apply(
            E2EKubernetesDeployer.helm_template(
                config.USE_CASE_DIR / "deploy" / "sonarqube" / "helm",
                {
                    "runtime": {
                        "namespace": namespace,
                        "resource_prefix": config.RESOURCE_PREFIX,
                    }
                },
                template="kubernetes-up-1.yaml",
            )
        )
        values = {
            "runtime": {
                "name": SONARQUBE_NAME,
                "namespace": namespace,
                "labels": sonar_labels,
                "postgresName": SONARQUBE_POSTGRES_NAME,
                "postgresLabels": postgres_labels,
                "postgresImage": SONARQUBE_POSTGRES_IMAGE,
                "image": SONARQUBE_IMAGE,
                "nodePort": config.SONARQUBE_NODE_PORT,
                "javaOptions": SonarQubeRuntime.JVM_OPTIONS,
            }
        }
        for component, template in (
            ("sonarqube-postgres", "resources.yaml"),
            ("sonarqube", "kubernetes-up-2.yaml"),
        ):
            SonarQubeRuntime._apply(
                E2EKubernetesDeployer.helm_template(
                    config.USE_CASE_DIR / "deploy" / component / "helm",
                    values,
                    template=template,
                )
            )
        SonarQubeRuntime._wait_rollout(
            f"statefulset/{SONARQUBE_POSTGRES_NAME}", min(timeout, 300)
        )
        SonarQubeRuntime._wait_rollout(
            f"deployment/{SONARQUBE_NAME}", max(timeout, 360)
        )
        SonarQubeRuntime._forward_client(SONARQUBE_NAME, 9000, "FLOWGENT_SONARQUBE_URL")
        if not SonarQubeRuntime.wait_for_sonarqube(max(timeout, 360)):
            return False
        token = SonarQubeRuntime._ensure_api_token()
        token_hash = hashlib.sha256(token.encode()).hexdigest()
        mcp_labels = SonarQubeRuntime._labels("mcp")
        SonarQubeRuntime._apply(
            E2EKubernetesDeployer.helm_template(
                config.USE_CASE_DIR / "deploy" / "sonarqube-mcp" / "helm",
                {
                    "runtime": {
                        "sonarqube_mcp_name": SONARQUBE_MCP_NAME,
                        "namespace": namespace,
                        "mcp_labels": mcp_labels,
                        "token": token,
                        "sonarqube_name": SONARQUBE_NAME,
                        "sonarqube_mcp_node_port": config.SONARQUBE_MCP_NODE_PORT,
                        "token_hash": token_hash,
                        "sonarqube_mcp_image": SONARQUBE_MCP_IMAGE,
                        "parameter_9": f"http://{SONARQUBE_NAME}:9000",
                    }
                },
                template="resources.yaml",
            )
        )
        SonarQubeRuntime._wait_rollout(
            f"deployment/{SONARQUBE_MCP_NAME}", min(timeout, 300)
        )
        SonarQubeRuntime._forward_client(
            SONARQUBE_MCP_NAME, 8080, "FLOWGENT_E2E_SONARQUBE_MCP_URL"
        )
        print(f"  SonarQube K8s stack ready in {namespace}")
        return True

    @staticmethod
    def _ensure_api_token() -> str:
        endpoint = SonarQubeRuntime.url()
        default = ("admin", "admin")
        retained = ("admin", SONARQUBE_PASSWORD)
        validated = requests.get(
            f"{endpoint}/api/authentication/validate", auth=retained, timeout=15
        )
        if not (validated.status_code == 200 and validated.json().get("valid")):
            changed = requests.post(
                f"{endpoint}/api/users/change_password",
                auth=default,
                data={
                    "login": "admin",
                    "password": SONARQUBE_PASSWORD,
                    "previousPassword": "admin",
                },
                timeout=30,
            )
            if changed.status_code not in (200, 204):
                raise RuntimeError(
                    f"could not initialize SonarQube administrator: HTTP {changed.status_code}"
                )
        requests.post(
            f"{endpoint}/api/user_tokens/revoke",
            auth=retained,
            data={"name": "flowgent-e2e"},
            timeout=20,
        )
        generated = requests.post(
            f"{endpoint}/api/user_tokens/generate",
            auth=retained,
            data={"name": "flowgent-e2e"},
            timeout=20,
        )
        if generated.status_code != 200 or not generated.json().get("token"):
            raise RuntimeError(
                f"could not generate SonarQube MCP token: HTTP {generated.status_code}"
            )
        token = generated.json()["token"]
        os.environ["SONARQUBE_TOKEN"] = token
        config.SONARQUBE_TOKEN = token
        return token

    @staticmethod
    def compose_up(compose_file=None, timeout=DEFAULT_TIMEOUT):
        """
        Bring up the SonarQube + PostgreSQL stack via docker compose.

        Uses an idempotent ``up -d`` so verifier reruns preserve SonarQube data.
        The init container initializes credentials only when needed.
        """
        from util.docker import E2EDockerDeployer

        sonarqube_url = SonarQubeRuntime.url()
        if sonarqube_url != "http://localhost:9000":
            print(f"  Using externally managed SonarQube: {sonarqube_url}")
            return True
        files = (
            (Path(compose_file),)
            if compose_file
            else tuple(
                sorted(
                    config.USE_CASE_DIR.glob("deploy/sonarqube*/docker-compose.yaml")
                )
            )
        )
        environment = {
            **E2EDockerDeployer.detect(),
            # Compose interpolates extends files before merging overrides. The
            # MCP service is not started until a real token has been generated.
            "SONARQUBE_TOKEN": os.getenv("SONARQUBE_TOKEN") or "bootstrap-pending",
        }
        result = E2EDockerDeployer.compose(
            files,
            "e2e-flowgent-sonarqube",
            "up",
            "-d",
            "sonarqube-postgres",
            "sonarqube",
            "sonarqube-init",
            environment=environment,
            timeout=timeout,
        )
        if not result.passed or not SonarQubeRuntime.wait_for_sonarqube(timeout):
            return False
        token = SonarQubeRuntime._ensure_api_token()
        return E2EDockerDeployer.compose(
            files,
            "e2e-flowgent-sonarqube",
            "up",
            "-d",
            "--force-recreate",
            "sonarqube-mcp",
            environment={**environment, "SONARQUBE_TOKEN": token},
            timeout=timeout,
        ).passed

    @staticmethod
    def wait_for_sonarqube(timeout=DEFAULT_TIMEOUT):
        print(f"\n-- Waiting for SonarQube API (timeout={timeout}s) --")
        sonarqube_url = SonarQubeRuntime.url()
        api_status = f"{sonarqube_url}/api/system/status"
        api_health = f"{sonarqube_url}/api/system/health"
        deadline = time.time() + timeout

        while time.time() < deadline:
            try:
                resp = requests.get(api_status, timeout=10)
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
                            health_resp = requests.get(api_health, timeout=10)
                            if health_resp.status_code == 200:
                                health = health_resp.json()
                                print(
                                    f"  SonarQube health: {health.get('health', 'UNKNOWN')}"
                                )
                            else:
                                print(
                                    "  SonarQube health details unavailable "
                                    f"(status={health_resp.status_code})"
                                )
                        except requests.RequestException as exc:
                            print(f"  SonarQube health details unavailable: {exc}")
                        print(f"  SonarQube ready at {sonarqube_url}")
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
                f"{SonarQubeRuntime.url()}/api/authentication/validate",
                auth=("admin", SONARQUBE_PASSWORD),
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
        return False

    @staticmethod
    def _mcp_payload(response: requests.Response) -> dict:
        payloads = []
        for line in response.text.splitlines():
            if line.startswith("data:"):
                payloads.append(json.loads(line[5:].strip()))
        if payloads:
            return payloads[-1]
        payload = response.json()
        if not isinstance(payload, dict):
            raise ValueError("MCP response is not a JSON object")
        return payload

    @staticmethod
    def wait_for_mcp(timeout=DEFAULT_TIMEOUT):
        """Require a real MCP handshake and the SonarQube issue-search tool."""
        print(f"\n-- Waiting for SonarQube MCP bridge (timeout={timeout}s) --")
        mcp_url = SonarQubeRuntime.mcp_url()
        deadline = time.time() + timeout
        last_error = "not attempted"
        while time.time() < deadline:
            try:
                headers = {
                    "Accept": "application/json, text/event-stream",
                    "Content-Type": "application/json",
                }
                initialized = requests.post(
                    mcp_url,
                    headers=headers,
                    json={
                        "jsonrpc": "2.0",
                        "id": 1,
                        "method": "initialize",
                        "params": {
                            "protocolVersion": "2025-03-26",
                            "capabilities": {},
                            "clientInfo": {
                                "name": "flowgent-e2e-readiness",
                                "version": "1",
                            },
                        },
                    },
                    timeout=10,
                )
                initialized.raise_for_status()
                init_payload = SonarQubeRuntime._mcp_payload(initialized)
                if init_payload.get("error"):
                    raise RuntimeError(f"MCP initialize error: {init_payload['error']}")
                session_id = initialized.headers.get("mcp-session-id")
                if session_id:
                    headers["Mcp-Session-Id"] = session_id
                notification = requests.post(
                    mcp_url,
                    headers=headers,
                    json={
                        "jsonrpc": "2.0",
                        "method": "notifications/initialized",
                        "params": {},
                    },
                    timeout=10,
                )
                notification.raise_for_status()
                listed = requests.post(
                    mcp_url,
                    headers=headers,
                    json={
                        "jsonrpc": "2.0",
                        "id": 2,
                        "method": "tools/list",
                        "params": {},
                    },
                    timeout=20,
                )
                listed.raise_for_status()
                list_payload = SonarQubeRuntime._mcp_payload(listed)
                tools = list_payload.get("result", {}).get("tools", [])
                names = {tool.get("name") for tool in tools if isinstance(tool, dict)}
                if REQUIRED_MCP_TOOL not in names:
                    raise RuntimeError(
                        f"required tool {REQUIRED_MCP_TOOL!r} missing from {len(names)} tools"
                    )
                print(
                    f"  SonarQube MCP ready at {mcp_url}: "
                    f"{len(names)} tools, {REQUIRED_MCP_TOOL}=present"
                )
                return True
            except (requests.RequestException, ValueError, RuntimeError) as exc:
                last_error = str(exc)
                time.sleep(5)
        print(f"  ERROR: SonarQube MCP not ready: {last_error}")
        return False


class SonarQubeDeployer(BaseDeployer):
    """Own the SonarQube fixture consumed by the security-fixer graph."""

    component = "sonarqube"

    def deploy(self) -> None:
        if self.context.deployer == "k8s":
            if not self.step(
                "reconcile isolated SonarQube Kubernetes stack",
                lambda: SonarQubeRuntime.kubernetes_up(self.context.timeout_seconds),
            ):
                raise RuntimeError("SonarQube Kubernetes deployment failed")
            return
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
        if not self.step(
            "verify SonarQube credentials", SonarQubeRuntime.verify_credentials
        ):
            raise RuntimeError("SonarQube credential verification failed")
        if not self.step(
            "verify SonarQube MCP handshake and tools",
            lambda: SonarQubeRuntime.wait_for_mcp(self.context.timeout_seconds),
        ):
            raise RuntimeError("SonarQube MCP bridge did not become ready")
