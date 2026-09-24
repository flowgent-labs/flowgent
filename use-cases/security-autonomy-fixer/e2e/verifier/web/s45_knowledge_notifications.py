"""Scenario 45: verify published Knowledge safety and notification CRUD."""

from __future__ import annotations

import uuid

from playwright.sync_api import expect

from common.model import RunContext, VerificationResult
from verifier import BaseVerifier
from verifier.web.browser import BrowserConsoleVerifier


class KnowledgeNotificationsVerifier(BrowserConsoleVerifier, BaseVerifier):
    scenario_id = "45"
    title = "Web Console — Published Knowledge and Notification CRUD"

    def __init__(self, context: RunContext) -> None:
        super().__init__(context)
        suffix = uuid.uuid4().hex[:8]
        self.notification_original = f"ui-webhook-{suffix}"
        self.notification_name = f"{self.notification_original}-v2"
        self.notification_created = False

    def run(self) -> VerificationResult:
        return self.execute(self._run_scenario)

    def _run_scenario(self) -> None:
        with self.browser_session():
            try:
                self.step("UI-01: enforce published-only Knowledge management", self._knowledge)
                self.step("UI-02: create and revise a Webhook notification channel", self._notification)
                self.step("UI-03: inspect namespace runtime configuration UI", self._runtime_config)
                self.step("UI-04: delete the scenario-owned notification record", self._delete)
            finally:
                self._cleanup()

    def _knowledge(self) -> None:
        page = self.browser_page
        page.goto(f"{self.origin}/memory/namespace", wait_until="domcontentloaded")
        expect(page.get_by_test_id("memory-published-only")).to_be_visible(timeout=20_000)
        expect(page.get_by_test_id("memory-create")).to_have_count(0)
        status = page.evaluate(
            """async (namespace) => {
              const response = await fetch(`/api/v1/${namespace}/knowledge`, {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({title: 'forbidden-direct-write', content: 'must not publish'})
              });
              return response.status;
            }""",
            self.web_namespace,
        )
        if status != 405:
            raise AssertionError(f"Direct Knowledge publication returned HTTP {status}, want 405")
        self.screenshot("UI-01", "browser exposed only approved published Knowledge")
        self.details.append(
            "Memory UI was read-only and the authenticated direct Knowledge POST was rejected with HTTP 405; publication requires the candidate approval API."
        )

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
        page.goto(f"{self.origin}/notifications", wait_until="domcontentloaded")
        page.once("dialog", lambda dialog: dialog.accept())
        page.get_by_test_id(f"notification-delete-{self.notification_name}").click()
        expect(page.get_by_test_id(f"notification-card-{self.notification_name}")).to_have_count(0, timeout=20_000)
        self.notification_created = False
        self.screenshot("UI-04", "browser deleted the scenario-owned notification record")
        self.details.append("Deleted the scenario-owned notification channel.")

    def _cleanup(self) -> None:
        page = self.browser_page
        if self.notification_created:
            page.goto(f"{self.origin}/notifications", wait_until="domcontentloaded")
            for name in (self.notification_name, self.notification_original):
                delete = page.get_by_test_id(f"notification-delete-{name}")
                if delete.count() == 1:
                    page.once("dialog", lambda dialog: dialog.accept())
                    delete.click()
            self.notification_created = False
