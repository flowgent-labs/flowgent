#!/usr/bin/env python3
"""
Deploy Script S11 — Flowgent Helm deployment and readiness check.

Fully redeploys the Flowgent Helm chart and waits for all pods to be Ready
before handing off to the verifier suite.

Usage:
  python3 s11_flowgent_deployer.py [--namespace NS] [--release NAME] [--timeout S]
"""

import sys
import os
import time
import argparse
import base64
import copy
import ipaddress
import json
import secrets
import socket
import subprocess
import tempfile
import requests
from urllib.parse import urlsplit, urlunsplit
import yaml

from common import HELM_CHART, config, run_cmd

DEFAULT_NAMESPACE = config.SYSTEM_NAMESPACE
DEFAULT_RELEASE = config.RELEASE_NAME
DEFAULT_TIMEOUT = 300
FLOWGENT_IMAGE = "localhost/flowgent-core:latest"
FLOWGENT_PG_CONTAINER = os.getenv("FLOWGENT_E2E_PG_CONTAINER", "sigbot_e2e_164364_postgres")
WORKLOAD_NAMESPACE_PREFIX = os.getenv(
    "FLOWGENT_K8S_WORKLOAD_NAMESPACE_PREFIX",
    f"{config.RESOURCE_PREFIX}-workload-",
)
TENANT_NAMESPACE = os.getenv("FLOWGENT_NAMESPACE_ID", config.NAMESPACE_ID)
RUNTIME_CREDENTIAL_SECRET = os.getenv("FLOWGENT_E2E_RUNTIME_SECRET", f"{config.RESOURCE_PREFIX}-runtime-env")
RUNTIME_CREDENTIAL_KEYS = (
    "GITHUB_TOKEN",
    "GH_TOKEN",
    "SONARQUBE_TOKEN",
    "DEEPSEEK_API_KEY",
    "DEEPSEEK_API_KEY_FLOWGENT",
)
RUNTIME_PROXY_KEYS = (
    "HTTP_PROXY",
    "HTTPS_PROXY",
    "ALL_PROXY",
    "http_proxy",
    "https_proxy",
    "all_proxy",
)
PROXY_ALLOWLIST_ENV = "FLOWGENT_E2E_PROXY_ALLOWLIST_ENTRY"
NOTIFICATION_TOKEN_ENV = "FLOWGENT_E2E_NOTIFICATION_TOKEN"
NOTIFICATION_RECEIVER = f"{config.RESOURCE_PREFIX}-webhook"
AUTH_TOKEN_ENV = "FLOWGENT_E2E_AUTH_TOKEN"
AUTH_SECRET = os.getenv("FLOWGENT_E2E_AUTH_SECRET", f"{config.RESOURCE_PREFIX}-authorization")
NO_PROXY_DEFAULTS = (
    "localhost",
    "127.0.0.1",
    "::1",
    ".svc",
    ".svc.cluster.local",
    ".cluster.local",
    "10.0.0.0/8",
    "172.16.0.0/12",
    "192.168.0.0/16",
    f"{config.RELEASE_NAME}-apiserver",
    f"{config.RELEASE_NAME}-emqx",
    f"{config.RELEASE_NAME}-jaeger",
)
HOST_WORKSPACE = os.path.realpath(
    os.getenv("FLOWGENT_E2E_HOST_WORKSPACE", "/mnt/disk1/flowgent/e2e")
)

def _usable_kubeconfig(path: str) -> bool:
    return bool(path) and os.path.isfile(path) and os.path.getsize(path) > 0


def _select_kubeconfig() -> str:
    candidates = (
        os.environ.get("KUBECONFIG", ""),
        os.path.expanduser("~/.kube/config"),
        "/etc/rancher/k3s/k3s.yaml",
    )
    for path in candidates:
        if _usable_kubeconfig(path):
            return path
    return ""


selected_kubeconfig = _select_kubeconfig()
if selected_kubeconfig:
    os.environ["KUBECONFIG"] = selected_kubeconfig


def _kubectl(args, timeout=60):
    return run_cmd(["kubectl"] + args, timeout=timeout)


def _helm(args, timeout=300):
    return run_cmd(["helm"] + args, timeout=timeout)


def _run_sensitive(cmd, printable, timeout=120, input_text=None, print_stdout=True):
    """Run a command whose argv/stdin may contain credentials without logging them."""
    print(f"  $ {printable}")
    try:
        result = subprocess.run(
            cmd,
            input=input_text,
            timeout=timeout,
            capture_output=True,
            text=True,
        )
        if print_stdout and result.stdout:
            for line in result.stdout.splitlines():
                print(f"    {line}")
        if result.stderr:
            for line in result.stderr.splitlines():
                print(f"    [stderr] {line}")
        return result.returncode, result.stdout.strip()
    except subprocess.TimeoutExpired:
        print(f"    ERROR: timed out after {timeout}s")
        return 1, ""
    except FileNotFoundError:
        print(f"    ERROR: command not found: {cmd[0]}")
        return 1, ""


