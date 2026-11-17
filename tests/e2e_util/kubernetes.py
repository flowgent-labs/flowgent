"""Generic local-cluster discovery, resource operations and ownership cleanup."""

from __future__ import annotations

import json
import os
import shlex
import shutil
import socket
import subprocess
import sys
import time
from collections.abc import Callable, Iterable
from contextlib import contextmanager
from pathlib import Path

from .images import E2EImagesDefinitions
from .process import CommandResult, E2EProcessHelper

KUBERNETES_IMAGE_LOADER_ENV = "FLOWGENT_E2E_KUBERNETES_IMAGE_LOADER"
KUBERNETES_CLUSTER_ENV = "FLOWGENT_E2E_KUBERNETES_CLUSTER"


class E2EKubernetesDeployer:
    """Generic commands shared by service deployers and scenario verifiers."""

    @staticmethod
    @contextmanager
    def port_forward(namespace: str, resource: str, remote_port: int):
        """Expose a cluster resource without assuming host routes to ClusterIP."""
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            local_port = listener.getsockname()[1]
        process = subprocess.Popen(
            (
                "kubectl",
                "port-forward",
                "-n",
                namespace,
                resource,
                f"{local_port}:{remote_port}",
                "--address=127.0.0.1",
            ),
            stdout=subprocess.DEVNULL,
            stderr=subprocess.PIPE,
            text=True,
            **E2EProcessHelper.process_group_options(),
        )
        try:
            deadline = time.monotonic() + 20
            while time.monotonic() < deadline:
                if process.poll() is not None:
                    raise RuntimeError(process.stderr.read()[-600:])
                try:
                    with socket.create_connection(("127.0.0.1", local_port), timeout=1):
                        break
                except OSError:
                    time.sleep(0.2)
            else:
                raise RuntimeError(f"port-forward did not become ready: {resource}")
            yield local_port
        finally:
            E2EProcessHelper._terminate_tree(process)

    @staticmethod
    def command(
        command: tuple[str, ...], *, allowed_codes: set[int] | None = None
    ) -> CommandResult:
        result = E2EProcessHelper.run(command, timeout_seconds=300, stream=False)
        if result.return_code not in (allowed_codes or {0}):
            raise RuntimeError(
                f"Kubernetes/image command failed ({result.return_code}): {result.output}"
            )
        return result

    @staticmethod
    def ensure_image(image: str) -> None:
        from .images import E2EImagesDefinitions

        E2EImagesDefinitions.ensure_local(image)

    @staticmethod
    def resource(
        kind: str, namespace: str = "", name: str = "", selector: str = ""
    ) -> dict:
        command = ["kubectl", "get", kind]
        if namespace:
            command.extend(["-n", namespace])
        if name:
            command.append(name)
        if selector:
            command.extend(["-l", selector])
        command.extend(["-o", "json"])
        return json.loads(E2EKubernetesDeployer.command(tuple(command)).output)

    @staticmethod
    def apply(manifest: dict) -> None:
        from .images import E2EImagesDefinitions

        manifest = E2EImagesDefinitions.rewrite(manifest)
        E2EKubernetesDeployer.prepare_images(manifest)
        completed = subprocess.run(
            ["kubectl", "apply", "-f", "-"],
            input=json.dumps(manifest),
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            timeout=90,
        )
        if completed.returncode:
            raise RuntimeError(f"apply resources failed: {completed.stderr[-600:]}")

    @staticmethod
    def prepare_images(manifest) -> None:
        references = E2EImagesDefinitions.references(manifest)
        if not references:
            return
        loader = KubernetesLocalImageLoader.detect()
        for image in sorted(references):
            E2EImagesDefinitions.prepare_runtime(image)
            loader.import_image(image)

    @staticmethod
    def install(
        release: str,
        chart: Path,
        namespace: str,
        *,
        values: dict | None = None,
        timeout: int = 300,
    ):
        """Install the official chart with the same image policy as direct apply."""
        environment = {
            "FLOWGENT_E2E_PRELOAD_IMAGES": "1",
            "PYTHONPATH": os.pathsep.join(
                filter(
                    None,
                    [str(Path(__file__).resolve().parents[1]), os.getenv("PYTHONPATH")],
                )
            ),
        }
        with E2EProcessHelper.private_file(json.dumps(values or {})) as path:
            command = (
                "helm",
                "upgrade",
                "--install",
                release,
                str(chart),
                "-n",
                namespace,
                "--create-namespace",
                "--wait",
                "--timeout",
                f"{timeout}s",
                "-f",
                str(path),
                "--post-renderer",
                sys.executable,
                "--post-renderer-args",
                "-m",
                "--post-renderer-args",
                "e2e_util.images",
            )
            return E2EProcessHelper.run(
                command, environment=environment, timeout_seconds=timeout + 60
            )

    @staticmethod
    def resources(document: dict):
        """Flatten chart List objects before passing them to kubectl."""
        if document.get("kind") == "List":
            for item in document.get("items", []):
                yield from E2EKubernetesDeployer.resources(item)
        else:
            yield document

    @staticmethod
    def ensure_namespace(namespace: str, owner: str, component: str) -> None:
        E2EKubernetesDeployer.apply(
            {
                "apiVersion": "v1",
                "kind": "Namespace",
                "metadata": {
                    "name": namespace,
                    "labels": {
                        "flowgent.io/e2e-owner": owner,
                        "flowgent.io/e2e-component": component,
                    },
                },
            }
        )

    @staticmethod
    def rollout(namespace: str, name: str, *, restart: bool = False) -> None:
        if restart:
            E2EKubernetesDeployer.command(
                ("kubectl", "rollout", "restart", f"deployment/{name}", "-n", namespace)
            )
        E2EKubernetesDeployer.command(
            (
                "kubectl",
                "rollout",
                "status",
                f"deployment/{name}",
                "-n",
                namespace,
                "--timeout=120s",
            )
        )

    @staticmethod
    def helm_template(chart: Path, values: dict, *, template: str = "") -> dict:
        """Render component-owned Helm resources, keeping runtime values private."""
        import yaml

        from .images import E2EImagesDefinitions

        with E2EProcessHelper.private_file(json.dumps(values)) as handle:
            command = ["helm", "template", "e2e", str(chart), "-f", str(handle)]
            if template:
                command.extend(["--show-only", "templates/" + template])
            result = subprocess.run(
                command, capture_output=True, text=True, timeout=120
            )
        if result.returncode:
            raise RuntimeError(f"Helm render failed: {result.stderr}")
        documents = [
            resource
            for doc in yaml.safe_load_all(result.stdout)
            if doc
            for resource in E2EKubernetesDeployer.resources(doc)
        ]
        return E2EImagesDefinitions.rewrite(
            {"apiVersion": "v1", "kind": "List", "items": documents}
        )


