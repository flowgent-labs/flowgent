# Security Autonomy Fixer V2 — Real End-to-End

**Date:** 2026-05-29
**Parent:** [10-L2-USE-CASES.md](10-L2-USE-CASES.md)
**AgentFlow:** [`examples/flows/01-security-autonomy-fix-v2.yaml`](../examples/flows/01-security-autonomy-fix-v2.yaml)

> V2 is the **target baseline** — full 12-phase pipeline from GitHub webhook to
> SonarQube re-scan verification. All steps use real external services.
> For the current working version with webhook simulated, see
> [10-L2-E2E-security-fixer-v1.md](10-L2-E2E-security-fixer-v1.md).

---

## 1. Architecture

```
GitHub PR webhook
  │
  ▼
Flowgent API Server (/_/webhooks/github)
  │
  ▼
JobManager → DAG → TaskManager → Executors
  │
  ├── Phase 1:  get-commit (GitHub MCP)
  ├── Phase 2:  scan-sonarqube (SonarQube MCP — real issues)
  ├── Phase 3:  scan-sonatypeiq (Sonatype IQ MCP)
  ├── Phase 4:  fetch-safe-deps (Skill — Nexus3 firewall check)
  ├── Phase 5:  aggregate-issues (Agent: issue-detector)
  ├── Phase 6:  generate-fixes (Agent: fixer-agent)
  ├── Phase 7:  review-security/quality/arch (3 Agents parallel)
  ├── Phase 8:  tribunal (Majority vote)
  ├── Phase 9:  supervisor-check
  ├── Phase 10: human-approval
  ├── Phase 11: commit-fixes → create-pr (GitHub MCP)
  ├── Phase 12: trigger-rescan → check-resolved (SonarQube MCP)
  ├── Phase 13: summary-report (Agent)
  └── Phase 14: notify-pr/email/teams
```

---

## 2. Prerequisites

| Service | Endpoint | Credentials |
|---------|----------|-------------|
| SonarQube | `https://sonarqube.wl4g.com` | admin / Abcd1234@sonar, token: `squ_413955...` |
| GitHub | `https://github.com/wl4g/rengine` | PAT with repo + PR scope (`source ~/.bashrc`) |
| Sonatype IQ | (external) | TBD |
| Nexus3 | (external) | TBD |
| EMQX | K3s `flowgent-emqx:1883` | internal |

### 2.1 SonarQube Setup

See [20-L2-SonarQube-Deploy.md](20-L2-SonarQube-Deploy.md).

Verify:
```bash
curl -s -u admin:Abcd1234@sonar https://sonarqube.wl4g.com/api/system/status
# {"status":"UP","version":"26.4.0.121862"}

curl -s -u squ_413955935468a7aacc7977053eda43d585e4da04: \
  https://sonarqube.wl4g.com/api/measures/component?component=rengine \
  -d "metricKeys=bugs,vulnerabilities,code_smells,coverage"
# {"component":{"measures":[{"metric":"bugs","value":"13"},...]}}
```

### 2.2 GitHub Webhook

```bash
# Create webhook on rengine repo
gh api repos/wl4g/rengine/hooks \
  -f name=web -f active=true \
  -f config.url=https://flowgent.wl4g.com/_/webhooks/github \
  -f config.content_type=json \
  -f events[]=pull_request -f events[]=push
```

### 2.3 Registered Agents

All 6 agents must be registered (loaded from `examples/agents/`):
- `supervisor`, `issue-detector`, `fixer-agent`
- `security-reviewer`, `quality-reviewer`, `arch-reviewer`

### 2.4 Registered MCPs

```yaml
mcps:
  - name: github
    command: ["/bin/github-mcp"]
    env: { MCP_UPSTREAM_TOKEN_FILE: /path/to/.credentials }
  - name: sonarqube
    command: ["/bin/sonarqube-mcp"]
    env:
      SONARQUBE_URL: "https://sonarqube.wl4g.com"
      SONARQUBE_TOKEN: "squ_413955935468a7aacc7977053eda43d585e4da04"
```

---

## 3. Verification Checklist

Each phase verified with real data. All checks must pass.

### 3.1 Webhook → Trigger

| Step | Verify | Method |
|------|--------|--------|
| Create PR on rengine | Webhook received by Flowgent | `kubectl logs deploy/flowgent-apiserver` |
| Flow triggered | `agentflow_runs` row with `trigger_type=webhook` | PG query |
| Run status PENDING → RUNNING | Status transitions | `GET /api/v1/default/runs/{id}` |

### 3.2 SonarQube Scan (Real Issues)

| Step | Verify | Expected |
|------|--------|----------|
| `scan-sonarqube` executed | Task status SUCCESS | `task_runs` row with `node_id=scan-sonarqube` |
| Issues returned | Output contains real SonarQube issues | BLOCKER/CRITICAL/MAJOR severities present |
| Total issues > 0 | SonarQube project has data | rengine: ~13 bugs, ~1382 code smells, ~11 vulnerabilities |

### 3.3 Agent Analysis

| Step | Verify | Expected |
|------|--------|----------|
| `aggregate-issues` executed | Agent output is valid JSON | `node_memories` row with parsed issues |
| Deduplication | No duplicate file+rule pairs | Agent instruction enforced |

### 3.4 Fix Generation

| Step | Verify | Expected |
|------|--------|----------|
| `generate-fixes` executed | Patches generated for top issues | JSON with `patches[]` array |
| Each patch targets a real file | File paths match rengine repo | e.g. `service/src/main/java/...` |

### 3.5 Review + Vote

| Step | Verify | Expected |
|------|--------|----------|
| 3 reviewers executed | All 3 agents produce `decision: true/false` | Parallel execution |
| `tribunal` majority vote | Vote output is deterministic | `decision` matches majority |

### 3.6 Commit & PR

| Step | Verify | Expected |
|------|--------|----------|
| Branch created | `security-bot/fix-{run_id}` on rengine | GitHub API |
| Patches committed | Files modified in branch | PR diff shows changes |
| PR opened | PR URL returned | `create-pr.pr_url` is valid |

### 3.7 SonarQube Re-Scan

| Step | Verify | Expected |
|------|--------|----------|
| Re-analysis triggered | SonarQube CE task queued | `trigger-rescan` output |
| Analysis completed | `wait-rescan` returns SUCCESS | Within 120s timeout |
| Issues resolved | `compare-results` shows resolution | `resolved_count > 0` expected |

### 3.8 Notification

| Step | Verify | Expected |
|------|--------|----------|
| PR comment posted | Comment on PR with report | GitHub API |
| Summary report | Agent generates markdown report | `summary-report` output |

---

## 4. Post-Run PG Verification

```sql
-- All nodes executed and their status
SELECT tr.node_id, tr.status,
       tr.output IS NOT NULL AS has_output,
       nm.content IS NOT NULL AS has_memory
FROM task_runs tr
LEFT JOIN agent_memories nm ON nm.agent_id = tr.node_id
WHERE tr.agentflow_run_id = '<run_id>'
ORDER BY tr.sequence;

-- Expected: 14 nodes, all SUCCESS except end (noop)
```

## 5. Post-Run SonarQube Verification

```bash
# Check issues before fix
curl -s -u admin:Abcd1234@sonar \
  "https://sonarqube.wl4g.com/api/issues/search?componentKeys=rengine&statuses=OPEN&ps=1" \
  | jq '.total'

# Check quality gate
curl -s -u admin:Abcd1234@sonar \
  "https://sonarqube.wl4g.com/api/qualitygates/project_status?projectKey=rengine" \
  | jq '.projectStatus.status'
```
