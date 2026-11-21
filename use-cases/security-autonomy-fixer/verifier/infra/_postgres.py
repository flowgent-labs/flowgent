"""Kubernetes-native pgvector persistence for the isolated Flowgent E2E."""

from __future__ import annotations

import os
import subprocess
import time

import psycopg2
from util.constant import E2EOtherDefinitions as config
from util.images import E2EImagesDefinitions
from util.kubernetes import E2EKubernetesDeployer
from util.process import E2EProcessHelper
from verifier.context import BaseDeployer

PGVECTOR_NAME = "pgvector"
PGVECTOR_IMAGE = os.getenv(
    "FLOWGENT_E2E_PGVECTOR_IMAGE",
    E2EImagesDefinitions.get("pgvector"),
)


class PgvectorRuntime:
    """Reconcile and probe one durable pgvector StatefulSet."""

    @staticmethod
    def _credentials() -> tuple[str, str, str]:
        return (
            os.getenv("FLOWGENT_PG_USER", "test"),
            os.getenv("FLOWGENT_PG_PASSWORD", "test"),
            os.getenv("FLOWGENT_PG_DATABASE", "flowgent"),
        )

    @staticmethod
    def service_fqdn() -> str:
        return f"{PGVECTOR_NAME}.{config.STORAGE_NAMESPACE}.svc.cluster.local"

    @staticmethod
    def _apply(manifest: dict) -> None:
        E2EKubernetesDeployer.apply(manifest)

    @staticmethod
    def deploy(namespace: str = config.STORAGE_NAMESPACE) -> None:
        user, password, database = PgvectorRuntime._credentials()
        labels = {
            "app.kubernetes.io/name": "pgvector",
            "app.kubernetes.io/instance": PGVECTOR_NAME,
            "app.kubernetes.io/component": "storage",
            "app.kubernetes.io/part-of": config.RESOURCE_PREFIX,
            "flowgent.io/e2e-owner": config.RESOURCE_PREFIX,
        }
        PgvectorRuntime._apply(
            {
                "apiVersion": "v1",
                "kind": "Namespace",
                "metadata": {
                    "name": namespace,
                    "labels": {"flowgent.io/e2e-owner": config.RESOURCE_PREFIX},
                },
            }
        )
        PgvectorRuntime._apply(
            E2EKubernetesDeployer.helm_template(
                config.HELM_CHART,
                {
                    "postgresql": {
                        "enabled": True,
                        "name": PGVECTOR_NAME,
                        "namespace": namespace,
                        "labels": labels,
                        "username": user,
                        "password": password,
                        "database": database,
                        "service": {
                            "type": "NodePort",
                            "nodePort": config.PG_NODE_PORT,
                        },
                        "containerImage": PGVECTOR_IMAGE,
                    }
                },
                template="postgresql.yaml",
            )
        )
        result = subprocess.run(
            [
                "kubectl",
                "rollout",
                "status",
                f"statefulset/{PGVECTOR_NAME}",
                "-n",
                namespace,
                "--timeout=240s",
            ],
            capture_output=True,
            text=True,
            timeout=270,
        )
        if result.returncode != 0:
            # Capture evidence before --clean-after-run removes failed pods/PVCs.
            for command in (
                ("get", "pods,pvc", "-n", namespace, "-o", "wide"),
                ("get", "events", "-n", namespace, "--sort-by=.metadata.creationTimestamp"),
                ("logs", f"statefulset/{PGVECTOR_NAME}", "-n", namespace, "--tail=80"),
                ("logs", "deployment/local-path-provisioner", "-n", "kube-system", "--tail=80"),
            ):
                E2EProcessHelper.run(("kubectl", *command), timeout_seconds=30)
            raise RuntimeError(
                f"pgvector StatefulSet not ready: {(result.stdout + result.stderr)[-600:]}"
            )
        PgvectorRuntime._configure_client()
        PgvectorRuntime.verify()

    @staticmethod
    def _configure_client() -> None:
        user, password, database = PgvectorRuntime._credentials()
        values = {
            "FLOWGENT_PG_HOST": "127.0.0.1",
            "FLOWGENT_PG_PORT": str(config.PG_NODE_PORT),
            "FLOWGENT_PG_USER": user,
            "FLOWGENT_PG_PASSWORD": password,
            "FLOWGENT_PG_DATABASE": database,
        }
        os.environ.update(values)
        config.PG_HOST = values["FLOWGENT_PG_HOST"]
        config.PG_PORT = int(values["FLOWGENT_PG_PORT"])
        config.PG_USER = user
        config.PG_PASSWORD = password
        config.PG_DATABASE = database

    @staticmethod
    def connection(*, database: str | None = None):
        user, password, default_database = PgvectorRuntime._credentials()
        return psycopg2.connect(
            host="127.0.0.1",
            port=config.PG_NODE_PORT,
            user=user,
            password=password,
            dbname=database or default_database,
            connect_timeout=10,
        )

    @staticmethod
    def verify(timeout: int = 90) -> None:
        deadline = time.monotonic() + timeout
        last_error = "not attempted"
        while time.monotonic() < deadline:
            try:
                connection = PgvectorRuntime.connection()
                connection.autocommit = True
                try:
                    from util.pgvector import E2EPgvectorImporter

                    version = E2EPgvectorImporter.initialize(connection)
                finally:
                    connection.close()
                print(
                    f"  Kubernetes pgvector ready: namespace={config.STORAGE_NAMESPACE}, "
                    f"service={PGVECTOR_NAME}, extension={version}, native_distance=1.000000"
                )
                return
            except Exception as error:
                last_error = str(error)
                time.sleep(3)
        raise RuntimeError(f"native pgvector TCP probe failed: {last_error}")

    @staticmethod
    def reset_schema(schema: str) -> None:
        from util.postgres import E2EPostgresImporter

        user, _, _ = PgvectorRuntime._credentials()
        connection = PgvectorRuntime.connection()
        connection.autocommit = True
        try:
            E2EPostgresImporter.reset_schema(connection, schema, user)
        finally:
            connection.close()
        print(f"  PostgreSQL schema reset: {config.PG_DATABASE}/{schema}")


class PgvectorDeployer(BaseDeployer):
    """Own the K8s pgvector dependency and its persistent volume."""

    component = "pgvector"

    def deploy(self) -> None:
        self.step("reconcile Kubernetes pgvector", PgvectorRuntime.deploy)

    def verify(self) -> None:
        self.step("verify native vector write and distance", PgvectorRuntime.verify)
