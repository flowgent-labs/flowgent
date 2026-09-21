"""Shared real-browser support for Web-console E2E scenarios."""

from __future__ import annotations

from contextlib import contextmanager
import os
from typing import Any, Iterator

try:
    from playwright.sync_api import Page, expect, sync_playwright
except ImportError:
    Page = Any
    expect = None
    sync_playwright = None

from common import config
from common.agentflow import FLOW_ID as FIXTURE_FLOW_ID
from common.model import RunContext
class BrowserConsoleVerifier:
    """Mixin that equips a direct ``BaseVerifier`` scenario with a real browser."""

    page: Page | None = None

    def __init__(self, context: RunContext) -> None:
        super().__init__(context)
        self.page = None

    @property
    def origin(self) -> str:
        explicit_origin = os.getenv("FLOWGENT_E2E_WEB_URL", "").rstrip("/")
        if explicit_origin:
            return explicit_origin
        port = 21080 if self.context.deployer == "docker" else 31080
        return f"http://127.0.0.1:{port}"

    @property
    def web_namespace(self) -> str:
        return os.getenv("FLOWGENT_E2E_WEB_NAMESPACE", config.NAMESPACE_ID)

    @property
    def browser_page(self) -> Page:
        if self.page is None:
            raise RuntimeError("Chromium page was not initialized")
        return self.page

    @contextmanager
    def browser_session(self) -> Iterator[Page]:
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
                    ],
                )
            except Exception as error:
                raise RuntimeError(
                    "Playwright Chromium is unavailable; run 'python3 -m playwright install chromium'."
                ) from error
            try:
                context = browser.new_context(locale="en-US", viewport={"width": 1440, "height": 1100})
                self.page = context.new_page()
                # Route through an existing Flow view so this fresh browser gets
                # the namespace where the E2E runtime and AuthGuard grants live.
                self.page.goto(
                    f"{self.origin}/{self.web_namespace}/{FIXTURE_FLOW_ID}", wait_until="domcontentloaded"
                )
                expect(self.page.get_by_test_id("flow-id")).to_have_value(FIXTURE_FLOW_ID, timeout=20_000)
                yield self.page
            finally:
                self.page = None
                browser.close()

    def screenshot(self, case_id: str, title: str) -> None:
        path = self.evidence_path(case_id, title, "png")
        self.browser_page.screenshot(path=str(path), full_page=True, animations="disabled")
        self.record_evidence(case_id, title, "image/png", path)
