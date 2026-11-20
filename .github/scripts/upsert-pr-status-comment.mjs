#!/usr/bin/env node

const repository = process.env.GITHUB_REPOSITORY;
const pullRequest = process.env.FLOWGENT_PR_NUMBER;
const token = process.env.GH_TOKEN || process.env.GITHUB_TOKEN;
const kind = process.env.FLOWGENT_COMMENT_KIND;
const phase = process.env.FLOWGENT_COMMENT_PHASE;
const runUrl = process.env.FLOWGENT_RUN_URL;
const dirtyTag = process.env.FLOWGENT_DIRTY_TAG;
const dirtyChartVersion = process.env.FLOWGENT_DIRTY_CHART_VERSION;
const buildResult = process.env.FLOWGENT_BUILD_RESULT;
const e2eKubernetesResult = process.env.FLOWGENT_E2E_KUBERNETES_RESULT;
const e2eDockerResult = process.env.FLOWGENT_E2E_DOCKER_RESULT;
const releaseResult = process.env.FLOWGENT_RELEASE_RESULT;
const prepareResult = process.env.FLOWGENT_PREPARE_RESULT;
const releaseVersion = process.env.FLOWGENT_RELEASE_VERSION;
const releaseTag = process.env.FLOWGENT_RELEASE_TAG;
const shouldRelease = process.env.FLOWGENT_SHOULD_RELEASE;

if (!repository || !pullRequest || !token || !kind || !phase || !runUrl) {
  throw new Error("Missing Flowgent PR status comment context.");
}

const [owner, repo] = repository.split("/", 2);
const marker = `<!-- flowgent-${kind}-status -->`;
const apiBase = `https://api.github.com/repos/${owner}/${repo}/issues/${pullRequest}/comments`;

const resultIcon = (result) => {
  if (result === "success") return "✅";
  if (["failure", "cancelled", "timed_out"].includes(result)) return "❌";
  return "⏳";
};

const displayResult = (result) => result || "pending";

const decodeSummary = (encoded) => {
  if (!encoded) return "E2E summary was not generated; see the evidence artifact.";
  const summary = Buffer.from(encoded, "base64").toString("utf8").trim();
  if (!summary) return "E2E summary was empty; see the evidence artifact.";
  if (summary.length > 48_000) {
    return `${summary.slice(0, 48_000)}\n\n[Summary truncated; see the evidence artifact for the complete report.]`;
  }
  return summary;
};

const renderCi = () => {
  const artifactUrl = `${runUrl}#artifacts`;
  const details = [marker, "## Flowgent PR CI", "", `- Run: [Actions log](${runUrl})`];

  if (phase === "started") {
    details.push("- Status: ⏳ CI started: building dirty images, testing, and running the security-autonomy-fixer E2E.");
    return details.join("\n");
  }

  details.push(`- Build / UT / IT: ${resultIcon(buildResult)} ${displayResult(buildResult)}`);
  if (buildResult === "success" && dirtyTag) {
    details.push(
      "",
      "### PR artifacts",
      "",
      `- Runtime: \`ghcr.io/flowgent-labs/flowgent:${dirtyTag}\``,
      `- Web: \`ghcr.io/flowgent-labs/flowgent-web:${dirtyTag}\``,
    );
    if (dirtyChartVersion) {
      details.push(`- Helm chart: [\`flowgent-${dirtyChartVersion}.tgz\`](${artifactUrl})`);
    }
  }

  if (phase === "build") {
    details.push("", buildResult === "success"
      ? "- Status: ⏳ Dirty images and chart are ready; full E2E is running."
      : "- Status: ❌ Build failed; E2E was not started.");
    return details.join("\n");
  }

  details.push(`- Kubernetes E2E: ${resultIcon(e2eKubernetesResult)} ${displayResult(e2eKubernetesResult)}`);
  details.push(`- Docker Compose E2E: ${resultIcon(e2eDockerResult)} ${displayResult(e2eDockerResult)}`);
  if (e2eKubernetesResult === "skipped" || e2eDockerResult === "skipped") {
    details.push("- One or both E2E jobs did not start because the build stage did not succeed.");
  } else {
    details.push(`- Evidence: [reports and screenshots](${artifactUrl})`);
    details.push(
      "", "### Kubernetes E2E summary", "", "```text",
      decodeSummary(process.env.FLOWGENT_E2E_KUBERNETES_SUMMARY_BASE64).replaceAll("```", "``\\`"), "```",
      "", "### Docker Compose E2E summary", "", "```text",
      decodeSummary(process.env.FLOWGENT_E2E_DOCKER_SUMMARY_BASE64).replaceAll("```", "``\\`"), "```",
    );
  }
  const passed = buildResult === "success" && e2eKubernetesResult === "success" && e2eDockerResult === "success";
  details.push("", passed
    ? "**CI completed successfully.**"
    : `**CI failed.** Review the [Actions log](${runUrl}) and E2E evidence.`);
  return details.join("\n");
};

const renderRelease = () => {
  const details = [marker, "## Flowgent Release", "", `- Run: [Actions log](${runUrl})`];
  if (phase === "started") {
    details.push("- Status: ⏳ Release started; evaluating the merged PR title and preparing immutable artifacts.");
    return details.join("\n");
  }
  if (prepareResult && prepareResult !== "success") {
    details.push("", `**Release preparation failed.** Review the [Actions log](${runUrl}).`);
    return details.join("\n");
  }
  if (shouldRelease !== "true") {
    details.push("- Status: ⏭️ No release. Only merged PR titles beginning with `refactor:`, `feat:`, or `fix:` publish artifacts.");
    return details.join("\n");
  }
  details.push(`- Version: \`${releaseVersion}\` (${releaseTag})`);
  if (releaseResult === "success") {
    details.push(
      "",
      "### Official artifacts",
      "",
      `- Runtime: \`ghcr.io/flowgent-labs/flowgent:${releaseVersion}\``,
      `- Web: \`ghcr.io/flowgent-labs/flowgent-web:${releaseVersion}\``,
      `- Helm chart: \`oci://ghcr.io/flowgent-labs/charts/flowgent --version ${releaseVersion}\``,
      `- Release: [${releaseTag}](https://github.com/${repository}/releases/tag/${releaseTag})`,
      "",
      "**Release completed successfully.**",
    );
  } else {
    details.push("", `**Release failed.** Review the [Actions log](${runUrl}).`);
  }
  return details.join("\n");
};

const body = kind === "ci" ? renderCi() : renderRelease();
if (process.env.FLOWGENT_COMMENT_DRY_RUN === "true") {
  console.log(body);
  process.exit(0);
}

const headers = {
  Accept: "application/vnd.github+json",
  Authorization: `Bearer ${token}`,
  "X-GitHub-Api-Version": "2022-11-28",
  "Content-Type": "application/json",
};

const request = async (url, options = {}) => {
  const response = await fetch(url, { ...options, headers: { ...headers, ...options.headers } });
  if (!response.ok) {
    throw new Error(`GitHub API ${options.method || "GET"} ${url} failed: ${response.status} ${await response.text()}`);
  }
  return response.status === 204 ? undefined : response.json();
};

const comments = await request(`${apiBase}?per_page=100`);
const existing = comments.find(
  (comment) => comment.user?.login === "github-actions[bot]" && comment.body?.includes(marker),
);
if (existing) {
  await request(`https://api.github.com/repos/${owner}/${repo}/issues/comments/${existing.id}`, {
    method: "PATCH",
    body: JSON.stringify({ body }),
  });
} else {
  await request(apiBase, { method: "POST", body: JSON.stringify({ body }) });
}
