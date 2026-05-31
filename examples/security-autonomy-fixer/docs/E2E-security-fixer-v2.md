# Security Autonomy Fixer V2 — Real GitHub Webhook → SonarQube Fix

**Date:** 2026-05-30
**Scope:** K8s ResourceManager + PG + EMQX on K3s — GitHub webhook triggered, real SonarQube fix
**Parent:** [10-L2-USE-CASES.md](../../../docs/10-L2-USE-CASES.md)
**AgentFlow:** [`security-autonomy-fixer-v2.yaml`](../flows/security-autonomy-fixer-v2.yaml)

> V2 is the **target baseline** — identical pipeline to V1 but with GitHub webhook
> trigger enabled (not commented out). Requires real SonarQube instance, GitHub PAT
> with repo + PR scope, and all MCP binaries registered.
>
> For the baseline API-triggered distributed test, see
> [11-L2-E2E-security-fixer-v1.md](../docs/E2E-security-fixer-v1.md).

---

## 1. What's Different from V1

| Aspect | V1 (API Trigger) | V2 (Webhook Trigger) |
|--------|------------------|----------------------|
| Trigger | `POST /api/v1/default/agentflows/trigger` | GitHub PR webhook → `/_/webhooks/github` |
| SonarQube | Optional — API mock or local container | **Required** — real instance, rengine project with 700+ issues |
| GitHub MCP | Optional — simulated commit/PR | **Required** — real PAT, real commits, real PRs on wl4g/rengine |
| Verification | White-box: PG rows, MQTT topics, Jaeger traces | Black-box: real PR opened, issues resolved on SonarQube, quality gate passes |
| Deploy mode | `global.mode=session` (shared pool) | Same — session mode |

---

## 2. Additional Prerequisites (beyond V1)

| Service | Endpoint | Credentials |
|---------|----------|-------------|
| SonarQube | `http://172.29.235.101:9000` | admin / Abcd1234@sonar, token: `squ_413955...` |
| GitHub (rengine) | `https://github.com/wl4g/rengine` | PAT with `repo` + `pull_requests` scope |
| GitHub MCP | `/bin/github-mcp` | `GITHUB_TOKEN` env var |
| SonarQube MCP | `/bin/sonarqube-mcp` | `SONARQUBE_URL` + `SONARQUBE_TOKEN` env vars |

### 2.1 SonarQube — Verify rengine Project

```bash
# System status
curl -s -u admin:Abcd1234@sonar http://172.29.235.101:9000/api/system/status
# → {"status":"UP","version":"26.4.0.121862"}

# rengine real issues
curl -s -u squ_413955935468a7aacc7977053eda43d585e4da04: \
  "http://172.29.235.101:9000/api/issues/search?projectKeys=rengine&severities=BLOCKER,CRITICAL&ps=5" \
  | jq '{total: .total, issues: [.issues[].message]}'
# → {"total": 700+, "issues": ["...", ...]}
```

### 2.2 GitHub Webhook Setup

```bash
gh api repos/wl4g/rengine/hooks \
  -f name=web -f active=true \
  -f config.url=https://flowgent.wl4g.com/_/webhooks/github \
  -f config.content_type=json \
  -f events[]=pull_request -f events[]=push
```

### 2.3 MCP Registration

```yaml
# flowgent.yaml
orchestration:
  mcps:
    - name: github
      enabled: true
      command: ["/bin/github-mcp"]
      env:
        GITHUB_TOKEN: "${GITHUB_TOKEN}"
    - name: sonarqube
      enabled: true
      command: ["/bin/sonarqube-mcp"]
      env:
        SONARQUBE_URL: "http://172.29.235.101:9000"
        SONARQUBE_TOKEN: "squ_413955935468a7aacc7977053eda43d585e4da04"
```

---

## 3. Pipeline (14 Phases)