def _cleanup_runtime_resources(system_namespace=DEFAULT_NAMESPACE):
    print("\n-- Cleaning runtime-cluster resources --")
    # Never use cluster-wide Flowgent labels here: the adjacent AuthGuard E2E
    # and older Flowgent releases may be running concurrently on this host.
    for ns in sorted({system_namespace, _workload_namespace()}):
        _kubectl([
            "delete", "deployment", "-n", ns,
            "-l", "app.kubernetes.io/component in (taskmanager,sandbox)",
            "--ignore-not-found=true", "--force", "--grace-period=0",
        ], timeout=120)
        _kubectl([
            "delete", "pod", "-n", ns,
            "-l", "flowgent/role in (worker,sandbox-worker)",
            "--ignore-not-found=true", "--force", "--grace-period=0", "--wait=false",
        ], timeout=60)
    workload_ns = _workload_namespace()
    _kubectl([
        "delete", "deployment", "-n", workload_ns,
        "-l", "flowgent.io/runtime-boundary=flow-jobmanager",
        "--ignore-not-found=true", "--force", "--grace-period=0",
    ], timeout=120)
    _kubectl([
        "delete", "pod", "-n", workload_ns,
        "-l", "flowgent.io/runtime-boundary=flow-jobmanager",
        "--ignore-not-found=true", "--force", "--grace-period=0", "--wait=false",
    ], timeout=60)
    _wait_labeled_resources_gone(
        "deployments", "flowgent.io/runtime-boundary=flow-jobmanager", workload_ns, timeout=60
    )
    _wait_labeled_resources_gone(
        "pods", "flowgent.io/runtime-boundary=flow-jobmanager", workload_ns, timeout=60
    )
    _cleanup_runtime_workspace()


def _cleanup_runtime_workspace():
    """Remove only regenerable E2E runtime workspaces after all app pods exit."""
    tenant_workspace = os.path.realpath(os.path.join(HOST_WORKSPACE, TENANT_NAMESPACE))
    if (
        not os.path.isdir(tenant_workspace)
        or tenant_workspace == HOST_WORKSPACE
        or os.path.commonpath((HOST_WORKSPACE, tenant_workspace)) != HOST_WORKSPACE
    ):
        return
    result = subprocess.run(
        ["sudo", "find", tenant_workspace, "-mindepth", "1", "-depth", "-delete"],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.PIPE,
        text=True,
        timeout=120,
    )
    if result.returncode != 0:
        print(f"  WARN: E2E workspace cleanup incomplete: {result.stderr.strip()[:300]}")
        return
    print(f"  E2E runtime workspace cleaned: {tenant_workspace}")


def _wait_labeled_resources_gone(kind, selector, namespace, timeout=60):
    deadline = time.time() + timeout
    while time.time() < deadline:
        rc, out = _kubectl(
            ["get", kind, "-n", namespace, "-l", selector, "-o", "json"], timeout=20
        )
        if rc != 0:
            time.sleep(2)
            continue
        try:
            items = json.loads(out).get("items", [])
        except json.JSONDecodeError:
            time.sleep(2)
            continue
        if not items:
            print(f"  {kind} with {selector}: cleaned")
            return True
        names = [f"{i.get('metadata', {}).get('namespace')}/{i.get('metadata', {}).get('name')}" for i in items[:5]]
        print(f"  Waiting for {len(items)} {kind} to disappear: {names}")
        time.sleep(3)
    print(f"  WARN: {kind} with {selector} still exist after {timeout}s")
    return False


def _force_finalize_terminating_runtime_namespaces():
    rc, out = _kubectl([
        "get", "ns", "-l", "flowgent.io/runtime-boundary=namespace", "-o", "json",
    ], timeout=20)
    if rc != 0:
        return
    try:
        namespaces = json.loads(out).get("items", [])
    except json.JSONDecodeError:
        return
    for ns in namespaces:
        meta = ns.get("metadata", {})
        name = meta.get("name")
        if not name or not meta.get("deletionTimestamp"):
            continue
        print(f"  Finalizing stuck terminating namespace: {name}")
        ns.setdefault("spec", {})["finalizers"] = []
        result = subprocess.run(
            ["kubectl", "replace", "--raw", f"/api/v1/namespaces/{name}/finalize", "-f", "-"],
            input=json.dumps(ns),
            capture_output=True,
            text=True,
            timeout=30,
        )
        if result.returncode != 0:
            print(f"  WARN: namespace finalize failed for {name}: {result.stderr[:200]}")


def _uninstall_release(release, namespace):
    print("\n-- Uninstalling existing Helm release --")
    rc, out = _helm(["list", "-n", namespace, "-q", "--filter", f"^{release}$"], timeout=30)
    if rc != 0 or release not in out.splitlines():
        print(f"  Release '{release}' not installed in namespace '{namespace}'.")
        return True
    # A GatewayClass cannot disappear while one of its Gateways still exists.
    # Delete this release's precisely named Gateway while its Envoy Gateway
    # controller is still running, otherwise Helm may remove the controller
    # first and strand the GatewayClass finalizer indefinitely.
    gateway_name = f"{config.RESOURCE_PREFIX}-gateway"
    _kubectl([
        "delete", "gateway", gateway_name, "-n", namespace,
        "--ignore-not-found=true", "--wait=true", "--timeout=90s",
    ], timeout=120)
    rc, _ = _helm(["uninstall", release, "-n", namespace, "--wait", "--timeout", "180s"], timeout=240)
    if rc != 0:
        print(f"  ERROR: helm uninstall failed for {release}")
        return False
    return True


def cleanup_after_run(release, namespace):
    """Remove only the isolated resources owned by this E2E deployment."""
    print("\n-- Cleaning isolated Flowgent E2E deployment --")
    ok = _uninstall_release(release, namespace)
    _cleanup_runtime_resources(namespace)
    workload_namespace = _workload_namespace()
    for target_namespace in sorted({namespace, workload_namespace}):
        rc, _ = _kubectl([
            "delete", "namespace", target_namespace,
            "--ignore-not-found=true", "--wait=true", "--timeout=180s",
        ], timeout=210)
        ok = ok and rc == 0
    _kubectl([
        "delete", "gatewayclass", f"{config.RESOURCE_PREFIX}-gateway",
        "--ignore-not-found=true", "--wait=true", "--timeout=90s",
    ], timeout=120)
    return ok


