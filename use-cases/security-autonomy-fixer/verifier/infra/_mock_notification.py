"""Deployment lifecycle for the authenticated notification fixture."""

import os
import secrets

from util.constant import E2EOtherDefinitions as config
from util.images import E2EImagesDefinitions
from util.kubernetes import E2EKubernetesDeployer

NOTIFICATION_TOKEN_ENV = "FLOWGENT_E2E_NOTIFICATION_TOKEN"
NOTIFICATION_RECEIVER = "mock-notification"
MOCK_NOTIFICATION_IMAGE = E2EImagesDefinitions.get("python")
MOCK_NOTIFICATION_SCRIPT = (
    config.USE_CASE_DIR / "deploy/mock-notification-service/source/server.py"
)


class MockNotificationDeployment:
    @staticmethod
    def deploy(namespace):
        """Deploy a real authenticated HTTP receiver for notification delivery E2E."""
        token = os.environ.get(NOTIFICATION_TOKEN_ENV)
        if not token:
            token = secrets.token_urlsafe(32)
            os.environ[NOTIFICATION_TOKEN_ENV] = token
        E2EKubernetesDeployer.ensure_namespace(
            namespace, config.RESOURCE_PREFIX, "mock-notification"
        )
        labels = {
            "app.kubernetes.io/name": "mock-notification",
            "app.kubernetes.io/part-of": config.RESOURCE_PREFIX,
            "flowgent.io/e2e-owner": config.RESOURCE_PREFIX,
        }
        resources = E2EKubernetesDeployer.helm_template(
            config.USE_CASE_DIR / "deploy/mock-notification-service/helm",
            {
                "runtime": {
                    "notification_receiver": NOTIFICATION_RECEIVER,
                    "namespace": namespace,
                    "labels": labels,
                    "token": token,
                    "parameter_5": f"{NOTIFICATION_RECEIVER}-script",
                    "parameter_6": MOCK_NOTIFICATION_SCRIPT.read_text(encoding="utf-8"),
                    "parameter_7": {"app": NOTIFICATION_RECEIVER, **labels},
                    "mock_notification_image": MOCK_NOTIFICATION_IMAGE,
                }
            },
            template="receiver.yaml",
        )
        E2EKubernetesDeployer.apply(resources)
        E2EKubernetesDeployer.rollout(namespace, NOTIFICATION_RECEIVER, restart=True)
        print(
            f"  Authenticated notification receiver ready: {NOTIFICATION_RECEIVER}.{namespace}.svc.cluster.local:8080"
        )
        return True
