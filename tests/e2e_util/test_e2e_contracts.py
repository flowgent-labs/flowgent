"""Migration checks for runtime detection, actual Helm rendering and report ownership."""

from __future__ import annotations

import importlib
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

from e2e_util.images import E2EImagesDefinitions
from e2e_util.kubernetes import E2EKubernetesDeployer, KubernetesLocalImageLoader
from e2e_util.process import E2EProcessHelper
from e2e_util.report import E2EReportHelper, VerificationResult

ROOT = Path(__file__).resolve().parents[2]
CASE = ROOT / "use-cases/security-autonomy-fixer"


class RuntimeDetectionTests(unittest.TestCase):
    def test_sonarqube_uses_the_approved_native_arm_exception(self):
        for engine, expected in (
            ("linux/arm64", "linux/arm64"),
            ("linux/amd64", "linux/amd64"),
        ):
            with patch.dict(
                os.environ,
                {"E2E_ENGINE_PLATFORM": engine, "IN_GFW": "true"},
                clear=True,
            ):
                image = E2EImagesDefinitions.get("sonarqube")
                self.assertEqual(E2EImagesDefinitions.platform(image), expected)
                self.assertEqual(
                    E2EImagesDefinitions.platform("postgres18"), "linux/amd64"
                )
                self.assertEqual(
                    E2EImagesDefinitions.platform("sonarqube-mcp"), "linux/amd64"
                )
                if engine == "linux/arm64":
                    self.assertTrue(image.endswith("-arm64"))

    def test_host_and_container_builds_use_their_own_proxy_addresses(self):
        with patch.dict(
            os.environ,
            {
                "HTTPS_PROXY": "http://127.0.0.1:8800",
                "FLOWGENT_E2E_BUILD_PROXY": "http://192.168.5.2:8800",
            },
            clear=True,
        ):
            self.assertEqual(
                E2EImagesDefinitions.build_environment(container=False)["HTTPS_PROXY"],
                "http://127.0.0.1:8800",
            )
            self.assertEqual(
                E2EImagesDefinitions.build_environment()["HTTPS_PROXY"],
                "http://192.168.5.2:8800",
            )

    def test_emulation_flags_follow_the_engine_architecture(self):
        with patch.dict(os.environ, {}, clear=True):
            self.assertEqual(
                E2EImagesDefinitions.runtime_environment("linux/arm64")[
                    "E2E_EMQX_ERL_AFLAGS"
                ],
                "+JPperf true",
            )
            self.assertEqual(
                E2EImagesDefinitions.runtime_environment("linux/amd64")[
                    "E2E_EMQX_ERL_AFLAGS"
                ],
                "",
            )

    def test_compose_preloads_active_profiles_before_starting_services(self):
        from e2e_util.docker import E2EDockerDeployer

        configuration = {
            "services": {
                "cache": {"image": "cache:1"},
                "database": {"image": "database:1", "profiles": ["database"]},
                "stdio": {"image": "stdio:1", "profiles": ["manual-stdio"]},
            }
        }
        with (
            patch.dict(os.environ, {"COMPOSE_PROFILES": ""}),
            patch.object(
                E2EDockerDeployer, "compose_command", return_value=("docker", "compose")
            ),
            patch.object(
                E2EProcessHelper,
                "run",
                side_effect=[
                    SimpleNamespace(passed=True, output=json.dumps(configuration)),
                    SimpleNamespace(passed=True),
                ],
            ) as commands,
            patch.object(E2EImagesDefinitions, "prepare_runtime") as prepare,
        ):
            E2EDockerDeployer.compose(
                (CASE / "deploy/flowgent/docker-compose.yaml",),
                "test",
                "--profile",
                "database",
                "up",
                "-d",
            )
            self.assertEqual(
                [call.args[0] for call in prepare.call_args_list],
                ["cache:1", "database:1"],
            )
            self.assertFalse(commands.call_args_list[0].kwargs["stream"])

    def test_single_platform_image_metadata_fallback_keeps_architecture_check(self):
        for platform in ("linux/amd64", "linux/arm64"):
            with (
                self.subTest(platform=platform),
                patch.object(
                    E2EProcessHelper,
                    "run",
                    side_effect=[
                        SimpleNamespace(passed=False, output="No such image"),
                        SimpleNamespace(
                            passed=True, output=f"sha256:verified {platform}"
                        ),
                    ],
                ) as command,
            ):
                self.assertEqual(
                    E2EImagesDefinitions.inspect("example:tag")["platform"], platform
                )
                self.assertNotIn("--platform", command.call_args_list[1].args[0])

    def test_local_runtime_matrix(self):
        matrix = (
            ("colima", "docker://27.0", "v1.32+k3s", "docker"),
            ("colima-review", "containerd://2.0", "v1.32+k3s", "containerd"),
            ("orbstack", "containerd://2.0", "v1.32+k3s", "docker"),
            ("native", "docker://27.0", "v1.32", "docker"),
            ("native", "containerd://2.0", "v1.32+k3s", "k3s"),
            ("native", "containerd://2.0", "v1.32", "containerd"),
        )
        for context, runtime, version, expected in matrix:
            with self.subTest(context=context, runtime=runtime, version=version):
                nodes = {
                    "items": [
                        {
                            "metadata": {"name": "node"},
                            "status": {
                                "nodeInfo": {
                                    "architecture": "arm64",
                                    "kubeletVersion": version,
                                    "containerRuntimeVersion": runtime,
                                }
                            },
                        }
                    ]
                }

                def command(argv, **_):
                    if argv[:3] == ("kubectl", "config", "current-context"):
                        output = context
                    elif argv[:3] == ("kubectl", "get", "nodes"):
                        output = json.dumps(nodes)
                    elif "{{.Name}}" in argv:
                        output = "node"
                    else:
                        output = "shared-daemon"
                    return SimpleNamespace(output=output, return_code=0)

                with (
                    patch.dict(os.environ, {}, clear=True),
                    patch.object(E2EKubernetesDeployer, "command", side_effect=command),
                    patch.object(
                        KubernetesLocalImageLoader,
                        "_active_cluster_is_local",
                        return_value=True,
                    ),
                    patch(
                        "e2e_util.kubernetes.shutil.which", return_value="/bin/runtime"
                    ),
                    patch("e2e_util.kubernetes.os.geteuid", return_value=0),
                ):
                    loader = KubernetesLocalImageLoader.detect()
                    self.assertEqual(loader.kind, expected)
                    self.assertEqual(loader.node_platform, "linux/arm64")

    def test_architecture_is_unified_even_on_arm64_nodes(self):
        with patch.dict(os.environ, {}, clear=True):
            loader = KubernetesLocalImageLoader("docker", platform="linux/arm64")
            self.assertEqual(loader.platform, "linux/amd64")
            with patch.object(
                E2EImagesDefinitions,
                "inspect",
                return_value={"id": "fixture", "platform": "linux/arm64"},
            ):
                with self.assertRaises(RuntimeError):
                    loader.validate_image("fixture")
            with patch.object(
                E2EImagesDefinitions,
                "inspect",
                return_value={"id": "fixture", "platform": "linux/amd64"},
            ):
                self.assertEqual(loader.validate_image("fixture"), "linux/amd64")

    def test_registry_selection_is_shared_by_both_backends(self):
        for mode, index in (("IN_GFW", 1), ("NON_GFW", 0)):
            with (
                patch.dict(os.environ, {"IN_GFW": mode}, clear=True),
                patch("e2e_util.images.sys.platform", "linux"),
            ):
                for name, pair in E2EImagesDefinitions.MATRIX.items():
                    expected = pair[index]
                    self.assertEqual(E2EImagesDefinitions.get(name), expected)
                    self.assertEqual(
                        E2EImagesDefinitions.rewrite({"image": pair[1]})["image"],
                        expected,
                    )
                    self.assertEqual(
                        E2EImagesDefinitions.environment()[
                            "E2E_IMAGE_" + name.upper().replace("-", "_")
                        ],
                        expected,
                    )

    def test_command_timeout_stops_nested_children(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "child.pid"
            script = (
                "import subprocess,sys,time;from pathlib import Path;"
                "child=subprocess.Popen([sys.executable,'-c','import time;time.sleep(120)']);"
                f"Path({str(path)!r}).write_text(str(child.pid));time.sleep(120)"
            )
            result = E2EProcessHelper.run(
                (sys.executable, "-c", script), timeout_seconds=1, stream=False
            )
            self.assertEqual(result.return_code, 124)
            self.assertTrue(path.is_file())
            child = int(path.read_text())
            state = subprocess.run(
                ["ps", "-p", str(child), "-o", "stat="], capture_output=True, text=True
            ).stdout.strip()
            try:
                self.assertTrue(
                    not state or state.startswith("Z"),
                    f"child {child} survived command timeout: {state}",
                )
            finally:
                if state and not state.startswith("Z"):
                    import signal

                    os.kill(child, signal.SIGKILL)

    def test_reports_archive_preserves_retained_state(self):
        with tempfile.TemporaryDirectory() as temporary:
            helper = type("Reports", (E2EReportHelper,), {"directory": Path(temporary)})
            (helper.directory / ".state").mkdir()
            result = VerificationResult(
                "11", "Readiness", False, 0.1, error="fixture failure"
            )
            helper.write_round(1, result)
            summary = helper.write_summary([[result]])
            self.assertIn("0/1", summary.read_text())
            archive = helper.archive()
            self.assertTrue((helper.directory / ".state").is_dir())
            self.assertTrue((archive / "00_summary.md").is_file())
            self.assertTrue((archive / "round-01/evidence/11").is_dir())


class ComponentRenderingTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        sys.path.insert(0, str(CASE))

    @staticmethod
    def flatten(document):
        if document.get("kind") == "List":
            return [
                child
                for item in document["items"]
                for child in ComponentRenderingTests.flatten(item)
            ]
        return [document]

    def test_authguard_registration_has_valid_returns_and_formal_theme_assets(self):
        from urllib.parse import urlsplit

        from verifier.infra._authguard import AuthGuardRuntime

        registration = AuthGuardRuntime.application_registration()
        for uri in registration["returnUris"]:
            self.assertIn(urlsplit(uri).hostname, registration["hosts"])
        for uri in (registration["logo"], registration["theme"]["stylesheet"]):
            self.assertTrue(
                (
                    ROOT / "deploy/helm/flowgent/files/authguard-theme" / Path(uri).name
                ).is_file()
            )

    def test_component_manifests_preserve_runtime_contracts(self):
        pg = importlib.import_module("verifier.infra._postgres")
        llm = importlib.import_module("verifier.infra._mock_llm")
        sonar = importlib.import_module("verifier.infra._sonarqube")
        notification = importlib.import_module("verifier.infra._mock_notification")
        github = importlib.import_module("verifier.infra._mock_github")
        ldap = importlib.import_module("verifier.infra._ldap")
        captured = []
        successful = SimpleNamespace(returncode=0, stdout="", stderr="")
        with (
            patch.object(
                pg, "subprocess", SimpleNamespace(run=lambda *_, **__: successful)
            ),
            patch.object(
                llm, "subprocess", SimpleNamespace(run=lambda *_, **__: successful)
            ),
            patch.object(pg.PgvectorRuntime, "verify"),
            patch.object(pg.PgvectorRuntime, "_configure_client"),
            patch.object(sonar.SonarQubeRuntime, "_wait_rollout"),
            patch.object(sonar.SonarQubeRuntime, "_forward_client"),
            patch.object(
                sonar.SonarQubeRuntime, "wait_for_sonarqube", return_value=True
            ),
            patch.object(
                sonar.SonarQubeRuntime, "_ensure_api_token", return_value="fixture-only"
            ),
            patch.object(E2EKubernetesDeployer, "apply", side_effect=captured.append),
            patch.object(E2EKubernetesDeployer, "rollout"),
        ):
            pg.PgvectorRuntime.deploy()
            llm.MockLLMRuntime.deploy()
            sonar.SonarQubeRuntime.kubernetes_up()
            notification.MockNotificationDeployment.deploy("e2e-test-notifications")
            github.MockGitHubDeployment.deploy("e2e-test-github")
            ldap.LDAPDeployment.deploy("e2e-test-authguard", "fixture-config")
        for document in captured:
            if document.get("kind") == "List":
                self.assertFalse(
                    any(item.get("kind") == "List" for item in document["items"])
                )
        resources = [child for doc in captured for child in self.flatten(doc)]
        by_name = {(doc["kind"], doc["metadata"]["name"]): doc for doc in resources}
        authguard = importlib.import_module("verifier.infra._authguard")
        self.assertIn(("Service", authguard.LDAP_NAME), by_name)
        for key in [
            ("StatefulSet", "pgvector"),
            ("StatefulSet", "sonarqube-postgres"),
            ("Deployment", "sonarqube"),
            ("Deployment", "sonarqube-mcp"),
            ("Deployment", "mock-llm"),
            ("Deployment", "mock-notification"),
            ("Deployment", "mock-github"),
            ("Deployment", "ldap"),
        ]:
            self.assertIn(key, by_name)
        for doc in resources:
            self.assertTrue(doc["metadata"].get("name"), doc)
            if doc["kind"] not in {"Deployment", "StatefulSet"}:
                continue
            spec = doc["spec"]["template"]["spec"]
            self.assertTrue(spec["containers"])
            for container in spec["containers"]:
                self.assertTrue(container.get("image"), doc["metadata"])
            labels = doc["spec"]["template"]["metadata"]["labels"]
            for key, value in doc["spec"]["selector"]["matchLabels"].items():
                self.assertEqual(labels.get(key), value)
        pg_container = by_name[("StatefulSet", "pgvector")]["spec"]["template"]["spec"][
            "containers"
        ][0]
        self.assertTrue(pg_container["readinessProbe"])
        sonar_pvcs = [
            r
            for r in resources
            if r["kind"] == "PersistentVolumeClaim"
            and r["metadata"]["name"].startswith("sonarqube-")
        ]
        self.assertEqual(len(sonar_pvcs), 3)

    def test_both_case_runners_remain_independent(self):
        for name in ("security-autonomy-fixer", "autotest-generator"):
            directory = ROOT / "use-cases" / name
            result = subprocess.run(
                [sys.executable, "-S", str(directory / "runner.py"), "--help"],
                capture_output=True,
                text=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertFalse((directory / "e2e").exists())
            self.assertFalse((directory / "common").exists())


class VerticalScenarioContractTests(unittest.TestCase):
    def test_canonical_graph_domain_vocabulary(self):
        import re
        removed = ["Agent" + "Flow", "Flow" + "Run", "Task" + "Run", "Sub" + "flow", "Sub" + "Flow", "Node" + "Spec", "Flow" + "Info"]
        pattern = re.compile(r"\b(?:" + "|".join(removed) + r")[A-Za-z0-9_]*\b")
        failures = []
        roots = [ROOT / "pkg", ROOT / "web/src", ROOT / "migration", ROOT / "deploy", ROOT / "docs", ROOT / "use-cases"]
        suffixes = {".go", ".ts", ".tsx", ".py", ".sql", ".yaml", ".md", ".svg"}
        for root in roots:
            for path in root.rglob("*"):
                if not path.is_file() or path.suffix not in suffixes or any(part in {".venv", "reports", "node_modules", "dist"} for part in path.parts):
                    continue
                for line_number, line in enumerate(path.read_text().splitlines(), 1):
                    if pattern.search(line):
                        failures.append(f"{path.relative_to(ROOT)}:{line_number}: {line.strip()}")
        self.assertEqual(failures, [], "Removed domain vocabulary remains:\n" + "\n".join(failures[:30]))

    @classmethod
    def setUpClass(cls):
        sys.path.insert(0, str(CASE))

    @staticmethod
    def completed(output):
        return {"status": "SUCCESS", "output": output}

    def test_agent_reseeding_keeps_the_selected_provider_model(self):
        from unittest.mock import Mock

        from verifier.agentgraph import SecurityAutonomyFixture

        session = Mock()
        session.get.return_value.status_code = 200
        session.put.return_value.status_code = 200
        with patch.dict(
            os.environ, {"FLOWGENT_E2E_AGENT_MODEL": "deepseek/deepseek-chat"}
        ):
            SecurityAutonomyFixture.seed_agents_and_mcps(session)
        agents = [
            call.kwargs["json"]
            for call in session.put.call_args_list
            if "/agents/" in call.args[0]
        ]
        self.assertEqual(len(agents), 7)
        self.assertTrue(
            all(agent["model"] == "deepseek/deepseek-chat" for agent in agents)
        )

    def test_log_collection_reads_only_the_producing_cluster_and_running_containers(
        self,
    ):
        from verifier.context import FlowgentRunEvidence

        evidence = {
            "graph_run_id": "run-ui",
            "namespace": "test",
            "runtime_mode": "session",
            "plan_id": "plan-ui",
        }
        pods = {
            "items": [
                {
                    "metadata": {"name": "session-worker"},
                    "status": {
                        "containerStatuses": [
                            {"name": "worker", "state": {"running": {}}},
                            {"name": "waiting", "state": {"waiting": {}}},
                        ]
                    },
                }
            ]
        }
        with (
            patch.object(
                FlowgentRunEvidence, "runtime_namespaces", return_value=("system",)
            ),
            patch.object(
                E2EKubernetesDeployer, "resource", return_value=pods
            ) as resource,
            patch.object(
                E2EProcessHelper,
                "run",
                return_value=SimpleNamespace(
                    passed=True, output="INFO plan_id=plan-ui task succeeded"
                ),
            ) as command,
        ):
            self.assertEqual(
                len(
                    FlowgentRunEvidence.logs(SimpleNamespace(deployer="k8s"), evidence)
                ),
                1,
            )
        resource.assert_called_once_with(
            "pods", "system", selector="flowgent.io/runtime-cluster=session"
        )
        self.assertEqual(command.call_count, 1)
        self.assertIn("worker", command.call_args.args[0])

    def test_scanner_requires_a_delivery_commit_and_never_uses_a_tree_or_latest_job(
        self,
    ):
        Scanner = importlib.import_module(
            "deploy.sonar-scanner-service.source.server"
        ).Scanner

        sha = "a" * 40
        self.assertEqual(
            Scanner.commit({"text": json.dumps({"commit": {"sha": sha}})}), sha
        )
        self.assertIsNone(Scanner.commit({"tree": {"sha": sha}}))
        with self.assertRaisesRegex(ValueError, "delivery commit"):
            Scanner.start(
                {
                    "repo": "rengine",
                    "ref": "master",
                    "require_delivery": True,
                    "delivery_existing": {"tree": {"sha": sha}},
                }
            )
        with self.assertRaisesRegex(ValueError, "latest-task fallback"):
            Scanner.wait({"job_id": "missing-exact-job"})

    def test_plan_log_correlation_accepts_actual_go_text_and_json_formats(self):
        from verifier.otel.s52_logs import E2ES52LogsVerifier

        event = {
            "level": "INFO",
            "msg": "slot worker task succeeded",
            "plan_id": "plan-ui",
        }
        text_line = "[pod/taskmanager] 2026/11/13 14:25:51 INFO slot worker task succeeded slot_id=tm-1 plan_id=plan-ui node_instance_id=console-sandbox"
        for line in (text_line, "[pod/taskmanager] " + json.dumps(event)):
            parsed = E2ES52LogsVerifier.parse_event(line)
            for key, value in event.items():
                self.assertEqual(parsed[key], value)

    def test_info_lifecycle_requires_start_result_and_success_without_debug(self):
        from unittest.mock import Mock

        from verifier.context import FlowgentRunEvidence
        from verifier.otel.s52_logs import E2ES52LogsVerifier

        verifier = object.__new__(E2ES52LogsVerifier)
        events = [
            {"level": "INFO", "msg": "sandbox trigger received", "plan_id": "plan-ui"},
            {
                "level": "INFO",
                "msg": "sandbox result published",
                "plan_id": "plan-ui",
                "has_error": False,
            },
            {
                "level": "INFO",
                "msg": "slot worker task succeeded",
                "plan_id": "plan-ui",
            },
        ]
        verifier.execution = {
            "graph_run_id": "run-ui",
            "namespace": "test",
            "node_instance_id": "sandbox",
            "plan_id": "plan-ui",
            "logs": [json.dumps(event) for event in events],
        }
        verifier.record_evidence = Mock()
        with (
            tempfile.TemporaryDirectory() as directory,
            patch.object(
                FlowgentRunEvidence, "persisted", return_value={"plan_id": "plan-ui"}
            ),
            patch.object(
                verifier, "evidence_path", return_value=Path(directory) / "events.json"
            ),
        ):
            verifier._verify_logs()
            verifier.execution["logs"] = verifier.execution["logs"][:-1]
            with self.assertRaisesRegex(AssertionError, "slot worker task succeeded"):
                verifier._verify_logs()

    def test_missing_human_gate_cannot_pass_a_false_condition(self):
        from verifier.agentgraph.s33_remediation import E2ES33RemediationVerifier

        tasks = {
            "committee": self.completed({"decision": False}),
            "is-approved": self.completed({"result": False}),
        }
        with self.assertRaisesRegex(AssertionError, "human-approval"):
            E2ES33RemediationVerifier.verify_condition_human(tasks)

    def test_committee_must_match_the_actual_review_votes(self):
        from verifier.agentgraph.s33_remediation import E2ES33RemediationVerifier

        tasks = {
            node: self.completed({"decision": True})
            for node in ("review-security", "review-quality", "review-arch")
        }
        tasks["committee"] = self.completed(
            {"decision": False, "approve": 0, "total": 3}
        )
        with self.assertRaisesRegex(AssertionError, "differs"):
            E2ES33RemediationVerifier.verify_vote(tasks)

    def test_missing_report_is_not_a_path_dependent_pass(self):
        from verifier.agentgraph.s34_delivery_report import E2ES34DeliveryReportVerifier

        with self.assertRaisesRegex(AssertionError, "summary-report"):
            E2ES34DeliveryReportVerifier.verify_report({})

    def test_aggregation_cannot_invent_source_findings(self):
        from verifier.agentgraph.s32_discovery_analyze import (
            E2ES32DiscoveryAnalyzeVerifier,
        )

        issue = {
            "id": "invented",
            "repo": "repository",
            "severity": "high",
            "type": "sast",
            "file": "README.md",
            "description": "fixture",
        }
        tasks = {
            "aggregate-issues": self.completed({"issues": [issue]}),
            "scan-sonarqube": self.completed({"issues": [{"key": "actual-sonar-id"}]}),
        }
        with self.assertRaisesRegex(AssertionError, "invented"):
            E2ES32DiscoveryAnalyzeVerifier.verify_analyze(tasks)

    def test_mcp_sonar_envelope_preserves_actual_identifiers(self):
        from verifier.agentgraph import SecurityAutonomyFixture

        envelope = {
            "content": [
                {
                    "type": "text",
                    "text": json.dumps({"issues": [{"key": "source-123"}]}),
                }
            ]
        }
        self.assertEqual(
            SecurityAutonomyFixture.sonar_issues(envelope), [{"key": "source-123"}]
        )
        with self.assertRaises(AssertionError):
            SecurityAutonomyFixture.sonar_issues({"status": "completed"})

    def test_wrong_prometheus_values_do_not_pass_on_presence(self):
        from verifier.otel.s51_metrics import E2ES51MetricsVerifier

        verifier = object.__new__(E2ES51MetricsVerifier)
        verifier.execution = {"agent_graph_id": "unique-ui-graph"}
        verifier.context = SimpleNamespace(deployer="k8s")
        verifier.details = []
        verifier.prometheus = SimpleNamespace(
            query=lambda expression: [
                {"value": [0, "1" if expression.startswith("up{") else "2"]}
            ]
        )
        with (
            patch("verifier.otel.s51_metrics.time.monotonic", side_effect=[0, 1, 31]),
            patch("verifier.otel.s51_metrics.time.sleep"),
            patch(
                "verifier.context.RUN_ID_PATH",
                SimpleNamespace(read_text=lambda: "main-run"),
            ),
            patch(
                "verifier.otel.s51_metrics.SecurityAutonomyFixture.get_tasks",
                return_value=[],
            ),
            patch(
                "verifier.otel.s51_metrics.SecurityAutonomyFixture.active_path",
                return_value=["node"],
            ),
            patch(
                "verifier.otel.s51_metrics.SecurityAutonomyFixture.assert_persisted_attempts"
            ),
        ):
            with self.assertRaisesRegex(AssertionError, "do not match"):
                verifier._verify_metrics()

    def test_autotest_preflight_blocks_before_deployment(self):
        directory = ROOT / "use-cases/autotest-generator"
        code = """
import contextlib, io, runner, sys, tempfile
from pathlib import Path
from unittest.mock import patch
from util.report import E2EReportHelper
with tempfile.TemporaryDirectory() as directory, patch.object(E2EReportHelper, 'directory', Path(directory)), patch.object(runner.E2ERunner, 'run_pipeline', side_effect=AssertionError('deployment must not run')), patch.object(sys, 'argv', ['runner.py', '--no-verify']), contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
    try:
        runner.E2ERunner.main()
    except SystemExit as error:
        assert error.code == 1, error.code
    else:
        raise AssertionError('Incomplete application was accepted')
"""
        result = subprocess.run(
            [sys.executable, "-c", code], cwd=directory, capture_output=True, text=True
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        empty = subprocess.run(
            [
                sys.executable,
                "-c",
                "import runner;runner.SCENARIOS={};assert runner.E2ERunner.run_verification(None) is False",
            ],
            cwd=directory,
            capture_output=True,
            text=True,
        )
        self.assertEqual(empty.returncode, 0, empty.stderr)

    def test_all_case_scenarios_use_the_explicit_class_contract(self):
        import ast
        import re

        for directory in (ROOT / "use-cases").iterdir():
            if not directory.is_dir():
                continue
            for path in directory.glob("verifier/*/s[0-9]*.py"):
                tree = ast.parse(path.read_text())
                self.assertFalse(
                    any(isinstance(node, ast.FunctionDef) for node in tree.body),
                    str(path),
                )
                classes = [node for node in tree.body if isinstance(node, ast.ClassDef)]
                self.assertEqual(len(classes), 1, str(path))
                match = re.fullmatch(r"s(\d+)_(.+)", path.stem)
                expected = (
                    "E2ES"
                    + match[1]
                    + "".join(part.title() for part in match[2].split("_"))
                    + "Verifier"
                )
                self.assertEqual(classes[0].name, expected)
                methods = {
                    node.name
                    for node in classes[0].body
                    if isinstance(node, ast.FunctionDef)
                }
                self.assertTrue({"deploy", "init", "verifier"} <= methods, str(path))

    def test_telemetry_keeps_both_ui_and_main_scenario_coverage(self):
        from unittest.mock import Mock

        from verifier.otel.s13_tracing import E2ES13TracingVerifier
        from verifier.otel.s52_logs import E2ES52LogsVerifier

        for implementation, ui_operation, main_operation in (
            (E2ES13TracingVerifier, "_verify_trace", "_verify_main_trace"),
            (E2ES52LogsVerifier, "_verify_logs", "_verify_main_logs"),
        ):
            verifier = object.__new__(implementation)
            verifier.execute = lambda operation: operation()
            verifier.step = lambda label, operation: operation()
            setattr(verifier, ui_operation, Mock())
            setattr(verifier, main_operation, Mock())
            verifier.verifier(None)
            getattr(verifier, ui_operation).assert_called_once()
            getattr(verifier, main_operation).assert_called_once()

    def test_prometheus_chart_has_only_scoped_pod_discovery_permissions(self):
        document = E2EKubernetesDeployer.helm_template(
            ROOT / "deploy/helm/flowgent",
            {
                "prometheus": {
                    "enabled": True,
                    "name": "prometheus",
                    "namespace": "e2e-test-system",
                    "image": "prometheus-test:local",
                    "serviceAccountName": "prometheus",
                    "labels": {"flowgent.io/e2e-owner": "e2e-test"},
                    "watchedNamespaces": ["e2e-test-system", "e2e-test-workload"],
                }
            },
            template="prometheus.yaml",
        )
        resources = document["items"]
        roles = [item for item in resources if item["kind"] == "Role"]
        self.assertEqual(
            {item["metadata"]["namespace"] for item in roles},
            {"e2e-test-system", "e2e-test-workload"},
        )
        self.assertFalse(any(item["kind"] == "ClusterRole" for item in resources))
        for role in roles:
            self.assertEqual(
                role["rules"],
                [
                    {
                        "apiGroups": [""],
                        "resources": ["pods"],
                        "verbs": ["get", "list", "watch"],
                    }
                ],
            )
        deployment = next(item for item in resources if item["kind"] == "Deployment")
        self.assertEqual(
            deployment["spec"]["template"]["spec"]["serviceAccountName"], "prometheus"
        )

    def test_e2e_profile_enables_real_metrics_and_full_trace_sampling(self):
        import yaml
        from util.constant import E2EOtherDefinitions as config
        from verifier.infra._application import FlowgentRuntime

        rendered = E2EKubernetesDeployer.helm_template(
            config.HELM_CHART,
            {"runtime": {"flowgent-config": FlowgentRuntime.telemetry_profile()}},
            template="configmap.yaml",
        )
        document = next(
            item for item in rendered["items"] if item.get("kind") == "ConfigMap"
        )
        management = yaml.safe_load(document["data"]["flowgent.yaml"])["mgmt"]
        self.assertEqual(management["otel"]["sample_rate"], 1.0)
        self.assertTrue(management["metrics"]["enabled"])
        self.assertTrue(management["metrics"]["prometheus"])


class DeploymentStandardTests(unittest.TestCase):
    def test_external_middleware_omits_workloads_and_preserves_connections(self):
        import yaml

        values = {
            name: {"enabled": False}
            for name in ("postgresql", "redis", "emqx", "jaeger", "prometheus", "envoy")
        }
        values["postgresql"]["externalSecret"] = "external-pg"
        values["emqx"]["externalSecret"] = "external-mqtt"
        values["redis"]["externalSecret"] = "external-redis"
        values["cache"] = {
            "provider": "Redis",
            "redis": {"nodes": ["redis://cache.example:6379"]},
        }
        values["messaging"] = {"mqtt": {"broker": "tcp://mqtt.example:1883"}}
        values["jaeger"].update(
            {
                "collectorEndpoint": "http://otel.example:4318",
                "queryEndpoint": "http://traces.example:16686",
            }
        )
        document = E2EKubernetesDeployer.helm_template(
            ROOT / "deploy/helm/flowgent", values
        )
        workloads = [
            item
            for item in document["items"]
            if item["kind"] in {"Deployment", "StatefulSet"}
        ]
        for item in workloads:
            self.assertNotIn(
                item["metadata"].get("labels", {}).get("app.kubernetes.io/component"),
                {"postgresql", "redis", "emqx", "jaeger", "prometheus", "envoy"},
            )
        api = next(
            item
            for item in workloads
            if item["metadata"]["name"].endswith("-apiserver")
        )
        environment = {
            item["name"]: item
            for item in api["spec"]["template"]["spec"]["containers"][0]["env"]
        }
        for name, secret in (
            ("FLOWGENT__STORAGE__POSTGRES__DSN", "external-pg"),
            ("FLOWGENT__MESSAGER__MQTT__BROKER", "external-mqtt"),
            ("FLOWGENT_REDIS_PASSWORD", "external-redis"),
        ):
            self.assertEqual(
                environment[name]["valueFrom"]["secretKeyRef"]["name"], secret
            )
        cm = next(
            item
            for item in document["items"]
            if item["kind"] == "ConfigMap" and "flowgent.yaml" in item.get("data", {})
        )
        config = yaml.safe_load(cm["data"]["flowgent.yaml"])
        self.assertEqual(
            config["cache"]["redis"]["nodes"], ["redis://cache.example:6379"]
        )
        self.assertEqual(
            config["messager"]["mqtt"]["broker"], "tcp://mqtt.example:1883"
        )
        self.assertEqual(config["mgmt"]["otel"]["endpoint"], "http://otel.example:4318")
        self.assertEqual(
            config["mgmt"]["otel"]["query_endpoint"], "http://traces.example:16686"
        )

    def test_formal_compose_middleware_switches_and_external_endpoints(self):
        environment = {
            **os.environ,
            **{
                name + "_ENABLED": "0"
                for name in (
                    "POSTGRES",
                    "REDIS",
                    "EMQX",
                    "JAEGER",
                    "PROMETHEUS",
                    "ENVOY",
                    "AUTHGUARD",
                )
            },
            "POSTGRES_DSN": "postgres://user:password@database.example:5432/flowgent",
            "MQTT_BROKER": "tcp://mqtt.example:1883",
            "OTLP_ENDPOINT": "http://otel.example:4318",
        }
        result = subprocess.run(
            [
                "docker",
                "compose",
                "-f",
                str(ROOT / "deploy/docker/docker-compose.yaml"),
                "config",
                "--format",
                "json",
            ],
            env=environment,
            capture_output=True,
            text=True,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        services = json.loads(result.stdout)["services"]
        for name, service in services.items():
            if name not in {"flowgent", "web"}:
                self.assertEqual(service["deploy"]["replicas"], 0, name)
        app = services["flowgent"]["environment"]
        self.assertEqual(
            app["FLOWGENT__STORAGE__POSTGRES__DSN"], environment["POSTGRES_DSN"]
        )
        self.assertEqual(
            app["FLOWGENT__MESSAGER__MQTT__BROKER"], environment["MQTT_BROKER"]
        )
        self.assertEqual(
            app["FLOWGENT__MGMT__OTEL__ENDPOINT"], environment["OTLP_ENDPOINT"]
        )

    def test_sonar_compose_inheritance_renders_in_both_regions_and_architectures(self):
        import shutil

        docker = shutil.which("docker")
        if not docker:
            self.skipTest("Docker CLI is required to validate Compose inheritance")
        command = [docker, "compose"]
        if subprocess.run([*command, "version"], capture_output=True).returncode:
            fallback = shutil.which("docker-compose")
            if not fallback:
                self.skipTest("Compose is required to validate inheritance")
            command = [fallback]
        for case in (ROOT / "use-cases").iterdir():
            if not case.is_dir():
                continue
            files = sorted(case.glob("deploy/sonarqube*/docker-compose.yaml"))
            if not files:
                continue
            for region in ("true", "false"):
                for architecture in ("linux/arm64", "linux/amd64"):
                    with patch.dict(
                        os.environ,
                        {
                            "IN_GFW": region,
                            "E2E_ENGINE_PLATFORM": architecture,
                        },
                        clear=True,
                    ):
                        images = E2EImagesDefinitions.environment()
                    result = subprocess.run(
                        [
                            *command,
                            "-p",
                            "e2e-contract",
                            *(part for path in files for part in ("-f", str(path))),
                            "config",
                            "--format",
                            "json",
                        ],
                        env={
                            **{
                                name: os.environ[name]
                                for name in (
                                    "PATH",
                                    "HOME",
                                    "USERPROFILE",
                                    "SYSTEMROOT",
                                    "TEMP",
                                    "TMP",
                                )
                                if name in os.environ
                            },
                            "FLOWGENT_REPO_DIR": str(ROOT),
                            "SONARQUBE_TOKEN": "contract-fixture",
                            **images,
                        },
                        capture_output=True,
                        text=True,
                    )
                    self.assertEqual(result.returncode, 0, result.stderr)
                    service = json.loads(result.stdout)["services"]["sonarqube"]
                    resources = service["deploy"]["resources"]
                    self.assertLessEqual(
                        int(resources["reservations"]["memory"]),
                        int(resources["limits"]["memory"]),
                    )
                    self.assertEqual(service["platform"], architecture)

    def test_all_components_and_self_project_links_follow_the_contract(self):
        for case in (ROOT / "use-cases").iterdir():
            if not case.is_dir():
                continue
            for component in (case / "deploy").iterdir():
                if not component.is_dir() or component.name.startswith("__"):
                    continue
                with self.subTest(case=case.name, component=component.name):
                    for name in (
                        "Dockerfile",
                        "docker-compose.yaml",
                    ):
                        self.assertTrue(
                            (component / name).is_file(), str(component / name)
                        )
                    chart = (
                        ROOT / "deploy/helm/flowgent"
                        if component.name == "flowgent"
                        else component / "helm"
                    )
                    self.assertTrue((chart / "Chart.yaml").is_file(), str(chart))
                    self.assertFalse(
                        any(
                            path.name == "tests"
                            for path in (component / "source").rglob("*")
                            if path.is_dir()
                        ),
                        str(component),
                    )
                    if component.name == "flowgent":
                        self.assertFalse((component / "source").exists())
                        self.assertFalse((component / "helm").exists())
                        self.assertFalse((component / "helm").is_symlink())
                        for path in component.iterdir():
                            if path.is_symlink():
                                self.assertTrue(path.exists(), str(path))
                                self.assertTrue(
                                    Path(
                                        os.path.abspath(path.parent / path.readlink())
                                    ).is_relative_to(ROOT / "deploy"),
                                    str(path),
                                )
                            else:
                                self.assertIn(
                                    path.name, {"config", "docker-compose.yaml"}
                                )
                        for name in ("Dockerfile", "Dockerfile.dockerignore"):
                            self.assertTrue(
                                (component / name).is_symlink(), str(component / name)
                            )
                    else:
                        self.assertTrue((component / "source").is_dir())

    def test_platform_and_solana_policy_for_all_host_families(self):
        for host in ("linux", "darwin", "win32"):
            for mode in ("true", "false"):
                with (
                    self.subTest(host=host, mode=mode),
                    patch.dict(os.environ, {"IN_GFW": mode}, clear=True),
                    patch("e2e_util.images.sys.platform", host),
                ):
                    environment = E2EImagesDefinitions.environment()
                    self.assertEqual(
                        environment["DOCKER_DEFAULT_PLATFORM"], "linux/amd64"
                    )
                    for name in (
                        *E2EImagesDefinitions.MATRIX,
                        *E2EImagesDefinitions.LOCAL_IMAGES,
                    ):
                        expected = (
                            "linux/arm64"
                            if host == "darwin" and name == "solana"
                            else "linux/amd64"
                        )
                        self.assertEqual(E2EImagesDefinitions.platform(name), expected)
                        self.assertEqual(
                            environment[
                                "E2E_PLATFORM_" + name.upper().replace("-", "_")
                            ],
                            expected,
                        )
                    if host == "darwin":
                        self.assertEqual(
                            E2EImagesDefinitions.get("solana"),
                            E2EImagesDefinitions.MACOS_IMAGES["solana"][
                                int(mode == "true")
                            ],
                        )

    def test_registry_rewrite_does_not_corrupt_longer_tags(self):
        source = E2EImagesDefinitions.MATRIX["postgres18"][1]
        with patch.dict(os.environ, {"IN_GFW": "false"}, clear=True):
            self.assertEqual(
                E2EImagesDefinitions.rewrite(source),
                E2EImagesDefinitions.MATRIX["postgres18"][0],
            )
            unknown = "docker.io/emqx/emqx:5.5.0-not-in-the-matrix"
            self.assertEqual(E2EImagesDefinitions.rewrite(unknown), unknown)
            payload = 'image: "' + source + '"\n'
            self.assertIn(
                E2EImagesDefinitions.MATRIX["postgres18"][0],
                E2EImagesDefinitions.rewrite(payload),
            )

    def test_compose_images_and_platforms_have_no_literal_fallbacks(self):
        import re

        import yaml

        for path in (ROOT / "use-cases").glob("*/deploy/*/docker-compose.yaml"):
            for service in (
                yaml.safe_load(path.read_text()).get("services", {}).values()
            ):
                self.assertRegex(
                    service["image"], r"^\$\{E2E_IMAGE_[A-Z0-9_]+\}$", str(path)
                )
                suffix = re.fullmatch(
                    r"\$\{E2E_IMAGE_([A-Z0-9_]+)\}", service["image"]
                )[1]
                self.assertEqual(
                    service["platform"], "${E2E_PLATFORM_" + suffix + "}", str(path)
                )

    def test_private_values_file_can_be_opened_by_a_child(self):
        with E2EProcessHelper.private_file('{"fixture":true}') as path:
            result = E2EProcessHelper.run(
                (
                    sys.executable,
                    "-c",
                    "import pathlib,sys;print(pathlib.Path(sys.argv[1]).read_text())",
                    str(path),
                ),
                stream=False,
            )
            self.assertTrue(result.passed)
            self.assertEqual(json.loads(result.output), {"fixture": True})
        self.assertFalse(path.exists())

    def test_windows_process_tree_cleanup_uses_taskkill(self):
        from unittest.mock import Mock

        process = Mock(pid=1234)
        process.poll.return_value = None
        with (
            patch("e2e_util.process.sys.platform", "win32"),
            patch(
                "e2e_util.process.subprocess.run",
                return_value=SimpleNamespace(returncode=0),
            ) as taskkill,
        ):
            self.assertIn("creationflags", E2EProcessHelper.process_group_options())
            E2EProcessHelper._terminate_tree(process)
            taskkill.assert_called_once_with(
                ["taskkill", "/PID", "1234", "/T", "/F"],
                capture_output=True,
                timeout=10,
            )
            process.communicate.assert_called_once()

    def test_completed_process_cleanup_does_not_signal_a_reused_pid(self):
        from unittest.mock import Mock

        process = Mock(pid=1234)
        process.poll.return_value = 0
        with patch("e2e_util.process.os.killpg") as kill:
            E2EProcessHelper._terminate_tree(process)
        kill.assert_not_called()

    def test_docker_discovery_uses_amd64_for_native_and_vm_contexts(self):
        from e2e_util.docker import E2EDockerDeployer

        for context in (
            "default",
            "colima",
            "colima-review",
            "orbstack",
            "desktop-linux",
        ):

            def run(command, **_):
                return SimpleNamespace(
                    passed=True,
                    output=context
                    if command[:3] == ("docker", "context", "show")
                    else json.dumps({"Architecture": "aarch64", "OSType": "linux"}),
                )

            with (
                self.subTest(context=context),
                patch.dict(os.environ, {}, clear=True),
                patch.object(E2EProcessHelper, "run", side_effect=run),
            ):
                self.assertEqual(
                    E2EDockerDeployer.detect()["DOCKER_DEFAULT_PLATFORM"], "linux/amd64"
                )

    def test_formal_chart_images_are_resolved_for_both_regions(self):
        with patch("e2e_util.images.sys.platform", "linux"):
            for mode in ("true", "false"):
                with (
                    self.subTest(mode=mode),
                    patch.dict(
                        os.environ,
                        {"IN_GFW": mode, "PATH": os.environ["PATH"]},
                        clear=True,
                    ),
                ):
                    core, tag = E2EImagesDefinitions.parts("flowgent")
                    web, web_tag = E2EImagesDefinitions.parts("flowgent-web")
                    document = E2EKubernetesDeployer.helm_template(
                        ROOT / "deploy/helm/flowgent",
                        {
                            "global": {"image": {"repository": core, "tag": tag}},
                            "web": {"image": {"repository": web, "tag": web_tag}},
                            "postgresql": {"enabled": True},
                            "emqx": {
                                "enabled": True,
                                "image": E2EImagesDefinitions.get("emqx"),
                            },
                        },
                    )
                    references = E2EImagesDefinitions.references(document)
                    for reference in references:
                        self.assertIsNotNone(
                            E2EImagesDefinitions.key_for(reference), reference
                        )
                        if (
                            E2EImagesDefinitions.key_for(reference)
                            not in E2EImagesDefinitions.LOCAL_IMAGES
                        ):
                            self.assertEqual(
                                reference.startswith("registry.cn-"),
                                mode == "true",
                                reference,
                            )
                    postgres = next(
                        item
                        for item in document["items"]
                        if item.get("kind") == "StatefulSet"
                        and item["metadata"]["name"].endswith("-postgresql")
                    )
                    environment = {
                        item["name"]: item.get("value")
                        for item in postgres["spec"]["template"]["spec"]["containers"][
                            0
                        ]["env"]
                    }
                    self.assertEqual(
                        environment["PGDATA"], "/var/lib/postgresql/data/pgdata"
                    )
                    container = postgres["spec"]["template"]["spec"]["containers"][0]
                    secret_name = container["envFrom"][0]["secretRef"]["name"]
                    secret = next(
                        item
                        for item in document["items"]
                        if item["kind"] == "Secret"
                        and item["metadata"]["name"] == secret_name
                    )
                    self.assertIn("POSTGRES_USER", secret["stringData"])
                    self.assertNotIn("POSTGRESQL_USERNAME", secret["stringData"])

    def test_legacy_stdio_components_package_their_real_transport(self):
        for component in ("github", "sonarqube-mcp"):
            document = E2EKubernetesDeployer.helm_template(
                ROOT / "use-cases/autotest-generator/deploy" / component / "helm",
                {
                    "enabled": True,
                    "image": "stdio-test:local",
                    "existingSecret": "fixture",
                    "upstreamURL": "http://sonarqube:9000",
                },
            )
            self.assertEqual([item["kind"] for item in document["items"]], ["Pod"])
            container = document["items"][0]["spec"]["containers"][0]
            self.assertTrue(container["stdin"])
            self.assertNotIn("ports", container)

    def test_jump_host_transfer_uses_the_real_upstream_and_destination(self):
        image = E2EImagesDefinitions.MATRIX["redis"][1]
        with (
            patch.dict(os.environ, {"IN_GFW": "true"}, clear=True),
            patch("e2e_util.images.sys.platform", "linux"),
            patch("e2e_util.images.Path.is_file", return_value=True),
            patch.object(
                E2EProcessHelper, "run", return_value=SimpleNamespace(passed=True)
            ) as run,
            patch.object(
                E2EImagesDefinitions,
                "inspect",
                return_value={"id": "verified", "platform": "linux/amd64"},
            ),
        ):
            E2EImagesDefinitions._pull(image, jump_host=True)
            command = run.call_args_list[0].args[0]
            environment = run.call_args_list[0].kwargs["environment"]
            self.assertEqual(
                command[1:],
                (
                    "docker.io/bitnamilegacy/redis-cluster:7.0.14",
                    "bitnami_redis-cluster",
                ),
            )
            self.assertEqual(environment["MIRROR_NS"], "wl4g-k8s")
            self.assertEqual(environment["MIRROR_PLATFORM"], "linux/amd64")

    def test_case_python_does_not_embed_concrete_image_references(self):
        import ast

        for case in (ROOT / "use-cases").iterdir():
            if not case.is_dir():
                continue
            for directory in (case / "deploy", case / "util", case / "verifier"):
                for path in directory.rglob("*.py"):
                    if path.is_symlink():
                        continue
                    for node in ast.walk(ast.parse(path.read_text())):
                        if isinstance(node, ast.Constant) and isinstance(
                            node.value, str
                        ):
                            self.assertNotIn(
                                node.value,
                                E2EImagesDefinitions._substitutions(),
                                f"Image literal outside the matrix: {path}",
                            )