def _ensure_authorization_secret(namespace):
    """Create an explicit test-only auth secret without ever printing values."""
    bootstrap_token = os.environ.get(AUTH_TOKEN_ENV)
    if not bootstrap_token:
        bootstrap_token = secrets.token_urlsafe(48)
        os.environ[AUTH_TOKEN_ENV] = bootstrap_token
    manifest = {
        "apiVersion": "v1",
        "kind": "Secret",
        "metadata": {"name": AUTH_SECRET, "namespace": namespace},
        "type": "Opaque",
        "stringData": {
            "bootstrap-token": bootstrap_token,
            "controller-token": secrets.token_urlsafe(48),
            "notifier-token": secrets.token_urlsafe(48),
            "a2a-token": secrets.token_urlsafe(48),
        },
    }
    result = subprocess.run(
        ["kubectl", "apply", "-f", "-"],
        input=yaml.safe_dump(manifest),
        capture_output=True,
        text=True,
        timeout=30,
    )
    if result.returncode != 0:
        print(f"  ERROR: apply authorization secret failed: {result.stderr[:200]}")
        return False
    print(f"  Authorization secret ready: {namespace}/{AUTH_SECRET} (values redacted)")
    return True


def _docker_container_ip(container):
    rc, out = run_cmd([
        "docker", "inspect", container,
        "--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}",
    ], timeout=20)
    if rc == 0 and out.strip():
        return out.strip().split()[0]
    return os.getenv("FLOWGENT_PG_HOST", "127.0.0.1")


def _runtime_env(namespace, release):
    pg_host = _docker_container_ip(FLOWGENT_PG_CONTAINER)
    pg_port = os.getenv("FLOWGENT_PG_PORT", "5432")
    pg_user = os.getenv("FLOWGENT_PG_USER", "test")
    pg_password = os.getenv("FLOWGENT_PG_PASSWORD", "test")
    pg_database = os.getenv("FLOWGENT_PG_DATABASE", "flowgent")
    pg_dsn = (
        f"postgres://{pg_user}:{pg_password}@{pg_host}:{pg_port}/{pg_database}"
        f"?sslmode=disable&options=-csearch_path%3D{config.PG_SCHEMA}"
    )
    mqtt_broker = f"tcp://{release}-emqx.{namespace}.svc.cluster.local:1883"
    api_url = f"http://{release}-apiserver.{namespace}.svc.cluster.local:9999"
    jaeger_endpoint = f"{release}-jaeger.{namespace}.svc.cluster.local:4318"
    return {
        "FLOWGENT__STORAGE__TYPE": "POSTGRE",
        "FLOWGENT__STORAGE__POSTGRES__DSN": pg_dsn,
        "FLOWGENT__STORAGE__POSTGRES__SCHEMA": config.PG_SCHEMA,
        "FLOWGENT__STORAGE__POSTGRES__HOST": pg_host,
        "FLOWGENT__STORAGE__POSTGRES__PORT": pg_port,
        "FLOWGENT__STORAGE__POSTGRES__USERNAME": pg_user,
        "FLOWGENT__STORAGE__POSTGRES__PASSWORD": pg_password,
        "FLOWGENT__STORAGE__POSTGRES__DATABASE": pg_database,
        "FLOWGENT__MESSAGER__TYPE": "mqtt",
        "FLOWGENT__MESSAGER__MQTT__BROKER": mqtt_broker,
        "FLOWGENT__RUNTIME__API_SERVER_URL": api_url,
        "FLOWGENT__RUNTIME__SYSTEM_NAMESPACE": namespace,
        "FLOWGENT__RUNTIME__K8S_NAMESPACE": namespace,
        "FLOWGENT__RUNTIME__NAMESPACE__DEFAULT_NAMESPACE": TENANT_NAMESPACE,
        "FLOWGENT__RUNTIME__NAMESPACE__NAMESPACE_PREFIX": WORKLOAD_NAMESPACE_PREFIX,
        "FLOWGENT__RUNTIME__RESOURCE_OWNER": config.RESOURCE_PREFIX,
        "FLOWGENT__RUNTIME__JM_IMAGE": FLOWGENT_IMAGE,
        "FLOWGENT__RUNTIME__TM_IMAGE": FLOWGENT_IMAGE,
        "FLOWGENT__SANDBOX__DEPLOYMENT__IMAGE": FLOWGENT_IMAGE,
        "FLOWGENT__MGMT__OTEL__ENDPOINT": jaeger_endpoint,
    }


def _set_runtime_env(namespace, release, deployments):
    env_map = _runtime_env(namespace, release)
    env_args = [f"{k}={v}" for k, v in env_map.items()]
    ok = True
    for deploy in deployments:
        rc, _ = _run_sensitive(
            ["kubectl", "set", "env", f"deployment/{deploy}", "-n", namespace, *env_args],
            f"kubectl set env deployment/{deploy} -n {namespace} <runtime env redacted>",
            timeout=60,
        )
        ok = ok and rc == 0
    return ok


def _service_cluster_ip(namespace, service):
    rc, out = _kubectl([
        "get", "svc", service, "-n", namespace,
        "-o", "jsonpath={.spec.clusterIP}",
    ], timeout=20)
    return out.strip() if rc == 0 else ""


def _workload_namespace():
    return f"{WORKLOAD_NAMESPACE_PREFIX}{TENANT_NAMESPACE}"


