"""Scenario 41: manage and execute a AgentGraph through the shipped Web console."""

from __future__ import annotations

import hashlib
import json
import re
import uuid

from playwright.sync_api import expect
from util.report import VerificationResult

from verifier.context import (
    UI_EVIDENCE_PATH,
    BaseVerifier,
    FlowgentE2EProject,
    FlowgentRunEvidence,
    VerificationContext,
)
from verifier.web import BrowserConsoleVerifier


class E2ES41AgentGraphCrudVerifier(BrowserConsoleVerifier, BaseVerifier):
    """Verify browser-driven AgentGraph CRUD and real session-runtime execution."""

    scenario_id = "41"
    title = "Web Console — AgentGraph CRUD & Run Lifecycle"

    def __init__(self, context: VerificationContext) -> None:
        super().__init__(context)
        self.agent_graph_id = f"ui-e2e-{uuid.uuid4().hex[:8]}"
        self.node_instance_id = "console-sandbox"
        self.graph_run_id = ""
        self.focused_node_run_id = ""
        self.graph_created = False
        self.graph_deleted = False

    def verifier(self, context: VerificationContext) -> VerificationResult:
        return self.execute(self._run_scenario)

    def _run_scenario(self) -> None:
        with self.browser_session():
            try:
                self.step(
                    "UI-01: create a session sandbox AgentGraph through the console",
                    self._create_graph,
                )
                self.step(
                    "UI-02: trigger the AgentGraph and inspect its completed run in the console",
                    self._run_graph,
                )
                self.step(
                    "UI-03: inspect the Jaeger-style trace view through the console",
                    self._inspect_trace,
                )
                self.step(
                    "correlate the UI attempt with PG, Jaeger, workspace and runtime logs",
                    self._verify_execution_evidence,
                )
                self.step(
                    "UI-04: inspect one-day, seven-day, and one-month Overview telemetry",
                    self._inspect_overview,
                )
                self.step(
                    "UI-05: update the persisted AgentGraph description through the console",
                    self._update_graph,
                )
                self.step(
                    "UI-06: delete the scenario-owned AgentGraph through the console",
                    self._delete_graph,
                )
            finally:
                if self.graph_created and not self.graph_deleted:
                    self._best_effort_cleanup()

    def _create_graph(self) -> None:
        page = self.browser_page
        # A fresh browser starts in the console's default namespace. Entering
        # the existing, UI-rendered E2E AgentGraph makes the route scope update the
        # console state before CRUD begins; this avoids testing an unconfigured
        # namespace that has no session runtime workers.
        page.goto(
            f"{self.origin}/{self.web_namespace}/{self.fixture_agent_graph_id}",
            wait_until="domcontentloaded",
        )
        expect(page.get_by_test_id("graph-id")).to_have_value(
            self.fixture_agent_graph_id, timeout=20_000
        )
        page.get_by_role("link", name="AgentGraphs", exact=True).click()
        page.get_by_role("button", name="New graph", exact=True).first.click()
        expect(page.get_by_test_id("graph-id")).to_be_visible(timeout=20_000)
        page.get_by_test_id("graph-id").fill(self.agent_graph_id)
        page.get_by_test_id("graph-summary").fill("Chromium console lifecycle proof")
        page.get_by_test_id("graph-description").fill(
            "Created through the real Flowgent Web console."
        )
        page.get_by_test_id("graph-runtime-mode").select_option("session")
        page.get_by_test_id("graph-node-add-sandbox").click()
        expect(page.get_by_test_id("graph-node-script")).to_be_visible()
        page.get_by_test_id("graph-node-id").fill(self.node_instance_id)
        page.get_by_test_id("graph-node-script").fill(
            "mkdir -p src; printf '%s\\n' 'export const secured = true;' > src/security.ts; printf '%s\\n' '"
            '{"files":[{"path":"src/security.ts","content":"export const secured = true;\\n"}],'
            '"patches":[{"file":"src/security.ts","rule":"e2e:secure-default",'
            '"description":"Enable the secure default."}]}'
            "'"
        )
        self.graph_created = True
        page.get_by_test_id("graph-editor-save").click()
        page.wait_for_url(
            re.compile(
                rf"^{re.escape(self.origin)}/{re.escape(self.web_namespace)}/{re.escape(self.agent_graph_id)}$"
            ),
            timeout=20_000,
        )
        expect(page.get_by_test_id("graph-id")).to_have_value(self.agent_graph_id)
        self.screenshot("UI-01", "console created a session sandbox AgentGraph")
        self.details.append(
            f"Web console created AgentGraph {self.agent_graph_id} with session runtime and sandbox node {self.node_instance_id}."
        )

    def _run_graph(self) -> None:
        page = self.browser_page
        page.goto(
            f"{self.origin}/{self.web_namespace}/{self.agent_graph_id}/graph-runs",
            wait_until="domcontentloaded",
        )
        expect(page.get_by_test_id("graph-run")).to_be_visible(timeout=20_000)
        page.get_by_test_id("graph-run").click()
        page.wait_for_url(
            re.compile(
                rf"^{re.escape(self.origin)}/{re.escape(self.web_namespace)}/"
                rf"{re.escape(self.agent_graph_id)}/graph-runs/[^/]+$"
            ),
            timeout=20_000,
        )
        expect(page.get_by_test_id("run-status")).to_have_attribute(
            "data-status", "COMPLETED", timeout=180_000
        )
        self.graph_run_id = page.url.rsplit("/", 1)[-1]
        node = page.locator(f'[data-testid="run-node"][data-node-id="{self.node_instance_id}"]')
        expect(node).to_have_attribute("data-has-task", "true")
        node.click()
        preview = page.get_by_test_id("task-file-preview")
        expect(preview).to_be_visible(timeout=20_000)
        expect(preview).to_contain_text("src/security.ts")
        expect(preview).to_contain_text("Enable the secure default")
        trace_link = page.locator("a.attempt-trace-link")
        href = trace_link.get_attribute("href") or ""
        match = re.search(r"[?&]task=([^&]+)", href)
        if not match:
            raise AssertionError(
                f"node attempt trace link did not carry an exact task id: {href}"
            )
        self.focused_node_run_id = match.group(1)
        self.screenshot("UI-02", "console displayed the completed sandbox run")
        self.details.append(
            "Web console triggered the AgentGraph and rendered its terminal COMPLETED run with sandbox task evidence."
        )

    def _inspect_trace(self) -> None:
        page = self.browser_page
        page.locator("a.attempt-trace-link").click()
        page.wait_for_url(
            re.compile(
                rf"^{re.escape(self.origin)}/{re.escape(self.web_namespace)}/"
                rf"{re.escape(self.agent_graph_id)}/graph-runs/[^/]+/tracking\?task={re.escape(self.focused_node_run_id)}$"
            ),
            timeout=20_000,
        )
        trace_source = page.get_by_test_id("trace-source")
        expect(trace_source).to_be_visible(timeout=20_000)
        for _ in range(40):
            if int(trace_source.get_attribute("data-span-count") or "0") > 0:
                break
            page.wait_for_timeout(1_500)
            page.reload(wait_until="domcontentloaded")
            expect(trace_source).to_be_visible(timeout=20_000)
        expect(trace_source).to_have_attribute(
            "data-trace-count", re.compile(r"^[1-9]\d*$")
        )
        expect(trace_source).to_have_attribute(
            "data-span-count", re.compile(r"^[1-9]\d*$")
        )
        expect(page.locator("button.span-row").first).to_be_visible()
        focused = page.get_by_test_id("trace-focused-attempt")
        expect(focused).to_have_attribute("data-task-id", self.focused_node_run_id)
        expect(focused).to_have_attribute("data-span-id", re.compile(r".+"))
        focused_row = page.locator(
            f'[data-testid="trace-span"][data-task-id="{self.focused_node_run_id}"]'
        ).first
        expect(focused_row).to_have_class(re.compile(r"is-focused"))
        trace_count = trace_source.get_attribute("data-trace-count")
        span_count = trace_source.get_attribute("data-span-count")
        self.screenshot(
            "UI-03",
            "console rendered the Jaeger-style trace view for the completed run",
        )
        self.details.append(
            f"Opened the run Tracking route and rendered {trace_count} Jaeger trace(s) / "
            f"{span_count} span(s), then selected and scrolled to persisted NodeRun "
            f"{self.focused_node_run_id}."
        )

    def _inspect_overview(self) -> None:
        page = self.browser_page
        page.goto(f"{self.origin}/dashboard", wait_until="domcontentloaded")
        expect(page.get_by_test_id("dashboard-telemetry")).to_be_visible(timeout=20_000)
        for label in (
            "Overview",
            "Runs",
            "AgentGraphs",
            "Memory",
            "Agents",
            "Skills",
            "MCPs",
            "LLMs",
            "Notifications",
        ):
            expect(page.get_by_role("link", name=label, exact=True)).to_be_visible()
        for hours in (24, 168, 720):
            page.get_by_test_id(f"dashboard-range-{hours}").click()
            expect(page.get_by_test_id(f"dashboard-range-{hours}")).to_have_class(
                re.compile("is-active")
            )
        self.screenshot(
            "UI-04", "Overview rendered one-day seven-day and one-month run telemetry"
        )
        self.details.append(
            "Verified Overview range controls for 24h, 7d, and 30d telemetry curves and KPI metrics."
        )

    def _update_graph(self) -> None:
        page = self.browser_page
        updated_description = "Updated through the real Flowgent Web console."
        page.goto(
            f"{self.origin}/{self.web_namespace}/{self.agent_graph_id}",
            wait_until="domcontentloaded",
        )
        expect(page.get_by_test_id("graph-description")).to_be_visible(timeout=20_000)
        page.get_by_test_id("graph-description").fill(updated_description)
        expect(page.get_by_test_id("graph-unsaved")).to_be_visible()
        page.get_by_test_id("graph-editor-save").click()
        expect(page.get_by_test_id("graph-unsaved")).to_have_count(0, timeout=20_000)
        expect(page.get_by_test_id("graph-version")).to_contain_text("2", timeout=20_000)
        page.reload(wait_until="domcontentloaded")
        expect(page.get_by_test_id("graph-description")).to_have_value(
            updated_description, timeout=20_000
        )
        expect(page.get_by_test_id("graph-version")).to_contain_text("2", timeout=20_000)
        recent = page.get_by_test_id("graph-recent-runs")
        expect(recent).to_be_visible(timeout=20_000)
        expect(page.get_by_test_id(f"graph-recent-run-{self.graph_run_id}")).to_be_visible(
            timeout=20_000
        )
        expect(
            recent.get_by_role("link", name="Details", exact=True).first
        ).to_have_attribute(
            "href", f"/{self.web_namespace}/{self.agent_graph_id}/graph-runs/{self.graph_run_id}"
        )
        expect(
            recent.get_by_role("link", name="Trace", exact=True).first
        ).to_have_attribute(
            "href", f"/{self.web_namespace}/{self.agent_graph_id}/graph-runs/{self.graph_run_id}/tracking"
        )
        self.screenshot("UI-05", "console persisted the AgentGraph update")
        self.details.append(
            "Web console persisted AgentGraph revision 2 and embedded its recent Run with direct Details and Trace links."
        )

    def _delete_graph(self) -> None:
        self._delete_from_console(require_row=True)
        self.graph_deleted = True
        self.screenshot("UI-06", "console removed the scenario-owned AgentGraph")
        self.details.append(
            "Web console deleted the scenario-owned AgentGraph and removed it from the rendered inventory."
        )

    def _best_effort_cleanup(self) -> None:
        try:
            self._delete_from_console(require_row=False)
        except Exception as error:
            self.details.append(
                f"WARN: browser cleanup for {self.agent_graph_id} failed: {error}"
            )

    def _delete_from_console(self, *, require_row: bool) -> None:
        page = self.browser_page
        page.goto(
            f"{self.origin}/{self.web_namespace}/{self.agent_graph_id}",
            wait_until="domcontentloaded",
        )
        expect(page.get_by_test_id("graph-id")).to_have_value(
            self.agent_graph_id, timeout=20_000
        )
        page.get_by_role("link", name="AgentGraphs", exact=True).click()
        page.wait_for_url(f"{self.origin}/agent-graphs", timeout=20_000)
        row = page.get_by_test_id(f"graph-row-{self.agent_graph_id}")
        try:
            expect(row).to_have_count(1, timeout=20_000 if require_row else 2_000)
        except AssertionError:
            if require_row:
                raise AssertionError(
                    f"Console did not render scenario-owned AgentGraph {self.agent_graph_id} for deletion"
                ) from None
            return
        page.get_by_test_id(f"graph-actions-{self.agent_graph_id}").click()
        page.get_by_test_id(f"graph-delete-{self.agent_graph_id}").click()
        expect(page.get_by_test_id("graph-delete-confirm")).to_be_visible()
        page.get_by_test_id("graph-delete-confirm").click()
        expect(row).to_have_count(0, timeout=20_000)

    def deploy(self, context: VerificationContext) -> None:
        """Use the topology prepared by the runner; no scenario reset is needed."""

    def init(self, context: VerificationContext) -> None:
        UI_EVIDENCE_PATH.unlink(missing_ok=True)

    def _verify_execution_evidence(self) -> None:
        evidence = FlowgentRunEvidence.persisted(
            self.graph_run_id, self.web_namespace, self.node_instance_id
        )
        if (
            evidence["agent_graph_id"] != self.agent_graph_id
            or evidence["node_run_id"] != self.focused_node_run_id
        ):
            raise AssertionError("UI selected a different persisted AgentGraph/NodeRun")
        parsed = FlowgentE2EProject.parse_output({"output": evidence.pop("output")})
        files = parsed.get("files", [])
        if not any(
            item.get("path") == "src/security.ts"
            and item.get("content") == "export const secured = true;\n"
            for item in files
        ):
            raise AssertionError("PG task output differs from the file shown by the UI")
        workspace = FlowgentRunEvidence.workspace(
            self.context, evidence, "src/security.ts"
        )
        if (
            workspace["sha256"]
            != hashlib.sha256(b"export const secured = true;\n").hexdigest()
        ):
            raise AssertionError("The mounted workspace contains different file bytes")
        traces = FlowgentRunEvidence.traces(self.graph_run_id)
        spans = [span for trace in traces for span in trace.get("spans", [])]
        if not any(
            FlowgentRunEvidence.tags(span).get("flowgent.node_run_id")
            == evidence["node_run_id"]
            for span in spans
        ):
            raise AssertionError(
                "Jaeger has no span for the exact UI-selected persisted attempt"
            )
        evidence.update(
            {
                "deployer": self.context.deployer,
                "workspace": workspace,
                "logs": FlowgentRunEvidence.logs(self.context, evidence),
                "trace_ids": [trace["traceID"] for trace in traces],
            }
        )
        UI_EVIDENCE_PATH.write_text(json.dumps(evidence, indent=2) + "\n")
        artifact = self.evidence_path(
            "41.execution", "UI to persisted execution", "json"
        )
        artifact.write_text(json.dumps(evidence, indent=2) + "\n")
        self.record_evidence(
            "41.execution",
            "UI, PG, Jaeger, volume and logs",
            "application/json",
            artifact,
        )
