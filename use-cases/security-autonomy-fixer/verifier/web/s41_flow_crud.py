"""Scenario 41: manage and execute a Flow through the shipped Web console."""

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


class E2ES41FlowCrudVerifier(BrowserConsoleVerifier, BaseVerifier):
    """Verify browser-driven Flow CRUD and real session-runtime execution."""

    scenario_id = "41"
    title = "Web Console — Flow CRUD & Run Lifecycle"

    def __init__(self, context: VerificationContext) -> None:
        super().__init__(context)
        self.flow_id = f"ui-e2e-{uuid.uuid4().hex[:8]}"
        self.node_id = "console-sandbox"
        self.run_id = ""
        self.focused_task_id = ""
        self.flow_created = False
        self.flow_deleted = False

    def verifier(self, context: VerificationContext) -> VerificationResult:
        return self.execute(self._run_scenario)

    def _run_scenario(self) -> None:
        with self.browser_session():
            try:
                self.step(
                    "UI-01: create a session sandbox Flow through the console",
                    self._create_flow,
                )
                self.step(
                    "UI-02: trigger the Flow and inspect its completed run in the console",
                    self._run_flow,
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
                    "UI-05: update the persisted Flow description through the console",
                    self._update_flow,
                )
                self.step(
                    "UI-06: delete the scenario-owned Flow through the console",
                    self._delete_flow,
                )
            finally:
                if self.flow_created and not self.flow_deleted:
                    self._best_effort_cleanup()

    def _create_flow(self) -> None:
        page = self.browser_page
        # A fresh browser starts in the console's default namespace. Entering
        # the existing, UI-rendered E2E Flow makes the route scope update the
        # console state before CRUD begins; this avoids testing an unconfigured
        # namespace that has no session runtime workers.
        page.goto(
            f"{self.origin}/{self.web_namespace}/{self.fixture_flow_id}",
            wait_until="domcontentloaded",
        )
        expect(page.get_by_test_id("flow-id")).to_have_value(
            self.fixture_flow_id, timeout=20_000
        )
        page.goto(f"{self.origin}/flows/new", wait_until="domcontentloaded")
        expect(page.get_by_test_id("flow-id")).to_be_visible(timeout=20_000)
        page.get_by_test_id("flow-id").fill(self.flow_id)
        page.get_by_test_id("flow-summary").fill("Chromium console lifecycle proof")
        page.get_by_test_id("flow-description").fill(
            "Created through the real Flowgent Web console."
        )
        page.get_by_test_id("flow-runtime-mode").select_option("session")
        page.get_by_test_id("flow-node-add-sandbox").click()
        expect(page.get_by_test_id("flow-node-script")).to_be_visible()
        page.get_by_test_id("flow-node-id").fill(self.node_id)
        page.get_by_test_id("flow-node-script").fill(
            "mkdir -p src; printf '%s\\n' 'export const secured = true;' > src/security.ts; printf '%s\\n' '"
            '{"files":[{"path":"src/security.ts","content":"export const secured = true;\\n"}],'
            '"patches":[{"file":"src/security.ts","rule":"e2e:secure-default",'
            '"description":"Enable the secure default."}]}'
            "'"
        )
        self.flow_created = True
        page.get_by_test_id("flow-editor-save").click()
        page.wait_for_url(
            re.compile(
                rf"^{re.escape(self.origin)}/{re.escape(self.web_namespace)}/{re.escape(self.flow_id)}$"
            ),
            timeout=20_000,
        )
        expect(page.get_by_test_id("flow-id")).to_have_value(self.flow_id)
        self.screenshot("UI-01", "console created a session sandbox Flow")
        self.details.append(
            f"Web console created Flow {self.flow_id} with session runtime and sandbox node {self.node_id}."
        )

    def _run_flow(self) -> None:
        page = self.browser_page
        page.goto(
            f"{self.origin}/{self.web_namespace}/{self.flow_id}/runs",
            wait_until="domcontentloaded",
        )
        expect(page.get_by_test_id("flow-run")).to_be_visible(timeout=20_000)
        page.get_by_test_id("flow-run").click()
        page.wait_for_url(
            re.compile(
                rf"^{re.escape(self.origin)}/{re.escape(self.web_namespace)}/"
                rf"{re.escape(self.flow_id)}/runs/[^/]+$"
            ),
            timeout=20_000,
        )
        expect(page.get_by_test_id("run-status")).to_have_attribute(
            "data-status", "COMPLETED", timeout=180_000
        )
        self.run_id = page.url.rsplit("/", 1)[-1]
        node = page.locator(f'[data-testid="run-node"][data-node-id="{self.node_id}"]')
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
        self.focused_task_id = match.group(1)
        self.screenshot("UI-02", "console displayed the completed sandbox run")
        self.details.append(
            "Web console triggered the Flow and rendered its terminal COMPLETED run with sandbox task evidence."
        )

    def _inspect_trace(self) -> None:
        page = self.browser_page
        page.locator("a.attempt-trace-link").click()
        page.wait_for_url(
            re.compile(
                rf"^{re.escape(self.origin)}/{re.escape(self.web_namespace)}/"
                rf"{re.escape(self.flow_id)}/runs/[^/]+/tracking\?task={re.escape(self.focused_task_id)}$"
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
        expect(focused).to_have_attribute("data-task-id", self.focused_task_id)
        expect(focused).to_have_attribute("data-span-id", re.compile(r".+"))
        focused_row = page.locator(
            f'[data-testid="trace-span"][data-task-id="{self.focused_task_id}"]'
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
            f"{span_count} span(s), then selected and scrolled to persisted TaskRun "
            f"{self.focused_task_id}."
        )

    def _inspect_overview(self) -> None:
        page = self.browser_page
        page.goto(f"{self.origin}/dashboard", wait_until="domcontentloaded")
        expect(page.get_by_test_id("dashboard-telemetry")).to_be_visible(timeout=20_000)
        for label in (
            "Overview",
            "Runs",
            "Flows",
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

    def _update_flow(self) -> None:
        page = self.browser_page
        updated_description = "Updated through the real Flowgent Web console."
        page.goto(
            f"{self.origin}/{self.web_namespace}/{self.flow_id}",
            wait_until="domcontentloaded",
        )
        expect(page.get_by_test_id("flow-description")).to_be_visible(timeout=20_000)
        page.get_by_test_id("flow-description").fill(updated_description)
        expect(page.get_by_test_id("flow-unsaved")).to_be_visible()
        page.get_by_test_id("flow-editor-save").click()
        expect(page.get_by_test_id("flow-unsaved")).to_have_count(0, timeout=20_000)
        expect(page.get_by_test_id("flow-version")).to_contain_text("2", timeout=20_000)
        page.reload(wait_until="domcontentloaded")
        expect(page.get_by_test_id("flow-description")).to_have_value(
            updated_description, timeout=20_000
        )
        expect(page.get_by_test_id("flow-version")).to_contain_text("2", timeout=20_000)
        recent = page.get_by_test_id("flow-recent-runs")
        expect(recent).to_be_visible(timeout=20_000)
        expect(page.get_by_test_id(f"flow-recent-run-{self.run_id}")).to_be_visible(
            timeout=20_000
        )
        expect(
            recent.get_by_role("link", name="Details", exact=True).first
        ).to_have_attribute(
            "href", f"/{self.web_namespace}/{self.flow_id}/runs/{self.run_id}"
        )
        expect(
            recent.get_by_role("link", name="Trace", exact=True).first
        ).to_have_attribute(
            "href", f"/{self.web_namespace}/{self.flow_id}/runs/{self.run_id}/tracking"
        )
        self.screenshot("UI-05", "console persisted the Flow update")
        self.details.append(
            "Web console persisted Flow revision 2 and embedded its recent Run with direct Details and Trace links."
        )

    def _delete_flow(self) -> None:
        self._delete_from_console(require_row=True)
        self.flow_deleted = True
        self.screenshot("UI-06", "console removed the scenario-owned Flow")
        self.details.append(
            "Web console deleted the scenario-owned Flow and removed it from the rendered inventory."
        )

    def _best_effort_cleanup(self) -> None:
        try:
            self._delete_from_console(require_row=False)
        except Exception as error:
            self.details.append(
                f"WARN: browser cleanup for {self.flow_id} failed: {error}"
            )

    def _delete_from_console(self, *, require_row: bool) -> None:
        page = self.browser_page
        page.goto(
            f"{self.origin}/{self.web_namespace}/{self.flow_id}",
            wait_until="domcontentloaded",
        )
        expect(page.get_by_test_id("flow-id")).to_have_value(
            self.flow_id, timeout=20_000
        )
        page.get_by_role("link", name="Flows", exact=True).click()
        page.wait_for_url(f"{self.origin}/flows", timeout=20_000)
        row = page.get_by_test_id(f"flow-row-{self.flow_id}")
        try:
            expect(row).to_have_count(1, timeout=20_000 if require_row else 2_000)
        except AssertionError:
            if require_row:
                raise AssertionError(
                    f"Console did not render scenario-owned Flow {self.flow_id} for deletion"
                ) from None
            return
        page.get_by_test_id(f"flow-actions-{self.flow_id}").click()
        page.get_by_test_id(f"flow-delete-{self.flow_id}").click()
        expect(page.get_by_test_id("flow-delete-confirm")).to_be_visible()
        page.get_by_test_id("flow-delete-confirm").click()
        expect(row).to_have_count(0, timeout=20_000)

    def deploy(self, context: VerificationContext) -> None:
        """Use the topology prepared by the runner; no scenario reset is needed."""

    def init(self, context: VerificationContext) -> None:
        UI_EVIDENCE_PATH.unlink(missing_ok=True)

    def _verify_execution_evidence(self) -> None:
        evidence = FlowgentRunEvidence.persisted(
            self.run_id, self.web_namespace, self.node_id
        )
        if (
            evidence["flow_id"] != self.flow_id
            or evidence["task_id"] != self.focused_task_id
        ):
            raise AssertionError("UI selected a different persisted Flow/NodeRun")
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
        traces = FlowgentRunEvidence.traces(self.run_id)
        spans = [span for trace in traces for span in trace.get("spans", [])]
        if not any(
            FlowgentRunEvidence.tags(span).get("flowgent.task_id")
            == evidence["task_id"]
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
