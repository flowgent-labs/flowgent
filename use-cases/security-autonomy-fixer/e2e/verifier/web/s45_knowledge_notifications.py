"""Scenario 45: verify browser CRUD for Knowledge and notification channels."""

from __future__ import annotations

import uuid

from playwright.sync_api import expect

from common.model import RunContext, VerificationResult
from verifier import BaseVerifier
from verifier.web.browser import BrowserConsoleVerifier


class KnowledgeNotificationsVerifier(BrowserConsoleVerifier, BaseVerifier):
    scenario_id = "45"
    title = "Web Console — Knowledge and Notification CRUD"

    def __init__(self, context: RunContext) -> None:
        super().__init__(context)
        suffix = uuid.uuid4().hex[:8]
        self.knowledge_original = f"ui-knowledge-{suffix}"
        self.knowledge_title = f"{self.knowledge_original}-v2"
        self.notification_original = f"ui-webhook-{suffix}"
        self.notification_name = f"{self.notification_original}-v2"
        self.knowledge_created = False
        self.notification_created = False

    def run(self) -> VerificationResult:
        return self.execute(self._run_scenario)

    def _run_scenario(self) -> None:
        with self.browser_session():
            try:
                self.step("UI-01: create and revise share-scoped Knowledge", self._knowledge)
                self.step("UI-02: create and revise a Webhook notification channel", self._notification)
                self.step("UI-03: inspect namespace runtime configuration UI", self._runtime_config)
                self.step("UI-04: delete scenario-owned Knowledge and notification records", self._delete)
            finally:
                self._cleanup()

    def _knowledge(self) -> None:
        page = self.browser_page
        page.goto(f"{self.origin}/memory/share", wait_until="domcontentloaded")
        page.get_by_test_id("memory-create").click()
        page.get_by_test_id("memory-title").fill(self.knowledge_original)
        page.get_by_test_id("memory-content").fill("Browser-created shared operational knowledge.")
        page.get_by_test_id("memory-source-ref").fill("ui-e2e")
        page.get_by_test_id("memory-tags").fill("browser, security")
        page.get_by_test_id("memory-metadata").fill('{"origin":"browser-e2e"}')
        with page.expect_response(
            lambda response: response.request.method == "POST"
            and response.url.endswith(f"/api/v1/{self.web_namespace}/knowledge"),
            timeout=20_000,
        ) as request:
            page.get_by_test_id("memory-save").click()
        response = request.value
        if not response.ok:
            raise AssertionError(f"Knowledge create returned HTTP {response.status}: {response.text()[:400]}")
        self.screenshot("UI-01-request", "browser submitted share-scoped Knowledge")
        expect(page.get_by_test_id(f"memory-card-{self.knowledge_original}")).to_have_count(1, timeout=20_000)
        self.knowledge_created = True
        page.get_by_test_id(f"memory-card-{self.knowledge_original}").locator("button").first.click()
        page.get_by_test_id("memory-title").fill(self.knowledge_title)
        page.get_by_test_id("memory-content").fill("Browser-revised shared operational knowledge.")
        page.get_by_test_id("memory-save").click()
        expect(page.get_by_test_id(f"memory-card-{self.knowledge_title}")).to_have_count(1, timeout=20_000)
        self.screenshot("UI-01", "browser created and revised Knowledge through the Memory UI")
        self.details.append("Created, edited, and listed a share-scoped Knowledge entry entirely through the browser.")

    def _notification(self) -> None:
        page = self.browser_page
        page.goto(f"{self.origin}/notifications", wait_until="domcontentloaded")
        page.get_by_test_id("notification-create").click()
        page.get_by_test_id("notification-name").fill(self.notification_original)
        page.get_by_test_id("notification-provider").select_option("webhook")
        page.get_by_test_id("notification-secret-url").fill("https://notify.example.test/e2e")
        page.get_by_test_id("notification-save").click()
        expect(page.get_by_test_id(f"notification-card-{self.notification_original}")).to_have_count(1, timeout=20_000)
        self.notification_created = True
        page.get_by_test_id(f"notification-card-{self.notification_original}").locator("button").first.click()
        page.get_by_test_id("notification-name").fill(self.notification_name)
        page.get_by_test_id("notification-save").click()
        expect(page.get_by_test_id(f"notification-card-{self.notification_name}")).to_have_count(1, timeout=20_000)
        self.screenshot("UI-02", "browser created and revised encrypted Webhook notification channel")
        self.details.append("Created and renamed a Webhook notification through its write-only credential UI.")

    def _runtime_config(self) -> None:
        page = self.browser_page
        page.goto(
            f"{self.origin}/namespaces/{self.web_namespace}/settings/environment",
            wait_until="domcontentloaded",
        )
        expect(page.get_by_test_id("namespace-environment-settings")).to_be_visible(timeout=20_000)
        expect(page.get_by_test_id("effective-environment")).to_be_visible()
        self.screenshot("UI-03", "namespace runtime configuration displayed effective environment values")
        self.details.append("Rendered namespaced runtime configuration and effective inherited environment UI without altering shared values.")

    def _delete(self) -> None:
        page = self.browser_page
        page.goto(f"{self.origin}/memory/share", wait_until="domcontentloaded")
        page.once("dialog", lambda dialog: dialog.accept())
        page.get_by_test_id(f"memory-delete-{self.knowledge_title}").click()
        expect(page.get_by_test_id(f"memory-card-{self.knowledge_title}")).to_have_count(0, timeout=20_000)
        self.knowledge_created = False
        page.goto(f"{self.origin}/notifications", wait_until="domcontentloaded")
        page.once("dialog", lambda dialog: dialog.accept())
        page.get_by_test_id(f"notification-delete-{self.notification_name}").click()
        expect(page.get_by_test_id(f"notification-card-{self.notification_name}")).to_have_count(0, timeout=20_000)
        self.notification_created = False
        self.screenshot("UI-04", "browser deleted scenario-owned Knowledge and notification records")
        self.details.append("Deleted both scenario-owned console resources.")

    def _cleanup(self) -> None:
        page = self.browser_page
        if self.knowledge_created:
            page.goto(f"{self.origin}/memory/share", wait_until="domcontentloaded")
            for title in (self.knowledge_title, self.knowledge_original):
                delete = page.get_by_test_id(f"memory-delete-{title}")
                if delete.count() == 1:
                    page.once("dialog", lambda dialog: dialog.accept())
                    delete.click()
            self.knowledge_created = False
        if self.notification_created:
            page.goto(f"{self.origin}/notifications", wait_until="domcontentloaded")
            for name in (self.notification_name, self.notification_original):
                delete = page.get_by_test_id(f"notification-delete-{name}")
                if delete.count() == 1:
                    page.once("dialog", lambda dialog: dialog.accept())
                    delete.click()
            self.notification_created = False
