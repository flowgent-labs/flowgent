"""Shared verification context, lifecycle, API client and scenario execution."""

from __future__ import annotations

import contextlib
import importlib
import io
import json
import os
import re
import sys
import time
import traceback
from abc import ABC, abstractmethod
from dataclasses import dataclass
from pathlib import Path
from typing import TYPE_CHECKING, Callable, TypeVar

from util.constant import E2EOtherDefinitions as config
from util.process import CommandResult
from util.report import E2EReportHelper, EvidenceArtifact, VerificationResult

if TYPE_CHECKING:
    import requests


REPORTS_DIR = config.REPORTS_DIR
SCENARIOS = config.SCENARIOS


@dataclass(frozen=True)
class VerificationContext:
    round_number: int
    clean: bool
    timeout_seconds: int
    build_images: bool = True
    namespace: str = ""
    release: str = ""
    deployer: str = "k8s"


T = TypeVar("T")


class BaseDeployer(ABC):
    """Shared lifecycle state and step contract for every E2E deployer."""

    backend = ""
    component = ""

    def __init__(self, context: VerificationContext) -> None:
        self.context = context
        self.commands: list[CommandResult] = []
        self.details: list[str] = []
        self._step_number = 0

    def step(self, label: str, operation: Callable[[], T]) -> T:
        self._step_number += 1
        scope = (
            self.backend
            or self.component
            or self.__class__.__name__.removesuffix("Deployer").lower()
        )
        case_id = f"deploy.{scope}.{self._step_number:02d}"
        started = time.monotonic()
        print(f"    [{case_id}] {label} ...", flush=True)
        result = operation()
        if result is False:
            raise RuntimeError(f"Deployment step failed: {label}")
        print(f"    [{case_id}] PASS ({time.monotonic() - started:.2f}s)", flush=True)
        self.details.append(f"{label}: PASS")
        return result

    def prepare(
        self,
        *,
        skip_sonarqube: bool,
        skip_build: bool,
        skip_deploy: bool,
        skip_import: bool,
    ) -> None:
        """Run the common full-topology lifecycle for a backend deployer."""
        if not skip_sonarqube:
            self.step("deploy and verify SonarQube", self.deploy_sonarqube)
        if not skip_deploy:
            self.step("reset isolated persistence", self.reset_database)
        if not skip_build:
            self.step("build Flowgent and Web images", self.build_images)
        if not skip_deploy:
            self.step("deploy the complete topology", self.deploy)
            self.step("verify topology readiness", self.verify)
        self.step(
            "configure verifier client environment", self.configure_client_environment
        )
        if not skip_import:
            self.step("import use-case resources", self.import_config)

    @abstractmethod
    def deploy(self) -> None:
        """Create or update resources owned by this deployer."""

    @abstractmethod
    def verify(self) -> None:
        """Fail closed unless resources owned by this deployer are ready."""

    def cleanup(self) -> None:
        """Remove only resources owned by this deployer when supported."""

    def deploy_sonarqube(self) -> None:
        raise NotImplementedError(
            f"{self.__class__.__name__} is not a topology deployer"
        )

    def reset_database(self) -> None:
        raise NotImplementedError(
            f"{self.__class__.__name__} is not a topology deployer"
        )

    def build_images(self) -> None:
        raise NotImplementedError(
            f"{self.__class__.__name__} is not a topology deployer"
        )

    def verify_authguard(self, *, resource_scope: bool = True) -> None:
        raise NotImplementedError(
            f"{self.__class__.__name__} is not a topology deployer"
        )

    def import_config(self) -> None:
        raise NotImplementedError(
            f"{self.__class__.__name__} is not a topology deployer"
        )

    def configure_client_environment(self) -> None:
        raise NotImplementedError(
            f"{self.__class__.__name__} is not a topology deployer"
        )

    def runtime_exec(self, command: tuple[str, ...]) -> CommandResult:
        raise NotImplementedError(f"{self.__class__.__name__} has no runtime executor")


class E2EDeployerFactory:
    """Create the selected full-topology deployer without coupling verifiers."""

    @staticmethod
    def create(context: VerificationContext) -> BaseDeployer:
        if context.deployer == "k8s":
            from verifier.infra._kubernetes import FlowgentKubernetesDeployment

            return FlowgentKubernetesDeployment(context)
        if context.deployer == "docker":
            from verifier.infra._docker import FlowgentDockerDeployment

            return FlowgentDockerDeployment(context)
        raise ValueError(f"unsupported E2E deployer: {context.deployer}")


