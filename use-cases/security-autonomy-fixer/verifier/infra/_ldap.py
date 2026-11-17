"""Component-owned ldap deployment."""

from util.constant import E2EOtherDefinitions as config
from util.images import E2EImagesDefinitions
from util.kubernetes import E2EKubernetesDeployer


class LDAPDeployment:
    @staticmethod
    def deploy(namespace: str, configuration: str) -> None:
        values = {
            "namespace": namespace,
            "name": "ldap",
            "image": E2EImagesDefinitions.get("ldap"),
            "port": 389,
            "labels": {
                "flowgent.io/e2e-owner": config.RESOURCE_PREFIX,
                "app.kubernetes.io/part-of": config.RESOURCE_PREFIX,
            },
        }
        values["secretName"] = "ldap-config"
        values["secretData"] = {"ldap-config.cfg": configuration}
        values["volumes"] = [
            {"name": "config", "secret": {"secretName": "ldap-config"}}
        ]
        values["volumeMounts"] = [
            {
                "name": "config",
                "mountPath": "/app/config/config.cfg",
                "subPath": "ldap-config.cfg",
                "readOnly": True,
            }
        ]
        chart = config.USE_CASE_DIR / "deploy/mock-ldap-service/helm"
        E2EKubernetesDeployer.apply(E2EKubernetesDeployer.helm_template(chart, values))
        E2EKubernetesDeployer.rollout(namespace, "ldap", restart=True)
