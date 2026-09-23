"""Scenario 46: Hosted Login and business-UI sign-out through one Gateway."""

from __future__ import annotations

from common.model import RunContext, VerificationResult
from verifier import BaseVerifier
from verifier.web.browser import BrowserConsoleVerifier


class HostedLoginVerifier(BrowserConsoleVerifier, BaseVerifier):
    """Verify the real OAuth session and lower-left business sign-out control."""

    scenario_id = "46"
    title = "Hosted Login — OAuth Session and Business Sign-out"

    def __init__(self, context: RunContext) -> None:
        super().__init__(context)

    def run(self) -> VerificationResult:
        return self.execute(self._run_scenario)

    def _run_scenario(self) -> None:
        with self.browser_session(return_path="/dashboard"):
            self.step(
                "UI-46: sign out from Flowgent through AuthGuard on the shared Gateway",
                self.sign_out,
            )
