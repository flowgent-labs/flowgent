#!/usr/bin/env python3
"""Give the isolated Flowgent Envoy control/data planes stable E2E names."""

from __future__ import annotations

import os
import sys

import yaml


UPSTREAM_CONTROL_PLANE_NAME = "envoy-gateway"


class EnvoyGatewayManifestNormalizer:
    """Normalize only the E2E controller and generated data-plane identities."""

    NAMESPACED_KINDS = {
        "ConfigMap",
        "Deployment",
        "Job",
        "Role",
        "RoleBinding",
        "Secret",
        "Service",
        "ServiceAccount",
    }

    @staticmethod
    def default_control_plane_name(resource_prefix: str) -> str:
        # Namespaced workloads use their production name; the e2e-flowgent-*
        # namespace is the environment isolation boundary. Cluster-scoped RBAC
        # names remain release-qualified by the upstream chart.
        return "envoy-gateway"

    def __init__(self) -> None:
        resource_prefix = os.getenv("FLOWGENT_E2E_RESOURCE_PREFIX", "e2e-flowgent")
        release = os.getenv(
            "FLOWGENT_E2E_AUTHGUARD_RELEASE", f"{resource_prefix}-authguard"
        )
        self.resource_prefix = resource_prefix
        self.release_gateway_prefix = f"{release}-envoy-gateway"
        self.control_plane_name = os.getenv(
            "FLOWGENT_E2E_ENVOY_GATEWAY_NAME",
            self.default_control_plane_name(resource_prefix),
        )
        self.data_plane_name = os.getenv(
            "FLOWGENT_E2E_ENVOY_NAME", "envoy"
        )

    def render(self, manifest: str) -> str:
        # The vendored upstream controller chart hard-codes this token across
        # names, selectors, RBAC references, ConfigMaps, and pod volume names.
        # Rename the complete control-plane contract first, then restore the two
        # runtime-fixed discovery names documented by Envoy Gateway.
        normalized = manifest.replace(
            UPSTREAM_CONTROL_PLANE_NAME, self.control_plane_name
        )
        normalized = normalized.replace(
            f"secretName: {self.control_plane_name}",
            f"secretName: {UPSTREAM_CONTROL_PLANE_NAME}",
        )

        rendered: list[str] = []
        for source in normalized.split("\n---\n"):
            document = yaml.safe_load(source)
            if not isinstance(document, dict):
                if source.strip():
                    rendered.append(source)
                continue
            metadata = document.setdefault("metadata", {})
            if (
                document.get("kind") == "Service"
                and metadata.get("name") == self.control_plane_name
            ):
                # Envoy Gateway's data plane discovers the controller through
                # this fixed xDS service name.
                metadata["name"] = UPSTREAM_CONTROL_PLANE_NAME
            if document.get("kind") == "EnvoyProxy":
                kubernetes = (
                    document.setdefault("spec", {})
                    .setdefault("provider", {})
                    .setdefault("kubernetes", {})
                )
                kubernetes.setdefault("envoyDeployment", {})["name"] = (
                    self.data_plane_name
                )
                kubernetes.setdefault("envoyService", {})["name"] = (
                    self.data_plane_name
                )
            self._normalize_namespaced_names(document)
            self._normalize_cluster_rbac(document)
            rendered.append(yaml.safe_dump(document, sort_keys=False).rstrip())
        return "\n---\n".join(rendered) + "\n"

    def _production_name(self, value: str) -> str:
        if value.startswith(self.release_gateway_prefix):
            return "envoy-gateway" + value[len(self.release_gateway_prefix):]
        return value

    def _normalize_namespaced_names(self, document: dict) -> None:
        """Keep namespaced resources production-like and cluster RBAC unique."""
        kind = document.get("kind", "")
        metadata = document.get("metadata", {})
        name = metadata.get("name")
        if kind in self.NAMESPACED_KINDS and isinstance(name, str):
            metadata["name"] = self._production_name(name)

        if kind == "RoleBinding":
            role_ref = document.get("roleRef", {})
            if role_ref.get("kind") == "Role" and isinstance(role_ref.get("name"), str):
                role_ref["name"] = self._production_name(role_ref["name"])

        for subject in document.get("subjects", []) or []:
            if subject.get("kind") == "ServiceAccount" and isinstance(subject.get("name"), str):
                subject["name"] = self._production_name(subject["name"])

        pod_spec = document.get("spec", {}).get("template", {}).get("spec", {})
        service_account = pod_spec.get("serviceAccountName")
        if isinstance(service_account, str):
            pod_spec["serviceAccountName"] = self._production_name(service_account)

    def _normalize_cluster_rbac(self, document: dict) -> None:
        """Namespace cannot isolate controller-wide RBAC, so keep it unique."""
        if document.get("kind") not in {"ClusterRole", "ClusterRoleBinding"}:
            return

        def cluster_name(value: str) -> str:
            prefix = "envoy-gateway-envoy-gateway-"
            if value.startswith(prefix):
                return f"{self.resource_prefix}-envoy-gateway-" + value[len(prefix):]
            return value

        metadata = document.get("metadata", {})
        if isinstance(metadata.get("name"), str):
            metadata["name"] = cluster_name(metadata["name"])
        role_ref = document.get("roleRef", {})
        if isinstance(role_ref.get("name"), str):
            role_ref["name"] = cluster_name(role_ref["name"])

    def run(self) -> None:
        sys.stdout.write(self.render(sys.stdin.read()))


if __name__ == "__main__":
    EnvoyGatewayManifestNormalizer().run()