class KubernetesLocalImageLoader:
    """Load locally-built E2E images into the active local Kubernetes cluster."""

    _SUPPORTED = {
        "auto",
        "colima",
        "orbstack",
        "docker",
        "k3s",
        "kind",
        "minikube",
        "k3d",
        "containerd",
    }

    def __init__(
        self,
        kind: str,
        cluster: str = "",
        ctr_command: tuple[str, ...] = (),
        platform: str = "",
    ) -> None:
        self.kind = kind
        self.cluster = cluster
        self.ctr_command = ctr_command
        self.node_platform = platform
        self.platform = E2EImagesDefinitions.platform()
        os.environ["DOCKER_DEFAULT_PLATFORM"] = self.platform

    @classmethod
    def detect(cls) -> "KubernetesLocalImageLoader":
        requested = os.getenv(KUBERNETES_IMAGE_LOADER_ENV, "auto").lower()
        if requested not in cls._SUPPORTED:
            supported = ", ".join(sorted(cls._SUPPORTED))
            raise RuntimeError(
                f"{KUBERNETES_IMAGE_LOADER_ENV} must be one of: {supported}"
            )
        context = E2EKubernetesDeployer.command(
            ("kubectl", "config", "current-context")
        ).output.strip()
        nodes = json.loads(
            E2EKubernetesDeployer.command(
                ("kubectl", "get", "nodes", "-o", "json")
            ).output
        )
        node_info = [node["status"]["nodeInfo"] for node in nodes.get("items", [])]
        architectures = {node["architecture"] for node in node_info}
        if len(architectures) != 1 or not architectures <= {"amd64", "arm64"}:
            raise RuntimeError(
                f"E2E requires homogeneous amd64 or arm64 nodes: {architectures}"
            )
        platform = f"linux/{next(iter(architectures))}"
        os.environ.update(E2EImagesDefinitions.runtime_environment(platform))
        node_version = node_info[0]["kubeletVersion"].lower()
        runtime = node_info[0]["containerRuntimeVersion"]
        cluster = os.getenv(KUBERNETES_CLUSTER_ENV, "")

        if context == "orbstack":
            if requested not in {"auto", "orbstack"}:
                raise RuntimeError(
                    "the active OrbStack context requires the orbstack image loader"
                )
            os.environ["DOCKER_CONTEXT"] = "orbstack"
            E2EKubernetesDeployer.command(("docker", "info", "--format", "{{.ID}}"))
            return cls("docker", "orbstack", platform=platform)

        if context == "colima" or context.startswith("colima-"):
            if requested not in {"auto", "colima"}:
                raise RuntimeError(
                    "the active Colima context requires the colima image loader"
                )
            if not shutil.which("colima"):
                raise RuntimeError("the active Colima context requires the colima CLI")
            profile = (
                context.removeprefix("colima-") if context != "colima" else "default"
            )
            vm = ("colima", "-p", profile, "ssh", "--", "sudo", "-n")
            if runtime.startswith("docker://"):
                os.environ["DOCKER_CONTEXT"] = context
                host_id = E2EKubernetesDeployer.command(
                    ("docker", "info", "--format", "{{.ID}}")
                ).output.strip()
                vm_id = E2EKubernetesDeployer.command(
                    (*vm, "docker", "info", "--format", "{{.ID}}")
                ).output.strip()
                if not host_id or host_id != vm_id:
                    raise RuntimeError(
                        f"Docker context {context!r} does not share the Colima VM image store"
                    )
                return cls("docker", context, platform=platform)
            if runtime.startswith("containerd://"):
                for command in ((*vm, "k3s", "ctr"), (*vm, "ctr")):
                    probe = E2EKubernetesDeployer.command(
                        (*command, "-n", "k8s.io", "images", "list", "-q"),
                        allowed_codes={0, 1, 127},
                    )
                    if probe.return_code == 0:
                        return cls("containerd", context, command, platform)
            raise RuntimeError(f"unsupported or inaccessible Colima runtime: {runtime}")
        if requested in {"colima", "orbstack"}:
            raise RuntimeError(
                f"the active Kubernetes context is not {requested}: {context!r}"
            )

        if requested in {"auto", "kind"} and (
            requested == "kind" or context.startswith("kind-")
        ):
            if shutil.which("kind"):
                name = cluster or context.removeprefix("kind-")
                if name:
                    return cls("kind", name, platform=platform)
        if requested in {"auto", "minikube"} and (
            requested == "minikube" or context == "minikube"
        ):
            if shutil.which("minikube"):
                return cls("minikube", cluster or "minikube", platform=platform)
        if requested in {"auto", "k3d"} and (
            requested == "k3d" or context.startswith("k3d-")
        ):
            if shutil.which("k3d"):
                name = cluster or context.removeprefix("k3d-")
                if name:
                    return cls("k3d", name, platform=platform)
        active_cluster_is_local = cls._active_cluster_is_local(nodes)
        if (
            requested in {"auto", "docker", "k3s"}
            and runtime.startswith("docker://")
            and active_cluster_is_local
        ):
            daemon_name = E2EKubernetesDeployer.command(
                ("docker", "info", "--format", "{{.Name}}")
            ).output.strip()
            node_names = {node["metadata"]["name"].lower() for node in nodes["items"]}
            node_names.update(name.partition(".")[0] for name in tuple(node_names))
            if daemon_name.lower() not in node_names:
                raise RuntimeError(
                    "Docker daemon does not belong to the active local Kubernetes node"
                )
            return cls("docker", platform=platform)
        if (
            requested in {"auto", "k3s"}
            and (requested == "k3s" or "k3s" in node_version)
            and active_cluster_is_local
        ):
            ctr_command = cls._k3s_ctr_command()
            if ctr_command:
                return cls("k3s", ctr_command=ctr_command, platform=platform)
        if requested in {"auto", "containerd"} and active_cluster_is_local:
            ctr_command = cls._host_ctr_command()
            if ctr_command:
                return cls("containerd", ctr_command=ctr_command, platform=platform)

        raise RuntimeError(
            "no local Kubernetes image loader was detected for context "
            f"{context!r}; E2E local images require Colima, OrbStack, local Docker, k3s, kind, minikube, k3d, "
            "or host containerd. Select one with "
            f"{KUBERNETES_IMAGE_LOADER_ENV}."
        )

    @staticmethod
    def _active_cluster_is_local(nodes: dict) -> bool:
        local_names = {socket.gethostname().lower(), socket.getfqdn().lower()}
        local_names.update(name.partition(".")[0] for name in tuple(local_names))
        local_ips = set(
            E2EKubernetesDeployer.command(
                ("hostname", "-I"), allowed_codes={0, 1}
            ).output.split()
        )
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
        if getattr(os, "geteuid", lambda: 1)() == 0:
            return ("k3s", "ctr")
        if shutil.which("sudo"):
            return ("sudo", "-n", "k3s", "ctr")
        return ()

    @staticmethod
    def _host_ctr_command() -> tuple[str, ...]:
        if not shutil.which("ctr"):
            return ()
        candidates = (
            [("ctr",)]
            if getattr(os, "geteuid", lambda: 1)() == 0
            else [("sudo", "-n", "ctr")]
        )
        for command in candidates:
            if (
                E2EKubernetesDeployer.command(
                    (*command, "-n", "k8s.io", "images", "list", "-q"),
                    allowed_codes={0, 1},
                ).return_code
                == 0
            ):
                return command
        return ()

    @property
    def description(self) -> str:
        return self.kind if not self.cluster else f"{self.kind}:{self.cluster}"

    def import_image(self, image: str) -> None:
        platform = self.validate_image(image)
        if self.kind == "docker":
            return  # The kubelet and builder already use the same Docker daemon.
        if self.kind == "kind":
            E2EKubernetesDeployer.command(
                ("kind", "load", "docker-image", image, "--name", self.cluster)
            )
            return
        if self.kind == "minikube":
            E2EKubernetesDeployer.command(
                ("minikube", "image", "load", image, "-p", self.cluster)
            )
            return
        if self.kind == "k3d":
            E2EKubernetesDeployer.command(
                ("k3d", "image", "import", image, "--cluster", self.cluster)
            )
            return
        if self.kind not in {"k3s", "containerd"}:
            raise RuntimeError(f"unsupported Kubernetes image loader: {self.kind}")
        if not self.ctr_command:
            raise RuntimeError(f"{self.kind} image loader has no ctr command")
        exporter = ("docker", "save", image)
        importer = (
            *self.ctr_command,
            "-n",
            "k8s.io",
            "images",
            "import",
            "--platform",
            platform,
            "-",
        )
        for _ in range(2):
            E2EKubernetesDeployer.command(
                (
                    "bash",
                    "-o",
                    "pipefail",
                    "-c",
                    f"{shlex.join(exporter)} | {shlex.join(importer)}",
                )
            )
            references = E2EKubernetesDeployer.command(
                (*self.ctr_command, "-n", "k8s.io", "images", "list", "-q")
            ).output.splitlines()
            if image in references:
                return
        raise RuntimeError(
            f"containerd did not retain imported image reference: {image}"
        )

    def validate_image(self, image: str) -> str:
        metadata = E2EImagesDefinitions.inspect(image)
        required = E2EImagesDefinitions.platform(image)
        if not metadata or metadata["platform"] != required:
            raise RuntimeError(
                f"image {image} must be loaded for {required}; node platform is {self.node_platform}"
            )
        return required


