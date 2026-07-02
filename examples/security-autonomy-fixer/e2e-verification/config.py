"""
Flowgent E2E Verification — Unified Configuration.

All verification scenarios read from this single config source.
Override via environment variables or by editing the defaults below.
"""

import os

# ── K3s / K8s API ───────────────────────────────────────────────
K3S_APISERVER_URL = os.getenv("FLOWGENT_K3S_APISERVER", "http://localhost:9999")
K3S_A2A_URL       = os.getenv("FLOWGENT_K3S_A2A",       "http://localhost:9992")
K3S_KUBECONFIG    = os.getenv("KUBECONFIG",              os.path.expanduser("~/.kube/config"))
K3S_NAMESPACE     = os.getenv("FLOWGENT_K3S_NAMESPACE",  "default")
K3S_TENANT        = os.getenv("FLOWGENT_K3S_TENANT",     "default")

# ── PostgreSQL ───────────────────────────────────────────────────
PG_HOST     = os.getenv("FLOWGENT_PG_HOST",     "localhost")
PG_PORT     = int(os.getenv("FLOWGENT_PG_PORT", "5432"))
PG_USER     = os.getenv("FLOWGENT_PG_USER",     "flowgent")
PG_PASSWORD = os.getenv("FLOWGENT_PG_PASSWORD", "flowgent")
PG_DATABASE = os.getenv("FLOWGENT_PG_DATABASE", "flowgent")
PG_INTERNAL = os.getenv("FLOWGENT_PG_INTERNAL", "true") == "true"   # in-cluster vs external

# ── EMQX MQTT ────────────────────────────────────────────────────
EMQX_HOST     = os.getenv("FLOWGENT_EMQX_HOST",     "localhost")
EMQX_PORT     = int(os.getenv("FLOWGENT_EMQX_PORT", "1883"))
EMQX_DASHBOARD = int(os.getenv("FLOWGENT_EMQX_DASHBOARD", "18083"))

# ── Jaeger / OTEL ────────────────────────────────────────────────
JAEGER_UI_URL  = os.getenv("FLOWGENT_JAEGER_UI",  "http://localhost:16686")
JAEGER_OTLP    = os.getenv("FLOWGENT_JAEGER_OTLP", "http://localhost:4318")

# ── SonarQube ────────────────────────────────────────────────────
SONARQUBE_URL   = os.getenv("FLOWGENT_SONARQUBE_URL",   "http://localhost:9000")
SONARQUBE_TOKEN = os.getenv("FLOWGENT_SONARQUBE_TOKEN", "")

# ── K3s / kubectl paths ──────────────────────────────────────────
KUBECTL_BIN = os.getenv("KUBECTL_BIN", "kubectl")
HELM_BIN    = os.getenv("HELM_BIN", "helm")

# ── Timeouts ─────────────────────────────────────────────────────
FLOW_TIMEOUT_S        = int(os.getenv("FLOWGENT_FLOW_TIMEOUT_S", "300"))
POLL_INTERVAL_S       = int(os.getenv("FLOWGENT_POLL_INTERVAL_S", "5"))
POD_READY_TIMEOUT_S   = int(os.getenv("FLOWGENT_POD_READY_TIMEOUT_S", "60"))

# ── Helper ───────────────────────────────────────────────────────
def pg_dsn():
    return f"postgres://{PG_USER}:{PG_PASSWORD}@{PG_HOST}:{PG_PORT}/{PG_DATABASE}?sslmode=disable"
