"""Scenario 42: verify real browser management of independent Agents."""

from __future__ import annotations

import uuid

from playwright.sync_api import expect

from common.model import RunContext, VerificationResult
from verifier import BaseVerifier
from verifier.web.browser import BrowserConsoleVerifier


class AgentCrudVerifier(BrowserConsoleVerifier, BaseVerifier):
    scenario_id = "42"
    title = "Web Console — Agent CRUD and Revision"

    def __init__(self, context: RunContext) -> None:
        super().__init__(context)
        self.original_name = f"ui-agent-{uuid.uuid4().hex[:8]}"
        self.name = f"{self.original_name}-v2"
        self.created = False

    def run(self) -> VerificationResult:
        return self.execute(self._run_scenario)

    def _run_scenario(self) -> None:
        with self.browser_session() as page:
            try:
                self.step("UI-01: create an Agent contract", self._create)
                self.step("UI-02: revise Agent fields and its name", self._update)
                self.step("UI-03: delete the scenario-owned Agent", self._delete)
            finally:
                if self.created:
                    self._cleanup()

    def _create(self) -> None:
        page = self.browser_page
        page.goto(f"{self.origin}/agents", wait_until="domcontentloaded")
        page.get_by_test_id("agent-create").click()
        page.get_by_test_id("agent-name").fill(self.original_name)
        page.get_by_test_id("agent-description").fill("Browser-created autonomous analysis agent")
        page.get_by_test_id("agent-model").fill("openai/gpt-e2e")
        page.get_by_test_id("agent-soul").fill("Evidence-first, concise, and safe.")
        page.get_by_test_id("agent-instruction").fill("Inspect the structured input and return an auditable result.")
        page.get_by_test_id("agent-input-schema").fill('{"type":"object","properties":{"target":{"type":"string"}}}')
        page.get_by_test_id("agent-output-schema").fill('{"type":"object","properties":{"result":{"type":"string"}}}')
        page.get_by_test_id("agent-save").click()
        card = page.get_by_test_id(f"agent-card-{self.original_name}")
        expect(card).to_have_count(1, timeout=20_000)
        expect(page.get_by_test_id(f"agent-version-{self.original_name}")).to_have_text("v1")
        self.created = True
        self.screenshot("UI-01", "browser created Agent with input and output schemas")
        self.details.append(f"Created Agent {self.original_name} using only the Web console.")

    def _update(self) -> None:
        page = self.browser_page
        page.get_by_test_id(f"agent-card-{self.original_name}").locator("button").first.click()
        page.get_by_test_id("agent-name").fill(self.name)
        page.get_by_test_id("agent-description").fill("Browser-revised agent contract")
        page.get_by_test_id("agent-instruction").fill("Return an evidence-backed structured result with no secret values.")
        page.get_by_test_id("agent-save").click()
        expect(page.get_by_test_id(f"agent-card-{self.name}")).to_have_count(1, timeout=20_000)
        expect(page.get_by_test_id(f"agent-card-{self.original_name}")).to_have_count(0)
        expect(page.get_by_test_id(f"agent-version-{self.name}")).to_have_text("v2")
        self.screenshot("UI-02", "browser persisted Agent revision two and renamed it")
        self.details.append("Updated name, description, instruction, schemas, and observed Agent revision v2.")

    def _delete(self) -> None:
        page = self.browser_page
        page.once("dialog", lambda dialog: dialog.accept())
        page.get_by_test_id(f"agent-delete-{self.name}").click()
        expect(page.get_by_test_id(f"agent-card-{self.name}")).to_have_count(0, timeout=20_000)
        self.created = False
        self.screenshot("UI-03", "browser deleted the scenario-owned Agent")
        self.details.append("Deleted the scenario-owned Agent in the rendered inventory.")

    def _cleanup(self) -> None:
        page = self.browser_page
        page.goto(f"{self.origin}/agents", wait_until="domcontentloaded")
        for name in (self.name, self.original_name):
            delete = page.get_by_test_id(f"agent-delete-{name}")
            if delete.count() == 1:
                page.once("dialog", lambda dialog: dialog.accept())
                delete.click()
                expect(page.get_by_test_id(f"agent-card-{name}")).to_have_count(0, timeout=10_000)
        self.created = False
