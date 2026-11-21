"""Case entry point for shared report helpers."""

from e2e_util.report import E2EReportHelper as ReportHelper
from e2e_util.report import EvidenceArtifact, VerificationResult

from util.constant import E2EOtherDefinitions as config

REPORTS_DIR = config.REPORTS_DIR


class E2EReportHelper(ReportHelper):
    directory = REPORTS_DIR


__all__ = ["E2EReportHelper", "EvidenceArtifact", "VerificationResult"]
