"""Immutable contracts shared by deployment, verification, and reporting."""

from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path


@dataclass(frozen=True)
class CommandResult:
    command: tuple[str, ...]
    cwd: Path
    return_code: int
    duration_seconds: float
    output: str

    @property
    def passed(self) -> bool:
        return self.return_code == 0


@dataclass(frozen=True)
class RunContext:
    round_number: int
    clean: bool
    timeout_seconds: int
    build_images: bool = True
    namespace: str = ""
    release: str = ""
    deployer: str = "k8s"


@dataclass(frozen=True)
class EvidenceArtifact:
    case_id: str
    title: str
    kind: str
    path: Path


@dataclass
class VerificationResult:
    scenario_id: str
    title: str
    passed: bool
    duration_seconds: float
    output: str = ""
    error: str | None = None
    details: list[str] = field(default_factory=list)
    commands: list[CommandResult] = field(default_factory=list)
    evidence: list[EvidenceArtifact] = field(default_factory=list)