T = TypeVar("T")


class BaseVerifier(ABC):
    """Compose deployment access, assertions, steps, and evidence output."""

    scenario_id = ""
    title = ""

    def __init__(self, context: VerificationContext) -> None:
        STATE_DIR.mkdir(parents=True, exist_ok=True)
        self.context = context
        self.infrastructure = E2EDeployerFactory.create(context)
        self.commands = self.infrastructure.commands
        self.details = self.infrastructure.details
        self.evidence: list[EvidenceArtifact] = []
        self._step_number = 0

    def step(self, label: str, operation: Callable[[], T]) -> T:
        self._step_number += 1
        case_id = f"{self.scenario_id}.{self._step_number:02d}"
        detail_offset = len(self.details)
        started = time.monotonic()
        print(f"    [{case_id}] {label} ...", flush=True)
        try:
            value = operation()
        except Exception:
            print(
                f"    [{case_id}] FAIL ({time.monotonic() - started:.2f}s)", flush=True
            )
            raise
        duration = time.monotonic() - started
        self._write_step_evidence(
            case_id, label, duration, self.details[detail_offset:]
        )
        print(f"    [{case_id}] PASS ({duration:.2f}s)", flush=True)
        return value

    def execute(self, operation: Callable[[], None]) -> VerificationResult:
        started = time.monotonic()
        passed = False
        error: str | None = None
        try:
            operation()
            passed = True
        except Exception:
            error = traceback.format_exc()
            self.details.append(error)
        return VerificationResult(
            scenario_id=self.scenario_id,
            title=self.title,
            passed=passed,
            duration_seconds=time.monotonic() - started,
            error=error,
            details=self.details,
            commands=self.commands,
            evidence=self.evidence,
        )

    def run(self) -> VerificationResult:
        """Run scenario setup before verification and report setup failures."""
        started = time.monotonic()
        try:
            self.deploy(self.context)
            self.init(self.context)
        except Exception:
            error = traceback.format_exc()
            return VerificationResult(
                self.scenario_id,
                self.title,
                False,
                time.monotonic() - started,
                error=error,
                details=[*self.details, error],
                commands=self.commands,
                evidence=self.evidence,
            )
        result = self.verifier(self.context)
        result.duration_seconds = time.monotonic() - started
        return result

    @abstractmethod
    def verifier(self, context: VerificationContext) -> VerificationResult:
        """Execute the scenario assertions."""

    def _write_step_evidence(
        self,
        case_id: str,
        title: str,
        duration_seconds: float,
        assertions: list[str],
    ) -> None:
        path = self.evidence_path(case_id, title, "json")
        payload = {
            "scenarioId": self.scenario_id,
            "caseId": case_id,
            "title": title,
            "status": "PASS",
            "durationSeconds": round(duration_seconds, 6),
            "assertions": assertions
            or ["scenario operation completed without assertion failure: PASS"],
        }
        path.write_text(
            json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8"
        )
        self.record_evidence(case_id, title, "application/json", path)

    def evidence_path(self, case_id: str, title: str, suffix: str) -> Path:
        """Allocate a report-owned path for a verifier artifact."""
        return E2EReportHelper.evidence_path(
            self.context.round_number,
            self.scenario_id,
            case_id,
            title,
            suffix,
        )

    def record_evidence(self, case_id: str, title: str, kind: str, path: Path) -> None:
        """Attach a verifier artifact to the scenario report."""
        self.evidence.append(EvidenceArtifact(case_id, title, kind, path))


STATE_DIR = REPORTS_DIR / ".state"


RUN_ID_PATH = STATE_DIR / "last_graph_run_id"

MQTT_AUDIT_PATH = STATE_DIR / "last_mqtt_audit.json"

PR_BASELINE_PATH = STATE_DIR / "last_pr_baseline.json"
UI_EVIDENCE_PATH = STATE_DIR / "ui_run.json"
MAIN_LOG_PATH = STATE_DIR / "main_run_logs.json"


