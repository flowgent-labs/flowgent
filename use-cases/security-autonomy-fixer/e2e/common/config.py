"""
Flowgent E2E Verification — Unified Configuration.

All verification scenarios read from this single config source.
Override via environment variables or by editing the defaults below.
"""

import os


def _is_kubeconfig_candidate(path: str) -> bool:
    return (
        bool(path)
        and os.path.isfile(path)
        and os.access(path, os.R_OK)
        and os.path.getsize(path) > 0
    )


def _default_kubeconfig() -> str:
    candidates = (
        os.getenv("KUBECONFIG", ""),
        os.path.expanduser("~/.kube/config"),
        "/etc/rancher/k3s/k3s.yaml",
    )
    for path in candidates:
        if _is_kubeconfig_candidate(path):
            return path
    return os.path.expanduser("~/.kube/config")


# ── Isolated deployment identity / local tunnels ─────────────────
RESOURCE_PREFIX = os.getenv("FLOWGENT_E2E_RESOURCE_PREFIX", "e2e-flowgent")
RELEASE_NAME = os.getenv("FLOWGENT_E2E_RELEASE", RESOURCE_PREFIX)
LOCAL_API_PORT = int(os.getenv("FLOWGENT_E2E_API_PORT", "29999"))
LOCAL_A2A_PORT = int(os.getenv("FLOWGENT_E2E_A2A_PORT", "19992"))
LOCAL_MQTT_PORT = int(os.getenv("FLOWGENT_E2E_MQTT_PORT", "11883"))
LOCAL_EMQX_DASHBOARD_PORT = int(os.getenv("FLOWGENT_E2E_EMQX_DASHBOARD_PORT", "18084"))
LOCAL_JAEGER_PORT = int(os.getenv("FLOWGENT_E2E_JAEGER_PORT", "16688"))
LOCAL_AUTHN_PORT = int(os.getenv("FLOWGENT_E2E_AUTHN_PORT", "18082"))
LOCAL_AUTHZ_MGMT_PORT = int(os.getenv("FLOWGENT_E2E_AUTHZ_MGMT_PORT", "19091"))
LOCAL_GATEWAY_PORT = int(os.getenv("FLOWGENT_E2E_GATEWAY_PORT", "18089"))

# ── K8S / K8s API ───────────────────────────────────────────────
K8S_APISERVER_URL = os.getenv("FLOWGENT_K8S_APISERVER", f"http://localhost:{LOCAL_API_PORT}")
K8S_A2A_URL       = os.getenv("FLOWGENT_K8S_A2A",       f"http://localhost:{LOCAL_A2A_PORT}")
K8S_KUBECONFIG    = _default_kubeconfig()
os.environ["KUBECONFIG"] = K8S_KUBECONFIG
SYSTEM_NAMESPACE  = os.getenv("FLOWGENT_SYSTEM_NAMESPACE", f"{RESOURCE_PREFIX}-system")
NAMESPACE_ID      = os.getenv("FLOWGENT_NAMESPACE_ID", "security-fixer")
K8S_NAMESPACE     = SYSTEM_NAMESPACE
# Must match runtime.namespace.namespace_prefix (etc/flowgent.yaml /
# helm values.yaml runtime.namespace.namespacePrefix, both default "flowgent-").
# This is the workload namespace prefix where application runtime clusters run:
# namespace = "{prefix}{namespace_id}" (per namespace, not per Flow), not the
# system namespace.
K8S_WORKLOAD_NAMESPACE_PREFIX = os.getenv(
    "FLOWGENT_K8S_WORKLOAD_NAMESPACE_PREFIX",
    f"{RESOURCE_PREFIX}-workload-",
)
K8S_WORKLOAD_NAMESPACE = f"{K8S_WORKLOAD_NAMESPACE_PREFIX}{NAMESPACE_ID}"

# ── PostgreSQL ───────────────────────────────────────────────────
PG_HOST     = os.getenv("FLOWGENT_PG_HOST",     "localhost")
PG_PORT     = int(os.getenv("FLOWGENT_PG_PORT", "5432"))
PG_USER     = os.getenv("FLOWGENT_PG_USER",     "test")
PG_PASSWORD = os.getenv("FLOWGENT_PG_PASSWORD", "test")
PG_DATABASE = os.getenv("FLOWGENT_PG_DATABASE", "flowgent")
PG_SCHEMA   = os.getenv("FLOWGENT_E2E_PG_SCHEMA", "e2e_flowgent")
PG_INTERNAL = os.getenv("FLOWGENT_PG_INTERNAL", "true") == "true"   # in-cluster vs external

# ── EMQX MQTT ────────────────────────────────────────────────────
EMQX_HOST     = os.getenv("FLOWGENT_EMQX_HOST",     "localhost")
EMQX_PORT     = int(os.getenv("FLOWGENT_EMQX_PORT", str(LOCAL_MQTT_PORT)))
EMQX_DASHBOARD = int(os.getenv("FLOWGENT_EMQX_DASHBOARD", str(LOCAL_EMQX_DASHBOARD_PORT)))

# ── Jaeger / OTEL ────────────────────────────────────────────────
JAEGER_UI_URL  = os.getenv("FLOWGENT_JAEGER_UI",  f"http://localhost:{LOCAL_JAEGER_PORT}")
JAEGER_OTLP    = os.getenv("FLOWGENT_JAEGER_OTLP", "http://localhost:4318")

# ── SonarQube ────────────────────────────────────────────────────
SONARQUBE_URL   = os.getenv("FLOWGENT_SONARQUBE_URL",   "http://localhost:9000")
SONARQUBE_TOKEN = os.getenv("FLOWGENT_SONARQUBE_TOKEN", "")

# ── K8S / kubectl paths ──────────────────────────────────────────
KUBECTL_BIN = os.getenv("KUBECTL_BIN", "kubectl")
HELM_BIN    = os.getenv("HELM_BIN", "helm")

# ── Timeouts ─────────────────────────────────────────────────────
FLOW_TIMEOUT_S        = int(os.getenv("FLOWGENT_FLOW_TIMEOUT_S", "300"))
POLL_INTERVAL_S       = int(os.getenv("FLOWGENT_POLL_INTERVAL_S", "5"))
POD_READY_TIMEOUT_S   = int(os.getenv("FLOWGENT_POD_READY_TIMEOUT_S", "60"))


def pg_dsn():
    return (
        f"postgres://{PG_USER}:{PG_PASSWORD}@{PG_HOST}:{PG_PORT}/{PG_DATABASE}"
        f"?sslmode=disable&options=-csearch_path%3D{PG_SCHEMA}"
    )


def apply_pg_override(dsn: str):
    """Apply a --pg DSN override onto module-level PG_* globals.

    Used by runner.py CLI to allow overriding
    PostgreSQL connection parameters at runtime.
    """
    import sys as _sys
    # Keep a reference to the config module so caller-site globals are patched.
    mod = _sys.modules[__name__]
    parts = dsn.replace("postgres://", "").split("@")
    user_pass = parts[0].split(":")
    host_db = parts[1].split("/")
    host_port = host_db[0].split(":")
    mod.PG_USER = user_pass[0]
    if len(user_pass) > 1:
        mod.PG_PASSWORD = user_pass[1]
    mod.PG_HOST = host_port[0]
    if len(host_port) > 1:
        mod.PG_PORT = int(host_port[1])
    mod.PG_DATABASE = host_db[1].split("?")[0]