def _kubectl_jsonpath(jsonpath):
    result = subprocess.run(
        ["kubectl", "get", "nodes", "-o", f"jsonpath={jsonpath}"],
        capture_output=True,
        text=True,
        timeout=20,
    )
    if result.returncode != 0:
        return ""
    return result.stdout.strip()


def _node_internal_ip():
    return _kubectl_jsonpath('{.items[0].status.addresses[?(@.type=="InternalIP")].address}')


def _pod_proxy_host():
    override = os.getenv("FLOWGENT_E2E_POD_PROXY_HOST")
    if override:
        return override
    # A host proxy that listens beyond loopback is reachable through the
    # node's InternalIP.  The first PodCIDR address is commonly a CNI bridge,
    # but k3s/firewall rules do not guarantee that pods may connect back to it.
    node_ip = _node_internal_ip()
    if node_ip:
        return node_ip
    pod_cidr = _kubectl_jsonpath("{.items[0].spec.podCIDR}")
    if pod_cidr:
        try:
            network = ipaddress.ip_network(pod_cidr, strict=False)
            return str(next(network.hosts()))
        except Exception:
            pass
    return ""


def _rewrite_local_proxy_for_pod(value):
    if not value:
        return value
    parsed = urlsplit(value)
    host = (parsed.hostname or "").lower()
    if host not in ("localhost", "127.0.0.1", "::1"):
        return value
    pod_host = _pod_proxy_host()
    if not pod_host:
        return value
    userinfo = ""
    if parsed.username:
        userinfo = parsed.username
        if parsed.password:
            userinfo += f":{parsed.password}"
        userinfo += "@"
    port = f":{parsed.port}" if parsed.port else ""
    return urlunsplit((parsed.scheme, f"{userinfo}{pod_host}{port}", parsed.path, parsed.query, parsed.fragment))


def _proxy_allowlist_entry(proxy_data):
    for key in ("HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"):
        value = proxy_data.get(key)
        if not value:
            continue
        parsed = urlsplit(value)
        host = parsed.hostname
        if not host:
            continue
        port = parsed.port
        if not port:
            port = 443 if parsed.scheme == "https" else 80
        return f"{host}:{port}"
    return "github.com"


def configure_runtime_proxy_env():
    """Resolve pod-reachable proxy settings and their sandbox allowlist entry."""
    proxy_data = _runtime_proxy_data()
    os.environ[PROXY_ALLOWLIST_ENV] = _proxy_allowlist_entry(proxy_data)
    return proxy_data


def _merge_no_proxy(existing):
    merged = []
    seen = set()
    for item in (existing or "").split(","):
        value = item.strip()
        if value and value not in seen:
            merged.append(value)
            seen.add(value)
    for value in NO_PROXY_DEFAULTS:
        if value not in seen:
            merged.append(value)
            seen.add(value)
    return ",".join(merged)


def _runtime_proxy_data():
    data = {}
    for key in RUNTIME_PROXY_KEYS:
        value = os.environ.get(key)
        if value:
            data[key] = _rewrite_local_proxy_for_pod(value)

    for upper, lower in (("HTTP_PROXY", "http_proxy"), ("HTTPS_PROXY", "https_proxy"), ("ALL_PROXY", "all_proxy")):
        if upper in data and lower not in data:
            data[lower] = data[upper]
        if lower in data and upper not in data:
            data[upper] = data[lower]

    if data:
        no_proxy = _merge_no_proxy(os.environ.get("NO_PROXY") or os.environ.get("no_proxy") or "")
        data["NO_PROXY"] = no_proxy
        data["no_proxy"] = no_proxy
    return data


def _runtime_credential_data():
    data = {key: os.environ[key] for key in RUNTIME_CREDENTIAL_KEYS if os.environ.get(key)}
    if "GITHUB_TOKEN" not in data and data.get("GH_TOKEN"):
        data["GITHUB_TOKEN"] = data["GH_TOKEN"]
    if "DEEPSEEK_API_KEY_FLOWGENT" not in data and data.get("DEEPSEEK_API_KEY"):
        data["DEEPSEEK_API_KEY_FLOWGENT"] = data["DEEPSEEK_API_KEY"]
    if "DEEPSEEK_API_KEY" not in data and data.get("DEEPSEEK_API_KEY_FLOWGENT"):
        data["DEEPSEEK_API_KEY"] = data["DEEPSEEK_API_KEY_FLOWGENT"]
    proxy_data = configure_runtime_proxy_env()
    data.update(proxy_data)
    return data


def _ensure_runtime_credential_secret(namespaces):
    data = _runtime_credential_data()
    if not data:
        print(f"  WARN: no runtime credential env vars found; deleting stale {RUNTIME_CREDENTIAL_SECRET} secrets")
        for ns in namespaces:
            _kubectl(["delete", "secret", RUNTIME_CREDENTIAL_SECRET, "-n", ns, "--ignore-not-found=true"], timeout=20)
        return False

    encoded = {k: base64.b64encode(v.encode()).decode() for k, v in data.items()}
    ok = True
    for ns in namespaces:
        secret = {
            "apiVersion": "v1",
            "kind": "Secret",
            "metadata": {"name": RUNTIME_CREDENTIAL_SECRET, "namespace": ns},
            "type": "Opaque",
            "data": encoded,
        }
        result = subprocess.run(
            ["kubectl", "apply", "-f", "-"],
            input=json.dumps(secret),
            capture_output=True,
            text=True,
            timeout=30,
        )
        if result.returncode != 0:
            print(f"  ERROR: apply runtime credential secret failed for {ns}: {result.stderr[:200]}")
            ok = False
            continue
    if ok:
        print(f"  Runtime credential Secret ready: {RUNTIME_CREDENTIAL_SECRET} namespaces={','.join(namespaces)} keys={','.join(sorted(data))}")
        print(f"  Runtime proxy allowlist entry: {os.environ.get(PROXY_ALLOWLIST_ENV, 'github.com')}")
    return ok