class FlowgentE2EProject:
    """Own Flowgent API, database, and manifest operations for verifiers."""

    @staticmethod
    def pg_connect():
        """Use the configured isolated database; existing callers decide requiredness."""
        from util.constant import E2EOtherDefinitions as config
        from util.postgres import E2EPostgresChecker

        return E2EPostgresChecker.connect(config.postgres_dsn(), required=False)

    @staticmethod
    def headers() -> dict:
        """Direct E2E control-plane calls carry no Flowgent-owned credentials."""
        return {}

    @classmethod
    def session(cls) -> requests.Session:
        """Create the canonical direct control-plane session for API/A2A tests."""
        import requests

        session = requests.Session()
        session.headers.update(cls.headers())
        session.headers["Content-Type"] = "application/json"
        return session

    @staticmethod
    def unwrap_k8s(data: dict) -> dict:
        """Normalize core.flowgent.io/v1 structured resource into a flat dict."""
        if "data" in data and "kind" in data:
            spec = data["data"] or {}
            flat = dict(spec) if isinstance(spec, dict) else {}
            metadata = data.get("metadata", {}) or {}
            if metadata.get("name"):
                flat["name"] = metadata["name"]
            if metadata.get("namespace"):
                flat.setdefault("namespace_id", metadata["namespace"])
            return flat
        return data

    @classmethod
    def resolve_env_vars(cls, value):
        """Recursively substitute ${VAR} placeholders from the environment."""
        if isinstance(value, str):
            return re.sub(
                r"\$\{(\w+)\}",
                lambda match: os.environ.get(match.group(1), match.group(0)),
                value,
            )
        if isinstance(value, dict):
            return {key: cls.resolve_env_vars(item) for key, item in value.items()}
        if isinstance(value, list):
            return [cls.resolve_env_vars(item) for item in value]
        return value

    @staticmethod
    def get_or_post(session, api_base, get_path, post_path, payload, kind):
        """Idempotently GET a resource, then POST it when genuinely absent."""
        response = session.get(f"{api_base}{get_path}")
        if response.status_code == 200:
            return True
        response = session.post(f"{api_base}{post_path}", json=payload)
        if response.status_code not in (200, 201):
            print(
                f"  WARN: failed to register {kind} {payload.get('name')}: {response.status_code} {response.text[:160]}"
            )
            return False
        return True

    @classmethod
    def _get_tasks_from_pg(cls, graph_run_id):
        """Query canonical NodeRuns when the read API cannot return the task list."""
        try:
            connection = cls.pg_connect()
            if not connection:
                return []
            cursor = connection.cursor()
            cursor.execute(
                "SELECT id, graph_run_id, node_instance_id, status, input, output, "
                "COALESCE(error->>'message','') AS error, attempt, "
                "COALESCE((metadata->>'max_retries')::int,0) AS max_retries, "
                "execution_id, COALESCE((metadata->>'sequence')::int,attempt) AS sequence, "
                "started_at, finished_at FROM orh_node_run "
                "WHERE graph_run_id=%s AND status<>'DELETED' ORDER BY node_instance_id,attempt",
                (graph_run_id,),
            )
            rows = cursor.fetchall()
            columns = [description[0] for description in cursor.description]
            tasks = []
            for row in rows:
                task = dict(zip(columns, row))
                for column in ("started_at", "finished_at"):
                    if task.get(column):
                        task[column] = task[column].isoformat()
                tasks.append(task)
            connection.close()
            return tasks
        except Exception as error:
            print(f"  PG task fallback failed: {error}")
            return []

    @classmethod
    def get_tasks(cls, session, api_base, namespace, graph_run_id):
        """Fetch a graph run's tasks, with a direct database fallback."""
        response = session.get(f"{api_base}/api/v1/{namespace}/graph-runs/{graph_run_id}/node-runs")
        if response.status_code == 200:
            tasks = response.json()
            if isinstance(tasks, list):
                return tasks
        return cls._get_tasks_from_pg(graph_run_id)

    @staticmethod
    def tasks_by_node(tasks):
        return {
            task.get("node_instance_id"): task
            for task in sorted(tasks, key=lambda task: int(task.get("attempt") or 1))
            if task.get("node_instance_id")
        }

    @staticmethod
    def parse_output(task):
        """Normalize a NodeRun output field into its structured representation."""
        output = task.get("output") or {}
        if isinstance(output, str):
            try:
                output = json.loads(output)
            except (json.JSONDecodeError, TypeError):
                return {"_raw": output}
        if not isinstance(output, dict):
            return {}
        parsed = output.get("parsed")
        if isinstance(parsed, dict):
            return parsed
        text = output.get("text")
        if isinstance(text, str):
            try:
                decoded = json.loads(text)
                if isinstance(decoded, dict):
                    return decoded
            except (json.JSONDecodeError, TypeError):
                pass
        return output

    @staticmethod
    def node_task(tasks_by_node, node_instance_id):
        return tasks_by_node.get(node_instance_id)

    @staticmethod
    def try_approve_pending_human(
        session, api_base, namespace, graph_run_id, connection=None
    ):
        """Approve a pending gate, preferring its canonical approval ID."""
        approval_id = None
        if connection:
            cursor = connection.cursor()
            cursor.execute(
                "SELECT id FROM orh_approval WHERE graph_run_id=%s AND status='pending' LIMIT 1",
                (graph_run_id,),
            )
            row = cursor.fetchone()
            if row:
                approval_id = row[0]
        if not approval_id:
            response = session.get(
                f"{api_base}/api/v1/{namespace}/graph-runs/{graph_run_id}/approvals"
            )
            if response.status_code == 200:
                for item in response.json() or []:
                    if item.get("graph_run_id") == graph_run_id and item.get("status") == "pending":
                        approval_id = item.get("id")
                        break
        if approval_id:
            response = session.post(
                f"{api_base}/api/v1/{namespace}/graph-runs/{graph_run_id}/approvals/{approval_id}/approve",
                json={"comment": "Approved by e2e verifier"},
            )
            print(
                f"  OK auto-approved human gate (approval={approval_id[:12]}...) status={response.status_code}"
            )
            return response.status_code == 200
        return False


