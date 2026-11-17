"""Scenario-owned official scanner deployment and initial real analysis."""

import json
import os
import secrets
import time
from contextlib import contextmanager

import requests
from util.constant import E2EOtherDefinitions as config
from util.images import E2EImagesDefinitions
from util.kubernetes import E2EKubernetesDeployer


class SonarScannerRuntime:
    NAME = "sonar-scanner"
    BASELINE = config.REPORTS_DIR / ".state" / "sonar_baseline.json"

    @staticmethod
    def configure_environment(context, token=None) -> dict:
        token = (
            token or os.getenv("SONAR_SCANNER_API_TOKEN") or secrets.token_urlsafe(32)
        )
        host = (
            "sonar-scanner"
            if context.deployer == "docker"
            else f"sonar-scanner.{context.namespace}.svc.cluster.local"
        )
        values = {
            "SONAR_SCANNER_API_TOKEN": token,
            "SONAR_SCANNER_MCP_URL": f"http://{host}:8080/mcp",
        }
        os.environ.update(values)
        return values

    @staticmethod
    def deploy(context) -> None:
        if context.deployer != "docker":
            values = {
                "name": "sonar-scanner",
                "namespace": context.namespace,
                "image": E2EImagesDefinitions.get("sonar-scanner"),
                "port": 8080,
                "podSecurityContext": {"fsGroup": 1000},
                "persistence": {
                    "enabled": True,
                    "name": "sonar-scanner-cache",
                    "size": "2Gi",
                },
                "labels": {"flowgent.io/e2e-owner": config.RESOURCE_PREFIX},
                "command": ["python3", "-u", "/opt/e2e/server.py"],
                "configName": "sonar-scanner-source",
                "configData": {
                    "server.py": (
                        config.USE_CASE_DIR
                        / "deploy/sonar-scanner-service/source/server.py"
                    ).read_text()
                },
                "env": [
                    {
                        "name": "SONAR_HOST_URL",
                        "value": f"http://sonarqube.{config.SONARQUBE_NAMESPACE}.svc.cluster.local:9000",
                    },
                    {"name": "SONAR_SCANNER_JAVA_OPTS", "value": "-Xms64m -Xmx512m"},
                    {"name": "SONAR_USER_HOME", "value": "/tmp/.sonar"},
                    *(
                        {
                            "name": name,
                            "valueFrom": {
                                "secretKeyRef": {
                                    "name": "flowgent-runtime-env",
                                    "key": key,
                                    "optional": optional,
                                }
                            },
                        }
                        for name, key, optional in (
                            ("SONAR_TOKEN", "SONARQUBE_TOKEN", False),
                            (
                                "SONAR_SCANNER_API_TOKEN",
                                "SONAR_SCANNER_API_TOKEN",
                                False,
                            ),
                            ("GITHUB_TOKEN", "GITHUB_TOKEN", True),
                            ("GH_TOKEN", "GH_TOKEN", True),
                            ("HTTPS_PROXY", "HTTPS_PROXY", True),
                            ("NO_PROXY", "NO_PROXY", True),
                        )
                    ),
                ],
                "volumes": [
                    {"name": "source", "configMap": {"name": "sonar-scanner-source"}},
                    {
                        "name": "cache",
                        "persistentVolumeClaim": {"claimName": "sonar-scanner-cache"},
                    },
                ],
                "volumeMounts": [
                    {
                        "name": "source",
                        "mountPath": "/opt/e2e/server.py",
                        "subPath": "server.py",
                        "readOnly": True,
                    },
                    {"name": "cache", "mountPath": "/tmp/.sonar"},
                ],
                "resources": {
                    "requests": {"cpu": "50m", "memory": "128Mi"},
                    "limits": {"cpu": "1", "memory": "3Gi"},
                },
            }
            E2EKubernetesDeployer.apply(
                E2EKubernetesDeployer.helm_template(
                    config.USE_CASE_DIR / "deploy/sonar-scanner-service/helm", values
                )
            )
            E2EKubernetesDeployer.rollout(
                context.namespace, "sonar-scanner", restart=True
            )
        with SonarScannerRuntime.client(context) as session:
            session.get(session.scanner_url + "/healthz", timeout=10).raise_for_status()

    @staticmethod
    @contextmanager
    def client(context):
        with requests.Session() as session:
            session.trust_env = False
            session.headers["Authorization"] = (
                "Bearer " + os.environ["SONAR_SCANNER_API_TOKEN"]
            )
            if context.deployer == "docker":
                session.scanner_url = f"http://127.0.0.1:{config.LOCAL_SCANNER_PORT}"
                yield session
            else:
                with E2EKubernetesDeployer.port_forward(
                    context.namespace, "service/sonar-scanner", 8080
                ) as port:
                    session.scanner_url = f"http://127.0.0.1:{port}"
                    yield session

    @staticmethod
    def initialize(context) -> dict:
        SonarScannerRuntime.BASELINE.unlink(missing_ok=True)
        with SonarScannerRuntime.client(context) as session:
            started = session.post(
                session.scanner_url + "/scans",
                json={"repo": "rengine", "ref": "master"},
                timeout=15,
            )
            started.raise_for_status()
            identifier = started.json()["job_id"]
            last_progress = None
            deadline = time.monotonic() + 1200
            while time.monotonic() < deadline:
                response = session.get(
                    session.scanner_url + "/scans/" + identifier, timeout=10
                )
                response.raise_for_status()
                result = response.json()
                progress = (
                    result.get("phase", result["status"]),
                    result.get("downloaded_bytes", 0) // 1048576,
                )
                if progress != last_progress:
                    print(
                        f"  Official scanner: {progress[0]}, source={progress[1]} MiB",
                        flush=True,
                    )
                    last_progress = progress
                if result["status"] == "FAILED":
                    raise AssertionError(
                        "Initial real Sonar analysis failed: " + result["error"]
                    )
                if result["status"] == "SUCCESS":
                    if not result.get("issue_ids"):
                        raise AssertionError(
                            "Positive remediation scenario needs real source findings"
                        )
                    SonarScannerRuntime.BASELINE.parent.mkdir(
                        parents=True, exist_ok=True
                    )
                    SonarScannerRuntime.BASELINE.write_text(
                        json.dumps(result, indent=2) + "\n"
                    )
                    print(
                        f"Real initial analysis: commit={result['commit']} CE={result['ce_task_id']} analysis={result['analysis_id']} issues={result['issue_total']}"
                    )
                    return result
                time.sleep(3)
        raise AssertionError("Initial official scanner analysis timed out")

    @staticmethod
    def bind_run(run_id: str) -> None:
        result = json.loads(SonarScannerRuntime.BASELINE.read_text())
        result["run_id"] = run_id
        SonarScannerRuntime.BASELINE.write_text(json.dumps(result, indent=2) + "\n")