def _ensure_notification_receiver(namespace):
    """Deploy a real authenticated HTTP receiver for notification delivery E2E."""
    token = os.environ.get(NOTIFICATION_TOKEN_ENV)
    if not token:
        token = secrets.token_urlsafe(32)
        os.environ[NOTIFICATION_TOKEN_ENV] = token
    server_script = r'''
import hmac, json, os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

expected = "Bearer " + os.environ["EXPECTED_TOKEN"]
receipts = []

class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(length)
        authorized = hmac.compare_digest(self.headers.get("Authorization", ""), expected)
        if authorized:
            receipts.append({"authorized": True, "bytes": len(body)})
        self.send_response(200 if authorized else 401)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps({"accepted": authorized}).encode())

    def do_GET(self):
        if self.path == "/healthz":
            payload, status = {"status": "ok"}, 200
        elif self.path == "/receipts":
            payload, status = {"authorized_count": len(receipts)}, 200
        else:
            payload, status = {"error": "not found"}, 404
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(payload).encode())

    def log_message(self, *_):
        return

ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
'''
    resources = {
        "apiVersion": "v1",
        "kind": "List",
        "items": [
            {
                "apiVersion": "v1",
                "kind": "Secret",
                "metadata": {"name": NOTIFICATION_RECEIVER, "namespace": namespace},
                "type": "Opaque",
                "stringData": {"token": token},
            },
            {
                "apiVersion": "apps/v1",
                "kind": "Deployment",
                "metadata": {"name": NOTIFICATION_RECEIVER, "namespace": namespace},
                "spec": {
                    "replicas": 1,
                    "selector": {"matchLabels": {"app": NOTIFICATION_RECEIVER}},
                    "template": {
                        "metadata": {"labels": {"app": NOTIFICATION_RECEIVER}},
                        "spec": {
                            "containers": [{
                                "name": "receiver",
                                "image": FLOWGENT_IMAGE,
                                "imagePullPolicy": "IfNotPresent",
                                "command": ["python3", "-c", server_script],
                                "env": [{
                                    "name": "EXPECTED_TOKEN",
                                    "valueFrom": {"secretKeyRef": {"name": NOTIFICATION_RECEIVER, "key": "token"}},
                                }],
                                "ports": [{"containerPort": 8080}],
                                "readinessProbe": {"httpGet": {"path": "/healthz", "port": 8080}},
                                "resources": {
                                    "requests": {"cpu": "10m", "memory": "24Mi"},
                                    "limits": {"cpu": "100m", "memory": "64Mi"},
                                },
                            }],
                        },
                    },
                },
            },
            {
                "apiVersion": "v1",
                "kind": "Service",
                "metadata": {"name": NOTIFICATION_RECEIVER, "namespace": namespace},
                "spec": {
                    "selector": {"app": NOTIFICATION_RECEIVER},
                    "ports": [{"name": "http", "port": 8080, "targetPort": 8080}],
                },
            },
        ],
    }
    result = subprocess.run(
        ["kubectl", "apply", "-f", "-"],
        input=json.dumps(resources), capture_output=True, text=True, timeout=60,
    )
    if result.returncode != 0:
        print(f"  ERROR: apply notification receiver failed: {result.stderr[:200]}")
        return False
    _kubectl(["rollout", "restart", f"deployment/{NOTIFICATION_RECEIVER}", "-n", namespace], timeout=30)
    rc, _ = _kubectl([
        "rollout", "status", f"deployment/{NOTIFICATION_RECEIVER}", "-n", namespace,
        "--timeout=120s",
    ], timeout=150)
    if rc == 0:
        print(f"  Authenticated notification receiver ready: {NOTIFICATION_RECEIVER}.{namespace}.svc.cluster.local:8080")
    return rc == 0


def _ensure_notification_channel(namespace, release):
    """Provision the encrypted channel used by the console-mode E2E."""
    token = os.environ.get(NOTIFICATION_TOKEN_ENV, "")
    api_token = os.environ.get(AUTH_TOKEN_ENV, "")
    if not token or not api_token:
        print("  ERROR: notification or API bootstrap token is unavailable")
        return False
    receiver_url = (
        f"http://{NOTIFICATION_RECEIVER}.{namespace}.svc.cluster.local:8080"
    )
    os.environ["FLOWGENT_E2E_NOTIFICATION_URL"] = receiver_url
    api_ip = _service_cluster_ip(namespace, f"{release}-apiserver")
    endpoint = f"http://{api_ip}:9999/api/v1/{TENANT_NAMESPACE}/notifications/channels"
    headers = {"Authorization": f"Bearer {api_token}", "Content-Type": "application/json"}
    session = requests.Session()
    session.trust_env = False
    try:
        response = session.get(endpoint, headers=headers, timeout=10)
        response.raise_for_status()
        for channel in response.json().get("items", []):
            if channel.get("name") == "security-autonomy-alerts" and channel.get("id"):
                session.delete(
                    f"{endpoint}/{channel['id']}", headers=headers, timeout=10,
                ).raise_for_status()
        response = session.post(
            endpoint,
            headers=headers,
            json={
                "name": "security-autonomy-alerts",
                "provider": "webhook",
                "enabled": True,
                "config": {
                    "url": receiver_url,
                    "headers": {"Authorization": f"Bearer {token}"},
                },
            },
            timeout=10,
        )
        if response.status_code != 201:
            print(f"  ERROR: notification channel create returned HTTP {response.status_code}")
            return False
    except requests.RequestException as exc:
        print(f"  ERROR: notification channel provisioning failed: {type(exc).__name__}")
        return False
    print("  Encrypted notification channel ready: security-autonomy-alerts")
    return True


