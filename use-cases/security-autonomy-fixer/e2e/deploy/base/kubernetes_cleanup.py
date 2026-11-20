"""Bounded, ownership-scoped cleanup for the retained Kubernetes E2E topology."""

from __future__ import annotations

from collections.abc import Callable, Iterable
import json
import os
import time

from common import config
from common.process import CommandRunner


class KubernetesCleanup:
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
        releases: Iterable[tuple[str, str]],
        namespaces: Iterable[str],
    ) -> None:
        self.releases = tuple(dict.fromkeys(releases))
        self.expected_namespaces = frozenset(namespaces)
        self.prefix = f"{config.RESOURCE_PREFIX}-"
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
        return CommandRunner.run(
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
        return namespace in self.expected_namespaces or namespace.startswith(self.prefix)

    def _discover_namespaces(self) -> set[str]:
        document = self._json(("kubectl", "get", "namespaces", "-o", "json"))
        if document is None:
            return set(self.expected_namespaces)
        discovered = set()
        for item in document.get("items", []):
            metadata = item.get("metadata", {})
            name = metadata.get("name", "")
            owner = metadata.get("labels", {}).get("flowgent.io/e2e-owner")
            if self._is_owned_namespace(name) or owner == config.RESOURCE_PREFIX:
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
        import subprocess

        for namespace in sorted(self._existing_namespaces(namespaces)):
            if not self._is_owned_namespace(namespace):
                raise RuntimeError(f"refusing to finalize unowned namespace: {namespace}")
            print(f"  Force-finalizing owned namespace through /finalize: {namespace}")
            body = {
                "apiVersion": "v1",
                "kind": "Namespace",
                "metadata": {"name": namespace},
                "spec": {"finalizers": []},
            }
            # CommandRunner deliberately has no stdin surface. Namespace
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
                release_namespace = annotations.get("meta.helm.sh/release-namespace", "")
                is_owned = (
                    labels.get("flowgent.io/e2e-owner") == config.RESOURCE_PREFIX
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
                print(f"  Removing finalizers from owned cluster resource: {resource}/{name}")
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
            release_rows = json.loads(release_result.output or "[]") if release_result.passed else []
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
                + repr(sorted(f"{kind}/{name}" for kind, name, _ in remaining_cluster_resources))
            )
        if failures:
            print("  ERROR: E2E cleanup verification failed: " + "; ".join(failures))
            return False
        print("  Cleanup verified: no target releases, namespaces, Pods, PVs, or cluster resources remain.")
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
        remaining = self._wait_namespaces_absent(
            namespaces, self.normal_wait_seconds
        )
        if remaining:
            print(
                "  WARN: graceful namespace deletion timed out; removing finalizers "
                f"only inside owned namespaces: {sorted(remaining)}"
            )
            self._force_remove_namespaced_resources(remaining)
            remaining = self._wait_namespaces_absent(
                remaining, self.force_wait_seconds
            )
        if remaining:
            self._force_finalize_namespaces(remaining)
            self._wait_namespaces_absent(remaining, self.force_wait_seconds)

        volumes |= self._capture_persistent_volumes(namespaces)
        self._remove_persistent_volumes(volumes)
        self._remove_cluster_resources(namespaces)
        return self._verify_absent(namespaces, volumes)
