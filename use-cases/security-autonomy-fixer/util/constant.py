"""Case constants and mutable client settings, owned by one definitions class."""

from __future__ import annotations

import os
from pathlib import Path


class E2EOtherDefinitions:
    E2E_DIR = Path(__file__).resolve().parents[1]

    USE_CASE_DIR = E2E_DIR

    PROJECT_ROOT = USE_CASE_DIR.parents[1]

    HELM_CHART = PROJECT_ROOT / "deploy" / "helm" / "flowgent"

    CONFIG_DIR = E2E_DIR / "deploy" / "flowgent" / "config"

    CONSOLE_BIN = PROJECT_ROOT / "bin" / "flowgent-core"

    CONSOLE_CFG = PROJECT_ROOT / "etc" / "flowgent.yaml"

    REPORTS_DIR = E2E_DIR / "reports"
    LOCAL_PROMETHEUS_PORT = int(os.getenv("FLOWGENT_E2E_PROMETHEUS_PORT", "19092"))
    LOCAL_SCANNER_PORT = int(os.getenv("FLOWGENT_E2E_SCANNER_PORT", "39093"))

    SCENARIOS = {
        "63": ("Checkpoint — Workspace rollback, retry and committed-state recovery", "verifier.agentgraph.s63_checkpoint_workspace"),
        "61": ("SubGraph — Static, Adaptive, CAS and Seal", "verifier.agentgraph.s61_dynamic_sub_graph"),
        "62": ("Durable execution — Claims, immutable results and Outbox", "verifier.agentgraph.s62_durable_delivery"),
        "01": (
            "E2E Structure — Layered Deployment and Verifier Contracts",
            "verifier.infra.s01_structure",
        ),
        "11": (
            "Infrastructure — Pre-Deployment & Pod Readiness",
            "verifier.infra.s11_infrastructure",
        ),
        "12": (
            "Console Import — Binary, Import Command, DB Verification",
            "verifier.infra.s12_console_import",
        ),
        "14": (
            "AuthGuard — LDAP Federation + GitHub OAuth + Envoy Policy",
            "verifier.infra.s14_authguard",
        ),
        "41": ("Web Console — AgentGraph CRUD & Run Lifecycle", "verifier.web.s41_agent_graph_crud"),
        "42": ("Web Console — Agent CRUD and Revision", "verifier.web.s42_agent_crud"),
        "43": (
            "Web Console — Skill CRUD and Workspace Uploads",
            "verifier.web.s43_skill_crud",
        ),
        "44": ("Web Console — MCP and LLM CRUD", "verifier.web.s44_integrations_crud"),
        "45": (
            "Web Console — Published Knowledge and Notification CRUD",
            "verifier.web.s45_knowledge_notifications",
        ),
        "46": (
            "Hosted Login — OAuth Session and Business Sign-out",
            "verifier.web.s46_hosted_login",
        ),
        "31": ("E2E Fixer — Seed & Trigger", "verifier.agentgraph.s31_seed_trigger"),
        "32": (
            "E2E Fixer — Discovery & Analyze",
            "verifier.agentgraph.s32_discovery_analyze",
        ),
        "33": ("E2E Fixer — Remediation", "verifier.agentgraph.s33_remediation"),
        "34": (
            "E2E Fixer — Delivery & Report",
            "verifier.agentgraph.s34_delivery_report",
        ),
        "35": (
            "PR Commits — Verify Fix Commits on Target PR",
            "verifier.agentgraph.s35_pr_commit",
        ),
        "36": (
            "Knowledge — Approved Publication and Scoped Retrieval",
            "verifier.agentgraph.s36_knowledge",
        ),
        "37": (
            "Volume Workspace — Pod Mount and Git Clone Evidence",
            "verifier.agentgraph.s37_volume_workspace",
        ),
        "21": (
            "API Server — Canonical CRUD, Revisions, Run State, and Publication",
            "verifier.core.s21_apiserver",
        ),
        "22": ("Notifier — Multi-Channel Delivery", "verifier.core.s22_notifier"),
        "23": (
            "Controller — Application Runtime Cluster Lifecycle",
            "verifier.core.s23_controller",
        ),
        "24": ("Messager — MQTT Topics + Sandbox Chain", "verifier.core.s24_messager"),
        "25": (
            "A2A Protocol — Agent Card & Task Submit",
            "verifier.core.s25_a2a_protocol",
        ),
    }

    SCENARIOS.update(
        {
            "13": (
                "OTEL — UI Run and Persisted Attempt Tracing",
                "verifier.otel.s13_tracing",
            ),
            "51": ("OTEL — UI Run Prometheus Counters", "verifier.otel.s51_metrics"),
            "52": ("OTEL — UI Execution Lifecycle Logs", "verifier.otel.s52_logs"),
        }
    )
    DEFAULT_SCENARIOS = tuple(SCENARIOS)

    RESOURCE_PREFIX = os.getenv("FLOWGENT_E2E_RESOURCE_PREFIX", "e2e-flowgent")

    RELEASE_NAME = os.getenv("FLOWGENT_E2E_RELEASE", "flowgent")

    AUTHGUARD_RELEASE_NAME = os.getenv(
        "FLOWGENT_E2E_AUTHGUARD_RELEASE", f"{RESOURCE_PREFIX}-authguard"
    )

    LOCAL_API_PORT = int(os.getenv("FLOWGENT_E2E_API_PORT", "29999"))

    LOCAL_A2A_PORT = int(os.getenv("FLOWGENT_E2E_A2A_PORT", "19992"))

    LOCAL_MQTT_PORT = int(os.getenv("FLOWGENT_E2E_MQTT_PORT", "11883"))

    LOCAL_EMQX_DASHBOARD_PORT = int(
        os.getenv("FLOWGENT_E2E_EMQX_DASHBOARD_PORT", "18084")
    )

    LOCAL_JAEGER_PORT = int(os.getenv("FLOWGENT_E2E_JAEGER_PORT", "16688"))

    LOCAL_AUTHN_PORT = int(os.getenv("FLOWGENT_E2E_AUTHN_PORT", "18082"))

    LOCAL_AUTHN_MGMT_PORT = int(os.getenv("FLOWGENT_E2E_AUTHN_MGMT_PORT", "18083"))

    LOCAL_AUTHZ_API_PORT = int(os.getenv("FLOWGENT_E2E_AUTHZ_API_PORT", "19090"))

    LOCAL_AUTHZ_MGMT_PORT = int(os.getenv("FLOWGENT_E2E_AUTHZ_MGMT_PORT", "19091"))

    LOCAL_GATEWAY_PORT = int(os.getenv("FLOWGENT_E2E_GATEWAY_PORT", "18089"))

    LOCAL_MOCK_GITHUB_PORT = int(os.getenv("FLOWGENT_E2E_MOCK_GITHUB_PORT", "18087"))

    LOCAL_PG_PORT = int(os.getenv("FLOWGENT_E2E_PG_PORT", "25432"))

    PG_NODE_PORT = int(os.getenv("FLOWGENT_E2E_PG_NODE_PORT", "32432"))

    SONARQUBE_NODE_PORT = int(os.getenv("FLOWGENT_E2E_SONARQUBE_NODE_PORT", "32000"))

    SONARQUBE_MCP_NODE_PORT = int(
        os.getenv("FLOWGENT_E2E_SONARQUBE_MCP_NODE_PORT", "32080")
    )

    K8S_APISERVER_URL = os.getenv(
        "FLOWGENT_K8S_APISERVER", f"http://localhost:{LOCAL_API_PORT}"
    )

    K8S_A2A_URL = os.getenv("FLOWGENT_K8S_A2A", f"http://localhost:{LOCAL_A2A_PORT}")

    K8S_KUBECONFIG = next(
        (
            path
            for path in (
                os.getenv("KUBECONFIG", ""),
                os.path.expanduser("~/.kube/config"),
                "/etc/rancher/k3s/k3s.yaml",
            )
            if path and (os.pathsep in path or os.path.isfile(path))
        ),
        os.path.expanduser("~/.kube/config"),
    )

    os.environ["KUBECONFIG"] = K8S_KUBECONFIG

    SYSTEM_NAMESPACE = os.getenv(
        "FLOWGENT_SYSTEM_NAMESPACE", f"{RESOURCE_PREFIX}-system"
    )

    AUTHGUARD_NAMESPACE = os.getenv(
        "FLOWGENT_AUTHGUARD_NAMESPACE", f"{RESOURCE_PREFIX}-authguard-system"
    )

    MOCK_GITHUB_NAMESPACE = os.getenv(
        "FLOWGENT_MOCK_GITHUB_NAMESPACE", f"{RESOURCE_PREFIX}-mock-github"
    )

    MOCK_NOTIFICATION_NAMESPACE = os.getenv(
        "FLOWGENT_MOCK_NOTIFICATION_NAMESPACE", f"{RESOURCE_PREFIX}-mock-notification"
    )

    MOCK_LLM_NAMESPACE = os.getenv(
        "FLOWGENT_MOCK_LLM_NAMESPACE", f"{RESOURCE_PREFIX}-mock-llm"
    )

    STORAGE_NAMESPACE = os.getenv(
        "FLOWGENT_STORAGE_NAMESPACE", f"{RESOURCE_PREFIX}-storage-system"
    )

    SONARQUBE_NAMESPACE = os.getenv(
        "FLOWGENT_SONARQUBE_NAMESPACE", f"{RESOURCE_PREFIX}-sonarqube-system"
    )

    NAMESPACE_ID = os.getenv("FLOWGENT_NAMESPACE_ID", "security-fixer")

    K8S_NAMESPACE = SYSTEM_NAMESPACE

    K8S_WORKLOAD_NAMESPACE_PREFIX = os.getenv(
        "FLOWGENT_K8S_WORKLOAD_NAMESPACE_PREFIX",
        f"{RESOURCE_PREFIX}-workload-",
    )

    K8S_WORKLOAD_NAMESPACE = f"{K8S_WORKLOAD_NAMESPACE_PREFIX}{NAMESPACE_ID}"

    K8S_MANAGED_NAMESPACES = (
        SYSTEM_NAMESPACE,
        AUTHGUARD_NAMESPACE,
        MOCK_GITHUB_NAMESPACE,
        MOCK_NOTIFICATION_NAMESPACE,
        MOCK_LLM_NAMESPACE,
        STORAGE_NAMESPACE,
        SONARQUBE_NAMESPACE,
        K8S_WORKLOAD_NAMESPACE,
    )

    PG_HOST = os.getenv("FLOWGENT_PG_HOST", "localhost")

    PG_PORT = int(os.getenv("FLOWGENT_PG_PORT", "5432"))

    PG_USER = os.getenv("FLOWGENT_PG_USER", "test")

    PG_PASSWORD = os.getenv("FLOWGENT_PG_PASSWORD", "test")

    PG_DATABASE = os.getenv("FLOWGENT_PG_DATABASE", "flowgent")

    PG_SCHEMA = os.getenv("FLOWGENT_E2E_PG_SCHEMA", "e2e_flowgent")

    PG_INTERNAL = os.getenv("FLOWGENT_PG_INTERNAL", "true") == "true"

    EMQX_HOST = os.getenv("FLOWGENT_EMQX_HOST", "localhost")

    EMQX_PORT = int(os.getenv("FLOWGENT_EMQX_PORT", str(LOCAL_MQTT_PORT)))

    EMQX_DASHBOARD = int(
        os.getenv("FLOWGENT_EMQX_DASHBOARD", str(LOCAL_EMQX_DASHBOARD_PORT))
    )

    JAEGER_UI_URL = os.getenv(
        "FLOWGENT_JAEGER_UI", f"http://localhost:{LOCAL_JAEGER_PORT}"
    )

    JAEGER_OTLP = os.getenv("FLOWGENT_JAEGER_OTLP", "http://localhost:4318")

    SONARQUBE_URL = os.getenv("FLOWGENT_SONARQUBE_URL", "http://localhost:9000")

    SONARQUBE_TOKEN = os.getenv("FLOWGENT_SONARQUBE_TOKEN", "")

    K8S_SONARQUBE_URL = os.getenv(
        "FLOWGENT_K8S_SONARQUBE_URL", f"http://127.0.0.1:{SONARQUBE_NODE_PORT}"
    )

    K8S_SONARQUBE_MCP_URL = os.getenv(
        "FLOWGENT_K8S_SONARQUBE_MCP_URL",
        f"http://127.0.0.1:{SONARQUBE_MCP_NODE_PORT}/mcp",
    )

    KUBECTL_BIN = os.getenv("KUBECTL_BIN", "kubectl")

    HELM_BIN = os.getenv("HELM_BIN", "helm")

    FLOW_TIMEOUT_S = int(os.getenv("FLOWGENT_FLOW_TIMEOUT_S", "300"))

    POLL_INTERVAL_S = int(os.getenv("FLOWGENT_POLL_INTERVAL_S", "5"))

    POD_READY_TIMEOUT_S = int(os.getenv("FLOWGENT_POD_READY_TIMEOUT_S", "60"))

    @staticmethod
    def postgres_dsn() -> str:
        return (
            f"postgres://{E2EOtherDefinitions.PG_USER}:{E2EOtherDefinitions.PG_PASSWORD}@{E2EOtherDefinitions.PG_HOST}:{E2EOtherDefinitions.PG_PORT}/{E2EOtherDefinitions.PG_DATABASE}"
            f"?sslmode=disable&options=-csearch_path%3D{E2EOtherDefinitions.PG_SCHEMA}"
        )

    @staticmethod
    def apply_postgres_override(dsn: str) -> None:
        """Apply a CLI DSN override to the module's canonical PG fields."""

        module = E2EOtherDefinitions
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


os.environ.setdefault("KUBECONFIG", E2EOtherDefinitions.K8S_KUBECONFIG)