class _Tee(io.StringIO):
    """Capture scenario output while preserving runner progress on stdout."""

    def __init__(self, stream: io.TextIOBase) -> None:
        super().__init__()
        self._stream = stream

    def write(self, value: str) -> int:
        self._stream.write(value)
        return super().write(value)

    def flush(self) -> None:
        self._stream.flush()
        super().flush()


class VerificationRunner:
    """Execute the ordered verifier matrix with one report/evidence contract."""

    @staticmethod
    def run_verifier(
        scenario_id: str, context: VerificationContext
    ) -> VerificationResult:
        title, module_name = SCENARIOS[scenario_id]
        started = time.monotonic()
        capture = _Tee(sys.stdout)
        try:
            module = importlib.import_module(module_name)
            with contextlib.redirect_stdout(capture):
                candidates = [
                    value
                    for value in vars(module).values()
                    if isinstance(value, type)
                    and value is not BaseVerifier
                    and value.__module__ == module.__name__
                    and issubclass(value, BaseVerifier)
                ]
                if len(candidates) != 1:
                    names = [candidate.__name__ for candidate in candidates]
                    raise TypeError(
                        f"{module_name} must define exactly one XxxVerifier(BaseVerifier), got {names}"
                    )
                verifier = candidates[0](context)
                result = verifier.run()
            if not isinstance(result, VerificationResult):
                raise TypeError(f"{module_name} verifier returned an invalid result")
            if result.scenario_id != scenario_id or result.title != title:
                raise ValueError(
                    f"{module_name} metadata differs from util.constant.SCENARIOS"
                )
            result.output = capture.getvalue()
            return result
        except Exception:
            return VerificationResult(
                scenario_id=scenario_id,
                title=title,
                passed=False,
                duration_seconds=time.monotonic() - started,
                output=capture.getvalue(),
                error=traceback.format_exc(),
                details=[traceback.format_exc()],
            )

    @classmethod
    def run_matrix(
        cls,
        context: VerificationContext,
        scenario_ids: list[str],
        *,
        archive_existing: bool = True,
    ) -> tuple[bool, list[VerificationResult]]:
        if archive_existing:
            archive = E2EReportHelper.archive()
            if archive:
                print(f"Archived previous reports: {archive}")

        deployer = E2EDeployerFactory.create(context)
        tunnels = None
        if any(scenario_id != "01" for scenario_id in scenario_ids):
            tunnels = deployer.tunnels().start()
        results: list[VerificationResult] = []
        try:
            for scenario_id in scenario_ids:
                if tunnels is not None and scenario_id != "01":
                    tunnels.ensure()
                title = SCENARIOS[scenario_id][0]
                print(f"  [{scenario_id}] {title}", flush=True)
                result = cls.run_verifier(scenario_id, context)
                results.append(result)
                E2EReportHelper.write_round(context.round_number, result)
                status = "PASS" if result.passed else "FAIL"
                print(f"  [{scenario_id}] {status} ({result.duration_seconds:.2f}s)")
        finally:
            if tunnels is not None:
                tunnels.stop()
        summary = E2EReportHelper.write_summary([results])
        passed = all(result.passed for result in results)
        print(
            f"Summary: {summary} ({sum(result.passed for result in results)}/{len(results)} passed)"
        )
        return passed, results


