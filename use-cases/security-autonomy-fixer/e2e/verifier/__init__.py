"""Verifier contract shared by all security-autonomy E2E scenarios."""

from __future__ import annotations

from abc import ABC, abstractmethod
import json
import re
import time
import traceback
from typing import Callable, TypeVar

from common.config import REPORTS_DIR
from common.model import EvidenceArtifact, RunContext, VerificationResult
from deploy import E2EDeployerFactory


T = TypeVar("T")


class BaseVerifier(ABC):
    """Compose deployment access, assertions, steps, and evidence output."""

    scenario_id = ""
    title = ""

    def __init__(self, context: RunContext) -> None:
        self.context = context
        self.infrastructure = E2EDeployerFactory.create(context)
        self.commands = self.infrastructure.commands
        self.details = self.infrastructure.details
        self.evidence: list[EvidenceArtifact] = []
        self._step_number = 0

    def step(self, label: str, operation: Callable[[], T]) -> T:
        self._step_number += 1
        case_id = f"{self.scenario_id}.{self._step_number:02d}"
        detail_offset = len(self.details)
        started = time.monotonic()
        print(f"    [{case_id}] {label} ...", flush=True)
        try:
            value = operation()
        except Exception:
            print(f"    [{case_id}] FAIL ({time.monotonic() - started:.2f}s)", flush=True)
            raise
        duration = time.monotonic() - started
        self._write_step_evidence(case_id, label, duration, self.details[detail_offset:])
        print(f"    [{case_id}] PASS ({duration:.2f}s)", flush=True)
        return value

    def execute(self, operation: Callable[[], None]) -> VerificationResult:
        started = time.monotonic()
        passed = False
        error: str | None = None
        try:
            operation()
            passed = True
        except Exception:
            error = traceback.format_exc()
            self.details.append(error)
        return VerificationResult(
            scenario_id=self.scenario_id,
            title=self.title,
            passed=passed,
            duration_seconds=time.monotonic() - started,
            error=error,
            details=self.details,
            commands=self.commands,
            evidence=self.evidence,
        )

    @abstractmethod
    def run(self) -> VerificationResult:
        """Execute this scenario through :meth:`execute`."""

    def _write_step_evidence(
        self,
        case_id: str,
        title: str,
        duration_seconds: float,
        assertions: list[str],
    ) -> None:
        slug = re.sub(r"[^a-z0-9]+", "_", title.lower()).strip("_")[:80]
        directory = REPORTS_DIR / f"round-{self.context.round_number:02d}" / "evidence" / self.scenario_id
        directory.mkdir(parents=True, exist_ok=True)
        path = directory / f"{case_id.replace('.', '_')}_{slug}.json"
        payload = {
            "scenarioId": self.scenario_id,
            "caseId": case_id,
            "title": title,
            "status": "PASS",
            "durationSeconds": round(duration_seconds, 6),
            "assertions": assertions or ["scenario operation completed without assertion failure: PASS"],
        }
        path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        self.evidence.append(EvidenceArtifact(case_id, title, "application/json", path))


__all__ = ["BaseVerifier"]
