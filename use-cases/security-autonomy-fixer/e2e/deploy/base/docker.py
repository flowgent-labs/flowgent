"""Docker Compose implementation of the complete Flowgent E2E topology."""

from __future__ import annotations

import base64
import json
import os
from pathlib import Path
import secrets
import socket
import time
from urllib import error, parse, request

import yaml

from deploy import authguard
from deploy.sonarqube import SonarQubeDeployer

from common import config
from deploy import BaseDeployer
from common.model import CommandResult, RunContext
from common.process import CommandRunner


COMPOSE_FILE = config.E2E_DIR / "deploy" / "docker" / "compose.yml"
PROJECT_NAME = "e2e-flowgent-docker"
STATE_DIR = config.REPORTS_DIR / ".state" / "docker"
DOCKER_EXTERNAL_API_PORT = 29998
DOCKER_MOCK_GITHUB_PORT = 18087
DOCKER_WEB_PORT = 21080
DOCKER_NOTIFICATION_PORT = 21081
ROOTFUL_PODMAN_SOCKET = Path("/run/podman/podman.sock")


class DirectEndpoints:
    """Compose equivalent of Kubernetes port-forwards using fixed isolated ports."""

    def __init__(self, deployer: "DockerDeployer") -> None:
        self.deployer = deployer

    def start(self) -> "DirectEndpoints":
        self.ensure()
        print("  Docker verification endpoints ready: API, A2A, EMQX, Jaeger")
        return self

    def ensure(self) -> None:
        self.deployer._probe_endpoints()

    def healthy(self) -> bool:
        try:
            self.deployer._probe_endpoints()
        except Exception:
            return False
        return True

    def stop(self) -> None:
        return None