def _wait_tcp(host, port, label, timeout=120):
    print(f"\n-- Waiting for {label} TCP readiness ({host}:{port}, timeout={timeout}s) --")
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            with socket.create_connection((host, int(port)), timeout=2):
                print(f"  {label} TCP ready: {host}:{port}")
                return True
        except OSError as exc:
            print(f"  {label} not accepting connections yet: {exc}")
            time.sleep(3)
    print(f"  ERROR: {label} did not accept TCP connections within {timeout}s")
    return False


def _ensure_workload_configmap(namespace, release):
    workload_ns = _workload_namespace()
    print(f"\n-- Ensuring workload namespace config ({workload_ns}) --")
    create_ns = subprocess.run(
        ["kubectl", "create", "namespace", workload_ns, "--dry-run=client", "-o", "json"],
        capture_output=True,
        text=True,
        timeout=20,
    )
    if create_ns.returncode != 0:
        print(f"  ERROR: render namespace failed: {create_ns.stderr[:200]}")
        return False
    apply_ns = subprocess.run(
        ["kubectl", "apply", "-f", "-"],
        input=create_ns.stdout,
        capture_output=True,
        text=True,
        timeout=30,
    )
    if apply_ns.returncode != 0:
        print(f"  ERROR: apply namespace failed: {apply_ns.stderr[:200]}")
        return False
    _kubectl([
        "label", "namespace", workload_ns,
        "flowgent.io/runtime-boundary=namespace",
        f"flowgent.io/namespace={TENANT_NAMESPACE}",
        "--overwrite",
    ], timeout=20)

    rc, cm_json = _run_sensitive(
        ["kubectl", "get", "configmap", f"{release}-config", "-n", namespace, "-o", "json"],
        f"kubectl get configmap {release}-config -n {namespace} -o json <output redacted>",
        timeout=20,
        print_stdout=False,
    )
    if rc != 0:
        return False
    cm = json.loads(cm_json)
    flowgent_yaml = yaml.safe_load(cm.get("data", {}).get("flowgent.yaml", "")) or {}
    flowgent_yaml.setdefault("mgmt", {}).setdefault("otel", {})["endpoint"] = (
        f"{release}-jaeger.{namespace}.svc.cluster.local:4318"
    )
    flowgent_yaml.setdefault("messager", {}).setdefault("mqtt", {})["broker"] = (
        f"tcp://{release}-emqx.{namespace}.svc.cluster.local:1883"
    )
    runtime = flowgent_yaml.setdefault("runtime", {})
    runtime["api_server_url"] = f"http://{release}-apiserver.{namespace}.svc.cluster.local:9999"
    runtime["system_namespace"] = namespace
    runtime["k8s_namespace"] = namespace
    runtime.setdefault("namespace", {})["default_namespace"] = TENANT_NAMESPACE
    runtime["jm_image"] = FLOWGENT_IMAGE
    runtime["tm_image"] = FLOWGENT_IMAGE
    sandbox_deploy = flowgent_yaml.setdefault("sandbox", {}).setdefault("deployment", {})
    sandbox_deploy["image"] = FLOWGENT_IMAGE
    base_labels = cm.get("metadata", {}).get("labels", {})

    for dest_ns in (namespace, workload_ns):
        dest_cm = copy.deepcopy(cm)
        dest_yaml = copy.deepcopy(flowgent_yaml)
        dest_runtime = dest_yaml.setdefault("runtime", {})
        dest_runtime["system_namespace"] = namespace
        dest_runtime["k8s_namespace"] = dest_ns
        dest_cm.setdefault("data", {})["flowgent.yaml"] = yaml.safe_dump(dest_yaml, sort_keys=False)
        dest_cm["metadata"] = {
            "name": f"{release}-config",
            "namespace": dest_ns,
            "labels": base_labels,
        }
        dest_cm.pop("status", None)
        apply_cm = subprocess.run(
            ["kubectl", "apply", "-f", "-"],
            input=json.dumps(dest_cm),
            capture_output=True,
            text=True,
            timeout=30,
        )
        if apply_cm.returncode != 0:
            print(f"  ERROR: apply configmap failed for {dest_ns}: {apply_cm.stderr[:200]}")
            return False
    print(f"  Workload config ready: {workload_ns}/{release}-config")
    return True


