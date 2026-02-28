# Flowgent Use Cases

Real-world use cases demonstrating Flowgent's orchestration capabilities.
Each use case links to its corresponding AgentFlow and Agent definition YAML files under
`examples/`. Structure and naming follow the convention described in the
[architecture doc](01-L1-Engine-Architecture.md#162-directory-layout).

---

## 1. Security Autonomy Fixer

Enterprise security vulnerability remediation with multi-agent team orchestration.

**Flow**: CI trigger → scan repos (SonarQube SAST, Sonatype IQ FOSS, skill-based
Nexus3 dependency firewall check) → detect issues → generate fixes → multi-agent
review (security/quality/architecture) → vote → supervisor check → human approval →
commit PR → SonarQube re-scan (max 3 iterations) → report → notify.

### AgentFlow Definitions

| File | Description |
|------|-------------|
| [`examples/flows/01-security-autonomy-fix-v1.yaml`](../examples/flows/01-security-autonomy-fix-v1.yaml) | **V1 Baseline** — 12-phase complete pipeline (discovery → notify) with iterative re-scan loop. GitHub webhook enabled. |
| [`examples/flows/01-security-autonomy-fix-v2.yaml`](../examples/flows/01-security-autonomy-fix-v2.yaml) | **V2** — Identical to V1 except GitHub PR webhook trigger commented out (pending webhook→SonarQube integration). Deploy this version. |
| [`examples/flows/01-sub-fix.yaml`](../examples/flows/01-sub-fix.yaml) | Sub-flow: analyze → patch → validate for individual issue |

> **Note:** V3 (iterative re-scan loop) was merged into V1. V1 is now the single source of truth. Once webhook→SonarQube integration is complete, V2 can be removed and V1 deployed directly.

### Agent Definitions

| File | Agent | Role |
|------|-------|------|
| [`examples/agents/01-supervisor.yaml`](../examples/agents/01-supervisor.yaml) | supervisor | Orchestration controller, enforces safety |
| [`examples/agents/02-issue-detector.yaml`](../examples/agents/02-issue-detector.yaml) | issue-detector | Parses scan results, normalizes findings |
| [`examples/agents/03-fixer-agent.yaml`](../examples/agents/03-fixer-agent.yaml) | fixer-agent | Generates minimal secure patches |
| [`examples/agents/04-security-reviewer.yaml`](../examples/agents/04-security-reviewer.yaml) | security-reviewer | Reviews fixes for security correctness |
| [`examples/agents/05-quality-reviewer.yaml`](../examples/agents/05-quality-reviewer.yaml) | quality-reviewer | Reviews code quality |
| [`examples/agents/06-arch-reviewer.yaml`](../examples/agents/06-arch-reviewer.yaml) | arch-reviewer | Reviews architectural impact |
| [`examples/agents/10-git-agent.yaml`](../examples/agents/10-git-agent.yaml) | git-agent | Git operations (branch, commit, PR) |

### Architecture

**Multi-Agent Team** with deterministic control plane:

```
Supervisor (per-repo)
  ├── Discovery Phase
  │     ├── SonarQube MCP (SAST — get_issues)
  │     ├── Sonatype IQ MCP (FOSS dependency vulns)
  │     └── Skill: dependency-firewall-check (replaces Nexus3 MCP)
  │           Uses copilot scripts (gh + nexus3 web API + gcloud)
  │           to fetch top-3 non-quarantined Maven dep versions.
  │           See §13.4 in architecture doc for design rationale.
  ├── Analyze → Fix → Review Board (3-round voting)
  │     ├── Security Reviewer
  │     ├── Quality Reviewer
  │     └── Architecture Reviewer
  ├── Tribunal → Supervisor → Condition → Human Approval
  ├── Commit & PR (branch → patch → pull request)
  ├── SonarQube Re-Scan Loop (max 3 iterations)
  │     ├── Trigger re-analysis → poll for completion
  │     ├── Compare pre/post issue lists
  │     └── Loop back to Fix if unresolved issues remain
  └── Report → Multi-Channel Notify (PR comment + email + webhook)
```

**Key features**: 12 DAG node types, iterative re-scan verification loop,
parallel review fan-out, deterministic majority vote, supervisor-controlled
autonomy (redirect/retry/inject/abort with quotas), human-in-the-loop approval
gate, skill-based dependency checking (no Nexus3 license required),
multi-channel notifier.

### MCP Tools & Skills Used

| Server/Skill | Tools | Source |
|--------------|-------|--------|
| GitHub | `get_latest_commit`, `create_branch`, `commit_and_push`, `create_pull_request` | `examples/mcp-github/` |
| SonarQube | `scan/get_issues`, `scan/trigger_analysis`, `scan/get_status` | `examples/mcp-sonarqube/` |
| Sonatype IQ | `get_jobs_by_commit`, `get_foss_solution` | `examples/mcp-sonatypeiq/` |
| **Skill**: `dependency-firewall-check` | Nexus3 dependency firewall check via copilot scripts | Replaces `sonatype-nexus3` MCP (see §13.4) |
| Sonatype Nexus3 | `get_foss_solution` | `examples/mcp-nexus3/` |

---

## 2. AutoTest Generation

Automated integration test generation driven by Confluence requirements.

**Flow**: Fetch Confluence requirement page → analyze and extract structured dev plan
→ plan test scenarios → generate Cucumber feature files → validate against project
type (Spring Boot / Flask / React) → commit.

### AgentFlow Definitions

| File | Description |
|------|-------------|
| [`examples/flows/20-autotest-generation-v1.yaml`](../examples/flows/20-autotest-generation-v1.yaml) | V1: AutoTest generation pipeline |

### Agent Definitions

| File | Agent | Role |
|------|-------|------|
| [`examples/agents/07-requirement-analyst.yaml`](../examples/agents/07-requirement-analyst.yaml) | requirement-analyst | Analyze Confluence requirements → structured dev plan |
| [`examples/agents/08-test-planner.yaml`](../examples/agents/08-test-planner.yaml) | test-planner | Design test scenarios from dev plan |
| [`examples/agents/09-test-generator.yaml`](../examples/agents/09-test-generator.yaml) | test-generator | Generate Cucumber feature files |

### MCP Tools Used

| MCP Server | Tools | Source |
|------------|-------|--------|
| Confluence | `analyze_requirements`, `plan_tests`, `generate_cucumber` | `examples/mcp-confluence/` (external) |

> Note: The `examples/mcp-test/` directory was removed. AutoTest generation uses the
> Confluence MCP for requirement fetching. Test planning and Cucumber generation are
> handled by the `test-planner` and `test-generator` agents directly.

---

## Config Files

| File | Mode | Backend | Use |
|------|------|---------|-----|
| `etc/flowgent-dev.yaml` | All-in-one (single binary) | SQLite + memory queue | Dev / CI |
| `etc/flowgent.yaml.fully.sample` | Reference (annotated) | Configurable | Starting point for custom configs |
| `deploy/helm/flowgent/values.yaml` | Distributed (K8s) | PostgreSQL + MQTT + Redis | Production |

---

## Adding New Use Cases

1. Create AgentFlow YAML in `examples/flows/` with numbered prefix (`NN-name.yaml`)
2. Create Agent YAMLs in `examples/agents/` if introducing new agent roles
3. Add a section in this catalog describing the use case, its architecture, and the
   linked config files
4. For new MCP integrations, add the MCP server source under `examples/mcp-*/`

Keep use case configs self-contained — all agents and flows referenced by a use case
should exist under `examples/`.
