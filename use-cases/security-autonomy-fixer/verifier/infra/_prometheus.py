"""Configure and probe case telemetry through the formal Prometheus deployment."""

from __future__ import annotations

import time

import requests
import yaml
from util.constant import E2EOtherDefinitions as config
from util.images import E2EImagesDefinitions
from util.kubernetes import E2EKubernetesDeployer


class PrometheusRuntime:
    def __init__(self, context):
        self.context = context
        self.forward = None
        self.url = f"http://127.0.0.1:{config.LOCAL_PROMETHEUS_PORT}"
        self.session = requests.Session()
        self.session.trust_env = False

    def configuration(self) -> dict:
        if self.context.deployer == "docker":
            document = yaml.safe_load(
                (config.REPORTS_DIR / ".state/docker/flowgent.yaml").read_text()
            )
            job = {
                "job_name": "flowgent-e2e",
                "metrics_path": "/metrics",
                "static_configs": [
                    {"targets": [f"flowgent:{document['mgmt']['port']}"]}
                ],
            }
        else:
            cm = E2EKubernetesDeployer.resource(
                "configmap", self.context.namespace, self.context.release + "-config"
            )
            document = yaml.safe_load(cm["data"]["flowgent.yaml"])
            job = {
                "job_name": "flowgent-e2e",
                "metrics_path": "/metrics",
                "kubernetes_sd_configs": [
                    {
                        "role": "pod",
                        "namespaces": {
                            "names": [
                                self.context.namespace,
                                config.K8S_WORKLOAD_NAMESPACE,
                            ]
                        },
                    }
                ],
                "relabel_configs": [
                    {
                        "source_labels": ["__meta_kubernetes_pod_container_image"],
                        "action": "keep",
                        "regex": r"(.*/)?flowgent(:|@).*",
                    },
                    {
                        "source_labels": ["__meta_kubernetes_pod_ip"],
                        "target_label": "__address__",
                        "replacement": "${1}:" + str(document["mgmt"]["port"]),
                    },
                    {
                        "source_labels": ["__meta_kubernetes_pod_name"],
                        "target_label": "pod",
                    },
                    {
                        "source_labels": ["__meta_kubernetes_namespace"],
                        "target_label": "namespace",
                    },
                    {
                        "source_labels": [
                            "__meta_kubernetes_pod_label_flowgent_io_run"
                        ],
                        "target_label": "run_id",
                    },
                ],
            }
        if document.get("mgmt", {}).get("otel", {}).get("sample_rate") != 1.0:
            raise AssertionError("E2E tracing requires explicit 100% sampling")
        return {
            "global": {"scrape_interval": "1s", "evaluation_interval": "1s"},
            "scrape_configs": [job],
        }

    def deploy(self) -> None:
        document = yaml.safe_dump(self.configuration(), sort_keys=False)
        image = E2EImagesDefinitions.get("prometheus")
        cached = E2EImagesDefinitions.prepare_runtime(image)
        if cached and self.context.deployer == "k8s":
            from util.kubernetes import KubernetesLocalImageLoader

            KubernetesLocalImageLoader.detect().import_image(image)
        if self.context.deployer == "docker":
            directory = config.REPORTS_DIR / ".state/prometheus"
            directory.mkdir(parents=True, exist_ok=True)
            (directory / "prometheus.yaml").write_text(document)
        else:
            values = {
                "name": "prometheus",
                "watchedNamespaces": [
                    self.context.namespace,
                    config.K8S_WORKLOAD_NAMESPACE,
                ],
                "namespace": self.context.namespace,
                "image": E2EImagesDefinitions.get("prometheus"),
                "labels": {"flowgent.io/e2e-owner": config.RESOURCE_PREFIX},
                "resources": {
                    "requests": {"cpu": "25m", "memory": "64Mi"},
                    "limits": {"cpu": "250m", "memory": "256Mi"},
                },
            }
            E2EKubernetesDeployer.apply(
                E2EKubernetesDeployer.helm_template(
                    config.HELM_CHART,
                    {
                        "prometheus": {
                            **values,
                            "enabled": True,
                            "configuration": document,
                        }
                    },
                    template="prometheus.yaml",
                )
            )
            E2EKubernetesDeployer.rollout(self.context.namespace, "prometheus")
            forward = E2EKubernetesDeployer.port_forward(
                self.context.namespace, "service/prometheus", 9090
            )
            port = forward.__enter__()
            self.forward = forward
            self.url = f"http://127.0.0.1:{port}"
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            try:
                if (
                    self.session.get(self.url + "/-/ready", timeout=2).status_code
                    == 200
                ):
                    return
            except requests.RequestException:
                pass
            time.sleep(1)
        self.stop()
        raise AssertionError("Prometheus did not become ready")

    def query(self, expression: str) -> list[dict]:
        response = self.session.get(
            self.url + "/api/v1/query", params={"query": expression}, timeout=10
        )
        response.raise_for_status()
        document = response.json()
        if (
            document.get("status") != "success"
            or document.get("data", {}).get("resultType") != "vector"
        ):
            raise AssertionError(
                "Prometheus did not return a successful instant-vector query"
            )
        return document["data"]["result"]

    def stop(self) -> None:
        if self.forward is not None:
            self.forward.__exit__(None, None, None)
            self.forward = None