class DockerDeployer(BaseDeployer):
    """Deploy the same security-autonomy use case with Docker Compose."""

    backend = "docker"

    def __init__(self, context: RunContext) -> None:
        super().__init__(context)
        self.sonarqube = SonarQubeDeployer(context)
        self.environment = self._compose_environment()
        self._write_runtime_files()
        self.configure_client_environment()

    def _compose(self, *arguments: str, timeout_seconds: int | None = None) -> CommandResult:
        result = CommandRunner.run(
            (
                "docker",
                "compose",
                "-p",
                PROJECT_NAME,
                "-f",
                str(COMPOSE_FILE),
                *arguments,
            ),
            cwd=config.PROJECT_ROOT,
            environment=self.environment,
            timeout_seconds=timeout_seconds or self.context.timeout_seconds,
        )
        self.commands.append(result)
        if not result.passed:
            raise RuntimeError(
                f"Docker Compose command failed ({result.return_code}): {' '.join(arguments)}"
            )
        return result

    def deploy_sonarqube(self) -> None:
        self.sonarqube.deploy()
        self.sonarqube.verify()

    def reset_database(self) -> None:
        self._compose("down", "--volumes", "--remove-orphans", timeout_seconds=180)

    def build_images(self) -> None:
        build_environment = {
            "GOFLAGS": "-p=1",
            "GOMAXPROCS": "1",
            "GOGC": "50",
            "HTTPS_PROXY": "http://127.0.0.1:8800",
            "IN_CN_GFW": "true",
        }
        for target in ("build:core", "build:image:core", "build:image:web"):
            result = CommandRunner.run(
                ("make", "-C", str(config.PROJECT_ROOT), target),
                cwd=config.PROJECT_ROOT,
                environment=build_environment,
                timeout_seconds=1200,
            )
            self.commands.append(result)
            if not result.passed:
                raise RuntimeError(f"image build failed: {target}")
        self._prepare_authguard_images()

    def deploy(self) -> None:
        self._write_runtime_files()
        self._prepare_authguard_images()
        self._compose("up", "-d", "--remove-orphans", timeout_seconds=600)

    def verify(self) -> None:
        status = self._compose("ps", "-a", timeout_seconds=30)
        if "Exit " in status.output:
            raise RuntimeError("one or more Docker E2E services exited during startup")
        self._wait_http(config.LOCAL_AUTHN_PORT, "/readyz", timeout_seconds=180)
        self._wait_http(config.LOCAL_AUTHZ_MGMT_PORT, "/readyz", timeout_seconds=180)
        self._probe_endpoints()

    def import_config(self) -> None:
        self._compose(
            "exec",
            "-T",
            "flowgent",
            "/app/flowgent",
            "--config",
            "/etc/flowgent/e2e.yaml",
            "console",
            "import",
            "/e2e/config",
            timeout_seconds=90,
        )
        # The all-in-one runtime loads LLM/MCP registries during startup.  A
        # restart after import gives Compose the same ordering as Kubernetes,
        # where fresh JobManager Pods start after metadata is persisted.
        self._compose("restart", "flowgent", timeout_seconds=90)
        self._wait_http(config.LOCAL_API_PORT, "/_/healthz", timeout_seconds=90)
        self._ensure_notification_channel()

    def cleanup(self) -> None:
        self._compose("down", "--volumes", "--remove-orphans", timeout_seconds=180)

    def tunnels(self) -> DirectEndpoints:
        return DirectEndpoints(self)

    def configure_client_environment(self) -> None:
        config.K8S_APISERVER_URL = f"http://127.0.0.1:{config.LOCAL_API_PORT}"
        config.K8S_A2A_URL = f"http://127.0.0.1:{config.LOCAL_A2A_PORT}"
        config.JAEGER_UI_URL = f"http://127.0.0.1:{config.LOCAL_JAEGER_PORT}"
        config.EMQX_HOST = "127.0.0.1"
        config.EMQX_PORT = config.LOCAL_MQTT_PORT
        config.PG_HOST = "127.0.0.1"
        config.PG_PORT = config.LOCAL_PG_PORT
        os.environ["FLOWGENT_E2E_DEPLOYER"] = "docker"
        os.environ["FLOWGENT_E2E_PROXY_ALLOWLIST_ENTRY"] = "http://host.docker.internal:8800"
        os.environ["AUTHGUARD__AUTHZ__SCOPE_DELIVERY__DIRECT_CONTEXT_HMAC_KEY"] = self.environment[
            "AUTHGUARD_ACCESS_CONTEXT_HMAC_KEY"
        ]
        os.environ["FLOWGENT__STORAGE__TYPE"] = "POSTGRE"
        os.environ["FLOWGENT__STORAGE__POSTGRES__DSN"] = config.E2EConfiguration.postgres_dsn()
        os.environ["FLOWGENT__STORAGE__POSTGRES__SCHEMA"] = config.PG_SCHEMA
        os.environ["FLOWGENT_E2E_NOTIFICATION_TOKEN"] = self.environment[
            "FLOWGENT_E2E_NOTIFICATION_TOKEN"
        ]
        os.environ["FLOWGENT_E2E_NOTIFICATION_URL"] = "http://notification-receiver:8080"

    def runtime_exec(self, command: tuple[str, ...]) -> CommandResult:
        return self._compose("exec", "-T", "flowgent", *command)

    def verify_authguard(self, *, resource_scope: bool = True) -> None:
        self._wait_http(config.LOCAL_AUTHZ_MGMT_PORT, "/readyz")
        token, principal_id = self._social_login()
        api_token = self.environment["AUTHGUARD_API_TOKEN"]
        materializations = (
            ("global-platform-security", "principal-global-platform-security"),
            ("security-automation-owners", "principal-security-automation-owners"),
            ("emea-appsec", "principal-emea-appsec"),
            ("global-model-risk", "principal-global-model-risk"),
            ("global-regulatory-audit", "principal-global-regulatory-audit"),
        )
        for text_value, materialized_id in materializations:
            status, _, body = authguard.AuthGuardRuntime._api(
                config.LOCAL_AUTHZ_MGMT_PORT,
                "/api/v1/principal-discovery/search",
                api_token,
                "POST",
                {
                    "text": text_value,
                    "kinds": ["GROUP"],
                    "provider_ids": ["e2e-flowgent-corporate-ldap"],
                    "per_provider_limit": 20,
                    "cursors": {},
                },
            )
            candidates = body.get("principals", [])
            if status != 200 or len(candidates) != 1:
                raise RuntimeError(
                    f"Docker LDAP discovery failed for {text_value}: HTTP {status}"
                )
            status, _, _ = authguard.AuthGuardRuntime._api(
                config.LOCAL_AUTHZ_MGMT_PORT,
                "/api/v1/principal-discovery/materialize",
                api_token,
                "POST",
                {"principal_id": materialized_id, "reference": candidates[0]["reference"]},
            )
            if status not in (200, 201, 409):
                raise RuntimeError(f"Docker LDAP materialization failed: HTTP {status}")
        status, _, current = authguard.AuthGuardRuntime._api(
            config.LOCAL_AUTHZ_MGMT_PORT, "/api/v1/policy", api_token
        )
        if status != 200:
            raise RuntimeError(f"Docker AuthGuard policy read failed: HTTP {status}")
        revision = int(current.get("revision", 1))
        status, _, persisted = authguard.AuthGuardRuntime._api(
            config.LOCAL_AUTHZ_MGMT_PORT,
            "/api/v1/policy",
            api_token,
            "PUT",
            authguard.AuthGuardRuntime._policy(revision, principal_id),
            {"If-Match": str(revision)},
        )
        if status not in (200, 409):
            raise RuntimeError(f"Docker AuthGuard policy update failed: HTTP {status}")
        if status == 200 and int(persisted.get("revision", 0)) <= revision:
            raise RuntimeError("Docker AuthGuard policy revision did not advance")
        self._verify_gateway(token, resource_scope=resource_scope)

    def _verify_gateway(self, token: str, *, resource_scope: bool) -> None:
        gateway = config.LOCAL_GATEWAY_PORT
        status, _, _ = authguard.AuthGuardRuntime._api(
            gateway, "/auth/login", "", headers={"Host": authguard.APPLICATION_HOST}
        )
        if status != 200:
            raise RuntimeError(f"Docker Hosted Login route failed: HTTP {status}")
        status, _, flows = authguard.AuthGuardRuntime._api(
            gateway,
            f"/api/v1/{config.NAMESPACE_ID}/flows",
            token,
            headers={"Host": authguard.APPLICATION_HOST},
        )
        if status != 200:
            raise RuntimeError(f"Docker gateway authorization failed: HTTP {status}")
        if resource_scope:
            items = flows if isinstance(flows, list) else flows.get("items", [])
            if any(item.get("flow_id") == "security-autonomy-fixer" for item in items):
                raise RuntimeError("Docker row-level AuthGuard DENY was not applied")
            status, _, _ = authguard.AuthGuardRuntime._api(
                gateway,
                f"/api/v1/{config.NAMESPACE_ID}/flows/security-autonomy-fixer",
                token,
                headers={"Host": authguard.APPLICATION_HOST},
            )
            if status != 403:
                raise RuntimeError(f"Docker exact-resource DENY returned HTTP {status}")
        status, _, _ = authguard.AuthGuardRuntime._api(
            DOCKER_EXTERNAL_API_PORT,
            f"/api/v1/{config.NAMESPACE_ID}/flows",
            "",
        )
        if status != 401:
            raise RuntimeError(f"Docker direct external API did not fail closed: HTTP {status}")

    def _social_login(self) -> tuple[str, str]:
        no_redirect = request.build_opener(
            type(
                "NoRedirect",
                (request.HTTPRedirectHandler,),
                {"redirect_request": lambda *_: None},
            )()
        )

        def redirect_location(url: str, host: str | None = None) -> str:
            try:
                no_redirect.open(
                    request.Request(url, headers={"Host": host} if host else {}),
                    timeout=15,
                )
            except error.HTTPError as response:
                if response.code in (302, 303, 307, 308):
                    return response.headers["Location"]
                raise
            raise RuntimeError(f"expected OAuth redirect from {url}")

        first = redirect_location(
            f"http://127.0.0.1:{config.LOCAL_AUTHN_PORT}/auth/oauth2/github/authorize"
            f"?return_uri=%2Fapi%2Fv1%2F{config.NAMESPACE_ID}%2Fflows",
            authguard.APPLICATION_HOST,
        )
        provider = parse.urlsplit(first)
        callback = parse.urlsplit(
            redirect_location(
                f"http://127.0.0.1:{DOCKER_MOCK_GITHUB_PORT}{provider.path}?{provider.query}"
            )
        )
        with request.urlopen(
            request.Request(
                f"http://127.0.0.1:{config.LOCAL_AUTHN_PORT}{callback.path}?{callback.query}",
                headers={"Host": authguard.APPLICATION_HOST},
            ),
            timeout=20,
        ) as response:
            login = json.loads(response.read())
        token = login.get("accessToken", "")
        principal_id = login.get("principal", {}).get("principalId", "")
        if not token or not principal_id:
            raise RuntimeError("Docker GitHub AuthN returned no canonical session")
        return token, principal_id

    def _write_runtime_files(self) -> None:
        STATE_DIR.mkdir(parents=True, exist_ok=True)
        (STATE_DIR / "init.sql").write_text(
            'CREATE SCHEMA IF NOT EXISTS "e2e_flowgent" AUTHORIZATION test;\n'
            'CREATE SCHEMA IF NOT EXISTS "e2e_flowgent_authguard" AUTHORIZATION test;\n',
            encoding="utf-8",
        )
        (STATE_DIR / "ldap-config.cfg").write_text(
            authguard.AuthGuardRuntime._ldap_config(self.environment["AUTHGUARD_LDAP_BIND_PASSWORD"]),
            encoding="utf-8",
        )
        authguard_document = yaml.safe_load(
            authguard.AuthGuardRuntime.runtime_config(
                "",
                "postgres",
                "5432",
                "test",
                mock_url="http://mock-github:8080",
                ldap_url="ldap://ldap:389",
                redis_url="redis://authguard-redis-0:6379",
            )
        )
        authguard_document["authn"]["providers"]["github"]["clientSecret"] = authguard.GITHUB_CLIENT_SECRET
        authguard_document["authn"]["token"]["privateKey"] = self.environment["AUTHGUARD_SESSION_PRIVATE_KEY"]
        authguard_document["authn"]["applications"] = {
            "flowgent": {
                "hosts": [authguard.APPLICATION_HOST, "localhost"],
                "displayName": authguard.APPLICATION_DISPLAY_NAME,
                "returnUris": ["https://authn.flowgent.local/**", "https://localhost/**"],
            }
        }
        authguard_document["authz"]["scope_delivery"]["direct_context_hmac_key"] = self.environment["AUTHGUARD_ACCESS_CONTEXT_HMAC_KEY"]
        authguard_document["authz"]["api_token"] = self.environment["AUTHGUARD_API_TOKEN"]
        authguard_document["authz"]["principal_discovery"]["ldap"][0]["auth"]["bind_password"] = self.environment["AUTHGUARD_LDAP_BIND_PASSWORD"]
        authguard_document["storage"]["postgres"]["password"] = "test"
        authguard_document["cache"]["redis"]["password"] = self.environment[
            "AUTHGUARD_REDIS_PASSWORD"
        ]
        (STATE_DIR / "authguard.yaml").write_text(
            yaml.safe_dump(authguard_document, sort_keys=False), encoding="utf-8"
        )

        flowgent_document = yaml.safe_load(config.CONSOLE_CFG.read_text(encoding="utf-8"))
        flowgent_document["authguard_adapter"].update(
            {"enabled": True, "partition": "prod", "service": "flowgent", "region": "global", "tenant": "example-corp"}
        )
        flowgent_document["storage"]["type"] = "POSTGRE"
        flowgent_document["storage"]["postgres"].update(
            {
                "dsn": "postgres://test:test@postgres:5432/flowgent?sslmode=disable&options=-csearch_path%3De2e_flowgent",
                "host": "postgres",
                "port": 5432,
                "database": "flowgent",
                "schema": "e2e_flowgent",
                "username": "test",
                "password": "test",
            }
        )
        flowgent_document["messager"] = {
            "type": "mqtt",
            "mqtt": {"broker": "tcp://emqx:1883", "client_id": "e2e-flowgent-docker"},
        }
        flowgent_document["mgmt"]["otel"].update(
            {"endpoint": "http://jaeger:4318", "query_endpoint": "http://jaeger:16686"}
        )
        flowgent_document["runtime"].update(
            {
                "resource_owner": "e2e-flowgent-docker",
                "api_server_url": "http://127.0.0.1:9990",
                "system_namespace": "e2e-flowgent-docker",
                "k8s_namespace": "e2e-flowgent-docker",
            }
        )
        flowgent_document["runtime"]["namespace"] = {
            "default_namespace": config.NAMESPACE_ID,
            "namespace_prefix": "e2e-flowgent-docker-",
        }
        flowgent_document["sandbox"]["workspace"] = "/var/flowgent"
        flowgent_document["sandbox"]["deployment"]["enabled"] = False
        (STATE_DIR / "flowgent.yaml").write_text(
            yaml.safe_dump(flowgent_document, sort_keys=False), encoding="utf-8"
        )

    def _compose_environment(self) -> dict[str, str]:
        STATE_DIR.mkdir(parents=True, exist_ok=True)
        secret_file = STATE_DIR / "secrets.json"
        compose_environment = dict(os.environ)
        # Keep the use-case fixtures backend-neutral: local developer shells
        # commonly expose GH_TOKEN while the MCP secret reference uses the
        # canonical GITHUB_TOKEN name.  Never persist either credential.
        if not compose_environment.get("GITHUB_TOKEN"):
            compose_environment["GITHUB_TOKEN"] = compose_environment.get("GH_TOKEN", "")
        if not compose_environment.get("DEEPSEEK_API_KEY_FLOWGENT"):
            compose_environment["DEEPSEEK_API_KEY_FLOWGENT"] = compose_environment.get(
                "DEEPSEEK_API_KEY", ""
            )
        if "DOCKER_HOST" not in compose_environment and ROOTFUL_PODMAN_SOCKET.exists():
            # `/usr/bin/docker` is a podman-remote wrapper on the E2E host.  Pin
            # Compose to that same API so it sees the images built/tagged by
            # build_images() instead of attempting to pull localhost/*.
            compose_environment["DOCKER_HOST"] = f"unix://{ROOTFUL_PODMAN_SOCKET}"
        if secret_file.is_file():
            persisted = json.loads(secret_file.read_text(encoding="utf-8"))
            if "FLOWGENT_E2E_NOTIFICATION_TOKEN" not in persisted:
                persisted["FLOWGENT_E2E_NOTIFICATION_TOKEN"] = secrets.token_urlsafe(32)
                secret_file.write_text(
                    json.dumps(persisted, indent=2) + "\n", encoding="utf-8"
                )
                secret_file.chmod(0o600)
            return {**compose_environment, **persisted}
        private_key, _ = authguard.AuthGuardRuntime._session_key_and_jwks()
        persisted = {
            "AUTHGUARD_REDIS_PASSWORD": secrets.token_urlsafe(32),
            "AUTHGUARD_LDAP_BIND_PASSWORD": secrets.token_urlsafe(32),
            "AUTHGUARD_ACCESS_CONTEXT_HMAC_KEY": secrets.token_urlsafe(48),
            "AUTHGUARD_API_TOKEN": secrets.token_urlsafe(48),
            "AUTHGUARD_SESSION_PRIVATE_KEY": private_key,
            "FLOWGENT_NOTIFICATION_KEY_V1": base64.b64encode(os.urandom(32)).decode(),
            "FLOWGENT_E2E_NOTIFICATION_TOKEN": secrets.token_urlsafe(32),
            "HTTPS_PROXY": os.getenv("HTTPS_PROXY", "http://127.0.0.1:8800"),
        }
        secret_file.write_text(json.dumps(persisted, indent=2) + "\n", encoding="utf-8")
        secret_file.chmod(0o600)
        return {**compose_environment, **persisted}

    def _prepare_authguard_images(self) -> None:
        from .kubernetes import (
            AUTHGUARD_IMAGE,
            AUTHGUARD_SOURCE_IMAGE,
            AUTHGUARD_WEB_IMAGE,
            AUTHGUARD_WEB_SOURCE_IMAGE,
        )

        for source, target in (
            (AUTHGUARD_SOURCE_IMAGE, AUTHGUARD_IMAGE),
            (AUTHGUARD_WEB_SOURCE_IMAGE, AUTHGUARD_WEB_IMAGE),
        ):
            inspect = CommandRunner.run(("docker", "image", "inspect", source), stream=False)
            if not inspect.passed:
                pull = CommandRunner.run(
                    ("docker", "pull", source),
                    environment={"HTTPS_PROXY": "http://127.0.0.1:8800"},
                    timeout_seconds=600,
                )
                if not pull.passed:
                    raise RuntimeError(f"failed to pull pinned image: {source}")
            tag = CommandRunner.run(("docker", "tag", source, target), timeout_seconds=30)
            if not tag.passed:
                raise RuntimeError(f"failed to tag Docker E2E image: {target}")

    def _ensure_notification_channel(self) -> None:
        endpoint = (
            f"http://127.0.0.1:{config.LOCAL_API_PORT}/api/v1/"
            f"{config.NAMESPACE_ID}/notifications/channels"
        )
        with request.urlopen(endpoint, timeout=10) as response:
            channels = json.loads(response.read()).get("items", [])
        for channel in channels:
            if channel.get("name") != "security-autonomy-alerts" or not channel.get("id"):
                continue
            delete = request.Request(
                f"{endpoint}/{channel['id']}", method="DELETE"
            )
            with request.urlopen(delete, timeout=10):
                pass
        body = json.dumps(
            {
                "name": "security-autonomy-alerts",
                "provider": "webhook",
                "enabled": True,
                "config": {
                    "url": "http://notification-receiver:8080",
                    "headers": {
                        "Authorization": (
                            "Bearer " + self.environment["FLOWGENT_E2E_NOTIFICATION_TOKEN"]
                        )
                    },
                },
            }
        ).encode()
        create = request.Request(
            endpoint,
            data=body,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with request.urlopen(create, timeout=10) as response:
            if response.status not in (200, 201):
                raise RuntimeError(
                    f"Docker notification channel returned HTTP {response.status}"
                )

    def _probe_endpoints(self) -> None:
        for port in (config.LOCAL_MQTT_PORT, config.LOCAL_API_PORT, config.LOCAL_A2A_PORT):
            with socket.create_connection(("127.0.0.1", port), timeout=3):
                pass
        for port, path in (
            (config.LOCAL_API_PORT, "/_/healthz"),
            (config.LOCAL_A2A_PORT, "/_/healthz"),
            (config.LOCAL_JAEGER_PORT, "/api/services"),
            (DOCKER_WEB_PORT, "/healthz"),
            (DOCKER_NOTIFICATION_PORT, "/healthz"),
        ):
            self._wait_http(port, path, timeout_seconds=180)

    @staticmethod
    def _wait_http(port: int, path: str, timeout_seconds: int = 60) -> None:
        deadline = time.monotonic() + timeout_seconds
        last_error: Exception | None = None
        while time.monotonic() < deadline:
            try:
                with request.urlopen(f"http://127.0.0.1:{port}{path}", timeout=3) as response:
                    if response.status == 200:
                        return
            except Exception as failure:
                last_error = failure
            time.sleep(1)
        raise RuntimeError(f"HTTP endpoint did not become ready: {port}{path}: {last_error}")
