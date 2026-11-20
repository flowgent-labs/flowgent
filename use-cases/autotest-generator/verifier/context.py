"""Shared AutoTest context and correlated, read-only execution evidence."""

from __future__ import annotations

import contextlib
import os
import time
import traceback
from abc import ABC, abstractmethod
from dataclasses import dataclass

from util.constant import E2EOtherDefinitions as config
from util.report import VerificationResult


@dataclass(frozen=True)
class VerificationContext:
    namespace: str = "default"
    release: str = "flowgent"
    timeout_seconds: int = 300
    deployer: str = "k8s"

    def request(self, path: str):
        import requests

        response = requests.get(config.K8S_APISERVER_URL.rstrip("/") + path, timeout=15)
        response.raise_for_status()
        return response

    def execution(self) -> dict:
        from util.postgres import E2EPostgresChecker

        graph_run_id = os.getenv("FLOWGENT_E2E_RUN_ID", "")
        if not graph_run_id:
            raise AssertionError(
                "A real AutoTest Run ID is required; business contracts remain documented in docs/development/e2e-runtime-gaps.md"
            )
        namespace = config.NAMESPACE_ID
        api = self.request(f"/api/v1/{namespace}/graph-runs/{graph_run_id}").json()
        with contextlib.closing(
            E2EPostgresChecker.connect(config.postgres_dsn())
        ) as connection:
            rows = E2EPostgresChecker.query(
                connection,
                "SELECT f.name,r.status,r.agent_graph_revision_id FROM orh_graph_run r JOIN orh_agent_graph f ON f.id=r.agent_graph_id AND f.namespace_id=r.namespace_id WHERE r.id=%s AND r.namespace_id=%s",
                (graph_run_id, namespace),
            )
            tasks = E2EPostgresChecker.query(
                connection,
                "SELECT DISTINCT ON (node_instance_id) node_instance_id,id,status,attempt,execution_id FROM orh_node_run WHERE graph_run_id=%s AND namespace_id=%s ORDER BY node_instance_id,attempt DESC",
                (graph_run_id, namespace),
            )
        if (
            len(rows) != 1
            or rows[0][0] != "autotest-generation"
            or rows[0][1] != "COMPLETED"
            or not rows[0][2]
        ):
            raise AssertionError(
                "The supplied ID does not identify a completed, revision-bound AutoTest run"
            )
        if api.get("status") != rows[0][1] or not tasks:
            raise AssertionError(
                "AutoTest API and PG execution evidence disagree or have no attempts"
            )
        executed = [row for row in tasks if row[2] in {"COMPLETED", "SUCCESS"}]
        if not executed or any(
            row[2] not in {"COMPLETED", "SUCCESS", "SKIPPED"} for row in tasks
        ):
            raise AssertionError(
                "The supplied AutoTest run has missing or nonterminal node evidence"
            )
        return {
            "graph_run_id": graph_run_id,
            "namespace": namespace,
            "agent_graph_id": rows[0][0],
            "revision": rows[0][2],
            "tasks": [
                {
                    "node_instance_id": row[0],
                    "node_run_id": row[1],
                    "attempt": row[3],
                    "execution_id": row[4],
                }
                for row in executed
            ],
        }


class BaseVerifier(ABC):
    scenario_id = ""
    title = ""

    def __init__(self, context: VerificationContext):
        self.context = context
        self.details = []

    def run(self) -> VerificationResult:
        started = time.monotonic()
        try:
            self.deploy(self.context)
            self.init(self.context)
            self.verifier(self.context)
            return VerificationResult(
                self.scenario_id,
                self.title,
                True,
                time.monotonic() - started,
                details=self.details,
            )
        except Exception:
            return VerificationResult(
                self.scenario_id,
                self.title,
                False,
                time.monotonic() - started,
                error=traceback.format_exc(),
                details=self.details,
            )

    @abstractmethod
    def deploy(self, context: VerificationContext) -> None: ...

    @abstractmethod
    def init(self, context: VerificationContext) -> None: ...

    @abstractmethod
    def verifier(self, context: VerificationContext) -> None: ...