def helm_install_or_upgrade(release, namespace):
    print(f"\n-- Full redeploy Flowgent Helm chart --")
    print(f"  Release: {release}, Namespace: {namespace}")
    print(f"  Chart:   {HELM_CHART}")

    if not os.path.isdir(HELM_CHART):
        print(f"  ERROR: Helm chart not found: {HELM_CHART}")
        return False

    if not _uninstall_release(release, namespace):
        return False
    # Stop Helm-owned controllers before removing the runtime objects they
    # create. Otherwise a session JobManager can recreate a TaskManager in the
    # short window between cleanup and release removal, leaving an orphan that
    # consumes capacity and can deadlock the next rollout.
    _cleanup_runtime_resources(namespace)

    create_namespace = subprocess.run(
        ["kubectl", "create", "namespace", namespace, "--dry-run=client", "-o", "yaml"],
        capture_output=True,
        text=True,
        timeout=20,
    )
    if create_namespace.returncode != 0:
        return False
    namespace_apply = subprocess.run(
        ["kubectl", "apply", "-f", "-"],
        input=create_namespace.stdout,
        capture_output=True,
        text=True,
        timeout=30,
    )
    if namespace_apply.returncode != 0:
        return False

    if not _ensure_authorization_secret(namespace):
        return False

    # Teardown temporarily leaves the locally built image unreferenced. Under
    # disk pressure k3s can collect it before the replacement pods are created,
    # so refresh it at the deployment boundary rather than relying on cache
    # state from a prior round.
    from common.images import import_core_image_to_k3s, import_ui_image_to_k3s
    if not import_core_image_to_k3s():
        return False
    if not import_ui_image_to_k3s():
        return False

    runtime_env = _runtime_env(namespace, release)
    from deploy.authguard_e2e import prepare as prepare_authguard
    pg_host = _docker_container_ip(FLOWGENT_PG_CONTAINER)
    pg_port = os.getenv("FLOWGENT_PG_PORT", "5432")
    pg_user = os.getenv("FLOWGENT_PG_USER", "test")
    pg_password = os.getenv("FLOWGENT_PG_PASSWORD", "test")
    try:
        authguard_values = prepare_authguard(
            namespace, FLOWGENT_PG_CONTAINER, pg_host, pg_port, pg_user, pg_password
        )
    except Exception as exc:
        print(f"  ERROR: preparing AuthGuard E2E failed: {exc}")
        return False

    cmd = [
        "install", release, HELM_CHART,
        "-n", namespace, "--create-namespace", "--timeout", "300s",
        "--set", "global.image.repository=localhost/flowgent-core",
        "--set", "global.image.tag=latest",
        "--set", "global.image.pullPolicy=IfNotPresent",
        "--set", "ui.image.repository=localhost/flowgent-ui",
        "--set", "ui.image.tag=latest",
        "--set", "ui.image.pullPolicy=IfNotPresent",
        "--set", f"runtime.systemNamespace={namespace}",
        "--set", f"runtime.namespace.defaultNamespace={TENANT_NAMESPACE}",
        "--set", f"runtime.namespace.namespacePrefix={WORKLOAD_NAMESPACE_PREFIX}",
        "--set", f"runtime.resourceOwner={config.RESOURCE_PREFIX}",
        "--set", f"runtime.credentialEnvSecret={RUNTIME_CREDENTIAL_SECRET}",
        # This isolated profile coexists with the adjacent AuthGuard E2E on a
        # single 4-core k3s node.  Lower requests preserve scheduler headroom;
        # ordinary component limits remain unchanged.
        "--set-string", "apiserver.resources.requests.cpu=50m",
        "--set-string", "a2a.resources.requests.cpu=50m",
        "--set-string", "controller.resources.requests.cpu=50m",
        "--set-string", "notifier.resources.requests.cpu=50m",
        "--set-string", "jaeger.resources.requests.cpu=50m",
        "--set-string", "runtime.session.jobmanager.resources.requests.cpu=50m",
        "--set-string", "runtime.session.taskmanager.resources.limits.cpu=50m",
        "--set-string", "runtime.session.sandbox.resources.limits.cpu=50m",
        "--set", "sandbox.minReplicas=0",
        "--set", "storage.type=POSTGRE",
        "--set-string", f"storage.postgres.dsn={runtime_env['FLOWGENT__STORAGE__POSTGRES__DSN']}",
        "--set-string", f"storage.postgres.schema={config.PG_SCHEMA}",
        "--set", "postgresql.enabled=false",
        "--set", "emqx.enabled=true",
        "--set", "redis.enabled=false",
        "--set", "apiserver.replicas=1",
        "--set", "controller.replicas=1",
        "--set", "notifier.replicas=1",
        "--set", "a2a.enabled=true",
        "--set", "a2a.replicas=2",
        "--set", "apiserver.service.nodePorts.rest=31999",
        "--set", "apiserver.service.nodePorts.mgmt=31991",
        "--set", "a2a.service.nodePort=31992",
        "--set", "ui.service.nodePort=31080",
        "--set", f"controller.serviceAccount.name={config.RESOURCE_PREFIX}-controller",
        "--set-string", f"authorization.existingSecret={AUTH_SECRET}",
        "--set", "wallet.enabled=false",
    ]
    with tempfile.NamedTemporaryFile(mode="w", suffix=".json") as authguard_values_file:
        json.dump(authguard_values, authguard_values_file)
        authguard_values_file.flush()
        cmd.extend(["-f", authguard_values_file.name])
        rc, _ = _run_sensitive(
            ["helm"] + cmd,
            f"helm install {release} {HELM_CHART} -n {namespace} <sensitive values redacted>",
            timeout=600,
        )
    if rc != 0:
        return False
    # The apiserver initializes its lifecycle publisher once at startup.  Make
    # the broker ready before the runtime-env rollout starts the apiserver.
    rc, _ = _kubectl([
        "rollout", "status", f"deployment/{release}-emqx", "-n", namespace,
        "--timeout=300s",
    ], timeout=330)
    if rc != 0:
        return False
    emqx_ip = _service_cluster_ip(namespace, f"{release}-emqx")
    if not emqx_ip or not _wait_tcp(emqx_ip, 1883, "EMQX MQTT", timeout=120):
        return False
    print("\n-- Applying runtime env to API server --")
    if not _set_runtime_env(namespace, release, [f"{release}-apiserver"]):
        return False
    if not _wait_rollout(namespace, f"{release}-apiserver", DEFAULT_TIMEOUT):
        return False
    if not health_check(namespace, release):
        return False

    print("\n-- Applying runtime env to dependent Helm deployments --")
    if not _set_runtime_env(namespace, release, [
        f"{release}-controller",
        f"{release}-notifier",
        f"{release}-a2a",
        f"{release}-session-jobmanager",
    ]):
        return False
    if not _ensure_workload_configmap(namespace, release):
        return False
    if not _ensure_runtime_credential_secret([namespace, _workload_namespace()]):
        return False
    if not _ensure_notification_receiver(namespace):
        return False
    if not _ensure_notification_channel(namespace, release):
        return False
    if not wait_for_rollouts(namespace, release, DEFAULT_TIMEOUT):
        return False
    from deploy.authguard_e2e import bootstrap_and_verify
    try:
        bootstrap_and_verify(namespace)
    except Exception as exc:
        print(f"  ERROR: AuthGuard E2E verification failed: {exc}")
        return False
    return True


