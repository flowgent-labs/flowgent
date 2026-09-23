"""Isolated Flowgent Kubernetes lifecycle and localhost verification tunnels."""

from __future__ import annotations

import glob
import json
import os
import re
import shutil
import shlex
import signal
import socket
import subprocess
import time
import urllib.request
from typing import Iterator

from common import config
from deploy import BaseDeployer
from common.model import CommandResult, RunContext
from common.process import CommandRunner


CORE_BUILD_TIMEOUT_SECONDS = 20 * 60
KUBERNETES_IMAGE_LOADER_ENV = "FLOWGENT_E2E_KUBERNETES_IMAGE_LOADER"
KUBERNETES_CLUSTER_ENV = "FLOWGENT_E2E_KUBERNETES_CLUSTER"
CORE_IMAGE = "localhost/flowgent:latest"
WEB_IMAGE = "localhost/flowgent-web:latest"
AUTHGUARD_IMAGE = "localhost/e2e-flowgent-authguard:0.1.0"
AUTHGUARD_WEB_IMAGE = "localhost/e2e-flowgent-authguard-web:0.1.0"
# Match AuthGuard's current E2E template.  The source images remain
# configurable, while Flowgent retags them to its own localhost/e2e-flowgent-
# names before either backend consumes them, so concurrent AuthGuard E2E runs
# cannot collide with this use case.
AUTHGUARD_SOURCE_IMAGE = os.getenv(
    "FLOWGENT_E2E_AUTHGUARD_IMAGE",
    "ghcr.io/wl4g/authguard:latest",
)
AUTHGUARD_WEB_SOURCE_IMAGE = os.getenv(
    "FLOWGENT_E2E_AUTHGUARD_WEB_IMAGE",
    "ghcr.io/wl4g/authguard-web:latest",
)


class KubernetesImageRuntime:
    """Class-owned operations for kubernetes."""

    @staticmethod
    def _image_command(
        command: tuple[str, ...], *, allowed_codes: set[int] | None = None
    ) -> CommandResult:
        result = CommandRunner.run(command, timeout_seconds=300, stream=False)
        accepted = allowed_codes or {0}
        if result.return_code not in accepted:
            raise RuntimeError(
                f"image runtime command failed ({result.return_code}): {' '.join(command)}\n"
                f"{result.output}"
            )
        return result

    @staticmethod
    def _ensure_local_image(image: str) -> None:
        if KubernetesImageRuntime._image_command(("docker", "image", "inspect", image), allowed_codes={0, 1, 125}).return_code != 0:
            KubernetesImageRuntime._image_command(("docker", "pull", image))

    @staticmethod
    def _import_image(image: str, label: str) -> bool:
        try:
            KubernetesImageRuntime._ensure_local_image(image)
            loader = KubernetesLocalImageLoader.detect()
            loader.import_image(image)
            print(f"  Imported {label} image through {loader.description}: {image}")
            return True
        except RuntimeError as error:
            print(f"  ERROR: importing {label} image to Kubernetes failed: {error}")
            return False

    @staticmethod
    def _import_core_image() -> bool:
        return KubernetesImageRuntime._import_image(CORE_IMAGE, "Flowgent")

    @staticmethod
    def _import_web_image() -> bool:
        return KubernetesImageRuntime._import_image(WEB_IMAGE, "Flowgent Web")

    @staticmethod
    def _tag_authguard_image(source: str, target: str, label: str) -> bool:
        try:
            KubernetesImageRuntime._ensure_local_image(source)
            KubernetesImageRuntime._image_command(("docker", "tag", source, target))
        except RuntimeError as error:
            print(f"  ERROR: preparing {label} image failed: {error}")
            return False
        return KubernetesImageRuntime._import_image(target, label)

    @staticmethod
    def _import_authguard_image() -> bool:
        return KubernetesImageRuntime._tag_authguard_image(AUTHGUARD_SOURCE_IMAGE, AUTHGUARD_IMAGE, "AuthGuard")

    @staticmethod
    def _import_authguard_web_image() -> bool:
        return KubernetesImageRuntime._tag_authguard_image(AUTHGUARD_WEB_SOURCE_IMAGE, AUTHGUARD_WEB_IMAGE, "AuthGuard Web")

    @staticmethod
    def _run_manual_access(context: RunContext) -> None:
        """Keep the K8s endpoints for the retained Flowgent deployment available."""
        core = PortForwards(context).start()
        specs = (
            (f"{context.release}-authguard-authn", config.LOCAL_AUTHN_PORT, 8082),
            (f"{context.release}-authguard", config.LOCAL_AUTHZ_MGMT_PORT, 9091),
        )
        processes = [
            subprocess.Popen(
                [
                    "kubectl",
                    "port-forward",
                    "-n",
                    context.namespace,
                    f"service/{service}",
                    f"{local}:{remote}",
                ],
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )
            for service, local, remote in specs
        ]
        try:
            time.sleep(2)
            if any(process.poll() is not None for process in processes):
                raise RuntimeError("one or more manual-access tunnels failed to bind")
            print("Flowgent/AuthGuard manual access is ready (Ctrl-C only closes tunnels):")
            print(f"  UI and Gateway:  http://127.0.0.1:{config.LOCAL_GATEWAY_PORT}")
            print(f"  Internal API:    http://127.0.0.1:{config.LOCAL_API_PORT}")
            print(f"  AuthN:           http://127.0.0.1:{config.LOCAL_AUTHN_PORT}")
            print(f"  AuthZ management:http://127.0.0.1:{config.LOCAL_AUTHZ_MGMT_PORT}")
            signal.pause()
        except KeyboardInterrupt:
            pass
        finally:
            for process in processes:
                process.terminate()
            core.stop()


