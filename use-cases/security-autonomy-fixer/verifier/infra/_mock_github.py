"""Component-owned mock-github deployment."""

from util.constant import E2EOtherDefinitions as config
from util.images import E2EImagesDefinitions
from util.kubernetes import E2EKubernetesDeployer


class MockGitHubDeployment:
    @staticmethod
    def deploy(namespace: str) -> None:
        values = {
            "namespace": namespace,
            "name": "mock-github",
            "image": E2EImagesDefinitions.get("python"),
            "port": 8080,
            "labels": {
                "flowgent.io/e2e-owner": config.RESOURCE_PREFIX,
                "app.kubernetes.io/part-of": config.RESOURCE_PREFIX,
            },
        }
        values["command"] = ["python3", "/opt/e2e/server.py"]
        values["configName"] = "mock-github-script"
        values["configData"] = {
            "server.py": (
                config.USE_CASE_DIR / "deploy/mock-github-service/source/server.py"
            ).read_text()
        }
        values["volumes"] = [
            {"name": "script", "configMap": {"name": "mock-github-script"}}
        ]
        values["volumeMounts"] = [
            {
                "name": "script",
                "mountPath": "/opt/e2e/server.py",
                "subPath": "server.py",
                "readOnly": True,
            }
        ]
        chart = config.USE_CASE_DIR / "deploy/mock-github-service/helm"
        E2EKubernetesDeployer.apply(E2EKubernetesDeployer.helm_template(chart, values))
        E2EKubernetesDeployer.rollout(namespace, "mock-github", restart=True)
