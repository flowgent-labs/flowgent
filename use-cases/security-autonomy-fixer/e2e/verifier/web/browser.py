"""Shared real-browser support for Web-console E2E scenarios."""

from __future__ import annotations

import base64
from contextlib import contextmanager
import json
import os
import re
import subprocess
from typing import Any, Iterator
from urllib.parse import quote

try:
    from playwright.sync_api import Page, expect, sync_playwright
except ImportError:
    Page = Any
    expect = None
    sync_playwright = None

from common import config
from common.model import RunContext


class BrowserConsoleVerifier:
    """Mixin that equips a direct ``BaseVerifier`` scenario with a real browser."""

    page: Page | None = None
    fixture_flow_id = "sub-fix"

    def __init__(self, context: RunContext) -> None:
        super().__init__(context)
        self.page = None

    @property
    def origin(self) -> str:
        explicit_origin = os.getenv("FLOWGENT_E2E_WEB_URL", "").rstrip("/")
        if explicit_origin:
            return explicit_origin
        return f"http://localhost:{config.LOCAL_GATEWAY_PORT}"

    @property
    def mock_github_host(self) -> str:
        if self.context.deployer == "docker":
            return "mock-github"
        # The mock has its own isolated namespace, while the Service itself uses
        # the same production-style name as a real deployment.  Keep this in
        # sync with AuthGuardRuntime.runtime_config so Chromium can resolve the
        # OAuth provider through the retained localhost port-forward.
        return f"mock-github.{config.MOCK_GITHUB_NAMESPACE}.svc.cluster.local"

    @property
    def web_namespace(self) -> str:
        return os.getenv("FLOWGENT_E2E_WEB_NAMESPACE", config.NAMESPACE_ID)

    @property
    def browser_page(self) -> Page:
        if self.page is None:
            raise RuntimeError("Chromium page was not initialized")
        return self.page

    @contextmanager
    def browser_session(self, return_path: str | None = None) -> Iterator[Page]:
        if sync_playwright is None or expect is None:
            raise RuntimeError(
                "Playwright is required for Web E2E; install "
                "use-cases/security-autonomy-fixer/e2e/requirements.txt and run "
                "'python3 -m playwright install chromium'."
            )
        with sync_playwright() as playwright:
            try:
                browser = playwright.chromium.launch(
                    headless=True,
                    args=[
                        "--proxy-server=direct://",
                        "--proxy-bypass-list=*",
                        "--disable-features=AsyncDns,UseDnsHttpsSvcbAlpn",
                        "--host-resolver-rules="
                        f"MAP {self.mock_github_host}:8080 "
                        f"127.0.0.1:{config.LOCAL_MOCK_GITHUB_PORT}",
                    ],
                )
            except Exception as error:
                raise RuntimeError(
                    "Playwright Chromium is unavailable; run 'python3 -m playwright install chromium'."
                ) from error
            try:
                context = browser.new_context(locale="en-US", viewport={"width": 1440, "height": 1100})
                self.page = context.new_page()
                fixture_route = return_path is None
                return_path = return_path or f"/{self.web_namespace}/{self.fixture_flow_id}"
                self.page.goto(
                    f"{self.origin}{return_path}",
                    wait_until="domcontentloaded",
                )
                self.page.wait_for_url(
                    f"{self.origin}/auth/login?return_to={quote(return_path, safe='')}",
                    timeout=20_000,
                )
                expect(self.page.get_by_test_id("login-provider-github")).to_be_visible(
                    timeout=20_000
                )
                self.page.get_by_test_id("login-provider-github").click()
                self.page.wait_for_url(f"{self.origin}{return_path}", timeout=30_000)
                if fixture_route:
                    expect(self.page.get_by_test_id("flow-id")).to_have_value(
                        self.fixture_flow_id, timeout=20_000
                    )
                else:
                    expect(
                        self.page.get_by_role(
                            "button", name="Account menu", exact=True
                        )
                    ).to_be_visible(timeout=20_000)
                session_status = self.page.evaluate(
                    "async () => (await fetch('/auth/session')).status"
                )
                if session_status != 200:
                    raise RuntimeError(
                        "Hosted Login did not establish the canonical Gateway session: "
                        f"HTTP {session_status}"
                    )
                self.details.append(
                    "Chromium proved the Flowgent session boundary redirected an anonymous "
                    "deep link to same-origin Hosted Login, then completed GitHub OAuth"
                )
                yield self.page
            finally:
                self.page = None
                browser.close()

    def screenshot(self, case_id: str, title: str) -> None:
        path = self.evidence_path(case_id, title, "png")
        self.browser_page.screenshot(path=str(path), full_page=True, animations="disabled")
        self.record_evidence(case_id, title, "image/png", path)

    def sign_out(self) -> None:
        """Prove that the business UI signs out through the shared Gateway origin."""
        from deploy.authguard import APPLICATION_DISPLAY_NAME

        page = self.browser_page
        session_before = page.evaluate(
            "async () => (await fetch('/auth/session')).status"
        )
        if session_before != 200:
            raise RuntimeError(
                f"Flowgent browser session was absent before sign-out: HTTP {session_before}"
            )
        page.get_by_role("button", name="Account menu", exact=True).click()
        expect(page.get_by_role("menuitem", name="Sign out", exact=True)).to_be_visible()
        self.screenshot("UI-47-before", "Flowgent account menu sign-out control")
        with page.expect_response(
            lambda response: response.request.method == "POST"
            and response.url.endswith("/auth/logout")
        ) as logout_response:
            page.get_by_role("menuitem", name="Sign out", exact=True).click()
        if logout_response.value.status != 204:
            raise RuntimeError(
                "Flowgent sign-out did not reach AuthGuard through Envoy: "
                f"HTTP {logout_response.value.status}"
            )
        page.wait_for_url(
            re.compile(
                rf"^{re.escape(self.origin)}/auth/login\?return_to=(?:%2F|/)dashboard$"
            ),
            timeout=20_000,
        )
        expect(
            page.get_by_role(
                "heading", name=f"Sign in to {APPLICATION_DISPLAY_NAME}"
            )
        ).to_be_visible()
        session_after = page.evaluate(
            "async () => (await fetch('/auth/session')).status"
        )
        if session_after != 401:
            raise RuntimeError(
                f"AuthGuard retained the session after Flowgent sign-out: HTTP {session_after}"
            )
        self.screenshot("UI-47", "Flowgent sign-out returned to Hosted Login")
        self.details.append(
            "Flowgent sign-out used the same-origin Gateway and proved "
            "session_before=200, logout=204, session_after=401 without a 404."
        )

    def acceptance_password_login(self) -> None:
        """Exercise the retained human account through the real Hosted Login form."""
        if self.context.deployer != "k8s":
            self.details.append(
                "Standalone acceptance-account browser proof is K8s-only; Docker OAuth proof passed"
            )
            return
        from deploy.authguard import ACCEPTANCE_SECRET

        result = subprocess.run(
            [
                "kubectl",
                "get",
                "secret",
                ACCEPTANCE_SECRET,
                "-n",
                config.AUTHGUARD_NAMESPACE,
                "-o",
                "json",
            ],
            capture_output=True,
            text=True,
            timeout=20,
        )
        if result.returncode != 0:
            raise RuntimeError("retained acceptance credential Secret is unavailable")
        encoded = json.loads(result.stdout).get("data", {})
        username = base64.b64decode(encoded.get("username", "")).decode()
        password = base64.b64decode(encoded.get("password", "")).decode()
        if not username or not password:
            raise RuntimeError("retained acceptance credentials are incomplete")

        page = self.browser_page
        expect(page.get_by_test_id("login-password-submit")).to_be_visible()
        expect(page.get_by_test_id("login-totp")).to_have_count(0)
        page.get_by_test_id("login-id").fill(username)
        page.get_by_test_id("login-password").fill(password)
        page.get_by_test_id("login-password-submit").click()
        page.wait_for_url(f"{self.origin}/dashboard", timeout=20_000)
        expect(page.get_by_role("button", name="Account menu", exact=True)).to_be_visible()
        expect(page.get_by_test_id("dashboard-telemetry")).to_be_visible(timeout=20_000)
        session_status = page.evaluate(
            "async () => (await fetch('/auth/session')).status"
        )
        runtime_namespace = page.evaluate(
            "JSON.parse(localStorage.getItem('flowgent-web-preferences')).state.namespace"
        )
        protected_status = page.evaluate(
            """async () => (await fetch(
              '/api/v1/security-fixer/flows/security-autonomy-fixer'
            )).status"""
        )
        if (
            session_status != 200
            or protected_status != 200
            or runtime_namespace != self.web_namespace
        ):
            raise RuntimeError(
                "standalone acceptance login did not traverse the complete security chain: "
                f"session={session_status}, protected_flow={protected_status}, "
                f"runtime_namespace={runtime_namespace}"
            )
        self.screenshot("UI-48", "Standalone password acceptance login without TOTP")
        self.details.append(
            "Hosted Login omitted TOTP, accepted the retained password account, established "
            "a canonical cookie, loaded the authorized runtime namespace, and accessed the "
            "fixture through Envoy plus Flowgent AuthZ"
        )