class FlowgentRunEvidence:
    """Correlate a real scenario's durable attempt, trace, files and log events."""

    @staticmethod
    def load_ui() -> dict:
        if not UI_EVIDENCE_PATH.is_file():
            raise AssertionError(
                "Scenario 41 must produce current UI-run evidence first"
            )
        evidence = json.loads(UI_EVIDENCE_PATH.read_text())
        if evidence.get("deployer") != os.getenv("FLOWGENT_E2E_DEPLOYER", "k8s"):
            raise AssertionError(
                "UI-run evidence belongs to another deployment backend"
            )
        return evidence

    @staticmethod
    def persisted(graph_run_id: str, namespace: str, node_instance_id: str) -> dict:
        from util.postgres import E2EPostgresChecker

        with contextlib.closing(
            E2EPostgresChecker.connect(config.postgres_dsn())
        ) as connection:
            rows = E2EPostgresChecker.query(
                connection,
                "SELECT r.status,r.agent_graph_revision_id,f.name,n.id,n.status,n.attempt,n.execution_id,n.output,r.runtime_mode,r.runtime_cluster_id "
                "FROM orh_graph_run r JOIN orh_agent_graph f ON f.id=r.agent_graph_id AND f.namespace_id=r.namespace_id "
                "JOIN orh_node_run n ON n.graph_run_id=r.id AND n.namespace_id=r.namespace_id "
                "WHERE r.id=%s AND r.namespace_id=%s AND n.node_instance_id=%s ORDER BY n.attempt DESC LIMIT 1",
                (graph_run_id, namespace, node_instance_id),
            )
        if not rows:
            raise AssertionError(f"No persisted NodeRun for {graph_run_id}/{node_instance_id}")
        (
            status,
            revision,
            agent_graph_id,
            node_run_id,
            task_status,
            attempt,
            execution_id,
            output,
            runtime_mode,
            runtime_cluster_id,
        ) = rows[0]
        if (
            status != "COMPLETED"
            or task_status not in {"COMPLETED", "SUCCESS"}
            or not revision
        ):
            raise AssertionError(
                f"Run/attempt is not durably completed: {status}/{task_status}"
            )
        return {
            "graph_run_id": graph_run_id,
            "namespace": namespace,
            "agent_graph_id": agent_graph_id,
            "node_instance_id": node_instance_id,
            "node_run_id": node_run_id,
            "attempt": attempt,
            "agent_graph_revision_id": revision,
            "plan_id": re.sub(r"-attempt-\d+$", "", execution_id),
            "output": output,
            "runtime_cluster_id": runtime_cluster_id,
            "runtime_mode": runtime_mode,
        }

    @staticmethod
    def traces(graph_run_id: str, *, timeout: int = 90) -> list[dict]:
        import requests

        session = requests.Session()
        session.trust_env = False
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            services = session.get(
                config.JAEGER_UI_URL.rstrip("/") + "/api/services", timeout=10
            )
            services.raise_for_status()
            matches = {}
            for service in services.json().get("data", []):
                if not service.startswith("flowgent"):
                    continue
                response = session.get(
                    config.JAEGER_UI_URL.rstrip("/") + "/api/traces",
                    params={
                        "service": service,
                        "tags": json.dumps({"run.id": graph_run_id}),
                        "lookback": "24h",
                        "limit": 100,
                    },
                    timeout=15,
                )
                response.raise_for_status()
                for trace in response.json().get("data", []):
                    if any(
                        FlowgentRunEvidence.tags(span).get("run.id") == graph_run_id
                        for span in trace.get("spans", [])
                    ):
                        matches[trace["traceID"]] = trace
            if matches:
                return list(matches.values())
            time.sleep(2)
        raise AssertionError(f"Jaeger has no correlated trace for run {graph_run_id}")

    @staticmethod
    def tags(span: dict) -> dict:
        return {
            tag["key"]: tag.get("value", tag.get("vStr"))
            for tag in span.get("tags", [])
        }

    @staticmethod
    def runtime_namespaces(
        context: VerificationContext, evidence: dict
    ) -> tuple[str, ...]:
        return tuple(
            dict.fromkeys(
                (
                    context.namespace or config.K8S_NAMESPACE,
                    config.K8S_WORKLOAD_NAMESPACE_PREFIX + evidence["namespace"],
                )
            )
        )

    @staticmethod
    def workspace(
        context: VerificationContext, evidence: dict, relative_path: str
    ) -> dict:
        from pathlib import PurePosixPath

        path = PurePosixPath(relative_path)
        if path.is_absolute() or ".." in path.parts:
            raise ValueError("Workspace evidence must use a relative file path")
        absolute = str(
            PurePosixPath(
                "/var/flowgent",
                evidence["namespace"],
                evidence["agent_graph_id"],
                evidence["graph_run_id"],
                evidence["plan_id"],
                path,
            )
        )
        program = (
            "import hashlib,pathlib; p=pathlib.Path("
            + repr(absolute)
            + "); print(hashlib.sha256(p.read_bytes()).hexdigest())"
        )
        if context.deployer == "docker":
            result = E2EDeployerFactory.create(context).runtime_exec(
                ("python3", "-c", program)
            )
            if not result.passed:
                raise AssertionError(
                    f"Run-produced workspace file is missing: {absolute}"
                )
            return {"path": absolute, "sha256": result.output.strip().splitlines()[-1]}
        from util.kubernetes import E2EKubernetesDeployer

        for namespace in FlowgentRunEvidence.runtime_namespaces(context, evidence):
            pods = E2EKubernetesDeployer.resource("pods", namespace).get("items", [])
            for pod in pods:
                if pod.get("status", {}).get("phase") != "Running":
                    continue
                for container in pod["spec"].get("containers", []):
                    if not any(
                        v.get("mountPath") == "/var/flowgent"
                        for v in container.get("volumeMounts", [])
                    ):
                        continue
                    command = (
                        "kubectl",
                        "exec",
                        "-n",
                        namespace,
                        pod["metadata"]["name"],
                        "-c",
                        container["name"],
                        "--",
                        "python3",
                        "-c",
                        program,
                    )
                    result = E2EKubernetesDeployer.command(
                        command, allowed_codes={0, 1, 2, 126, 127}
                    )
                    if result.passed:
                        return {
                            "path": absolute,
                            "sha256": result.output.strip().splitlines()[-1],
                            "pod": pod["metadata"]["name"],
                            "container": container["name"],
                            "namespace": namespace,
                        }
        raise AssertionError(
            f"No mounted runtime volume contains the run-produced file {absolute}"
        )

    @staticmethod
    def logs(context: VerificationContext, evidence: dict) -> list[str]:
        from util.process import E2EProcessHelper

        if context.deployer == "docker":
            from util.docker import E2EDockerDeployer

            from verifier.infra._docker import COMPOSE_FILES, PROJECT_NAME

            result = E2EDockerDeployer.compose(
                COMPOSE_FILES,
                PROJECT_NAME,
                "logs",
                "--no-color",
                "flowgent",
                environment={"FLOWGENT_E2E_CASE_DIR": str(config.USE_CASE_DIR)},
                timeout=30,
            )
            if not result.passed:
                raise AssertionError("Cannot collect scenario runtime logs")
            output = result.output
        else:
            from util.kubernetes import E2EKubernetesDeployer

            outputs = []
            cluster = evidence.get("runtime_cluster_id")
            if not cluster:
                if evidence.get("runtime_mode") not in {"session", "application"}:
                    raise AssertionError(
                        "Run evidence lacks its producing runtime mode"
                    )
                cluster = (
                    "session"
                    if evidence["runtime_mode"] == "session"
                    else "app-" + evidence["graph_run_id"]
                )
            for namespace in FlowgentRunEvidence.runtime_namespaces(context, evidence):
                pods = E2EKubernetesDeployer.resource(
                    "pods", namespace, selector=f"flowgent.io/runtime-cluster={cluster}"
                ).get("items", [])
                for pod in pods:
                    statuses = pod.get("status", {}).get("containerStatuses", [])
                    for status in statuses:
                        if "running" not in status.get("state", {}):
                            continue
                        result = E2EProcessHelper.run(
                            (
                                "kubectl",
                                "logs",
                                "-n",
                                namespace,
                                pod["metadata"]["name"],
                                "-c",
                                status["name"],
                                "--prefix=true",
                                "--tail=-1",
                            ),
                            stream=False,
                        )
                        if not result.passed:
                            raise AssertionError(
                                "Cannot collect the producing workload logs"
                            )
                        outputs.append(result.output)
            output = "\n".join(outputs)
        plans = evidence.get("plan_ids", [evidence.get("plan_id", "")])
        lines = [
            line
            for line in output.splitlines()
            if any(plan and plan in line for plan in plans)
        ]
        if not lines:
            raise AssertionError(
                "No runtime log events correlate with the producing execution"
            )
        return lines
