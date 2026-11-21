import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import test from "node:test";

const script = fileURLToPath(new URL("./upsert-pr-status-comment.mjs", import.meta.url));
const render = (summaries = {}) => execFileSync(process.execPath, [script], {
  encoding: "utf8",
  env: {
    ...process.env,
    GITHUB_REPOSITORY: "flowgent-labs/flowgent",
    FLOWGENT_PR_NUMBER: "2",
    GH_TOKEN: "fixture-token",
    FLOWGENT_COMMENT_KIND: "ci",
    FLOWGENT_COMMENT_PHASE: "final",
    FLOWGENT_COMMENT_DRY_RUN: "true",
    FLOWGENT_RUN_URL: "https://github.com/flowgent-labs/flowgent/actions/runs/123",
    FLOWGENT_BUILD_RESULT: "success",
    FLOWGENT_E2E_KUBERNETES_RESULT: "failure",
    FLOWGENT_E2E_DOCKER_RESULT: "success",
    FLOWGENT_E2E_KUBERNETES_SUMMARY_BASE64: "",
    FLOWGENT_E2E_DOCKER_SUMMARY_BASE64: "",
    ...Object.fromEntries(Object.entries(summaries).map(([backend, markdown]) => [
      `FLOWGENT_E2E_${backend}_SUMMARY_BASE64`,
      Buffer.from(markdown).toString("base64"),
    ])),
  },
});

test("renders report headings and tables as Markdown, preserving report code fences", () => {
  const report = "# E2E Verification Summary\n\n| ID | Result |\n| --- | --- |\n| 01 | PASS |\n\n```log\nexample output\n```";
  const body = render({ KUBERNETES: report, DOCKER: report });
  assert.ok(body.includes("### Kubernetes E2E summary\n\n" + report));
  assert.ok(body.includes("### Docker Compose E2E summary\n\n" + report));
  assert.ok(!body.includes("```text\n# E2E Verification Summary"));
  assert.ok(body.endsWith("and E2E evidence.\n"));
});

test("keeps combined reports within the comment budget and closes truncated fences", () => {
  const report = "# Long report\n\n```log\n" + "detail\n".repeat(10_000) + "```";
  const body = render({ KUBERNETES: report, DOCKER: report });
  assert.ok(body.length < 60_000);
  assert.equal((body.match(/Summary truncated/g) || []).length, 2);
  assert.equal((body.match(/^```/gm) || []).length, 4);
  assert.ok(body.includes("**CI failed.**"));
});

test("handles missing reports while preserving the final status", () => {
  const body = render();
  assert.equal((body.match(/summary was not generated/g) || []).length, 2);
  assert.ok(body.includes("Kubernetes E2E: ❌ failure"));
  assert.ok(body.includes("Docker Compose E2E: ✅ success"));
});
