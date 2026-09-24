"""Markdown reports and machine-readable evidence for repeatable E2E rounds."""

from __future__ import annotations

from datetime import datetime
import json
from pathlib import Path
import re
import shutil

from .model import EvidenceArtifact, VerificationResult
from .config import REPORTS_DIR


ANSI_ESCAPE = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]")


class E2EReportWriter:
    """Own evidence paths and Markdown report serialization for one E2E run."""

    @staticmethod
    def archive() -> Path | None:
        """Move the current evidence set aside without touching retained workloads."""
        REPORTS_DIR.mkdir(parents=True, exist_ok=True)
        current = [
            path
            for path in REPORTS_DIR.iterdir()
            if path.name not in {".gitignore", ".state"} and not path.name.startswith("archived-")
        ]
        if not current:
            return None
        archive = REPORTS_DIR / datetime.now().strftime("archived-%Y%m%d-%H%M%S-%f")
        archive.mkdir()
        for path in current:
            shutil.move(str(path), archive / path.name)
        return archive

    @staticmethod
    def evidence_path(round_number: int, scenario_id: str, case_id: str, title: str, suffix: str) -> Path:
        slug = E2EReportWriter.slug(title)[:80]
        directory = REPORTS_DIR / f"round-{round_number:02d}" / "evidence" / scenario_id
        directory.mkdir(parents=True, exist_ok=True)
        return directory / f"{case_id.lower().replace('.', '_')}_{slug}.{suffix}"

    @classmethod
    def write_execution_evidence(cls, round_number: int, result: VerificationResult) -> EvidenceArtifact:
        path = cls.evidence_path(round_number, result.scenario_id, result.scenario_id, result.title, "json")
        payload = {
            "scenarioId": result.scenario_id,
            "caseId": result.scenario_id,
            "title": result.title,
            "status": "PASS" if result.passed else "FAIL",
            "durationSeconds": round(result.duration_seconds, 6),
            "details": result.details,
            "output": ANSI_ESCAPE.sub("", result.output),
            "error": result.error,
        }
        path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        artifact = EvidenceArtifact(result.scenario_id, result.title, "application/json", path)
        result.evidence.append(artifact)
        return artifact

    @classmethod
    def write_round(cls, round_number: int, result: VerificationResult) -> Path:
        round_dir = REPORTS_DIR / f"round-{round_number:02d}"
        round_dir.mkdir(parents=True, exist_ok=True)
        if not result.evidence:
            cls.write_execution_evidence(round_number, result)
        report = round_dir / f"{result.scenario_id}_{cls.slug(result.title)}.md"
        status = "PASS" if result.passed else "FAIL"
        lines = [f"# [{status}] {result.scenario_id} {result.title}", "", f"- Round: {round_number}", f"- Duration: {result.duration_seconds:.2f}s"]
        lines.extend(f"- {detail}" for detail in result.details)
        if result.evidence:
            lines.extend(["", "## Evidence", ""])
            for artifact in result.evidence:
                relative = artifact.path.relative_to(round_dir)
                lines.append(f"- [{artifact.case_id} — {artifact.title}]({relative.as_posix()}) (`{artifact.kind}`)")
        if result.output:
            lines.extend(["", "## Output", "", "```text", ANSI_ESCAPE.sub("", result.output).rstrip(), "```"])
        if result.error:
            lines.extend(["", "## Error", "", "```text", result.error.rstrip(), "```"])
        report.write_text("\n".join(lines).rstrip() + "\n", encoding="utf-8")
        return report

    @staticmethod
    def write_summary(rounds: list[list[VerificationResult]]) -> Path:
        REPORTS_DIR.mkdir(parents=True, exist_ok=True)
        report = REPORTS_DIR / "00_summary.md"
        lines = ["# Security Autonomy Fixer E2E Summary", ""]
        for round_number, results in enumerate(rounds, start=1):
            passed = sum(result.passed for result in results)
            lines.extend([f"## Round {round_number}", "", f"Result: {passed}/{len(results)} verifier groups passed.", "", "| ID | Verifier | Result | Duration |", "| --- | --- | --- | ---: |"])
            for result in results:
                status = "PASS" if result.passed else "FAIL"
                lines.append(f"| {result.scenario_id} | {result.title} | {status} | {result.duration_seconds:.2f}s |")
            lines.append("")
        report.write_text("\n".join(lines).rstrip() + "\n", encoding="utf-8")
        return report

    @staticmethod
    def slug(value: str) -> str:
        return re.sub(r"[^a-z0-9]+", "_", value.lower()).strip("_")
