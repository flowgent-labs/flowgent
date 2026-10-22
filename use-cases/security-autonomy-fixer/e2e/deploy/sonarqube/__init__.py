#!/usr/bin/env python3
"""
Deploy Script S21 — SonarQube Middleware deployment and health check.

Reconciles the SonarQube, PostgreSQL, and SonarQube MCP compose stack without
deleting persistent volumes, then waits for the API to become healthy.

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

import hashlib
import json
import os
import subprocess
import time

import requests

from deploy import BaseDeployer
from common import config
from common.config import SONAR_COMPOSE
from common.model import RunContext
from common.process import CommandRunner

DEFAULT_TIMEOUT = 240
REQUIRED_MCP_TOOL = "GetIssuesSearch"
SONARQUBE_NAME = "sonarqube"
SONARQUBE_POSTGRES_NAME = f"{SONARQUBE_NAME}-postgres"
SONARQUBE_MCP_NAME = f"{SONARQUBE_NAME}-mcp"
SONARQUBE_IMAGE = (
    "registry.cn-shenzhen.aliyuncs.com/wl4g/"
    "sonarqube:26.4.0.121862-community-amd64"
)
SONARQUBE_POSTGRES_IMAGE = (
    "registry.cn-shenzhen.aliyuncs.com/wl4g/"
    "bitnami_postgresql:18.3-vmware-photon-5.0"
)
SONARQUBE_MCP_IMAGE = (
    "registry.cn-shenzhen.aliyuncs.com/wl4g/"
    "ghcr.io_flowgent-labs_sonarqube-mcp:v0.6.0"
)
SONARQUBE_PASSWORD = "Abcd1234@sonar"


class SonarQubeRuntime:
    """Class-owned operations for sonarqube."""

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
        result = subprocess.run(
            ["kubectl", "apply", "-f", "-"],
            input=json.dumps(manifest),
            capture_output=True,
            text=True,
            timeout=90,
        )
        if result.returncode != 0:
            raise RuntimeError(f"apply SonarQube resources failed: {result.stderr[-800:]}")

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
                "kubectl", "rollout", "status", resource,
                "-n", config.SONARQUBE_NAMESPACE, f"--timeout={timeout}s",
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
        SonarQubeRuntime._apply({
            "apiVersion": "v1",
            "kind": "Namespace",
            "metadata": {
                "name": namespace,
                "labels": {"flowgent.io/e2e-owner": config.RESOURCE_PREFIX},
            },
        })
        SonarQubeRuntime._apply({
            "apiVersion": "v1",
            "kind": "List",
            "items": [
                {
                    "apiVersion": "v1",
                    "kind": "Secret",
                    "metadata": {"name": SONARQUBE_NAME, "namespace": namespace, "labels": sonar_labels},
                    "type": "Opaque",
                    "stringData": {
                        "database-username": "sonarqube",
                        "database-password": "sonarqube",
                        "database-name": "sonarqube",
                    },
                },
                {
                    "apiVersion": "v1",
                    "kind": "Service",
                    "metadata": {"name": SONARQUBE_POSTGRES_NAME, "namespace": namespace, "labels": postgres_labels},
                    "spec": {
                        "selector": {"app.kubernetes.io/component": "postgresql", "app.kubernetes.io/instance": SONARQUBE_NAME},
                        "ports": [{"name": "postgres", "port": 5432, "targetPort": "postgres"}],
                    },
                },
                {
                    "apiVersion": "apps/v1",
                    "kind": "StatefulSet",
                    "metadata": {"name": SONARQUBE_POSTGRES_NAME, "namespace": namespace, "labels": postgres_labels},
                    "spec": {
                        "serviceName": SONARQUBE_POSTGRES_NAME,
                        "replicas": 1,
                        "selector": {"matchLabels": {"app.kubernetes.io/component": "postgresql", "app.kubernetes.io/instance": SONARQUBE_NAME}},
                        "template": {
                            "metadata": {"labels": postgres_labels},
                            "spec": {"containers": [{
                                "name": "postgresql",
                                "image": SONARQUBE_POSTGRES_IMAGE,
                                "imagePullPolicy": "IfNotPresent",
                                "env": [
                                    {"name": "POSTGRESQL_USERNAME", "valueFrom": {"secretKeyRef": {"name": SONARQUBE_NAME, "key": "database-username"}}},
                                    {"name": "POSTGRESQL_PASSWORD", "valueFrom": {"secretKeyRef": {"name": SONARQUBE_NAME, "key": "database-password"}}},
                                    {"name": "POSTGRESQL_DATABASE", "valueFrom": {"secretKeyRef": {"name": SONARQUBE_NAME, "key": "database-name"}}},
                                ],
                                "ports": [{"name": "postgres", "containerPort": 5432}],
                                "readinessProbe": {"exec": {"command": ["sh", "-c", "pg_isready -U $POSTGRESQL_USERNAME -d $POSTGRESQL_DATABASE"]}, "periodSeconds": 5},
                                "resources": {"requests": {"cpu": "50m", "memory": "256Mi"}, "limits": {"cpu": "750m", "memory": "1Gi"}},
                                "volumeMounts": [{"name": "data", "mountPath": "/bitnami/postgresql"}],
                            }]},
                        },
                        "volumeClaimTemplates": [{
                            "metadata": {"name": "data", "labels": postgres_labels},
                            "spec": {"accessModes": ["ReadWriteOnce"], "resources": {"requests": {"storage": "4Gi"}}},
                        }],
                    },
                },
                *[
                    {
                        "apiVersion": "v1",
                        "kind": "PersistentVolumeClaim",
                        "metadata": {"name": f"{SONARQUBE_NAME}-{volume}", "namespace": namespace, "labels": sonar_labels},
                        "spec": {"accessModes": ["ReadWriteOnce"], "resources": {"requests": {"storage": size}}},
                    }
                    for volume, size in (("data", "4Gi"), ("extensions", "1Gi"), ("logs", "1Gi"))
                ],
                {
                    "apiVersion": "v1",
                    "kind": "Service",
                    "metadata": {"name": SONARQUBE_NAME, "namespace": namespace, "labels": sonar_labels},
                    "spec": {
                        "type": "NodePort",
                        "selector": {"app.kubernetes.io/component": "server", "app.kubernetes.io/instance": SONARQUBE_NAME},
                        "ports": [{"name": "http", "port": 9000, "targetPort": "http", "nodePort": config.SONARQUBE_NODE_PORT}],
                    },
                },
                {
                    "apiVersion": "apps/v1",
                    "kind": "Deployment",
                    "metadata": {"name": SONARQUBE_NAME, "namespace": namespace, "labels": sonar_labels},
                    "spec": {
                        "replicas": 1,
                        "strategy": {"type": "Recreate"},
                        "selector": {"matchLabels": {"app.kubernetes.io/component": "server", "app.kubernetes.io/instance": SONARQUBE_NAME}},
                        "template": {
                            "metadata": {"labels": sonar_labels},
                            "spec": {
                                "securityContext": {"fsGroup": 1000},
                                "containers": [{
                                    "name": "sonarqube",
                                    "image": SONARQUBE_IMAGE,
                                    "imagePullPolicy": "IfNotPresent",
                                    "env": [
                                        {"name": "SONAR_JDBC_URL", "value": f"jdbc:postgresql://{SONARQUBE_POSTGRES_NAME}:5432/sonarqube"},
                                        {"name": "SONAR_JDBC_USERNAME", "valueFrom": {"secretKeyRef": {"name": SONARQUBE_NAME, "key": "database-username"}}},
                                        {"name": "SONAR_JDBC_PASSWORD", "valueFrom": {"secretKeyRef": {"name": SONARQUBE_NAME, "key": "database-password"}}},
                                        {"name": "SONAR_WEB_PORT", "value": "9000"},
                                        {"name": "SONAR_ES_JAVA_OPTS", "value": "-Xms512m -Xmx512m -Dlog4j2.formatMsgNoLookups=true"},
                                        {"name": "SONAR_FORCE_AUTHENTICATION", "value": "false"},
                                        {"name": "SONAR_ES_BOOTSTRAP_CHECKS_DISABLE", "value": "true"},
                                    ],
                                    "ports": [{"name": "http", "containerPort": 9000}],
                                    "readinessProbe": {"httpGet": {"path": "/api/system/status", "port": "http"}, "initialDelaySeconds": 30, "periodSeconds": 10, "failureThreshold": 30},
                                    "resources": {"requests": {"cpu": "250m", "memory": "1536Mi"}, "limits": {"cpu": "1500m", "memory": "4Gi"}},
                                    "volumeMounts": [
                                        {"name": "data", "mountPath": "/opt/sonarqube/data"},
                                        {"name": "extensions", "mountPath": "/opt/sonarqube/extensions"},
                                        {"name": "logs", "mountPath": "/opt/sonarqube/logs"},
                                    ],
                                }],
                                "volumes": [
                                    {"name": volume, "persistentVolumeClaim": {"claimName": f"{SONARQUBE_NAME}-{volume}"}}
                                    for volume in ("data", "extensions", "logs")
                                ],
                            },
                        },
                    },
                },
            ],
        })
        SonarQubeRuntime._wait_rollout(
            f"statefulset/{SONARQUBE_POSTGRES_NAME}", min(timeout, 300)
        )
        SonarQubeRuntime._wait_rollout(
            f"deployment/{SONARQUBE_NAME}", max(timeout, 360)
        )
        if not SonarQubeRuntime.wait_for_sonarqube(max(timeout, 360)):
            return False
        token = SonarQubeRuntime._ensure_api_token()
        token_hash = hashlib.sha256(token.encode()).hexdigest()
        mcp_labels = SonarQubeRuntime._labels("mcp")
        SonarQubeRuntime._apply({
            "apiVersion": "v1",
            "kind": "List",
            "items": [
                {
                    "apiVersion": "v1",
                    "kind": "Secret",
                    "metadata": {"name": SONARQUBE_MCP_NAME, "namespace": namespace, "labels": mcp_labels},
                    "type": "Opaque",
                    "stringData": {"token": token},
                },
                {
                    "apiVersion": "v1",
                    "kind": "Service",
                    "metadata": {"name": SONARQUBE_MCP_NAME, "namespace": namespace, "labels": mcp_labels},
                    "spec": {
                        "type": "NodePort",
                        "selector": {"app.kubernetes.io/component": "mcp", "app.kubernetes.io/instance": SONARQUBE_NAME},
                        "ports": [{"name": "http", "port": 8080, "targetPort": "http", "nodePort": config.SONARQUBE_MCP_NODE_PORT}],
                    },
                },
                {
                    "apiVersion": "apps/v1",
                    "kind": "Deployment",
                    "metadata": {"name": SONARQUBE_MCP_NAME, "namespace": namespace, "labels": mcp_labels},
                    "spec": {
                        "replicas": 1,
                        "selector": {"matchLabels": {"app.kubernetes.io/component": "mcp", "app.kubernetes.io/instance": SONARQUBE_NAME}},
                        "template": {
                            "metadata": {
                                "labels": mcp_labels,
                                # Rotate the MCP pod whenever its referenced
                                # upstream token changes; Secret updates alone
                                # do not refresh container environment values.
                                "annotations": {
                                    "flowgent.io/sonarqube-token-hash": token_hash,
                                },
                            },
                            "spec": {"containers": [{
                                "name": "mcp",
                                "image": SONARQUBE_MCP_IMAGE,
                                "imagePullPolicy": "IfNotPresent",
                                "env": [
                                    {"name": "HOME", "value": "/tmp"},
                                    {"name": "MCP__UPSTREAM__DEFAULT__ENDPOINT", "value": f"http://{SONARQUBE_NAME}:9000"},
                                    {"name": "MCP__UPSTREAM__DEFAULT__AUTH__STATIC__WEB_TOKEN", "valueFrom": {"secretKeyRef": {"name": SONARQUBE_MCP_NAME, "key": "token"}}},
                                    {"name": "MCP__NATIVE_TOOLS__EXPOSE__REGISTER_ALL_TOOLS_BY_DEFAULT", "value": "true"},
                                ],
                                "ports": [{"name": "http", "containerPort": 8080}],
                                "readinessProbe": {"tcpSocket": {"port": "http"}, "initialDelaySeconds": 5, "periodSeconds": 5},
                                "resources": {"requests": {"cpu": "25m", "memory": "64Mi"}, "limits": {"cpu": "500m", "memory": "512Mi"}},
                            }]},
                        },
                    },
                },
            ],
        })
        SonarQubeRuntime._wait_rollout(f"deployment/{SONARQUBE_MCP_NAME}", min(timeout, 300))
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
        return generated.json()["token"]

    @staticmethod
    def compose_up(compose_file=None, timeout=DEFAULT_TIMEOUT):
        """
        Bring up the SonarQube + PostgreSQL stack via docker compose.

        Uses an idempotent ``up -d`` so verifier reruns preserve SonarQube data.
        The init container initializes credentials only when needed.
        """
        compose_file = compose_file or SONAR_COMPOSE
        sonarqube_url = SonarQubeRuntime.url()
        if sonarqube_url != "http://localhost:9000":
            print(f"  Using externally managed SonarQube: {sonarqube_url}")
            return True
        print(f"\n-- Starting SonarQube middleware --")
        print(f"  Compose file: {compose_file}")

        if not os.path.isfile(compose_file):
            print(f"  ERROR: Compose file not found: {compose_file}")
            return False

        rc, _ = CommandRunner.legacy(
            ["docker", "compose", "-f", compose_file, "up", "-d"],
            timeout=timeout,
        )
        if rc != 0:
            print(f"  ERROR: docker compose up failed")
            return False

        print(f"  Compose stack started (postgres + sonarqube + sonarqube-init + sonarqube-mcp).")
        return True

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
                                print(f"  SonarQube health: {health.get('health', 'UNKNOWN')}")
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
                f"{SonarQubeRuntime.url()}/api/authentication/validate",
                auth=("admin", SONARQUBE_PASSWORD),
                timeout=10,
            )
            if resp.status_code == 200 and resp.json().get("valid"):
                print(f"  Admin credentials: OK")
                return True
            print(f"  WARN: Default admin credentials not valid (status={resp.status_code})")
        except Exception as e:
            print(f"  WARN: Could not verify credentials: {e}")
        return True  # non-fatal

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
                    json={"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": {}},
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
    """Own the SonarQube fixture consumed by the security-fixer flow."""

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
        if not self.step("verify SonarQube credentials", SonarQubeRuntime.verify_credentials):
            raise RuntimeError("SonarQube credential verification failed")
        if not self.step(
            "verify SonarQube MCP handshake and tools",
            lambda: SonarQubeRuntime.wait_for_mcp(self.context.timeout_seconds),
        ):
            raise RuntimeError("SonarQube MCP bridge did not become ready")
