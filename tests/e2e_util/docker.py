"""Generic Docker runtime detection and component Compose lifecycle."""

from __future__ import annotations

import json
import os
import shutil
from pathlib import Path

from .images import E2EImagesDefinitions
from .process import E2EProcessHelper


class E2EDockerDeployer:
    @staticmethod
    def detect() -> dict[str, str]:
        selected = os.getenv("DOCKER_CONTEXT", "")
        if not selected and not os.getenv("DOCKER_HOST"):
            result = E2EProcessHelper.run(("docker", "context", "show"), stream=False)
            if not result.passed:
                raise RuntimeError("Docker has no accessible active context")
            selected = result.output.strip()
        environment = {"DOCKER_CONTEXT": selected} if selected else {}
        probe = E2EProcessHelper.run(
            ("docker", "info", "--format", "{{json .}}"),
            environment=environment,
            stream=False,
        )
        if not probe.passed:
            raise RuntimeError(f"Docker daemon is unavailable for context {selected!r}")
        info = json.loads(probe.output)
        architecture = {"aarch64": "arm64", "x86_64": "amd64"}.get(
            info.get("Architecture"), info.get("Architecture")
        )
        if (
            architecture not in {"amd64", "arm64"}
            or info.get("OSType", "linux") != "linux"
        ):
            raise RuntimeError("E2E requires a Linux container engine on amd64/arm64")
        platform = E2EImagesDefinitions.platform()
        environment["DOCKER_DEFAULT_PLATFORM"] = platform
        environment.update(
            E2EImagesDefinitions.runtime_environment(f"linux/{architecture}")
        )
        os.environ.update(environment)
        kind = (
            "colima"
            if selected.startswith("colima")
            else "orbstack"
            if selected == "orbstack"
            else "native"
        )
        print(f"  Docker runtime: {kind} ({platform})")
        return environment

    @staticmethod
    def compose_command() -> tuple[str, ...]:
        result = E2EProcessHelper.run(("docker", "compose", "version"), stream=False)
        if result.passed:
            return ("docker", "compose")
        if shutil.which("docker-compose"):
            return ("docker-compose",)
        raise RuntimeError("Docker Compose v2 or docker-compose is required")

    @classmethod
    def compose(
        cls,
        files: tuple[Path, ...],
        project: str,
        *arguments: str,
        environment: dict | None = None,
        timeout: int = 300,
    ):
        if not files:
            raise ValueError("At least one component Compose file is required")
        command = (
            *cls.compose_command(),
            "-p",
            project,
            *(part for path in files for part in ("-f", str(path))),
        )
        deployment_environment = {
            "FLOWGENT_REPO_DIR": str(Path(__file__).resolve().parents[2]),
            "FLOWGENT_E2E_CASE_DIR": str(Path(files[0]).absolute().parents[2]),
            **(environment or {}),
            **E2EImagesDefinitions.environment(),
        }
        if "up" in arguments and "--dry-run" not in arguments:
            options = arguments[: arguments.index("up")]
            rendered = E2EProcessHelper.run(
                (*command, *options, "config", "--format", "json"),
                environment=deployment_environment,
                timeout_seconds=60,
                stream=False,
            )
            if not rendered.passed:
                raise RuntimeError(
                    "Compose configuration validation failed:\n" + rendered.output
                )
            document, _ = json.JSONDecoder().raw_decode(rendered.output.lstrip())
            profiles = set(
                deployment_environment.get(
                    "COMPOSE_PROFILES", os.getenv("COMPOSE_PROFILES", "")
                ).split(",")
            )
            for index, option in enumerate(options):
                if option == "--profile":
                    profiles.add(options[index + 1])
                elif option.startswith("--profile="):
                    profiles.add(option.split("=", 1)[1])
            for service in document.get("services", {}).values():
                if service.get("build") and "--build" in arguments:
                    continue
                enabled = (
                    not service.get("profiles")
                    or "*" in profiles
                    or profiles.intersection(service["profiles"])
                )
                if enabled and service.get("image"):
                    E2EImagesDefinitions.prepare_runtime(service["image"])
        return E2EProcessHelper.run(
            (*command, *arguments),
            environment=deployment_environment,
            timeout_seconds=timeout,
        )


class E2EDockerCleaner:
    @staticmethod
    def clean(
        files: tuple[Path, ...], project: str, *, environment: dict | None = None
    ):
        result = E2EDockerDeployer.compose(
            files,
            project,
            "down",
            "--volumes",
            "--remove-orphans",
            environment=environment,
        )
        if not result.passed:
            raise RuntimeError(f"Compose cleanup failed for {project}")
        return result
