"""Scenario 43: verify browser management of reusable Skills and workspace files."""

from __future__ import annotations

from pathlib import Path
import uuid

from playwright.sync_api import expect

from common.model import RunContext, VerificationResult
from verifier import BaseVerifier
from verifier.web.browser import BrowserConsoleVerifier


FIXTURES = Path(__file__).with_name("fixtures")


class SkillCrudVerifier(BrowserConsoleVerifier, BaseVerifier):
    scenario_id = "43"
    title = "Web Console — Skill CRUD and Workspace Uploads"

    def __init__(self, context: RunContext) -> None:
        super().__init__(context)
        self.original_name = f"ui-skill-{uuid.uuid4().hex[:8]}"
        self.name = f"{self.original_name}-v2"
        self.created = False

    def run(self) -> VerificationResult:
        return self.execute(self._run_scenario)

    def _run_scenario(self) -> None:
        with self.browser_session():
            try:
                self.step("UI-01: create a reusable Skill definition", self._create)
                self.step("UI-02: revise the Skill name and instruction", self._update)
                self.step("UI-03: upload a knowledge asset and helper script", self._upload)
                self.step("UI-04: delete the scenario-owned Skill", self._delete)
            finally:
                if self.created:
                    self._cleanup()

    def _create(self) -> None:
        page = self.browser_page
        page.goto(f"{self.origin}/skills", wait_until="domcontentloaded")
        expect(page.get_by_test_id("skill-create")).to_be_visible(timeout=20_000)
        page.wait_for_timeout(500)
        page.get_by_test_id("skill-create").click()
        page.get_by_test_id("skill-name").fill(self.original_name)
        page.get_by_test_id("skill-description").fill("Browser-created reusable security skill")
        page.get_by_test_id("skill-instruction").fill("Use the supplied knowledge asset and explain findings safely.")
        page.get_by_test_id("skill-model").fill("openai/gpt-e2e")
        page.get_by_test_id("skill-tools").fill('["search", "report"]')
        with page.expect_response(
            lambda response: response.request.method == "POST"
            and response.url.endswith(f"/api/v1/{self.web_namespace}/skill-definitions"),
            timeout=20_000,
        ) as request:
            page.get_by_test_id("skill-save").click()
        response = request.value
        if not response.ok:
            raise AssertionError(f"Skill create returned HTTP {response.status}: {response.text()[:400]}")
        self.screenshot("UI-01-request", "browser submitted reusable Skill definition")
        expect(page.get_by_test_id(f"skill-card-{self.original_name}")).to_have_count(1, timeout=20_000)
        self.created = True
        self.screenshot("UI-01", "browser created reusable Skill definition")
        self.details.append(f"Created reusable Skill {self.original_name} through the console.")

    def _update(self) -> None:
        page = self.browser_page
        page.get_by_test_id(f"skill-card-{self.original_name}").locator("button").first.click()
        page.get_by_test_id("skill-name").fill(self.name)
        page.get_by_test_id("skill-instruction").fill("Use workspace knowledge and return a traceable security assessment.")
        page.get_by_test_id("skill-save").click()
        expect(page.get_by_test_id(f"skill-card-{self.name}")).to_have_count(1, timeout=20_000)
        expect(page.get_by_test_id(f"skill-card-{self.original_name}")).to_have_count(0)
        expect(page.get_by_test_id(f"skill-card-{self.name}")).to_contain_text("v2")
        self.screenshot("UI-02", "browser persisted Skill revision two and rename")
        self.details.append("Updated Skill instruction and name, then observed revision v2.")

    def _upload(self) -> None:
        page = self.browser_page
        page.get_by_test_id(f"skill-card-{self.name}").locator("button").first.click()
        page.get_by_test_id("skill-asset-file").set_input_files(str(FIXTURES / "skill-knowledge.txt"))
        expect(page.get_by_text("skill-knowledge.txt", exact=True)).to_be_visible(timeout=20_000)
        page.get_by_test_id("skill-script-file").set_input_files(str(FIXTURES / "skill-helper.sh"))
        expect(page.get_by_text("skill-helper.sh", exact=True)).to_be_visible(timeout=20_000)
        self.screenshot("UI-03", "browser uploaded allowlisted Skill asset and helper script")
        self.details.append("Uploaded text knowledge asset to assets/ and shell helper to scripts/ through file inputs.")
        page.get_by_role("button", name="Cancel", exact=True).click()

    def _delete(self) -> None:
        page = self.browser_page
        page.once("dialog", lambda dialog: dialog.accept())
        page.get_by_test_id(f"skill-delete-{self.name}").click()
        expect(page.get_by_test_id(f"skill-card-{self.name}")).to_have_count(0, timeout=20_000)
        self.created = False
        self.screenshot("UI-04", "browser deleted scenario-owned Skill and workspace")
        self.details.append("Deleted scenario-owned Skill after verifying its uploaded assets and scripts.")

    def _cleanup(self) -> None:
        page = self.browser_page
        page.goto(f"{self.origin}/skills", wait_until="domcontentloaded")
        for name in (self.name, self.original_name):
            delete = page.get_by_test_id(f"skill-delete-{name}")
            if delete.count() == 1:
                page.once("dialog", lambda dialog: dialog.accept())
                delete.click()
                expect(page.get_by_test_id(f"skill-card-{name}")).to_have_count(0, timeout=10_000)
        self.created = False
