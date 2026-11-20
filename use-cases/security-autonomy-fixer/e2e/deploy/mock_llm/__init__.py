"""Kubernetes lifecycle for the isolated deterministic LLM fixture."""

from __future__ import annotations

import json
from pathlib import Path
import subprocess

from common import config
from common.model import RunContext
from deploy import BaseDeployer


MOCK_LLM_NAME = "mock-llm"
MOCK_LLM_IMAGE = "localhost/flowgent:latest"
MOCK_LLM_SCRIPT = (
    Path(__file__).resolve().parents[1]
    / "mock-llm-service"
    / "app"
    / "server.py"
)


class MockLLMRuntime:
    """Reconcile and probe one explicitly named OpenAI-compatible fixture."""

    @staticmethod
    def service_fqdn() -> str:
        return f"{MOCK_LLM_NAME}.{config.MOCK_LLM_NAMESPACE}.svc.cluster.local"

    @staticmethod
    def _apply(manifest: dict[str, object]) -> None:
        result = subprocess.run(
            ["kubectl", "apply", "-f", "-"],
            input=json.dumps(manifest),
            capture_output=True,
            text=True,
            timeout=90,
        )
        if result.returncode != 0:
            raise RuntimeError(f"apply mock LLM resources failed: {result.stderr[-600:]}")

    @staticmethod
    def deploy(namespace: str = config.MOCK_LLM_NAMESPACE) -> None:
        labels = {
            "app.kubernetes.io/name": MOCK_LLM_NAME,
            "app.kubernetes.io/instance": MOCK_LLM_NAME,
            "app.kubernetes.io/component": "llm-fixture",
            "app.kubernetes.io/part-of": config.RESOURCE_PREFIX,
            "flowgent.io/e2e-owner": config.RESOURCE_PREFIX,
        }
        MockLLMRuntime._apply(
            {
                "apiVersion": "v1",
                "kind": "Namespace",
                "metadata": {
                    "name": namespace,
                    "labels": {"flowgent.io/e2e-owner": config.RESOURCE_PREFIX},
                },
            }
        )
        MockLLMRuntime._apply(
            {
                "apiVersion": "v1",
                "kind": "List",
                "items": [
                    {
                        "apiVersion": "v1",
                        "kind": "ConfigMap",
                        "metadata": {
                            "name": f"{MOCK_LLM_NAME}-script",
                            "namespace": namespace,
                            "labels": labels,
                        },
                        "data": {
                            "server.py": MOCK_LLM_SCRIPT.read_text(encoding="utf-8")
                        },
                    },
                    {
                        "apiVersion": "apps/v1",
                        "kind": "Deployment",
                        "metadata": {
                            "name": MOCK_LLM_NAME,
                            "namespace": namespace,
                            "labels": labels,
                        },
                        "spec": {
                            "replicas": 1,
                            "selector": {
                                "matchLabels": {"app.kubernetes.io/instance": MOCK_LLM_NAME}
                            },
                            "template": {
                                "metadata": {"labels": labels},
                                "spec": {
                                    "volumes": [
                                        {
                                            "name": "script",
                                            "configMap": {
                                                "name": f"{MOCK_LLM_NAME}-script"
                                            },
                                        }
                                    ],
                                    "containers": [
                                        {
                                            "name": MOCK_LLM_NAME,
                                            "image": MOCK_LLM_IMAGE,
                                            "imagePullPolicy": "IfNotPresent",
                                            "command": ["python3", "/opt/e2e/server.py"],
                                            "ports": [
                                                {"name": "http", "containerPort": 8080}
                                            ],
                                            "volumeMounts": [
                                                {
                                                    "name": "script",
                                                    "mountPath": "/opt/e2e/server.py",
                                                    "subPath": "server.py",
                                                    "readOnly": True,
                                                }
                                            ],
                                            "readinessProbe": {
                                                "httpGet": {
                                                    "path": "/healthz",
                                                    "port": "http",
                                                },
                                                "periodSeconds": 3,
                                            },
                                            "resources": {
                                                "requests": {
                                                    "cpu": "10m",
                                                    "memory": "24Mi",
                                                },
                                                "limits": {
                                                    "cpu": "100m",
                                                    "memory": "64Mi",
                                                },
                                            },
                                        }
                                    ],
                                },
                            },
                        },
                    },
                    {
                        "apiVersion": "v1",
                        "kind": "Service",
                        "metadata": {
                            "name": MOCK_LLM_NAME,
                            "namespace": namespace,
                            "labels": labels,
                        },
                        "spec": {
                            "selector": {"app.kubernetes.io/instance": MOCK_LLM_NAME},
                            "ports": [
                                {"name": "http", "port": 8080, "targetPort": "http"}
                            ],
                        },
                    },
                ],
            }
        )
        result = subprocess.run(
            [
                "kubectl",
                "rollout",
                "status",
                f"deployment/{MOCK_LLM_NAME}",
                "-n",
                namespace,
                "--timeout=180s",
            ],
            capture_output=True,
            text=True,
            timeout=210,
        )
        if result.returncode != 0:
            raise RuntimeError(
                f"mock LLM Deployment not ready: {(result.stdout + result.stderr)[-600:]}"
            )

    @staticmethod
    def verify(namespace: str = config.MOCK_LLM_NAMESPACE) -> None:
        probe = (
            "import json,urllib.request;"
            "r=urllib.request.Request('http://127.0.0.1:8080/v1/chat/completions',"
            "data=json.dumps({'model':'e2e-deterministic','messages':["
            "{'role':'user','content':'Return a valid result'}]}).encode(),"
            "headers={'Content-Type':'application/json'});"
            "d=json.load(urllib.request.urlopen(r,timeout=5));"
            "assert d['choices'][0]['message']['content']"
        )
        result = subprocess.run(
            [
                "kubectl",
                "exec",
                "-n",
                namespace,
                f"deployment/{MOCK_LLM_NAME}",
                "--",
                "python3",
                "-c",
                probe,
            ],
            capture_output=True,
            text=True,
            timeout=30,
        )
        if result.returncode != 0:
            raise RuntimeError(
                f"mock LLM OpenAI compatibility probe failed: {result.stderr[-400:]}"
            )
        print(
            "  Deterministic OpenAI-compatible LLM fixture ready: "
            f"namespace={namespace}, service={MOCK_LLM_NAME}, model=e2e-deterministic"
        )


class MockLLMDeployer(BaseDeployer):
    """Own the explicit deterministic LLM fixture used by orchestration tests."""

    component = "mock-llm"

    def deploy(self) -> None:
        self.step("reconcile Kubernetes mock LLM", MockLLMRuntime.deploy)

    def verify(self) -> None:
        self.step("verify OpenAI-compatible completion", MockLLMRuntime.verify)
