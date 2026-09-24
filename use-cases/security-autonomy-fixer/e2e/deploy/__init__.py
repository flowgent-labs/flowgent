"""Deployment contracts and backend selection for the security-fixer E2E."""

from __future__ import annotations

from abc import ABC, abstractmethod
import time
from typing import Callable, TypeVar

from common.model import CommandResult, RunContext


T = TypeVar("T")


class BaseDeployer(ABC):
    """Shared lifecycle state and step contract for every E2E deployer."""

    backend = ""
    component = ""

    def __init__(self, context: RunContext) -> None:
        self.context = context
        self.commands: list[CommandResult] = []
        self.details: list[str] = []
        self._step_number = 0

    def step(self, label: str, operation: Callable[[], T]) -> T:
        self._step_number += 1
        scope = self.backend or self.component or self.__class__.__name__.removesuffix("Deployer").lower()
        case_id = f"deploy.{scope}.{self._step_number:02d}"
        started = time.monotonic()
        print(f"    [{case_id}] {label} ...", flush=True)
        result = operation()
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
        self.step("configure verifier client environment", self.configure_client_environment)
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
        raise NotImplementedError(f"{self.__class__.__name__} is not a topology deployer")

    def reset_database(self) -> None:
        raise NotImplementedError(f"{self.__class__.__name__} is not a topology deployer")

    def build_images(self) -> None:
        raise NotImplementedError(f"{self.__class__.__name__} is not a topology deployer")

    def verify_authguard(self, *, resource_scope: bool = True) -> None:
        raise NotImplementedError(f"{self.__class__.__name__} is not a topology deployer")

    def import_config(self) -> None:
        raise NotImplementedError(f"{self.__class__.__name__} is not a topology deployer")

    def configure_client_environment(self) -> None:
        raise NotImplementedError(f"{self.__class__.__name__} is not a topology deployer")

    def runtime_exec(self, command: tuple[str, ...]) -> CommandResult:
        raise NotImplementedError(f"{self.__class__.__name__} has no runtime executor")


class E2EDeployerFactory:
    """Create the selected full-topology deployer without coupling verifiers."""

    @staticmethod
    def create(context: RunContext) -> BaseDeployer:
        if context.deployer == "k8s":
            from .base.kubernetes import KubernetesDeployer

            return KubernetesDeployer(context)
        if context.deployer == "docker":
            from .base.docker import DockerDeployer

            return DockerDeployer(context)
        raise ValueError(f"unsupported E2E deployer: {context.deployer}")


__all__ = ["BaseDeployer", "E2EDeployerFactory"]
