"""Kubernetes lifecycle for the isolated deterministic LLM fixture."""

from __future__ import annotations

import subprocess

from util.constant import E2EOtherDefinitions as config
from util.images import E2EImagesDefinitions
from util.kubernetes import E2EKubernetesDeployer
from verifier.context import BaseDeployer

MOCK_LLM_NAME = "mock-llm"
MOCK_LLM_IMAGE = E2EImagesDefinitions.get("python")
MOCK_LLM_SCRIPT = (
    config.USE_CASE_DIR / "deploy" / "mock-llm-service" / "source" / "server.py"
)


class MockLLMRuntime:
    """Reconcile and probe one explicitly named OpenAI-compatible fixture."""

    @staticmethod
    def service_fqdn() -> str:
        return f"{MOCK_LLM_NAME}.{config.MOCK_LLM_NAMESPACE}.svc.cluster.local"

    @staticmethod
    def _apply(manifest: dict[str, object]) -> None:
        E2EKubernetesDeployer.apply(manifest)

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
            E2EKubernetesDeployer.helm_template(
                config.USE_CASE_DIR / "deploy" / "mock-llm-service" / "helm",
                {
                    "runtime": {
                        "namespace": namespace,
                        "resource_prefix": config.RESOURCE_PREFIX,
                    }
                },
                template="deploy-1.yaml",
            )
        )
        MockLLMRuntime._apply(
            E2EKubernetesDeployer.helm_template(
                config.USE_CASE_DIR / "deploy" / "mock-llm-service" / "helm",
                {
                    "runtime": {
                        "parameter_1": f"{MOCK_LLM_NAME}-script",
                        "namespace": namespace,
                        "labels": labels,
                        "parameter_4": MOCK_LLM_SCRIPT.read_text(encoding="utf-8"),
                        "mock_llm_name": MOCK_LLM_NAME,
                        "mock_llm_image": MOCK_LLM_IMAGE,
                    }
                },
                template="deploy-2.yaml",
            )
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
