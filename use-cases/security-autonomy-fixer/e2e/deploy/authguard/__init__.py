"""Isolated AuthGuard + LDAP + GitHub OAuth integration for Flowgent E2E."""

from __future__ import annotations

import base64
import hashlib
import json
import os
import secrets
import socket
import subprocess
import tempfile
import time
from contextlib import contextmanager
from pathlib import Path
from urllib import error, parse, request

import yaml
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa

from common import config
from deploy import BaseDeployer
from deploy.base.kubernetes import AUTHGUARD_IMAGE, AUTHGUARD_WEB_IMAGE, KubernetesImageManager
from common.model import RunContext
from common.process import CommandRunner


LDAP_IMAGE = "registry.cn-shenzhen.aliyuncs.com/wl4g/glauth:v2.5.0"
AUTH_SECRET = f"{config.RESOURCE_PREFIX}-authguard-secrets"
LDAP_NAME = f"{config.RESOURCE_PREFIX}-ldap"
MOCK_GITHUB_NAME = f"{config.RESOURCE_PREFIX}-github-oauth"
GATEWAY_NAME = f"{config.RESOURCE_PREFIX}-gateway"
GATEWAY_CLASS = GATEWAY_NAME
GATEWAY_CONTROLLER = f"flowgent.authguard.io/{config.RESOURCE_PREFIX}-gateway-controller"
# Keep the LoadBalancer listener clear of both the adjacent AuthGuard E2E's
# port 80 and this use case's host-side SonarQube MCP bridge on port 18080.
GATEWAY_LISTENER_PORT = 28080
APPLICATION_HOST = "authn.flowgent.local"
AUTHN_ISSUER = f"urn:authguard:{config.RESOURCE_PREFIX}:authn"
AUDIENCE = "flowgent"
AUTHGUARD_DATABASE = "flowgent"
AUTHGUARD_SCHEMA = "e2e_flowgent_authguard"
AUTHGUARD_REDIS_NAME = f"{config.RESOURCE_PREFIX}-authguard-redis"
GITHUB_CLIENT_ID = "e2e-flowgent-github"
GITHUB_CLIENT_SECRET = "e2e-flowgent-github-secret"
LDAP_ISSUER = f"urn:authguard:{config.RESOURCE_PREFIX}:ldap:example-corp"
MOCK_GITHUB_SCRIPT = (
    Path(__file__).resolve().parents[1]
    / "mocksvc-github-service"
    / "app"
    / "server.py"
)