class KubernetesLocalImageLoader:
    """AuthGuard-aligned loader for locally built images on local K8s runtimes."""

    _SUPPORTED = {"auto", "k3s", "kind", "minikube", "k3d", "containerd"}

    def __init__(
        self,
        kind: str,
        cluster: str = "",
        ctr_command: tuple[str, ...] = (),
    ) -> None:
        self.kind = kind
        self.cluster = cluster
        self.ctr_command = ctr_command

    @classmethod
    def detect(cls) -> "KubernetesLocalImageLoader":
        requested = os.getenv(KUBERNETES_IMAGE_LOADER_ENV, "auto").lower()
        if requested not in cls._SUPPORTED:
            supported = ", ".join(sorted(cls._SUPPORTED))
            raise RuntimeError(f"{KUBERNETES_IMAGE_LOADER_ENV} must be one of: {supported}")
        context = KubernetesImageRuntime._image_command(("kubectl", "config", "current-context")).output.strip()
        node_version = KubernetesImageRuntime._image_command(
            (
                "kubectl",
                "get",
                "nodes",
                "-o",
                "jsonpath={.items[0].status.nodeInfo.kubeletVersion}",
            )
        ).output.strip().lower()
        cluster = os.getenv(KUBERNETES_CLUSTER_ENV, "")
        active_cluster_is_local = cls._active_cluster_is_local()

        if requested in {"auto", "kind"} and (requested == "kind" or context.startswith("kind-")):
            if shutil.which("kind"):
                name = cluster or context.removeprefix("kind-")
                if name:
                    return cls("kind", name)
        if requested in {"auto", "minikube"} and (requested == "minikube" or context == "minikube"):
            if shutil.which("minikube"):
                return cls("minikube", cluster or "minikube")
        if requested in {"auto", "k3d"} and (requested == "k3d" or context.startswith("k3d-")):
            if shutil.which("k3d"):
                name = cluster or context.removeprefix("k3d-")
                if name:
                    return cls("k3d", name)
        if requested in {"auto", "k3s"} and (requested == "k3s" or "k3s" in node_version) and active_cluster_is_local:
            ctr_command = cls._k3s_ctr_command()
            if ctr_command:
                return cls("k3s", ctr_command=ctr_command)
        if requested in {"auto", "containerd"} and active_cluster_is_local:
            ctr_command = cls._host_ctr_command()
            if ctr_command:
                return cls("containerd", ctr_command=ctr_command)
        raise RuntimeError(
            "no local Kubernetes image loader was detected for context "
            f"{context!r}; E2E local images require k3s, kind, minikube, k3d, "
            f"or host containerd. Select one with {KUBERNETES_IMAGE_LOADER_ENV}."
        )

    @staticmethod
    def _active_cluster_is_local() -> bool:
        local_names = {socket.gethostname().lower(), socket.getfqdn().lower()}
        local_names.update(name.partition(".")[0] for name in tuple(local_names))
        local_ips = set(KubernetesImageRuntime._image_command(("hostname", "-I"), allowed_codes={0, 1}).output.split())
        nodes = json.loads(KubernetesImageRuntime._image_command(("kubectl", "get", "nodes", "-o", "json")).output)
        for node in nodes.get("items", []):
            name = node.get("metadata", {}).get("name", "").lower()
            if name in local_names or name.partition(".")[0] in local_names:
                return True
            addresses = node.get("status", {}).get("addresses", [])
            if any(address.get("address") in local_ips for address in addresses):
                return True
        return False

    @staticmethod
    def _k3s_ctr_command() -> tuple[str, ...]:
        if not shutil.which("k3s"):
            return ()
        if os.geteuid() == 0:
            return ("k3s", "ctr")
        if shutil.which("sudo"):
            return ("sudo", "-n", "k3s", "ctr")
        return ()

    @staticmethod
    def _host_ctr_command() -> tuple[str, ...]:
        if not shutil.which("ctr"):
            return ()
        candidates = [("ctr",)] if os.geteuid() == 0 else [("sudo", "-n", "ctr")]
        for command in candidates:
            if KubernetesImageRuntime._image_command(
                (*command, "-n", "k8s.io", "images", "list", "-q"),
                allowed_codes={0, 1},
            ).return_code == 0:
                return command
        return ()

    @property
    def description(self) -> str:
        return self.kind if not self.cluster else f"{self.kind}:{self.cluster}"

    def import_image(self, image: str) -> None:
        if self.kind == "kind":
            KubernetesImageRuntime._image_command(("kind", "load", "docker-image", image, "--name", self.cluster))
            return
        if self.kind == "minikube":
            KubernetesImageRuntime._image_command(("minikube", "image", "load", image, "-p", self.cluster))
            return
        if self.kind == "k3d":
            KubernetesImageRuntime._image_command(("k3d", "image", "import", image, "--cluster", self.cluster))
            return
        if self.kind not in {"k3s", "containerd"} or not self.ctr_command:
            raise RuntimeError(f"unsupported Kubernetes image loader: {self.kind}")
        exporter = ("docker", "save", image)
        importer = (*self.ctr_command, "-n", "k8s.io", "images", "import", "-")
        for _ in range(2):
            KubernetesImageRuntime._image_command(
                (
                    "bash",
                    "-o",
                    "pipefail",
                    "-c",
                    f"{shlex.join(exporter)} | {shlex.join(importer)}",
                )
            )
            references = KubernetesImageRuntime._image_command(
                (*self.ctr_command, "-n", "k8s.io", "images", "list", "-q")
            ).output.splitlines()
            if image in references:
                return
        raise RuntimeError(f"containerd did not retain imported image reference: {image}")
















