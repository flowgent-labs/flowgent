"""Static Flowgent E2E configuration, paths, ports, and scenario registry."""
from __future__ import annotations

import os
from pathlib import Path


E2E_DIR = Path(__file__).resolve().parents[1]
USE_CASE_DIR = E2E_DIR.parent
PROJECT_ROOT = USE_CASE_DIR.parents[1]
HELM_CHART = PROJECT_ROOT / "deploy" / "helm" / "flowgent"
SONAR_COMPOSE = PROJECT_ROOT / "deploy" / "docker" / "sonarqube" / "docker-compose.yml"
CONFIG_DIR = E2E_DIR / "config"
CONSOLE_BIN = PROJECT_ROOT / "bin" / "flowgent-core"
CONSOLE_CFG = PROJECT_ROOT / "etc" / "flowgent.yaml"
REPORTS_DIR = E2E_DIR / "reports"


SCENARIOS = {
    "01": ("E2E Structure — Layered Deployment and Verifier Contracts", "verifier.infra.s01_structure"),
    "11": ("Infrastructure — Pre-Deployment & Pod Readiness", "verifier.infra.s11_infrastructure"),
    "12": ("Console Import — Binary, Import Command, DB Verification", "verifier.infra.s12_console_import"),
    "13": ("OTEL — Jaeger Span Coverage", "verifier.infra.s13_otel"),
    "14": ("AuthGuard — LDAP Federation + GitHub OAuth + Envoy Policy", "verifier.infra.s14_authguard"),
    "41": ("Web Console — Flow CRUD & Run Lifecycle", "verifier.web.s41_flow_crud"),
    "42": ("Web Console — Agent CRUD and Revision", "verifier.web.s42_agent_crud"),
    "43": ("Web Console — Skill CRUD and Workspace Uploads", "verifier.web.s43_skill_crud"),
    "44": ("Web Console — MCP and LLM CRUD", "verifier.web.s44_integrations_crud"),
    "45": ("Web Console — Published Knowledge and Notification CRUD", "verifier.web.s45_knowledge_notifications"),
    "46": ("Hosted Login — OAuth Session and Business Sign-out", "verifier.web.s46_hosted_login"),
    "31": ("E2E Fixer — Seed & Trigger", "verifier.agentflow.s31_seed_trigger"),
    "32": ("E2E Fixer — Discovery & Analyze", "verifier.agentflow.s32_discovery_analyze"),
    "33": ("E2E Fixer — Remediation", "verifier.agentflow.s33_remediation"),
    "34": ("E2E Fixer — Delivery & Report", "verifier.agentflow.s34_delivery_report"),
    "35": ("PR Commits — Verify Fix Commits on Target PR", "verifier.agentflow.s35_pr_commit"),
    "36": ("Knowledge — Approved Publication and Scoped Retrieval", "verifier.agentflow.s36_knowledge"),
    "37": ("Volume Workspace — Pod Mount and Git Clone Evidence", "verifier.agentflow.s37_volume_workspace"),
    "21": ("API Server — Canonical CRUD, Revisions, Run State, and Publication", "verifier.core.s21_apiserver"),
    "22": ("Notifier — Multi-Channel Delivery", "verifier.core.s22_notifier"),
    "23": ("Controller — Application Runtime Cluster Lifecycle", "verifier.core.s23_controller"),
    "24": ("Messager — MQTT Topics + Sandbox Chain", "verifier.core.s24_messager"),
    "25": ("A2A Protocol — Agent Card & Task Submit", "verifier.core.s25_a2a_protocol"),
}
DEFAULT_SCENARIOS = tuple(SCENARIOS)


class KubernetesConfiguration:
    """Class-owned operations for config."""

    @staticmethod
    def _is_kubeconfig_candidate(path: str) -> bool:
        return (
            bool(path)
            and os.path.isfile(path)
            and os.access(path, os.R_OK)
            and os.path.getsize(path) > 0
        )

    @staticmethod
    def _default_kubeconfig() -> str:
        candidates = (
            os.getenv("KUBECONFIG", ""),
            os.path.expanduser("~/.kube/config"),
            "/etc/rancher/k3s/k3s.yaml",
        )
        for path in candidates:
            if KubernetesConfiguration._is_kubeconfig_candidate(path):
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
LOCAL_AUTHN_MGMT_PORT = int(os.getenv("FLOWGENT_E2E_AUTHN_MGMT_PORT", "18083"))
LOCAL_AUTHZ_API_PORT = int(os.getenv("FLOWGENT_E2E_AUTHZ_API_PORT", "19090"))
LOCAL_AUTHZ_MGMT_PORT = int(os.getenv("FLOWGENT_E2E_AUTHZ_MGMT_PORT", "19091"))
LOCAL_GATEWAY_PORT = int(os.getenv("FLOWGENT_E2E_GATEWAY_PORT", "18089"))
LOCAL_MOCK_GITHUB_PORT = int(os.getenv("FLOWGENT_E2E_MOCK_GITHUB_PORT", "18087"))
LOCAL_PG_PORT = int(os.getenv("FLOWGENT_E2E_PG_PORT", "25432"))

# ── K8S / K8s API ───────────────────────────────────────────────
K8S_APISERVER_URL = os.getenv("FLOWGENT_K8S_APISERVER", f"http://localhost:{LOCAL_API_PORT}")
K8S_A2A_URL       = os.getenv("FLOWGENT_K8S_A2A",       f"http://localhost:{LOCAL_A2A_PORT}")
K8S_KUBECONFIG    = KubernetesConfiguration._default_kubeconfig()
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


class E2EConfiguration:
    """Own mutable runtime configuration without spreading DSN parsing logic."""

    @staticmethod
    def postgres_dsn() -> str:
        return (
            f"postgres://{PG_USER}:{PG_PASSWORD}@{PG_HOST}:{PG_PORT}/{PG_DATABASE}"
            f"?sslmode=disable&options=-csearch_path%3D{PG_SCHEMA}"
        )

    @staticmethod
    def apply_postgres_override(dsn: str) -> None:
        """Apply a CLI DSN override to the module's canonical PG fields."""
        import sys as _sys

        module = _sys.modules[__name__]
        parts = dsn.replace("postgres://", "").split("@")
        user_pass = parts[0].split(":")
        host_db = parts[1].split("/")
        host_port = host_db[0].split(":")
        module.PG_USER = user_pass[0]
        if len(user_pass) > 1:
            module.PG_PASSWORD = user_pass[1]
        module.PG_HOST = host_port[0]
        if len(host_port) > 1:
            module.PG_PORT = int(host_port[1])
        module.PG_DATABASE = host_db[1].split("?")[0]
