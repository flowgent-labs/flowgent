"""Scenario 46: Hosted Login and business-UI sign-out through one Gateway."""

from __future__ import annotations

from playwright.sync_api import expect

from common.model import RunContext, VerificationResult
from verifier import BaseVerifier
from verifier.web.browser import BrowserConsoleVerifier


class HostedLoginVerifier(BrowserConsoleVerifier, BaseVerifier):
    """Verify branded account UX, the real OAuth session, and sign-out."""

    scenario_id = "46"
    title = "Hosted Login — OAuth Session and Business Sign-out"

    def __init__(self, context: RunContext) -> None:
        super().__init__(context)

    def run(self) -> VerificationResult:
        return self.execute(self._run_scenario)

    def _run_scenario(self) -> None:
        with self.browser_session(return_path="/dashboard"):
            self.step(
                "UI-46: inspect branded top-right account and embedded security UI",
                self._inspect_account_experience,
            )
            self.step(
                "UI-47: sign out from Flowgent through AuthGuard on the shared Gateway",
                self.sign_out,
            )
            self.step(
                "UI-48: sign in with the retained standalone acceptance account",
                self.acceptance_password_login,
            )

    def _inspect_account_experience(self) -> None:
        from deploy.authguard import APPLICATION_DISPLAY_NAME

        page = self.browser_page
        trigger = page.get_by_test_id("account-menu-trigger")
        expect(trigger).to_be_visible()
        box = trigger.bounding_box()
        viewport = page.viewport_size
        if not box or not viewport or box["x"] < viewport["width"] * 0.65 or box["y"] > 100:
            raise AssertionError(
                f"account trigger is not in the upper-right workspace header: box={box}, viewport={viewport}"
            )
        trigger.click()
        expect(page.get_by_test_id("account-application")).to_have_text(
            APPLICATION_DISPLAY_NAME
        )
        expect(page.get_by_test_id("account-brand-logo").first).to_have_attribute(
            "src", "/auth/assets/themes/custom/flowgent.svg"
        )

        original_url = page.url
        with page.expect_response(
            lambda response: "/auth/account/security?embedded=1" in response.url
        ) as security_response:
            page.get_by_role("menuitem", name="Account security", exact=True).click()
        response = security_response.value
        if response.status != 200:
            raise AssertionError(f"embedded account security returned HTTP {response.status}")
        headers = response.headers
        if headers.get("x-frame-options", "").upper() != "SAMEORIGIN":
            raise AssertionError(f"embedded account security frame policy is unsafe: {headers}")
        if "frame-ancestors 'self'" not in headers.get("content-security-policy", ""):
            raise AssertionError("embedded account security lacks same-origin CSP frame policy")
        if page.url != original_url:
            raise AssertionError(
                f"account security unexpectedly navigated the Flowgent SPA: {page.url}"
            )

        frame = page.frame_locator('[data-testid="authguard-security-frame"]')
        account_page = frame.locator(".account-security-page")
        expect(account_page).to_be_visible(timeout=20_000)
        embedded = account_page.get_attribute("data-embedded")
        if embedded is not None and embedded != "true":
            raise AssertionError(f"AuthGuard reported an invalid embedded state: {embedded}")
        expect(frame.locator("html")).to_have_attribute(
            "data-application-theme", "flowgent", timeout=20_000
        )
        self.screenshot("UI-46", "branded top-right account with embedded AuthGuard security")
        page.get_by_role("button", name="Close", exact=True).click()
        expect(page.get_by_test_id("authguard-security-frame")).to_have_count(0)
        self.details.append(
            "Flowgent rendered the Host-resolved AuthGuard application name/logo in the "
            "upper-right account menu and opened the same-origin credential page in a "
            "non-navigating modal with SAMEORIGIN plus frame-ancestors self protection."
        )