class KubernetesImageManager:
    """Class-owned image preparation for every local Kubernetes runtime."""

    @staticmethod
    def import_core() -> bool:
        return KubernetesImageRuntime._import_core_image()

    @staticmethod
    def import_web() -> bool:
        return KubernetesImageRuntime._import_web_image()

    @staticmethod
    def import_authguard() -> bool:
        return KubernetesImageRuntime._import_authguard_image()

    @staticmethod
    def import_authguard_web() -> bool:
        return KubernetesImageRuntime._import_authguard_web_image()


class PortForwards:
    """Own only the localhost tunnels required by black-box verifiers."""

    def __init__(self, context: RunContext) -> None:
        from deploy.authguard import GATEWAY_LISTENER_PORT, MOCK_GITHUB_NAME

        self.context = context
        self.processes: dict[str, subprocess.Popen] = {}
        release = context.release
        self.specs = (
            (f"{release}-apiserver", (f"{config.LOCAL_API_PORT}:9990",)),
            (f"{release}-a2a", (f"{config.LOCAL_A2A_PORT}:9992",)),
            (
                f"{release}-emqx",
                (
                    f"{config.LOCAL_MQTT_PORT}:1883",
                    f"{config.LOCAL_EMQX_DASHBOARD_PORT}:18083",
                ),
            ),
            (f"{release}-jaeger", (f"{config.LOCAL_JAEGER_PORT}:16686",)),
            (
                MOCK_GITHUB_NAME,
                (f"{config.LOCAL_MOCK_GITHUB_PORT}:8080",),
            ),
        )
        gateway = KubernetesImageRuntime._image_command(
            (
                "kubectl",
                "get",
                "service",
                "-n",
                context.namespace,
                "-l",
                f"gateway.envoyproxy.io/owning-gateway-name={config.RESOURCE_PREFIX}-gateway",
                "-o",
                "jsonpath={.items[0].metadata.name}",
            )
        ).output.strip()
        if not gateway:
            raise RuntimeError("Envoy Gateway data-plane Service was not created")
        self.specs += (
            (gateway, (f"{config.LOCAL_GATEWAY_PORT}:{GATEWAY_LISTENER_PORT}",)),
        )

    @staticmethod
    def _local_port(spec: str) -> int:
        return int(spec.split(":", 1)[0])

    @staticmethod
    def _reachable(port: int) -> bool:
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as connection:
            connection.settimeout(0.2)
            return connection.connect_ex(("127.0.0.1", port)) == 0

    def _ports_reachable(self, specs: tuple[str, ...]) -> bool:
        return all(self._reachable(self._local_port(spec)) for spec in specs)

    def _cleanup_stale(self) -> None:
        pattern = (
            f"kubectl port-forward -n {self.context.namespace} "
            f"service/{self.context.release}-"
        )
        subprocess.run(
            ["pkill", "-f", pattern],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            check=False,
        )
        time.sleep(0.5)

    def _service_probe_ok(self) -> bool:
        if not self._reachable(config.LOCAL_MQTT_PORT):
            return False
        probes = (
            f"http://127.0.0.1:{config.LOCAL_API_PORT}/_/healthz",
            f"http://127.0.0.1:{config.LOCAL_A2A_PORT}/_/healthz",
            f"http://127.0.0.1:{config.LOCAL_JAEGER_PORT}/api/services",
        )
        for url in probes:
            try:
                with urllib.request.urlopen(url, timeout=1) as response:
                    if response.status != 200:
                        return False
            except Exception:
                return False
        return True

    def _wait_healthy(self, timeout_seconds: int = 30) -> None:
        deadline = time.monotonic() + timeout_seconds
        stable_checks = 0
        while time.monotonic() < deadline:
            stable_checks = stable_checks + 1 if self._service_probe_ok() else 0
            if stable_checks >= 3:
                return
            time.sleep(0.5)
        raise RuntimeError("local verification tunnels did not stay healthy")

    def _start_one(self, service: str, specs: tuple[str, ...]) -> None:
        if self._ports_reachable(specs):
            print(f"  Reusing local ports for {service}: {', '.join(specs)}")
            return
        process = subprocess.Popen(
            [
                "kubectl",
                "port-forward",
                "-n",
                self.context.namespace,
                f"service/{service}",
                *specs,
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.PIPE,
            text=True,
        )
        self.processes[service] = process
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            if self._ports_reachable(specs) and process.poll() is None:
                return
            if process.poll() is not None:
                error = process.stderr.read().strip() if process.stderr else ""
                raise RuntimeError(f"port-forward {service} failed: {error}")
            time.sleep(0.2)
        raise RuntimeError(f"port-forward {service} did not become ready")

    def start(self) -> "PortForwards":
        self._cleanup_stale()
        self.ensure()
        print("  Local verification tunnels ready: apiserver, A2A, EMQX, Jaeger")
        return self

    def ensure(self) -> None:
        for service, specs in self.specs:
            process = self.processes.get(service)
            if self._ports_reachable(specs) and (process is None or process.poll() is None):
                continue
            self._start_one(service, specs)
        self._wait_healthy()

    def healthy(self) -> bool:
        return self._service_probe_ok()

    def stop(self) -> None:
        for process in reversed(tuple(self.processes.values())):
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()




class KubernetesManualAccess:
    """Open the retained release's explicit, isolated manual-access tunnels."""

    @staticmethod
    def start(context: RunContext) -> None:
        KubernetesImageRuntime._run_manual_access(context)


class KubernetesDeployer(BaseDeployer):
    """K8s backend for one isolated Flowgent release on any supported runtime."""

    def __init__(self, context: RunContext) -> None:
        from deploy.flowgent import FlowgentDeployer
        from deploy.sonarqube import SonarQubeDeployer

        super().__init__(context)
        self.flowgent = FlowgentDeployer(context)
        self.sonarqube = SonarQubeDeployer(context)
    backend = "k8s"

    def _run(
        self,
        command: tuple[str, ...],
        *,
        timeout_seconds: int | None = None,
        environment: dict[str, str] | None = None,
    ) -> CommandResult:
        result = CommandRunner.run(
            command,
            cwd=config.PROJECT_ROOT,
            environment=environment,
            timeout_seconds=timeout_seconds or self.context.timeout_seconds,
        )
        self.commands.append(result)
        if not result.passed:
            raise RuntimeError(f"command failed ({result.return_code}): {' '.join(command)}")
        return result

    def deploy_sonarqube(self) -> None:
        self.sonarqube.deploy()
        self.sonarqube.verify()

    def reset_database(self) -> None:
        container = os.getenv("FLOWGENT_E2E_PG_CONTAINER", "sigbot_e2e_164364_postgres")
        schema = config.PG_SCHEMA
        if not re.fullmatch(r"[a-z_][a-z0-9_]*", schema):
            raise ValueError(f"invalid isolated PostgreSQL schema: {schema!r}")
        user = os.getenv("FLOWGENT_PG_USER", "test")
        password = os.getenv("FLOWGENT_PG_PASSWORD", "test")
        database = os.getenv("FLOWGENT_PG_DATABASE", "flowgent")
        sql = (
            f'DROP SCHEMA IF EXISTS "{schema}" CASCADE; '
            f'CREATE SCHEMA "{schema}" AUTHORIZATION "{user}";'
        )
        self._run(
            (
                "docker",
                "exec",
                container,
                "sh",
                "-c",
                f"PGPASSWORD={password} psql -U {user} -d {database} -c '{sql}'",
            ),
            timeout_seconds=60,
        )

    def build_images(self) -> None:
        build_environment = {"GOFLAGS": "-p=1", "GOMAXPROCS": "1", "GOGC": "50"}
        self._run(
            ("make", "-C", str(config.PROJECT_ROOT), "build:core"),
            timeout_seconds=CORE_BUILD_TIMEOUT_SECONDS,
            environment=build_environment,
        )
        if not config.CONSOLE_BIN.is_file():
            raise RuntimeError(f"console binary missing after build: {config.CONSOLE_BIN}")
        self._ensure_disk_headroom("before image build")
        image_environment = {
            **build_environment,
            "HTTPS_PROXY": "http://127.0.0.1:8800",
            "IN_CN_GFW": "true",
        }
        for target in ("build:image:core", "build:image:web"):
            self._run(
                ("make", "-C", str(config.PROJECT_ROOT), target),
                timeout_seconds=1200,
                environment=image_environment,
            )
        self._ensure_disk_headroom("after image build", prune_builder_cache=False)

    def deploy(self) -> None:
        self.flowgent.deploy()

    def verify(self) -> None:
        self.flowgent.verify()

    def verify_authguard(self, *, resource_scope: bool = True) -> None:
        from deploy.authguard import AuthGuardDeployer
        from deploy.flowgent import FLOWGENT_PG_CONTAINER, FlowgentRuntime

        deployer = AuthGuardDeployer(
            self.context,
            pg_container=FLOWGENT_PG_CONTAINER,
            pg_host=FlowgentRuntime._docker_container_ip(FLOWGENT_PG_CONTAINER),
            pg_port=os.getenv("FLOWGENT_PG_PORT", "5432"),
            pg_user=os.getenv("FLOWGENT_PG_USER", "test"),
            pg_password=os.getenv("FLOWGENT_PG_PASSWORD", "test"),
        )
        deployer.verify(resource_scope=resource_scope)
        self.details.extend(deployer.details)

    def configure_client_environment(self) -> None:
        self.flowgent.configure()
        os.environ["FLOWGENT__STORAGE__TYPE"] = "POSTGRE"
        os.environ["FLOWGENT__STORAGE__POSTGRES__DSN"] = config.E2EConfiguration.postgres_dsn()
        os.environ["FLOWGENT__STORAGE__POSTGRES__SCHEMA"] = config.PG_SCHEMA

    def runtime_exec(self, command: tuple[str, ...]) -> CommandResult:
        pod = self._run(
            (
                "kubectl",
                "get",
                "pods",
                "-n",
                self.context.namespace,
                "-l",
                f"app.kubernetes.io/instance={self.context.release},app.kubernetes.io/component=apiserver",
                "-o",
                "jsonpath={.items[0].metadata.name}",
            ),
            timeout_seconds=30,
        ).output.strip()
        return self._run(
            (
                "kubectl",
                "exec",
                "-n",
                self.context.namespace,
                pod,
                "--",
                *command,
            ),
        )

    def import_config(self) -> None:
        for path, label in (
            (config.CONSOLE_BIN, "console binary"),
            (config.CONSOLE_CFG, "Flowgent config"),
            (config.CONFIG_DIR, "E2E config directory"),
        ):
            if not path.exists():
                raise RuntimeError(f"{label} is missing: {path}")
        self._run(
            (
                str(config.CONSOLE_BIN),
                "--config",
                str(config.CONSOLE_CFG),
                "console",
                "import",
                str(config.CONFIG_DIR),
            ),
            timeout_seconds=60,
        )

    def cleanup(self) -> None:
        self.flowgent.cleanup()

    def tunnels(self) -> PortForwards:
        return PortForwards(self.context)

    @staticmethod
    def _ensure_disk_headroom(
        label: str,
        *,
        min_free_gib: int = 12,
        max_used_percent: int = 88,
        prune_builder_cache: bool = True,
    ) -> None:
        usage = shutil.disk_usage("/")
        free_gib = usage.free / (1024**3)
        used_percent = int((usage.used / usage.total) * 100)
        if free_gib >= min_free_gib and used_percent <= max_used_percent:
            return
        print(
            f"  WARN: low disk headroom {label}: free={free_gib:.1f}GiB "
            f"used={used_percent}%"
        )
        if prune_builder_cache:
            result = CommandRunner.run(
                ("docker", "builder", "prune", "-af"),
                timeout_seconds=120,
            )
            if not result.passed:
                CommandRunner.run(("podman", "builder", "prune", "-af"), timeout_seconds=120)
        for path in glob.glob("/tmp/go-build*"):
            if os.path.isdir(path):
                shutil.rmtree(path, ignore_errors=True)
