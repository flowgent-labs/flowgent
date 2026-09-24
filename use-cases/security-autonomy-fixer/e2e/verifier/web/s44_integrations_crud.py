"""Scenario 44: verify browser CRUD for MCP and LLM integration records."""

from __future__ import annotations

import uuid

from playwright.sync_api import expect

from common.model import RunContext, VerificationResult
from verifier import BaseVerifier
from verifier.web.browser import BrowserConsoleVerifier


class IntegrationsCrudVerifier(BrowserConsoleVerifier, BaseVerifier):
    scenario_id = "44"
    title = "Web Console — MCP and LLM CRUD"

    def __init__(self, context: RunContext) -> None:
        super().__init__(context)
        suffix = uuid.uuid4().hex[:8]
        self.mcp_original = f"ui-mcp-{suffix}"
        self.mcp_name = f"{self.mcp_original}-v2"
        self.llm_name = f"ui-llm-{suffix}"
        self.mcp_created = False
        self.llm_created = False

    def run(self) -> VerificationResult:
        return self.execute(self._run_scenario)

    def _run_scenario(self) -> None:
        with self.browser_session():
            try:
                self.step("UI-01: create MCP with safe environment references", self._create_mcp)
                self.step("UI-02: update and rename MCP", self._update_mcp)
                self.step("UI-03: create LLM with OpenAI protocol and safe environment references", self._create_llm)
                self.step("UI-04: update LLM protocol metadata", self._update_llm)
                self.step("UI-05: delete MCP and LLM", self._delete)
            finally:
                self._cleanup()

    def _create_mcp(self) -> None:
        page = self.browser_page
        page.goto(f"{self.origin}/mcps", wait_until="domcontentloaded")
        page.get_by_test_id("mcp-create").click()
        page.get_by_test_id("mcp-name").fill(self.mcp_original)
        page.get_by_test_id("mcp-url").fill("https://mcp.example.test/mcp")
        page.get_by_test_id("mcp-header-refs").fill('{"Authorization":"${DEEPSEEK_API_KEY}"}')
        page.get_by_test_id("mcp-env-refs").fill('{"MCP_TENANT":"${DEEPSEEK_API_KEY}"}')
        page.get_by_test_id("mcp-save").click()
        expect(page.get_by_test_id(f"mcp-card-{self.mcp_original}")).to_have_count(1, timeout=20_000)
        self.mcp_created = True
        self.screenshot("UI-01", "browser created MCP using masked environment references")
        self.details.append("Created MCP with Streamable HTTP URL and ${ENV} references only; no secret value entered.")

    def _update_mcp(self) -> None:
        page = self.browser_page
        page.get_by_test_id(f"mcp-card-{self.mcp_original}").locator("button").first.click()
        page.get_by_test_id("mcp-name").fill(self.mcp_name)
        page.get_by_test_id("mcp-url").fill("https://mcp.example.test/v2/mcp")
        page.get_by_test_id("mcp-save").click()
        expect(page.get_by_test_id(f"mcp-card-{self.mcp_name}")).to_have_count(1, timeout=20_000)
        expect(page.get_by_test_id(f"mcp-card-{self.mcp_original}")).to_have_count(0)
        self.screenshot("UI-02", "browser persisted MCP rename and endpoint update")
        self.details.append("Updated MCP name and RPC URL through the browser.")

    def _create_llm(self) -> None:
        page = self.browser_page
        page.goto(f"{self.origin}/llms", wait_until="domcontentloaded")
        page.get_by_test_id("llm-create").click()
        page.get_by_test_id("llm-name").fill(self.llm_name)
        page.get_by_test_id("llm-type").select_option("openai")
        page.get_by_test_id("llm-endpoint").fill("https://llm.example.test/v1")
        page.get_by_test_id("llm-default-model").fill("gpt-e2e")
        page.get_by_test_id("llm-api-key-env").fill("DEEPSEEK_API_KEY")
        page.get_by_test_id("llm-env-refs").fill('{"LLM_TENANT":"${DEEPSEEK_API_KEY}"}')
        with page.expect_response(
            lambda response: response.request.method == "POST"
            and response.url.endswith(f"/api/v1/{self.web_namespace}/llm/providers"),
            timeout=20_000,
        ) as request:
            page.get_by_test_id("llm-save").click()
        response = request.value
        if not response.ok:
            raise AssertionError(
                f"LLM create returned HTTP {response.status}: {response.text()[:400]}"
            )
        self.llm_created = True
        expect(page.get_by_test_id(f"llm-card-{self.llm_name}")).to_have_count(1, timeout=20_000)
        self.screenshot("UI-03", "browser created typed LLM provider with environment references")
        self.details.append("Created LLM using strict openai type, Base URI, model, and masked environment references.")

    def _update_llm(self) -> None:
        page = self.browser_page
        page.get_by_test_id(f"llm-card-{self.llm_name}").locator("button").first.click()
        page.get_by_test_id("llm-type").select_option("anthropic")
        page.get_by_test_id("llm-endpoint").fill("https://llm.example.test/anthropic")
        page.get_by_test_id("llm-save").click()
        expect(page.get_by_test_id(f"llm-card-{self.llm_name}")).to_contain_text("anthropic", timeout=20_000)
        self.screenshot("UI-04", "browser updated LLM protocol metadata")
        self.details.append("Updated LLM adapter type from openai to anthropic and persisted its Base URI.")

    def _delete(self) -> None:
        page = self.browser_page
        page.goto(f"{self.origin}/mcps", wait_until="domcontentloaded")
        page.once("dialog", lambda dialog: dialog.accept())
        page.get_by_test_id(f"mcp-delete-{self.mcp_name}").click()
        expect(page.get_by_test_id(f"mcp-card-{self.mcp_name}")).to_have_count(0, timeout=20_000)
        self.mcp_created = False
        page.goto(f"{self.origin}/llms", wait_until="domcontentloaded")
        page.once("dialog", lambda dialog: dialog.accept())
        page.get_by_test_id(f"llm-delete-{self.llm_name}").click()
        expect(page.get_by_test_id(f"llm-card-{self.llm_name}")).to_have_count(0, timeout=20_000)
        self.llm_created = False
        self.screenshot("UI-05", "browser deleted scenario-owned MCP and LLM")
        self.details.append("Deleted the MCP and LLM definitions through rendered console controls.")

    def _cleanup(self) -> None:
        page = self.browser_page
        if self.mcp_created:
            page.goto(f"{self.origin}/mcps", wait_until="domcontentloaded")
            for name in (self.mcp_name, self.mcp_original):
                delete = page.get_by_test_id(f"mcp-delete-{name}")
                if delete.count() == 1:
                    page.once("dialog", lambda dialog: dialog.accept())
                    delete.click()
                    expect(page.get_by_test_id(f"mcp-card-{name}")).to_have_count(0, timeout=10_000)
            self.mcp_created = False
        if self.llm_created:
            page.goto(f"{self.origin}/llms", wait_until="domcontentloaded")
            delete = page.get_by_test_id(f"llm-delete-{self.llm_name}")
            if delete.count() == 1:
                page.once("dialog", lambda dialog: dialog.accept())
                delete.click()
                expect(page.get_by_test_id(f"llm-card-{self.llm_name}")).to_have_count(0, timeout=10_000)
            self.llm_created = False