```graph
GitHub PR opened/updated on wl4g/rengine
  │
  ▼
webhook → Flowgent API Server (/_/webhooks/github)
  │
  ▼
JobManager → DAG → 14 nodes dispatched via MQTT
  │
  ├── [1]  get-commit         (tool: GitHub MCP — fetch PR diff)
  ├── [2]  scan-sonarqube     (tool: SonarQube MCP — real issues)
  ├── [3]  scan-sonatypeiq    (tool: Sonatype IQ MCP)
  ├── [4]  fetch-safe-deps    (skill: Nexus3 firewall check)
  ├── [5]  aggregate-issues   (agent: issue-detector)
  ├── [6]  generate-fixes     (agent: fixer-agent)
  ├── [7]  review-security    (agent, parallel fan-out)
  ├── [8]  review-quality     (agent, parallel fan-out)
  ├── [9]  review-arch        (agent, parallel fan-out)
  ├── [10] tribunal           (tribunal: majority vote)
  ├── [11] supervisor         (agent: validate + retry gate)
  ├── [12] human-approval     (async token-based gate)
  ├── [13] commit-fixes       (tool: GitHub MCP — real commit)
  ├── [14] create-pr          (tool: GitHub MCP — real PR)
  │
  ▼
Post-PR: trigger-rescan → wait-rescan → check-resolved → summary-report → notify
```

---

## 4. Verification

Same three-layer verification as V1 (see [11-L2-E2E-security-fixer-v1.md](11-L2-E2E-security-fixer-v1.md) §5),
plus these V2-specific checks:

### 4.1 Webhook → Trigger

| # | Check | Expected |
|---|-------|----------|
| W1 | Webhook received | apiserver log: `POST /_/webhooks/github` |
| W2 | Run created | `agentflow_runs` row with `trigger_type=webhook` |
| W3 | Correct flow triggered | `agentflow_id = security-autonomy-fixer-v2` |

### 4.2 SonarQube → Real Issues

| # | Check | Expected |
|---|-------|----------|
| W4 | `scan-sonarqube` SUCCESS | Task output contains `issues[]` with real BLOCKER/CRITICAL items |
| W5 | Issues match SonarQube API | Same count as direct API query |
| W6 | Agent analyzed real data | `aggregate-issues` memory has categorized issue list |

### 4.3 GitHub → Real Commit + PR

| # | Check | Expected |
|---|-------|----------|
| W7 | Branch created on rengine | `security-bot/fix-{run_id}` visible in `gh pr list` |
| W8 | Patches committed | `git log` shows commit by flowgent bot |
| W9 | PR opened | `gh pr view` shows title, description, diff |
| W10 | PR linked to SonarQube | PR description references issue keys |

### 4.4 Post-Run SonarQube Verification

```bash
# Issues before fix
curl -s -u admin:Abcd1234@sonar \
  "http://172.29.235.101:9000/api/issues/search?componentKeys=rengine&statuses=OPEN&ps=1" \
  | jq '.total'

# Quality gate
curl -s -u admin:Abcd1234@sonar \
  "http://172.29.235.101:9000/api/qualitygates/project_status?projectKey=rengine" \
  | jq '.projectStatus.status'

# Verify PR on GitHub
gh pr list --repo wl4g/rengine --head security-bot/fix-*
```

| # | Check | Expected |
|---|-------|----------|
| W11 | Issues decreased | `total` POST-run < `total` PRE-run |
| W12 | Quality gate | `OK` or `WARN` (not `ERROR`) |
| W13 | PR mergeable | PR has no conflicts, CI passing |

---

## 5. Troubleshooting (V2-Specific)

| Symptom | Cause | Fix |
|---------|-------|-----|
| Webhook not received | Firewall / DNS / ngrok | Verify `config.url` reachable from GitHub |
| SonarQube MCP 401 | Wrong token | Regenerate at SonarQube → My Account → Security |
| GitHub MCP 401 | PAT expired or insufficient scope | Regenerate with `repo` + `pull_requests` |
| `scan-sonarqube` timeout | CE task not completing | Increase `wait-rescan` timeout (default 120s) |
| PR has no diff | Patches applied to wrong branch | Check `generate-fixes` output for file paths |
| `human-approval` stuck | Token not approved | `POST /api/v1/human/<token>/approve` |
