"""Scenario 14 — real LDAP, GitHub OAuth, Envoy, and AuthGuard E2E."""

from __future__ import annotations

from util.report import VerificationResult

from verifier.context import BaseVerifier, VerificationContext


class E2ES14AuthguardVerifier(BaseVerifier):
    scenario_id = "14"
    title = "AuthGuard — LDAP Federation + GitHub OAuth + Envoy Policy"

    def verifier(self, context: VerificationContext) -> VerificationResult:
        return self.execute(self._run_scenario)

    def _run_scenario(self) -> None:
        self.step(
            "verify LDAP federation, GitHub OAuth, Envoy, and resource-level policy",
            self.infrastructure.verify_authguard,
        )

    def deploy(self, context: VerificationContext) -> None:
        """Use the topology prepared by the runner; no scenario reset is needed."""

    def init(self, context: VerificationContext) -> None:
        """Retain initialization ordering inside the existing scenario steps."""