def _wait_rollout(namespace, deploy, timeout):
    rc, _ = _kubectl(["rollout", "status", f"deployment/{deploy}", "-n", namespace, f"--timeout={timeout}s"], timeout=timeout + 30)
    return rc == 0


def wait_for_rollouts(namespace, release, timeout):
    print(f"\n-- Waiting for deployment rollouts (timeout={timeout}s) --")
    ok = True
    for deploy in (
        f"{release}-apiserver",
        f"{release}-ui",
        f"{release}-controller",
        f"{release}-notifier",
        f"{release}-a2a",
        f"{release}-session-jobmanager",
        f"{release}-emqx",
        f"{release}-jaeger",
    ):
        ok = _wait_rollout(namespace, deploy, timeout) and ok
    return ok


def wait_for_pods(namespace, release, timeout):
    print(f"\n-- Waiting for pods (timeout={timeout}s) --")
    selector = f"app.kubernetes.io/instance={release}"
    deadline = time.time() + timeout

    while time.time() < deadline:
        result = subprocess.run(
            ["kubectl", "get", "pods", "-n", namespace, "-l", selector, "-o", "json"],
            capture_output=True,
            text=True,
            timeout=30,
        )
        if result.returncode != 0:
            print(f"  kubectl get pods failed, retrying...")
            time.sleep(10)
            continue

        try:
            pods = json.loads(result.stdout).get("items", [])
        except json.JSONDecodeError:
            print(f"  Failed to parse pod list, retrying...")
            time.sleep(10)
            continue

        if not pods:
            print(f"  No pods found yet with selector '{selector}'")
            time.sleep(10)
            continue

        all_ready = True
        for pod in pods:
            name = pod["metadata"]["name"]
            phase = pod.get("status", {}).get("phase", "Unknown")
            container_statuses = pod.get("status", {}).get("containerStatuses", [])
            ready = all(cs.get("ready", False) for cs in container_statuses)
            restarts = sum(cs.get("restartCount", 0) for cs in container_statuses)
            status = "OK" if ready else "NOT_READY"
            print(f"    {name:<40} phase={phase:<12} ready={status:<10} restarts={restarts}")
            if not ready and phase != "Succeeded":
                all_ready = False

        if all_ready:
            print(f"  All pods Ready.")
            return True

        time.sleep(10)

    print(f"  ERROR: Timed out waiting for pods after {timeout}s")
    return False


def health_check(namespace, release, timeout=60):
    print(f"\n-- Health check API server --")
    deadline = time.time() + timeout

    while time.time() < deadline:
        rc, out = run_cmd(
            ["kubectl", "get", "pods", "-n", namespace,
             "-l", f"app.kubernetes.io/instance={release},app.kubernetes.io/component=apiserver",
             "-o", "jsonpath={.items[0].metadata.name}"],
            timeout=15,
        )
        pod_name = out.strip()
        if not pod_name:
            print(f"  API server pod not found, retrying...")
            time.sleep(5)
            continue

        rc, _ = run_cmd(
            ["kubectl", "exec", "-n", namespace, pod_name, "--",
             "wget", "-q", "-O", "-", "http://localhost:9999/_/healthz"],
            timeout=15,
        )
        if rc == 0:
            print(f"  API server healthz: OK")
            return True
        print(f"  healthz returned {rc}, retrying...")
        time.sleep(5)

    print(f"  WARN: healthz check did not succeed within {timeout}s")
    return False


def main():
    parser = argparse.ArgumentParser(description="Deploy Flowgent via Helm")
    parser.add_argument("--namespace", "-n", default=DEFAULT_NAMESPACE)
    parser.add_argument("--release", "-r", default=DEFAULT_RELEASE)
    parser.add_argument("--timeout", "-t", type=int, default=DEFAULT_TIMEOUT)
    args = parser.parse_args()

    print("=" * 60)
    print("  Deploy S11: Flowgent Helm Deployment")
    print(f"  Chart:     {HELM_CHART}")
    print(f"  Release:   {args.release}")
    print(f"  Namespace: {args.namespace}")
    print("=" * 60)

    ok = True

    if not helm_install_or_upgrade(args.release, args.namespace):
        ok = False

    if ok and not wait_for_pods(args.namespace, args.release, args.timeout):
        ok = False

    if ok:
        health_check(args.namespace, args.release)

    print(f"\n{'=' * 60}")
    if ok:
        print("  Deploy S11 complete — Flowgent deployment ready.")
    else:
        print("  Deploy S11 finished with errors — check output above.")
    print(f"{'=' * 60}")

    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