class E2EKubernetesCleaner:
    """Remove one E2E topology, using forced finalization only as a fallback."""

    _CLUSTER_RESOURCE_TYPES = (
        "mutatingwebhookconfigurations.admissionregistration.k8s.io",
        "validatingwebhookconfigurations.admissionregistration.k8s.io",
        "clusterrolebindings.rbac.authorization.k8s.io",
        "clusterroles.rbac.authorization.k8s.io",
        "gatewayclasses.gateway.networking.k8s.io",
    )
    _SKIPPED_NAMESPACED_RESOURCES = {
        "events",
        "events.events.k8s.io",
    }

    def __init__(
        self,
        *,
        resource_prefix: str,
        releases: Iterable[tuple[str, str]],
        namespaces: Iterable[str],
    ) -> None:
        self.releases = tuple(dict.fromkeys(releases))
        self.expected_namespaces = frozenset(namespaces)
        self.owner = resource_prefix
        self.prefix = f"{resource_prefix}-"
        self.normal_wait_seconds = self._positive_seconds(
            "FLOWGENT_E2E_CLEANUP_GRACE_SECONDS", 90
        )
        self.force_wait_seconds = self._positive_seconds(
            "FLOWGENT_E2E_CLEANUP_FORCE_SECONDS", 45
        )

    @staticmethod
    def _positive_seconds(name: str, default: int) -> int:
        value = int(os.getenv(name, str(default)))
        if value < 1:
            raise ValueError(f"{name} must be at least 1 second")
        return value

    @staticmethod
    def _run(
        command: tuple[str, ...] | list[str],
        *,
        timeout: int = 60,
        stream: bool = True,
    ):
        return E2EProcessHelper.run(
            command,
            timeout_seconds=timeout,
            stream=stream,
        )

    @classmethod
    def _json(
        cls,
        command: tuple[str, ...] | list[str],
        *,
        timeout: int = 60,
    ) -> dict | None:
        result = cls._run(command, timeout=timeout, stream=False)
        if not result.passed:
            return None
        try:
            value = json.loads(result.output or "{}")
        except json.JSONDecodeError:
            return None
        return value if isinstance(value, dict) else None

    def _is_owned_namespace(self, namespace: str) -> bool:
        return namespace in self.expected_namespaces or namespace.startswith(
            self.prefix
        )

    def _discover_namespaces(self) -> set[str]:
        document = self._json(("kubectl", "get", "namespaces", "-o", "json"))
        if document is None:
            return set(self.expected_namespaces)
        discovered = set()
        for item in document.get("items", []):
            metadata = item.get("metadata", {})
            name = metadata.get("name", "")
            owner = metadata.get("labels", {}).get("flowgent.io/e2e-owner")
            if self._is_owned_namespace(name) or owner == self.owner:
                discovered.add(name)
        return discovered | set(self.expected_namespaces)

    def _existing_namespaces(self, candidates: Iterable[str]) -> set[str]:
        candidate_set = set(candidates)
        document = self._json(("kubectl", "get", "namespaces", "-o", "json"))
        if document is None:
            return candidate_set
        return {
            item.get("metadata", {}).get("name", "")
            for item in document.get("items", [])
            if item.get("metadata", {}).get("name", "") in candidate_set
        }

    def _wait_namespaces_absent(
        self, candidates: Iterable[str], timeout: int
    ) -> set[str]:
        candidate_set = set(candidates)
        deadline = time.monotonic() + timeout
        remaining = self._existing_namespaces(candidate_set)
        while remaining and time.monotonic() < deadline:
            time.sleep(2)
            remaining = self._existing_namespaces(candidate_set)
        return remaining

    def _uninstall_releases(self) -> None:
        result = self._run(("helm", "list", "-A", "-o", "json"), stream=False)
        try:
            rows = json.loads(result.output or "[]") if result.passed else []
        except json.JSONDecodeError:
            rows = []
        installed = {
            (item.get("namespace", ""), item.get("name", ""))
            for item in rows
            if isinstance(item, dict)
        }
        inspect_failed = not result.passed

        for release, namespace in self.releases:
            if not inspect_failed and (namespace, release) not in installed:
                print(f"  Helm release already absent: {namespace}/{release}")
                continue
            print(f"  Uninstalling Helm release: {namespace}/{release}")
            result = self._run(
                (
                    "helm",
                    "uninstall",
                    release,
                    "-n",
                    namespace,
                    "--wait",
                    "--timeout",
                    f"{self.normal_wait_seconds}s",
                ),
                timeout=self.normal_wait_seconds + 30,
            )
            if result.passed:
                continue
            print(
                "  WARN: normal Helm uninstall did not complete; retrying without "
                f"hooks before bounded finalizer recovery: {namespace}/{release}"
            )
            self._run(
                (
                    "helm",
                    "uninstall",
                    release,
                    "-n",
                    namespace,
                    "--no-hooks",
                ),
                timeout=60,
            )

    def _capture_persistent_volumes(self, namespaces: set[str]) -> set[str]:
        document = self._json(("kubectl", "get", "persistentvolumes", "-o", "json"))
        if document is None:
            return set()
        return {
            item.get("metadata", {}).get("name", "")
            for item in document.get("items", [])
            if item.get("spec", {}).get("claimRef", {}).get("namespace") in namespaces
            and item.get("metadata", {}).get("name")
        }

    def _namespaced_api_resources(self) -> tuple[str, ...]:
        result = self._run(
            (
                "kubectl",
                "api-resources",
                "--verbs=list,delete",
                "--namespaced=true",
                "-o",
                "name",
            ),
            timeout=60,
            stream=False,
        )
        if not result.passed:
            return ()
        return tuple(
            resource
            for resource in dict.fromkeys(result.output.splitlines())
            if resource and resource not in self._SKIPPED_NAMESPACED_RESOURCES
        )

    def _remaining_namespaced_resources(
        self, namespaces: set[str]
    ) -> list[tuple[str, str, str, tuple[str, ...]]]:
        remaining: list[tuple[str, str, str, tuple[str, ...]]] = []
        for resource in self._namespaced_api_resources():
            document = self._json(
                ("kubectl", "get", resource, "-A", "-o", "json"), timeout=30
            )
            if document is None:
                continue
            for item in document.get("items", []):
                metadata = item.get("metadata", {})
                namespace = metadata.get("namespace", "")
                name = metadata.get("name", "")
                if namespace in namespaces and name:
                    remaining.append(
                        (
                            resource,
                            namespace,
                            name,
                            tuple(metadata.get("finalizers") or ()),
                        )
                    )
        return remaining

    def _force_remove_namespaced_resources(self, namespaces: set[str]) -> None:
        resources = self._remaining_namespaced_resources(namespaces)
        pod_count = sum(1 for resource, *_ in resources if resource == "pods")
        print(
            f"  Forced cleanup fallback found {len(resources)} residual objects "
            f"including {pod_count} Pod(s)."
        )
        for resource, namespace, name, finalizers in resources:
            if finalizers:
                self._run(
                    (
                        "kubectl",
                        "patch",
                        resource,
                        name,
                        "-n",
                        namespace,
                        "--type=merge",
                        "-p",
                        '{"metadata":{"finalizers":[]}}',
                    ),
                    timeout=20,
                    stream=False,
                )
            self._run(
                (
                    "kubectl",
                    "delete",
                    resource,
                    name,
                    "-n",
                    namespace,
                    "--ignore-not-found=true",
                    "--wait=false",
                    "--force",
                    "--grace-period=0",
                ),
                timeout=20,
                stream=False,
            )

    def _force_finalize_namespaces(self, namespaces: set[str]) -> None:

        for namespace in sorted(self._existing_namespaces(namespaces)):
            if not self._is_owned_namespace(namespace):
                raise RuntimeError(
                    f"refusing to finalize unowned namespace: {namespace}"
                )
            print(f"  Force-finalizing owned namespace through /finalize: {namespace}")
            body = {
                "apiVersion": "v1",
                "kind": "Namespace",
                "metadata": {"name": namespace},
                "spec": {"finalizers": []},
            }
            # E2EProcessHelper deliberately has no stdin surface. Namespace
            # finalization is the one Kubernetes API call that requires a body.
            completed = subprocess.run(
                [
                    "kubectl",
                    "replace",
                    "--raw",
                    f"/api/v1/namespaces/{namespace}/finalize",
                    "-f",
                    "-",
                ],
                input=json.dumps(body),
                capture_output=True,
                text=True,
                timeout=30,
            )
            if completed.returncode != 0:
                print(
                    f"  WARN: namespace /finalize failed for {namespace}: "
                    f"{completed.stderr.strip()[-300:]}"
                )

    def _owned_cluster_resources(
        self, namespaces: set[str]
    ) -> list[tuple[str, str, tuple[str, ...]]]:
        owned: list[tuple[str, str, tuple[str, ...]]] = []
        for resource in self._CLUSTER_RESOURCE_TYPES:
            document = self._json(("kubectl", "get", resource, "-o", "json"))
            if document is None:
                continue
            for item in document.get("items", []):
                metadata = item.get("metadata", {})
                name = metadata.get("name", "")
                labels = metadata.get("labels", {})
                annotations = metadata.get("annotations", {})
                release_namespace = annotations.get(
                    "meta.helm.sh/release-namespace", ""
                )
                is_owned = (
                    labels.get("flowgent.io/e2e-owner") == self.owner
                    or release_namespace in namespaces
                    or name.startswith(self.prefix)
                    or any(namespace in name for namespace in namespaces)
                )
                if is_owned and name:
                    owned.append(
                        (resource, name, tuple(metadata.get("finalizers") or ()))
                    )
        return owned

    def _remove_cluster_resources(self, namespaces: set[str]) -> None:
        resources = self._owned_cluster_resources(namespaces)
        for resource, name, _ in resources:
            self._run(
                (
                    "kubectl",
                    "delete",
                    resource,
                    name,
                    "--ignore-not-found=true",
                    "--wait=false",
                ),
                timeout=30,
                stream=False,
            )

        deadline = time.monotonic() + self.force_wait_seconds
        remaining = self._owned_cluster_resources(namespaces)
        while remaining and time.monotonic() < deadline:
            time.sleep(2)
            remaining = self._owned_cluster_resources(namespaces)
        for resource, name, finalizers in remaining:
            if finalizers:
                print(
                    f"  Removing finalizers from owned cluster resource: {resource}/{name}"
                )
                self._run(
                    (
                        "kubectl",
                        "patch",
                        resource,
                        name,
                        "--type=merge",
                        "-p",
                        '{"metadata":{"finalizers":[]}}',
                    ),
                    timeout=20,
                    stream=False,
                )
            self._run(
                (
                    "kubectl",
                    "delete",
                    resource,
                    name,
                    "--ignore-not-found=true",
                    "--wait=false",
                    "--force",
                    "--grace-period=0",
                ),
                timeout=20,
                stream=False,
            )
        deadline = time.monotonic() + self.force_wait_seconds
        while self._owned_cluster_resources(namespaces) and time.monotonic() < deadline:
            time.sleep(2)

    def _remove_persistent_volumes(self, names: set[str]) -> None:
        for name in sorted(names):
            self._run(
                (
                    "kubectl",
                    "delete",
                    "persistentvolume",
                    name,
                    "--ignore-not-found=true",
                    "--wait=false",
                ),
                timeout=30,
                stream=False,
            )
        deadline = time.monotonic() + self.force_wait_seconds
        remaining = set(names)
        while remaining and time.monotonic() < deadline:
            document = self._json(("kubectl", "get", "persistentvolumes", "-o", "json"))
            existing = {
                item.get("metadata", {}).get("name", "")
                for item in (document or {}).get("items", [])
            }
            remaining &= existing
            if remaining:
                time.sleep(2)
        for name in sorted(remaining):
            print(f"  Removing finalizers from owned PersistentVolume: {name}")
            self._run(
                (
                    "kubectl",
                    "patch",
                    "persistentvolume",
                    name,
                    "--type=merge",
                    "-p",
                    '{"metadata":{"finalizers":[]}}',
                ),
                timeout=20,
                stream=False,
            )
            self._run(
                (
                    "kubectl",
                    "delete",
                    "persistentvolume",
                    name,
                    "--ignore-not-found=true",
                    "--wait=false",
                    "--force",
                    "--grace-period=0",
                ),
                timeout=20,
                stream=False,
            )
        deadline = time.monotonic() + self.force_wait_seconds
        while remaining and time.monotonic() < deadline:
            document = self._json(("kubectl", "get", "persistentvolumes", "-o", "json"))
            existing = {
                item.get("metadata", {}).get("name", "")
                for item in (document or {}).get("items", [])
            }
            remaining &= existing
            if remaining:
                time.sleep(2)

    def _verify_absent(self, namespaces: set[str], volumes: set[str]) -> bool:
        remaining_namespaces = self._existing_namespaces(namespaces)
        remaining_cluster_resources = self._owned_cluster_resources(namespaces)

        pod_document = self._json(("kubectl", "get", "pods", "-A", "-o", "json"))
        remaining_pods = [
            f"{item.get('metadata', {}).get('namespace')}/"
            f"{item.get('metadata', {}).get('name')}"
            for item in (pod_document or {}).get("items", [])
            if item.get("metadata", {}).get("namespace") in namespaces
            or item.get("metadata", {}).get("namespace", "").startswith(self.prefix)
        ]

        release_result = self._run(("helm", "list", "-A", "-o", "json"), stream=False)
        try:
            release_rows = (
                json.loads(release_result.output or "[]")
                if release_result.passed
                else []
            )
        except json.JSONDecodeError:
            release_rows = []
        remaining_releases = [
            f"{row.get('namespace')}/{row.get('name')}"
            for row in release_rows
            if isinstance(row, dict) and row.get("namespace") in namespaces
        ]

        pv_document = self._json(("kubectl", "get", "persistentvolumes", "-o", "json"))
        remaining_volumes = {
            item.get("metadata", {}).get("name", "")
            for item in (pv_document or {}).get("items", [])
            if item.get("metadata", {}).get("name", "") in volumes
            or item.get("spec", {}).get("claimRef", {}).get("namespace") in namespaces
        }

        failures = []
        if remaining_namespaces:
            failures.append(f"namespaces={sorted(remaining_namespaces)}")
        if remaining_releases:
            failures.append(f"releases={sorted(remaining_releases)}")
        if remaining_pods:
            failures.append(f"pods={sorted(remaining_pods)}")
        if remaining_volumes:
            failures.append(f"persistentVolumes={sorted(remaining_volumes)}")
        if remaining_cluster_resources:
            failures.append(
                "clusterResources="
                + repr(
                    sorted(
                        f"{kind}/{name}"
                        for kind, name, _ in remaining_cluster_resources
                    )
                )
            )
        if failures:
            print("  ERROR: E2E cleanup verification failed: " + "; ".join(failures))
            return False
        print(
            "  Cleanup verified: no target releases, namespaces, Pods, PVs, or cluster resources remain."
        )
        return True

    def run(self, before_namespace_delete: Callable[[], None] | None = None) -> bool:
        """Execute graceful cleanup, then force only resources still blocking deletion."""
        namespaces = self._discover_namespaces()
        volumes = self._capture_persistent_volumes(namespaces)
        self._uninstall_releases()
        if before_namespace_delete is not None:
            before_namespace_delete()

        namespaces = self._discover_namespaces()
        if namespaces:
            self._run(
                (
                    "kubectl",
                    "delete",
                    "namespace",
                    *sorted(namespaces),
                    "--ignore-not-found=true",
                    "--wait=false",
                ),
                timeout=60,
            )
        remaining = self._wait_namespaces_absent(namespaces, self.normal_wait_seconds)
        if remaining:
            print(
                "  WARN: graceful namespace deletion timed out; removing finalizers "
                f"only inside owned namespaces: {sorted(remaining)}"
            )
            self._force_remove_namespaced_resources(remaining)
            remaining = self._wait_namespaces_absent(remaining, self.force_wait_seconds)
        if remaining:
            self._force_finalize_namespaces(remaining)
            self._wait_namespaces_absent(remaining, self.force_wait_seconds)

        volumes |= self._capture_persistent_volumes(namespaces)
        self._remove_persistent_volumes(volumes)
        self._remove_cluster_resources(namespaces)
        return self._verify_absent(namespaces, volumes)
