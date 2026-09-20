"""Scenario 14 — real LDAP, GitHub OAuth, Envoy, and AuthGuard E2E."""
from __future__ import annotations

from common.model import RunContext, VerificationResult
from verifier import BaseVerifier


class AuthGuardVerifier(BaseVerifier):
    scenario_id = "14"
    title = "AuthGuard — LDAP Federation + GitHub OAuth + Envoy Policy"

    def run(self) -> VerificationResult:
        return self.execute(self._run_scenario)

    def _run_scenario(self) -> None:
        self.step(
            "verify LDAP federation, GitHub OAuth, Envoy, and resource-level policy",
            self.infrastructure.verify_authguard,
        )

def verifier(context: RunContext) -> VerificationResult:
    """Run the AuthGuard scenario."""
    return AuthGuardVerifier(context).run()
