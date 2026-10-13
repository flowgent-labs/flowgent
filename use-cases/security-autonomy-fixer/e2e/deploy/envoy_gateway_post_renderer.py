#!/usr/bin/env python3
"""Give the isolated Flowgent Envoy control/data planes stable E2E names."""

from __future__ import annotations

import os
import sys

import yaml


UPSTREAM_CONTROL_PLANE_NAME = "envoy-gateway"


class EnvoyGatewayManifestNormalizer:
    """Normalize only the E2E controller and generated data-plane identities."""

    @staticmethod
    def default_control_plane_name(resource_prefix: str) -> str:
        environment_prefix = (
            resource_prefix[: -len("-flowgent")]
            if resource_prefix.endswith("-flowgent")
            else resource_prefix
        )
        return f"{environment_prefix}-envoy-gateway"

    def __init__(self) -> None:
        resource_prefix = os.getenv("FLOWGENT_E2E_RESOURCE_PREFIX", "e2e-flowgent")
        self.control_plane_name = os.getenv(
            "FLOWGENT_E2E_ENVOY_GATEWAY_NAME",
            self.default_control_plane_name(resource_prefix),
        )
        self.data_plane_name = os.getenv(
            "FLOWGENT_E2E_ENVOY_NAME", f"{resource_prefix}-envoy"
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
                (
                    document.setdefault("spec", {})
                    .setdefault("provider", {})
                    .setdefault("kubernetes", {})
                    .setdefault("envoyDeployment", {})["name"]
                ) = self.data_plane_name
            rendered.append(yaml.safe_dump(document, sort_keys=False).rstrip())
        return "\n---\n".join(rendered) + "\n"

    def run(self) -> None:
        sys.stdout.write(self.render(sys.stdin.read()))


if __name__ == "__main__":
    EnvoyGatewayManifestNormalizer().run()