class AuthGuardRuntime:
    """Class-owned operations for authguard."""

    @staticmethod
    def _b64url(value: int) -> str:
        raw = value.to_bytes((value.bit_length() + 7) // 8, "big")
        return base64.urlsafe_b64encode(raw).decode().rstrip("=")

    @staticmethod
    def _session_key_and_jwks() -> tuple[str, str]:
        key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
        pem = key.private_bytes(
            serialization.Encoding.PEM,
            serialization.PrivateFormat.PKCS8,
            serialization.NoEncryption(),
        ).decode()
        public = key.public_key().public_numbers()
        jwks = {
            "keys": [{
                "kty": "RSA",
                "use": "sig",
                "alg": "RS256",
                "kid": f"{config.RESOURCE_PREFIX}-authn",
                "n": AuthGuardRuntime._b64url(public.n),
                "e": AuthGuardRuntime._b64url(public.e),
            }]
        }
        return pem, json.dumps(jwks, separators=(",", ":"))

    @staticmethod
    def _apply(manifest: dict) -> None:
        result = subprocess.run(
            ["kubectl", "apply", "-f", "-"],
            input=yaml.safe_dump(manifest, sort_keys=False),
            capture_output=True,
            text=True,
            timeout=60,
        )
        if result.returncode != 0:
            raise RuntimeError(f"kubectl apply failed: {result.stderr[:500]}")

    @staticmethod
    def _existing_secret_keys(namespace: str) -> set[str]:
        result = subprocess.run(
            ["kubectl", "get", "secret", AUTH_SECRET, "-n", namespace, "-o", "json"],
            capture_output=True,
            text=True,
            timeout=20,
        )
        if result.returncode != 0:
            return set()
        return set(json.loads(result.stdout).get("data", {}))

    @staticmethod
    def _ldap_config(bind_password: str) -> str:
        password_hash = hashlib.sha256(bind_password.encode()).hexdigest()
        users = (
            ("svc-authguard", "AuthGuard Directory Reader", 5001, 5500, "Platform Security"),
            ("platform-security-admin", "Global Platform Security Admin", 1001, 5501, "Global Security"),
            ("security-automation-owner", "Security Automation Owner", 1005, 5505, "Security Engineering"),
            ("emea-appsec-analyst", "EMEA Application Security Analyst", 1002, 5502, "EMEA AppSec"),
            ("model-risk-approver", "Global Model Risk Approver", 1003, 5503, "Model Risk"),
            ("regulatory-auditor", "Global Regulatory Auditor", 1004, 5504, "Internal Audit"),
        )
        blocks = [
            '[ldap]\n  enabled = true\n  listen = "0.0.0.0:389"',
            '[ldaps]\n  enabled = false',
            '[behaviors]\n  IgnoreCapabilities = true',
            '[backend]\n  datastore = "config"\n  baseDN = "dc=example,dc=org"\n  nameformat = "cn"\n  groupformat = "ou"',
        ]
        for name, display, uid, gid, department in users:
            blocks.append(
                "\n".join([
                    "[[users]]",
                    f'  name = "{name}"',
                    f'  givenname = "{display}"',
                    '  sn = "Example Corp"',
                    f'  mail = "{name}@example-corp.example"',
                    f"  uidnumber = {uid}",
                    f"  primarygroup = {gid}",
                    f'  passsha256 = "{password_hash}"',
                    f"  othergroups = [{gid}]",
                    "  [[users.customattributes]]",
                    f'    displayName = ["{display}"]',
                    f'    department = ["{department}"]',
                ])
            )
        groups = (
            ("Users", 5500, "Directory service accounts"),
            ("global-platform-security", 5501, "Global security platform administrators"),
            ("security-automation-owners", 5505, "Security autonomy workflow owners"),
            ("emea-appsec", 5502, "EMEA application security analysts"),
            ("global-model-risk", 5503, "Independent model risk approvers"),
            ("global-regulatory-audit", 5504, "Read-only regulatory auditors"),
        )
        for name, gid, description in groups:
            blocks.append(
                "\n".join([
                    "[[groups]]",
                    f'  name = "{name}"',
                    f"  gidnumber = {gid}",
                    f'  description = "{description}"',
                ])
            )
        return "\n\n".join(blocks) + "\n"

    @staticmethod
    def _ensure_secret(namespace: str) -> tuple[str, str]:
        required = {
            "AUTHGUARD_GITHUB_CLIENT_SECRET",
            "AUTHGUARD_LDAP_BIND_PASSWORD",
            "AUTHGUARD__STORAGE__POSTGRES__PASSWORD",
            "AUTHGUARD__CACHE__REDIS__PASSWORD",
            "AUTHGUARD__AUTHZ__API_TOKEN",
            "AUTHGUARD_ACCESS_CONTEXT_HMAC_KEY",
            "AUTHGUARD_AUTHN_SESSION_PRIVATE_KEY",
            "AUTHGUARD_AUTHN_SESSION_JWKS",
        }
        if required.issubset(AuthGuardRuntime._existing_secret_keys(namespace)):
            print(f"  Reusing isolated AuthGuard Secret: {namespace}/{AUTH_SECRET}")
            return AUTH_SECRET, ""
        private_key, jwks = AuthGuardRuntime._session_key_and_jwks()
        bind_password = secrets.token_urlsafe(36)
        access_context_hmac_key = secrets.token_urlsafe(48)
        data = {
            "AUTHGUARD_GITHUB_CLIENT_SECRET": GITHUB_CLIENT_SECRET,
            "AUTHGUARD_LDAP_BIND_PASSWORD": bind_password,
            "AUTHGUARD__STORAGE__POSTGRES__PASSWORD": os.getenv("FLOWGENT_PG_PASSWORD", "test"),
            "AUTHGUARD__CACHE__REDIS__PASSWORD": secrets.token_urlsafe(36),
            "AUTHGUARD__AUTHZ__API_TOKEN": secrets.token_urlsafe(48),
            "AUTHGUARD_ACCESS_CONTEXT_HMAC_KEY": access_context_hmac_key,
            "AUTHGUARD_AUTHN_SESSION_PRIVATE_KEY": private_key,
            "AUTHGUARD_AUTHN_SESSION_JWKS": jwks,
        }
        AuthGuardRuntime._apply({
            "apiVersion": "v1",
            "kind": "Secret",
            "metadata": {"name": AUTH_SECRET, "namespace": namespace},
            "type": "Opaque",
            "stringData": data,
        })
        print(f"  AuthGuard Secret ready: {namespace}/{AUTH_SECRET} (values redacted)")
        return AUTH_SECRET, bind_password

    @staticmethod
    def _secret_value(namespace: str, key: str) -> str:
        result = subprocess.run(
            ["kubectl", "get", "secret", AUTH_SECRET, "-n", namespace,
             "-o", f"jsonpath={{.data.{key}}}"],
            capture_output=True,
            text=True,
            timeout=20,
        )
        if result.returncode != 0 or not result.stdout:
            raise RuntimeError(f"missing {key} in {namespace}/{AUTH_SECRET}")
        return base64.b64decode(result.stdout).decode()

    @staticmethod
    def _ensure_support(namespace: str, bind_password: str) -> None:
        if not bind_password:
            bind_password = AuthGuardRuntime._secret_value(namespace, "AUTHGUARD_LDAP_BIND_PASSWORD")
        labels = {"app.kubernetes.io/part-of": config.RESOURCE_PREFIX}
        AuthGuardRuntime._apply({
            "apiVersion": "v1",
            "kind": "Secret",
            "metadata": {"name": f"{LDAP_NAME}-config", "namespace": namespace, "labels": labels},
            "type": "Opaque",
            "stringData": {"ldap-config.cfg": AuthGuardRuntime._ldap_config(bind_password)},
        })
        AuthGuardRuntime._apply({
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "metadata": {"name": f"{MOCK_GITHUB_NAME}-script", "namespace": namespace, "labels": labels},
            "data": {"server.py": MOCK_GITHUB_SCRIPT.read_text(encoding="utf-8")},
        })
        for name, image, port, volume in (
            (LDAP_NAME, LDAP_IMAGE, 389, "ldap"),
            (MOCK_GITHUB_NAME, "localhost/flowgent:latest", 8080, "github"),
        ):
            mounts = []
            volumes = []
            command = None
            if volume == "ldap":
                mounts = [{"name": "config", "mountPath": "/app/config/config.cfg", "subPath": "ldap-config.cfg", "readOnly": True}]
                volumes = [{"name": "config", "secret": {"secretName": f"{LDAP_NAME}-config"}}]
            else:
                command = ["python3", "/opt/e2e/server.py"]
                mounts = [{"name": "script", "mountPath": "/opt/e2e/server.py", "subPath": "server.py", "readOnly": True}]
                volumes = [{"name": "script", "configMap": {"name": f"{MOCK_GITHUB_NAME}-script"}}]
            container = {
                "name": name,
                "image": image,
                "imagePullPolicy": "IfNotPresent",
                "ports": [{"name": "service", "containerPort": port}],
                "volumeMounts": mounts,
                "readinessProbe": {"tcpSocket": {"port": "service"}, "periodSeconds": 3},
                "resources": {"requests": {"cpu": "10m", "memory": "24Mi"}, "limits": {"cpu": "150m", "memory": "96Mi"}},
            }
            if command:
                container["command"] = command
            AuthGuardRuntime._apply({
                "apiVersion": "apps/v1",
                "kind": "Deployment",
                "metadata": {"name": name, "namespace": namespace, "labels": labels},
                "spec": {
                    "replicas": 1,
                    "selector": {"matchLabels": {"app": name}},
                    "template": {"metadata": {"labels": {"app": name, **labels}}, "spec": {"containers": [container], "volumes": volumes}},
                },
            })
            AuthGuardRuntime._apply({
                "apiVersion": "v1",
                "kind": "Service",
                "metadata": {"name": name, "namespace": namespace, "labels": labels},
                "spec": {"selector": {"app": name}, "ports": [{"name": "service", "port": port, "targetPort": "service"}]},
            })
        # GLAUTH reads its mounted configuration only at process start, and the
        # mock provider script should follow the same deterministic lifecycle.
        # Restart only this use case's precisely named support Deployments after
        # their Secret/ConfigMap is updated, then wait before AuthGuard discovery.
        for name in (LDAP_NAME, MOCK_GITHUB_NAME):
            result = subprocess.run(
                ["kubectl", "rollout", "restart", f"deployment/{name}", "-n", namespace],
                capture_output=True,
                text=True,
                timeout=30,
            )
            if result.returncode != 0:
                raise RuntimeError(f"restart support deployment failed: {namespace}/{name}")
            result = subprocess.run(
                ["kubectl", "rollout", "status", f"deployment/{name}", "-n", namespace, "--timeout=120s"],
                capture_output=True,
                text=True,
                timeout=150,
            )
            if result.returncode != 0:
                raise RuntimeError(f"support deployment not ready: {namespace}/{name}")

    @staticmethod
    def _reset_authguard_schema(pg_container: str, pg_user: str, pg_password: str) -> None:
        sql = (
            f'DROP SCHEMA IF EXISTS "{AUTHGUARD_SCHEMA}" CASCADE; '
            f'CREATE SCHEMA "{AUTHGUARD_SCHEMA}" AUTHORIZATION "{pg_user}";'
        )
        result = subprocess.run(
            ["docker", "exec", "-e", f"PGPASSWORD={pg_password}", pg_container,
             "psql", "-v", "ON_ERROR_STOP=1", "-U", pg_user, "-d", AUTHGUARD_DATABASE, "-c", sql],
            capture_output=True,
            text=True,
            timeout=60,
        )
        if result.returncode != 0:
            raise RuntimeError(f"reset AuthGuard schema failed: {result.stderr[:400]}")
        print(f"  PostgreSQL schema reset: {AUTHGUARD_DATABASE}/{AUTHGUARD_SCHEMA}")

    @staticmethod
    def runtime_config(
        namespace: str,
        pg_host: str,
        pg_port: str,
        pg_user: str,
        *,
        mock_url: str | None = None,
        ldap_url: str | None = None,
        redis_url: str | None = None,
    ) -> str:
        mock_url = mock_url or f"http://{MOCK_GITHUB_NAME}.{namespace}.svc.cluster.local:8080"
        ldap_url = ldap_url or f"ldap://{LDAP_NAME}.{namespace}.svc.cluster.local:389"
        redis_url = redis_url or f"redis://{AUTHGUARD_REDIS_NAME}.{namespace}.svc.cluster.local:6379"
        postgres_url = (
            f"postgresql://{pg_host}:{pg_port}/{AUTHGUARD_DATABASE}?sslmode=disable"
            f"&options=-csearch_path%3D{AUTHGUARD_SCHEMA}"
        )
        document = {
            "authn": {
                "providers": {
                    "github": {
                        "type": "oauth2",
                        "issuer": "https://github.com",
                        "clientId": GITHUB_CLIENT_ID,
                        "clientSecret": "${AUTHGUARD_GITHUB_CLIENT_SECRET}",
                        "callbackUrl": f"http://{APPLICATION_HOST}:8082/auth/oauth2/github/callback",
                        "authorization": {"endpoint": f"{mock_url}/github/login/oauth/authorize", "scopes": ["read:user", "user:email"]},
                        "token": {"endpoint": f"{mock_url}/github/login/oauth/access_token", "method": "POST"},
                        "identity": {
                            "endpoint": f"{mock_url}/github/user",
                            "subject": "$.id",
                            "username": "$.login",
                            "email": "$.email",
                            "trustedClaims": {"tenant_id": "$.tenant_id"},
                        },
                    }
                },
                "accountLinking": {"strategy": "first-login", "authoritativeProviders": [], "allowLink": {}},
                "challengeTtl": "2m",
                "token": {
                    "issuer": AUTHN_ISSUER,
                    "audience": AUDIENCE,
                    "ttl": "30m",
                    "privateKey": "${AUTHGUARD_AUTHN_SESSION_PRIVATE_KEY}",
                },
            },
            "server": {
                "service_name": "authguard-authz", "host": "0.0.0.0", "port": 8080,
                "scope_port": 8081, "shutdown_timeout": "15s",
                "request": {"max_message_bytes": 1048576, "timeout": "5s"},
                "response": {"max_message_bytes": 1048576},
                "performance": {"worker_threads": 2, "max_in_flight_requests": 4096},
            },
            "logging": {"mode": "JSON", "level": "info,tower_http=info"},
            "mgmt": {
                "enabled": True, "host": "0.0.0.0", "port": 9091, "context_path": "/",
                "health": {"liveness_path": "/healthz", "readiness_path": "/readyz"},
                "metrics": {"enabled": True, "path": "/metrics"},
                "otel": {"enabled": False, "endpoint": "http://127.0.0.1:4317", "protocol": "grpc", "timeout": "5s", "sample_rate": 1.0},
            },
            "authz": {
                "identity": {"token_header": "authorization", "principal_id_claim": "principal_id", "principal_kind_claim": "principal_kind", "groups_claim": "authguard_group_ids"},
                "scope_delivery": {
                    "direct_urn_limit": 32,
                    "max_direct_header_bytes": 8192,
                    "direct_context_hmac_key": "${AUTHGUARD_ACCESS_CONTEXT_HMAC_KEY}",
                    "context_ttl": "30s",
                    "scope_token_ttl": "30s",
                },
                "principal_discovery": {
                    "keycloak": [],
                    "ldap": [{
                        "enabled": True, "discovery_id": "e2e-flowgent-corporate-ldap", "url": ldap_url,
                        "issuer": LDAP_ISSUER, "base_dn": "dc=example,dc=org",
                        "auth": {"bind_dn": "cn=svc-authguard,ou=Users,dc=example,dc=org", "bind_password": "${AUTHGUARD_LDAP_BIND_PASSWORD}"},
                        "user": {"search_base": "", "object_filter": "(objectClass=posixAccount)", "id_attribute": "uidNumber", "name_attribute": "cn", "display_name_attribute": "displayName", "email_attribute": "mail", "search_attributes": ["cn", "displayName", "mail", "uidNumber"]},
                        "group": {"search_base": "", "object_filter": "(objectClass=posixGroup)", "id_attribute": "gidNumber", "name_attribute": "ou", "display_name_attribute": "ou", "search_attributes": ["ou", "gidNumber"]},
                        "connect_timeout": "3s", "request_timeout": "10s", "max_page_size": 100, "allow_insecure": True,
                    }],
                    "custom": [],
                    "scim": {"enabled": False, "discovery_id": "disabled", "issuer": LDAP_ISSUER},
                },
                "resign": {"enabled": False, "max_ttl": "60s", "private_key_b64": ""},
                "api_token": "",
            },
            "storage": {
                "provider": "Postgres", "bootstrap_policy": None,
                "sqlite": {"url": "sqlite:///tmp/authguard.db", "max_connections": 1, "connect_timeout": "5s"},
                "postgres": {"url": postgres_url, "username": pg_user, "password": "${AUTHGUARD__STORAGE__POSTGRES__PASSWORD}", "max_connections": 10, "min_connections": 0, "connect_timeout": "5s", "idle_timeout": "10m", "validate_on_acquire": True},
            },
            "cache": {
                "provider": "Redis",
                "memory": {"initial_capacity": 32, "max_capacity": 65535, "ttl": "1h", "eviction_policy": "LRU"},
                "redis": {
                    "nodes": [redis_url],
                    "username": "default",
                    "password": "${AUTHGUARD__CACHE__REDIS__PASSWORD}",
                    "key_prefix": f"{config.RESOURCE_PREFIX}:authguard",
                    "connection_timeout": "3s",
                    "response_timeout": "6s",
                    "retries": 8,
                    "max_retry_wait": "15s",
                    "min_retry_wait": "500ms",
                    "read_from_replica": False,
                },
            },
        }
        return yaml.safe_dump(document, sort_keys=False)

    @staticmethod
    def prepare(namespace: str, pg_container: str, pg_host: str, pg_port: str, pg_user: str, pg_password: str) -> dict:
        print("\n-- Preparing isolated AuthGuard identity services --")
        if not KubernetesImageManager.import_authguard():
            raise RuntimeError("isolated AuthGuard image is unavailable")
        if not KubernetesImageManager.import_authguard_web():
            raise RuntimeError("isolated AuthGuard Web image is unavailable")
        secret_name, bind_password = AuthGuardRuntime._ensure_secret(namespace)
        AuthGuardRuntime._reset_authguard_schema(pg_container, pg_user, pg_password)
        AuthGuardRuntime._ensure_support(namespace, bind_password)
        jwks = AuthGuardRuntime._secret_value(namespace, "AUTHGUARD_AUTHN_SESSION_JWKS")
        return {
            "authguard-middleware": {
                "enabled": True,
                "adapter": {
                    "enabled": True,
                    "existingSecret": secret_name,
                    "hmacKey": "AUTHGUARD_ACCESS_CONTEXT_HMAC_KEY",
                },
                "fullnameOverride": f"{config.RESOURCE_PREFIX}-authguard",
                "secrets": {"provider": "kubernetes", "kubernetes": {"existingSecret": secret_name}},
                "redis_cluster": {
                    "enabled": True,
                    "fullnameOverride": AUTHGUARD_REDIS_NAME,
                    "existingSecret": secret_name,
                    "existingSecretPasswordKey": "AUTHGUARD__CACHE__REDIS__PASSWORD",
                    "cluster": {"nodes": 3, "replicas": 0},
                    "redis": {
                        "resources": {
                            "requests": {"cpu": "10m", "memory": "48Mi"},
                            "limits": {"cpu": "250m", "memory": "192Mi"},
                        }
                    },
                },
                "postgresql": {"enabled": False},
                "envoy_gateway": {
                    "enabled": True,
                    "nameOverride": f"{config.RESOURCE_PREFIX}-envoy-gateway",
                    "deployment": {"replicas": 1},
                    "config": {"envoyGateway": {"gateway": {"controllerName": GATEWAY_CONTROLLER}}},
                    "ext_authz": {
                        "enabled": True,
                        "gateway": {
                            "create": True,
                            "name": GATEWAY_NAME,
                            "listenerPort": GATEWAY_LISTENER_PORT,
                            "className": GATEWAY_CLASS,
                            "controllerName": GATEWAY_CONTROLLER,
                        },
                        "authguardRoute": {
                            "enabled": True,
                            "name": f"{config.RESOURCE_PREFIX}-api",
                            "hostnames": [APPLICATION_HOST],
                            "backend": {"name": f"{config.RELEASE_NAME}-apiserver", "port": 9999},
                        },
                        "authnRoute": {"enabled": True, "name": f"{config.RESOURCE_PREFIX}-authn", "hostnames": [APPLICATION_HOST]},
                    },
                },
                "authguard": {
                    "authn": {
                        "enabled": True,
                        "replicaCount": 1,
                        "applications": {
                            "flowgent": {
                                "hosts": [APPLICATION_HOST],
                                "displayName": "Flowgent Security Autonomy Fixer",
                                "returnUris": [f"https://{APPLICATION_HOST}/**"],
                            }
                        },
                        "image": {"repository": AUTHGUARD_IMAGE.rsplit(":", 1)[0], "tag": AUTHGUARD_IMAGE.rsplit(":", 1)[1], "pullPolicy": "Never"},
                        "canonical_jwt": {
                            "issuer": AUTHN_ISSUER,
                            "audiences": [AUDIENCE],
                            "local_jwks": {"inline": jwks, "existing_config_map": ""},
                            "remote_jwks": {"uri": "", "backend_refs": []},
                        },
                        "disruptionBudget": {"enabled": False},
                    },
                    "web": {
                        "enabled": True,
                        "replicaCount": 1,
                        "image": {
                            "repository": AUTHGUARD_WEB_IMAGE.rsplit(":", 1)[0],
                            "tag": AUTHGUARD_WEB_IMAGE.rsplit(":", 1)[1],
                            "pullPolicy": "Never",
                        },
                        "route": {
                            "enabled": True,
                            "hostnames": [APPLICATION_HOST],
                            "console": {"enabled": False, "hostnames": []},
                        },
                    },
                    "authz": {"replicaCount": 1, "image": {"repository": AUTHGUARD_IMAGE.rsplit(":", 1)[0], "tag": AUTHGUARD_IMAGE.rsplit(":", 1)[1], "pullPolicy": "Never"}, "disruptionBudget": {"enabled": False}, "networkPolicy": {"enabled": False}},
                    "authguard-config": AuthGuardRuntime.runtime_config(namespace, pg_host, pg_port, pg_user),
                },
            },
        }

    @contextmanager
    @staticmethod
    def _forward(namespace: str, service: str, remote_port: int):
        sock = subprocess.run(
            ["python3", "-c", "import socket;s=socket.socket();s.bind(('127.0.0.1',0));print(s.getsockname()[1]);s.close()"],
            capture_output=True, text=True, check=True, timeout=10,
        )
        local_port = int(sock.stdout.strip())
        process = subprocess.Popen(
            ["kubectl", "port-forward", "-n", namespace, f"service/{service}", f"{local_port}:{remote_port}"],
            stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, text=True,
        )
        try:
            deadline = time.time() + 20
            while time.time() < deadline:
                if process.poll() is not None:
                    raise RuntimeError(process.stderr.read()[:400])
                try:
                    with socket.create_connection(("127.0.0.1", local_port), timeout=1):
                        break
                except OSError:
                    time.sleep(0.2)
            else:
                raise RuntimeError(f"port-forward did not become ready: {service}")
            yield local_port
        finally:
            process.terminate()
            try:
                process.wait(timeout=3)
            except subprocess.TimeoutExpired:
                process.kill()

    @staticmethod
    def _api(port: int, path: str, token: str, method: str = "GET", payload: dict | None = None, headers: dict | None = None):
        def response_body(response) -> dict:
            raw = response.read()
            if not raw:
                return {}
            try:
                return json.loads(raw)
            except json.JSONDecodeError:
                return {"raw": raw.decode(errors="replace")}

        body = None if payload is None else json.dumps(payload).encode()
        req = request.Request(
            f"http://127.0.0.1:{port}{path}", data=body, method=method,
            headers={"Authorization": f"Bearer {token}", "Content-Type": "application/json", **(headers or {})},
        )
        try:
            with request.urlopen(req, timeout=20) as response:
                return response.status, response.headers, response_body(response)
        except error.HTTPError as response:
            return response.code, response.headers, response_body(response)

    @staticmethod
    def _social_login(namespace: str) -> tuple[str, str]:
        authn_service = f"{config.RESOURCE_PREFIX}-authguard-authn"
        with AuthGuardRuntime._forward(namespace, authn_service, 8082) as authn_port, AuthGuardRuntime._forward(namespace, MOCK_GITHUB_NAME, 8080) as mock_port:
            no_redirect = request.build_opener(type("NoRedirect", (request.HTTPRedirectHandler,), {"redirect_request": lambda *_: None})())

            def redirect_location(url: str, host: str | None = None) -> str:
                headers = {"Host": host} if host else {}
                try:
                    no_redirect.open(request.Request(url, headers=headers), timeout=15)
                except error.HTTPError as response:
                    if response.code in (302, 303, 307, 308) and response.headers.get("Location"):
                        return response.headers["Location"]
                    raise
                raise RuntimeError(f"expected OAuth redirect from {url}")

            first = redirect_location(
                f"http://127.0.0.1:{authn_port}/auth/oauth2/github/authorize?return_uri=%2Fapi%2Fv1%2F{config.NAMESPACE_ID}%2Fflows",
                APPLICATION_HOST,
            )
            provider = parse.urlsplit(first)
            callback = parse.urlsplit(redirect_location(f"http://127.0.0.1:{mock_port}{provider.path}?{provider.query}"))
            req = request.Request(
                f"http://127.0.0.1:{authn_port}{callback.path}?{callback.query}",
                headers={"Host": APPLICATION_HOST},
            )
            with request.urlopen(req, timeout=20) as response:
                login = json.loads(response.read())
        token = login.get("accessToken", "")
        principal_id = login.get("principal", {}).get("principalId", "")
        if not token or not principal_id:
            raise RuntimeError("GitHub AuthN did not return a canonical Principal session")
        return token, principal_id

    @staticmethod
    def _policy(revision: int, github_principal_id: str) -> dict:
        methods = {
            "flowgent.api.read": ["GET"],
            "flowgent.api.operate": ["POST", "PUT", "PATCH"],
            "flowgent.api.delete": ["DELETE"],
        }
        actions = []
        for action, action_methods in methods.items():
            matchers = []
            for depth in range(1, 7):
                suffix = "/".join(f"{{segment_{index}}}" for index in range(1, depth + 1))
                urn_suffix = "/".join(f"{{segment_{index}}}" for index in range(1, depth + 1))
                matchers.append({
                    "id": f"{action.rsplit('.', 1)[-1]}-depth-{depth}",
                    "methods": action_methods,
                    "hosts": [],
                    "path": f"/api/v1/{{namespace}}/{suffix}",
                    "resource_urn": f"urn:iam:prod:flowgent:global:example-corp:namespace/{{namespace}}/{urn_suffix}",
                    "parent_urns": [],
                })
            actions.append({"identifier": action, "description": action, "route_matchers": matchers})
        roles = [
            {"id": "flowgent-platform-security-admin", "name": "Global Platform Security Administrator", "description": "Break-glass governed administration of Flowgent security automation", "action_ids": list(methods)},
            {"id": "flowgent-security-automation-owner", "name": "Security Automation Owner", "description": "Own, operate, and retire security autonomy workflows", "action_ids": list(methods)},
            {"id": "flowgent-security-analyst", "name": "Application Security Analyst", "description": "Investigate findings and operate remediation flows without destructive administration", "action_ids": ["flowgent.api.read", "flowgent.api.operate"]},
            {"id": "flowgent-model-risk-approver", "name": "Independent Model Risk Approver", "description": "Review evidence and resolve human approval gates", "action_ids": ["flowgent.api.read", "flowgent.api.operate"]},
            {"id": "flowgent-regulatory-auditor", "name": "Regulatory Auditor", "description": "Read-only access to runs, traces, approvals, and immutable evidence", "action_ids": ["flowgent.api.read"]},
        ]
        tenant_scope = "urn:iam:prod:flowgent:global:example-corp:namespace/**"
        bindings = [
            {"id": "github-e2e-platform-admin", "principal_id": github_principal_id, "role_id": "flowgent-platform-security-admin", "effect": "ALLOW", "resource_urn": f"urn:iam:prod:flowgent:global:example-corp:namespace/{config.NAMESPACE_ID}/**", "conditions": {}},
            {"id": "github-e2e-denied-flow", "principal_id": github_principal_id, "role_id": "flowgent-platform-security-admin", "effect": "DENY", "resource_urn": f"urn:iam:prod:flowgent:global:example-corp:namespace/{config.NAMESPACE_ID}/flows/security-autonomy-fixer/**", "conditions": {}},
            {"id": "ldap-platform-security", "principal_id": "principal-global-platform-security", "role_id": "flowgent-platform-security-admin", "effect": "ALLOW", "resource_urn": tenant_scope, "conditions": {}},
            {"id": "ldap-security-automation-owner", "principal_id": "principal-security-automation-owners", "role_id": "flowgent-security-automation-owner", "effect": "ALLOW", "resource_urn": f"urn:iam:prod:flowgent:global:example-corp:namespace/{config.NAMESPACE_ID}/**", "conditions": {}},
            {"id": "ldap-emea-appsec", "principal_id": "principal-emea-appsec", "role_id": "flowgent-security-analyst", "effect": "ALLOW", "resource_urn": f"urn:iam:prod:flowgent:global:example-corp:namespace/{config.NAMESPACE_ID}/**", "conditions": {}},
            {"id": "ldap-model-risk", "principal_id": "principal-global-model-risk", "role_id": "flowgent-model-risk-approver", "effect": "ALLOW", "resource_urn": tenant_scope, "conditions": {"subject": {"mfa": True}}},
            {"id": "ldap-regulatory-audit", "principal_id": "principal-global-regulatory-audit", "role_id": "flowgent-regulatory-auditor", "effect": "ALLOW", "resource_urn": tenant_scope, "conditions": {}},
        ]
        return {"revision": revision, "actions": actions, "roles": roles, "role_bindings": bindings}

    @staticmethod
    def _wait_authguard_ready(namespace: str) -> None:
        resources = (
            f"statefulset/{AUTHGUARD_REDIS_NAME}",
            f"deployment/{config.RESOURCE_PREFIX}-authguard",
            f"deployment/{config.RESOURCE_PREFIX}-authguard-authn",
            f"deployment/{config.RESOURCE_PREFIX}-authguard-web",
            "deployment/envoy-gateway",
        )
        for resource in resources:
            result = subprocess.run(
                [
                    "kubectl",
                    "rollout",
                    "status",
                    resource,
                    "-n",
                    namespace,
                    "--timeout=300s",
                ],
                capture_output=True,
                text=True,
                timeout=330,
            )
            if result.returncode != 0:
                detail = (result.stdout + result.stderr).strip()[-600:]
                raise RuntimeError(f"AuthGuard resource not ready: {resource}: {detail}")
        deadline = time.time() + 300
        while time.time() < deadline:
            result = subprocess.run(
                ["kubectl", "get", "gateway", GATEWAY_NAME, "-n", namespace, "-o", "json"],
                capture_output=True,
                text=True,
                timeout=20,
            )
            if result.returncode == 0:
                gateway = json.loads(result.stdout)
                listener_conditions = [
                    condition
                    for listener in gateway.get("status", {}).get("listeners", [])
                    for condition in listener.get("conditions", [])
                ]
                if any(
                    condition.get("type") == "Programmed" and condition.get("status") == "True"
                    for condition in listener_conditions
                ):
                    return
            time.sleep(3)
        raise RuntimeError("AuthGuard Gateway listener was not programmed within 300s")

    @staticmethod
    def bootstrap_and_verify(namespace: str, *, verify_resource_scope: bool = True) -> None:
        print("\n-- Verifying LDAP federation, GitHub AuthN, and Envoy/AuthGuard authorization --")
        AuthGuardRuntime._wait_authguard_ready(namespace)
        token, github_principal_id = AuthGuardRuntime._social_login(namespace)
        api_token = AuthGuardRuntime._secret_value(namespace, "AUTHGUARD__AUTHZ__API_TOKEN")
        authz_service = f"{config.RESOURCE_PREFIX}-authguard"
        materializations = (
            ("global-platform-security", "GROUP", "principal-global-platform-security"),
            ("security-automation-owners", "GROUP", "principal-security-automation-owners"),
            ("emea-appsec", "GROUP", "principal-emea-appsec"),
            ("global-model-risk", "GROUP", "principal-global-model-risk"),
            ("global-regulatory-audit", "GROUP", "principal-global-regulatory-audit"),
        )
        with AuthGuardRuntime._forward(namespace, authz_service, 9091) as port:
            for text_value, kind, principal_id in materializations:
                status, _, result = AuthGuardRuntime._api(port, "/api/v1/principal-discovery/search", api_token, "POST", {"text": text_value, "kinds": [kind], "provider_ids": ["e2e-flowgent-corporate-ldap"], "per_provider_limit": 20, "cursors": {}})
                candidates = result.get("principals", [])
                if status != 200 or len(candidates) != 1:
                    raise RuntimeError(f"LDAP discovery failed for {text_value}: HTTP {status} candidates={len(candidates)}")
                status, _, _ = AuthGuardRuntime._api(port, "/api/v1/principal-discovery/materialize", api_token, "POST", {"principal_id": principal_id, "reference": candidates[0]["reference"]})
                if status != 201:
                    raise RuntimeError(f"LDAP materialization failed for {text_value}: HTTP {status}")
            status, headers, current = AuthGuardRuntime._api(port, "/api/v1/policy", api_token)
            if status != 200:
                raise RuntimeError(f"read AuthGuard policy failed: HTTP {status}")
            revision = int(current.get("revision", 1))
            status, _, persisted = AuthGuardRuntime._api(port, "/api/v1/policy", api_token, "PUT", AuthGuardRuntime._policy(revision, github_principal_id), {"If-Match": str(revision)})
            if status != 200 or int(persisted.get("revision", 0)) <= revision:
                raise RuntimeError(f"replace AuthGuard policy failed: HTTP {status}")

        if verify_resource_scope:
            # Config import intentionally follows Helm deployment in runner.py.
            # Only the post-import scenario may assert against this fixture; the
            # deployment bootstrap still proves the identity and edge contracts.
            with AuthGuardRuntime._forward(namespace, f"{config.RELEASE_NAME}-apiserver", 9990) as internal_api_port:
                status, _, _ = AuthGuardRuntime._api(
                    internal_api_port,
                    f"/api/v1/{config.NAMESPACE_ID}/flows/security-autonomy-fixer",
                    "",
                )
                if status != 200:
                    raise RuntimeError(
                        "resource-level AuthGuard fixture is absent from the trusted internal API: "
                        f"HTTP {status}"
                    )

        # Prove the canonical GitHub JWT traverses Envoy, is authorized by the
        # imported policy, and reaches Flowgent with a valid signed access context.
        rc, service = CommandRunner.legacy([
            "kubectl", "get", "service", "-n", namespace,
            "-l", f"gateway.envoyproxy.io/owning-gateway-name={GATEWAY_NAME}",
            "-o", "jsonpath={.items[0].metadata.name}",
        ], timeout=20)
        if rc != 0 or not service.strip():
            raise RuntimeError("Envoy data-plane Service was not created")
        with AuthGuardRuntime._forward(namespace, service.strip(), GATEWAY_LISTENER_PORT) as gateway_port:
            status, _, metadata = AuthGuardRuntime._api(
                gateway_port,
                "/.well-known/authn.json",
                "",
                headers={"Host": APPLICATION_HOST},
            )
            application = metadata.get("application", {})
            if status != 200 or application.get("id") != "flowgent":
                raise RuntimeError(
                    f"AuthN Application metadata failed: HTTP {status} application={application}"
                )
            status, _, _ = AuthGuardRuntime._api(
                gateway_port,
                "/auth/login",
                "",
                headers={"Host": APPLICATION_HOST},
            )
            if status != 200:
                raise RuntimeError(f"Hosted Login route failed: HTTP {status}")
            status, _, flow_page = AuthGuardRuntime._api(
                gateway_port,
                f"/api/v1/{config.NAMESPACE_ID}/flows",
                token,
                headers={"Host": APPLICATION_HOST},
            )
            if status != 200:
                raise RuntimeError(f"GitHub -> Envoy -> AuthGuard -> Flowgent request failed: HTTP {status}")
            if verify_resource_scope:
                flow_items = (
                    flow_page
                    if isinstance(flow_page, list)
                    else flow_page.get("items", [])
                )
                visible_flow_ids = {
                    item.get("flow_id")
                    for item in flow_items
                    if isinstance(item, dict)
                }
                if "security-autonomy-fixer" in visible_flow_ids:
                    raise RuntimeError(
                        "AuthGuard row-level DENY was not appended to the Flow repository query"
                    )
                status, _, _ = AuthGuardRuntime._api(
                    gateway_port,
                    f"/api/v1/{config.NAMESPACE_ID}/flows/security-autonomy-fixer",
                    token,
                    headers={"Host": APPLICATION_HOST},
                )
                if status != 403:
                    raise RuntimeError(
                        "AuthGuard exact-resource DENY did not fail closed: "
                        f"HTTP {status}"
                    )
            status, _, _ = AuthGuardRuntime._api(
                gateway_port,
                f"/api/v1/{config.RESOURCE_PREFIX}-forbidden/flows",
                token,
                headers={"Host": APPLICATION_HOST},
            )
            if status not in (401, 403):
                raise RuntimeError(
                    "namespace resource outside the AuthGuard grant did not fail closed: "
                    f"HTTP {status}"
                )
            status, _, _ = AuthGuardRuntime._api(
                gateway_port,
                f"/api/v1/{config.NAMESPACE_ID}/flows",
                "invalid",
                headers={"Host": APPLICATION_HOST},
            )
            if status not in (401, 403):
                raise RuntimeError(f"invalid JWT did not fail closed: HTTP {status}")
        with AuthGuardRuntime._forward(namespace, f"{config.RELEASE_NAME}-apiserver", 9999) as external_api_port:
            status, _, _ = AuthGuardRuntime._api(
                external_api_port,
                f"/api/v1/{config.NAMESPACE_ID}/flows",
                "",
            )
            if status != 401:
                raise RuntimeError(
                    "direct external Flowgent API without AuthGuard context did not fail closed: "
                    f"HTTP {status}"
                )
        scope_result = (
            "action/resource SQL scope + "
            if verify_resource_scope
            else "pre-import identity/edge bootstrap + "
        )
        print(
            "  AuthGuard E2E passed: Application metadata + Hosted Login + LDAP "
            f"discovery/materialization + GitHub OAuth + {scope_result}"
            "gateway/direct-port fail-closed"
        )


































class AuthGuardDeployer(BaseDeployer):
    """Configure and verify the optional AuthGuard middleware subchart."""

    component = "authguard"

    def __init__(
        self,
        context: RunContext,
        *,
        pg_container: str,
        pg_host: str,
        pg_port: str,
        pg_user: str,
        pg_password: str,
    ) -> None:
        super().__init__(context)
        self.pg_container = pg_container
        self.pg_host = pg_host
        self.pg_port = pg_port
        self.pg_user = pg_user
        self.pg_password = pg_password
        self.values: dict = {}

    def deploy(self) -> None:
        self.values = self.step(
            "prepare LDAP, GitHub identity, Redis, and AuthGuard values",
            lambda: AuthGuardRuntime.prepare(
                self.context.namespace,
                self.pg_container,
                self.pg_host,
                self.pg_port,
                self.pg_user,
                self.pg_password,
            ),
        )

    def verify(self, *, resource_scope: bool = True) -> None:
        self.step(
            "verify LDAP federation, GitHub authentication, Envoy, and resource policy",
            lambda: AuthGuardRuntime.bootstrap_and_verify(
                self.context.namespace,
                verify_resource_scope=resource_scope,
            ),
        )
