"""Kubernetes-native pgvector persistence for the isolated Flowgent E2E."""

from __future__ import annotations

import json
import os
import re
import subprocess
import time

import psycopg2
from psycopg2 import sql

from common import config
from common.model import RunContext
from deploy import BaseDeployer


PGVECTOR_NAME = "pgvector"
PGVECTOR_IMAGE = os.getenv(
    "FLOWGENT_E2E_PGVECTOR_IMAGE",
    "registry.cn-shenzhen.aliyuncs.com/wl4g/pgvector_pgvector:pg16",
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
        result = subprocess.run(
            ["kubectl", "apply", "-f", "-"],
            input=json.dumps(manifest),
            capture_output=True,
            text=True,
            timeout=90,
        )
        if result.returncode != 0:
            raise RuntimeError(f"apply pgvector resources failed: {result.stderr[-600:]}")

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
        PgvectorRuntime._apply({
            "apiVersion": "v1",
            "kind": "Namespace",
            "metadata": {
                "name": namespace,
                "labels": {"flowgent.io/e2e-owner": config.RESOURCE_PREFIX},
            },
        })
        PgvectorRuntime._apply({
            "apiVersion": "v1",
            "kind": "List",
            "items": [
                {
                    "apiVersion": "v1",
                    "kind": "Secret",
                    "metadata": {"name": PGVECTOR_NAME, "namespace": namespace, "labels": labels},
                    "type": "Opaque",
                    "stringData": {
                        "POSTGRES_USER": user,
                        "POSTGRES_PASSWORD": password,
                        "POSTGRES_DB": database,
                    },
                },
                {
                    "apiVersion": "v1",
                    "kind": "Service",
                    "metadata": {"name": PGVECTOR_NAME, "namespace": namespace, "labels": labels},
                    "spec": {
                        "type": "NodePort",
                        "selector": {"app.kubernetes.io/instance": PGVECTOR_NAME},
                        "ports": [{
                            "name": "postgres",
                            "port": 5432,
                            "targetPort": "postgres",
                            "nodePort": config.PG_NODE_PORT,
                        }],
                    },
                },
                {
                    "apiVersion": "apps/v1",
                    "kind": "StatefulSet",
                    "metadata": {"name": PGVECTOR_NAME, "namespace": namespace, "labels": labels},
                    "spec": {
                        "serviceName": PGVECTOR_NAME,
                        "replicas": 1,
                        "selector": {"matchLabels": {"app.kubernetes.io/instance": PGVECTOR_NAME}},
                        "template": {
                            "metadata": {"labels": labels},
                            "spec": {
                                "containers": [{
                                    "name": "postgres",
                                    "image": PGVECTOR_IMAGE,
                                    "imagePullPolicy": "IfNotPresent",
                                    "envFrom": [{"secretRef": {"name": PGVECTOR_NAME}}],
                                    "env": [{
                                        "name": "PGDATA",
                                        "value": "/var/lib/postgresql/data/pgdata",
                                    }],
                                    "ports": [{"name": "postgres", "containerPort": 5432}],
                                    "readinessProbe": {
                                        "exec": {"command": ["sh", "-c", "pg_isready -U $POSTGRES_USER -d $POSTGRES_DB"]},
                                        "initialDelaySeconds": 5,
                                        "periodSeconds": 3,
                                    },
                                    "livenessProbe": {
                                        "exec": {"command": ["sh", "-c", "pg_isready -U $POSTGRES_USER -d $POSTGRES_DB"]},
                                        "initialDelaySeconds": 20,
                                        "periodSeconds": 10,
                                    },
                                    "resources": {
                                        "requests": {"cpu": "50m", "memory": "128Mi"},
                                        "limits": {"cpu": "750m", "memory": "1Gi"},
                                    },
                                    "volumeMounts": [{
                                        "name": "data",
                                        "mountPath": "/var/lib/postgresql/data",
                                    }],
                                }],
                            },
                        },
                        "volumeClaimTemplates": [{
                            "metadata": {"name": "data", "labels": labels},
                            "spec": {
                                "accessModes": ["ReadWriteOnce"],
                                "resources": {"requests": {"storage": "4Gi"}},
                            },
                        }],
                    },
                },
            ],
        })
        result = subprocess.run(
            [
                "kubectl", "rollout", "status", f"statefulset/{PGVECTOR_NAME}",
                "-n", namespace, "--timeout=240s",
            ],
            capture_output=True,
            text=True,
            timeout=270,
        )
        if result.returncode != 0:
            raise RuntimeError(f"pgvector StatefulSet not ready: {(result.stdout + result.stderr)[-600:]}")
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
                    with connection.cursor() as cursor:
                        cursor.execute("CREATE EXTENSION IF NOT EXISTS vector")
                        cursor.execute(
                            "SELECT extversion FROM pg_extension WHERE extname='vector'"
                        )
                        version = cursor.fetchone()[0]
                        cursor.execute(
                            "SELECT '[1,2,3]'::vector <-> '[1,2,4]'::vector"
                        )
                        distance = float(cursor.fetchone()[0])
                    if distance != 1.0:
                        raise RuntimeError(f"unexpected vector distance {distance}")
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
        if not re.fullmatch(r"[a-z_][a-z0-9_]*", schema):
            raise ValueError(f"invalid isolated PostgreSQL schema: {schema!r}")
        user, _, _ = PgvectorRuntime._credentials()
        connection = PgvectorRuntime.connection()
        connection.autocommit = True
        try:
            with connection.cursor() as cursor:
                cursor.execute(
                    sql.SQL("DROP SCHEMA IF EXISTS {} CASCADE").format(sql.Identifier(schema))
                )
                cursor.execute(
                    sql.SQL("CREATE SCHEMA {} AUTHORIZATION {}").format(
                        sql.Identifier(schema), sql.Identifier(user)
                    )
                )
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
