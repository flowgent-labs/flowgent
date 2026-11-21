"""Case constants and mutable client settings, owned by one definitions class."""

import os
from pathlib import Path


class E2EOtherDefinitions:
    RESOURCE_PREFIX = "e2e-autotest"
    K8S_APISERVER_URL = os.getenv("FLOWGENT_K8S_APISERVER", "http://localhost:9999")

    K8S_A2A_URL = os.getenv("FLOWGENT_K8S_A2A", "http://localhost:9992")

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

    K8S_NAMESPACE = os.getenv("FLOWGENT_K8S_NAMESPACE", f"{RESOURCE_PREFIX}-system")

    K8S_APP_NAMESPACE_PREFIX = os.getenv(
        "FLOWGENT_K8S_APP_NAMESPACE_PREFIX", "flowgent-"
    )

    PG_HOST = os.getenv("FLOWGENT_PG_HOST", "localhost")

    PG_PORT = int(os.getenv("FLOWGENT_PG_PORT", "5432"))

    PG_USER = os.getenv("FLOWGENT_PG_USER", "flowgent")

    PG_PASSWORD = os.getenv("FLOWGENT_PG_PASSWORD", "flowgent")

    PG_DATABASE = os.getenv("FLOWGENT_PG_DATABASE", "flowgent")

    PG_INTERNAL = os.getenv("FLOWGENT_PG_INTERNAL", "true") == "true"

    EMQX_HOST = os.getenv("FLOWGENT_EMQX_HOST", "localhost")

    EMQX_PORT = int(os.getenv("FLOWGENT_EMQX_PORT", "1883"))

    EMQX_DASHBOARD = int(os.getenv("FLOWGENT_EMQX_DASHBOARD", "18083"))

    JAEGER_UI_URL = os.getenv("FLOWGENT_JAEGER_UI", "http://localhost:16686")

    JAEGER_OTLP = os.getenv("FLOWGENT_JAEGER_OTLP", "http://localhost:4318")

    SONARQUBE_URL = os.getenv("FLOWGENT_SONARQUBE_URL", "http://localhost:9000")

    SONARQUBE_TOKEN = os.getenv("FLOWGENT_SONARQUBE_TOKEN", "")

    KUBECTL_BIN = os.getenv("KUBECTL_BIN", "kubectl")

    HELM_BIN = os.getenv("HELM_BIN", "helm")

    FLOW_TIMEOUT_S = int(os.getenv("FLOWGENT_FLOW_TIMEOUT_S", "300"))

    POLL_INTERVAL_S = int(os.getenv("FLOWGENT_POLL_INTERVAL_S", "5"))

    POD_READY_TIMEOUT_S = int(os.getenv("FLOWGENT_POD_READY_TIMEOUT_S", "60"))

    USE_CASE_DIR = Path(__file__).resolve().parents[1]

    E2E_DIR = USE_CASE_DIR

    PROJECT_ROOT = USE_CASE_DIR.parents[1]

    HELM_CHART = PROJECT_ROOT / "deploy/helm/flowgent"

    CONFIG_DIR = USE_CASE_DIR / "deploy" / "flowgent" / "config"

    CONSOLE_BIN = PROJECT_ROOT / "bin/flowgent-core"

    CONSOLE_CFG = PROJECT_ROOT / "etc/flowgent.yaml"

    REPORTS_DIR = USE_CASE_DIR / "reports"

    NAMESPACE_ID = os.getenv("FLOWGENT_NAMESPACE_ID", "default")
    PROMETHEUS_URL = os.getenv("FLOWGENT_E2E_PROMETHEUS_URL", "http://127.0.0.1:19092")
    SCENARIOS = {
        "01": (
            "AutoTest — Application Resource and Data-Scope Preflight",
            "verifier.infra.s01_preflight",
        ),
        "11": (
            "AutoTest — Infrastructure Readiness",
            "verifier.infra.s11_infrastructure",
        ),
        "13": ("AutoTest — Persisted Run Tracing", "verifier.otel.s13_tracing"),
        "51": (
            "AutoTest — Run-Scoped Prometheus Counters",
            "verifier.otel.s51_metrics",
        ),
        "52": ("AutoTest — Persisted Plan Lifecycle Logs", "verifier.otel.s52_logs"),
    }

    @staticmethod
    def postgres_dsn():
        return f"postgres://{E2EOtherDefinitions.PG_USER}:{E2EOtherDefinitions.PG_PASSWORD}@{E2EOtherDefinitions.PG_HOST}:{E2EOtherDefinitions.PG_PORT}/{E2EOtherDefinitions.PG_DATABASE}?sslmode=disable"

    @staticmethod
    def apply_postgres_override(dsn):
        from urllib.parse import unquote, urlsplit

        parsed = urlsplit(dsn)
        E2EOtherDefinitions.PG_USER = unquote(
            parsed.username or E2EOtherDefinitions.PG_USER
        )
        E2EOtherDefinitions.PG_PASSWORD = unquote(
            parsed.password or E2EOtherDefinitions.PG_PASSWORD
        )
        E2EOtherDefinitions.PG_HOST = parsed.hostname or E2EOtherDefinitions.PG_HOST
        E2EOtherDefinitions.PG_PORT = parsed.port or E2EOtherDefinitions.PG_PORT
        E2EOtherDefinitions.PG_DATABASE = (
            parsed.path.lstrip("/").split("?")[0] or E2EOtherDefinitions.PG_DATABASE
        )


os.environ.setdefault("KUBECONFIG", E2EOtherDefinitions.K8S_KUBECONFIG)
