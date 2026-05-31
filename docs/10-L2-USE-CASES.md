# Flowgent Use Cases

Real-world use cases demonstrating Flowgent's orchestration capabilities.
Each use case links to its corresponding AgentFlow and Agent definition YAML files under
`examples/`. Structure and naming follow the convention described in the
[architecture doc](01-L1-Engine-Architecture.md#162-directory-layout).

---

## 1. Security Autonomy Fixer

Enterprise security vulnerability remediation with multi-agent team orchestration.

review (security/quality/architecture) → vote → supervisor check → human approval →
commit PR → SonarQube re-scan (max 3 iterations) → report → notify.

### 1.1 AgentFlow Definitions

| File | Description |
|------|-------------|
| [`security-autonomy-fixer-v1.yaml`](../examples/security-autonomy-fixer/flows/security-autonomy-fixer-v1.yaml) | **V1 Baseline** — 12-phase complete pipeline (discovery → notify) with iterative re-scan loop. GitHub webhook enabled. |
| [`security-autonomy-fixer-v2.yaml`](../examples/security-autonomy-fixer/flows/security-autonomy-fixer-v2.yaml) | **V2** — Identical to V1 except GitHub PR webhook trigger commented out (pending webhook→SonarQube integration). Deploy this version. |
| [`sub-fix.yaml`](../examples/security-autonomy-fixer/flows/sub-fix.yaml) | Sub-flow: analyze → patch → validate for individual issue |

> **E2E testing:**
> - [E2E-security-fixer-v1.md](../examples/security-autonomy-fixer/docs/E2E-security-fixer-v1.md) — V1 current: webhook simulated, white-box PG/EMQX/Jaeger verification

> - [E2E-security-fixer-v2.md)](../examples/security-autonomy-fixer/docs/E2E-security-fixer-v2.md) — V2 target: real GitHub webhook → SonarQube → fix → re-scan

> **Note:** V3 (iterative re-scan loop) was merged into the baseline. Once webhook→SonarQube integration is deployed, V1 (webhook simulated) can be retired and V2 (real webhook) becomes the single source of truth.

### 1.2 Agent Definitions

| File | Agent | Role |
|------|-------|------|
| [`agents/01-supervisor.yaml`](../examples/security-autonomy-fixer/agents/01-supervisor.yaml) | supervisor | Orchestration controller, enforces safety |
| [`agents/02-issue-detector.yaml`](../examples/security-autonomy-fixer/agents/02-issue-detector.yaml) | issue-detector | Parses scan results, normalizes findings |
| [`agents/03-fixer-agent.yaml`](../examples/security-autonomy-fixer/agents/03-fixer-agent.yaml) | fixer-agent | Generates minimal secure patches |
| [`agents/04-security-reviewer.yaml`](../examples/security-autonomy-fixer/agents/04-security-reviewer.yaml) | security-reviewer | Reviews fixes for security correctness |
| [`agents/05-quality-reviewer.yaml`](../examples/security-autonomy-fixer/agents/05-quality-reviewer.yaml) | quality-reviewer | Reviews code quality |
| [`agents/06-arch-reviewer.yaml`](../examples/security-autonomy-fixer/agents/06-arch-reviewer.yaml) | arch-reviewer | Reviews architectural impact |
| [`agents/10-git-agent.yaml`](../examples/security-autonomy-fixer/agents/07-git-agent.yaml) | git-agent | Git operations (branch, commit, PR) |

### 1.3 MCP Tools & Skills Used

| Server/Skill | Tools | Source |
|--------------|-------|--------|
| GitHub | `get_latest_commit`, `create_branch`, `commit_and_push`, `create_pull_request` | `examples/mcps/github/` |
| SonarQube | `scan/get_issues`, `scan/trigger_analysis`, `scan/get_status` | `examples/mcps/sonarqube/` |


### 1.3 Architecture

**Multi-Agent Team** with deterministic control plane:

```tree
Supervisor (per-repo)
  ├── Discovery Phase
  │     ├── SonarQube MCP (SAST — get_issues)
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
multi-channel notifier.

---

## 2. AutoTest Generation

Automated integration test generation driven by Confluence requirements.

**Flow**: Fetch Confluence requirement page → analyze and extract structured dev plan
→ plan test scenarios → generate Cucumber feature files → validate against project
type (Spring Boot / Flask / React) → commit.

### 2.1 AgentFlow Definitions

| File | Description |
|------|-------------|
| [`autotest-generation-v1.yaml`](../examples/autotest-generation/flows/autotest-generation-v1.yaml) | V1: AutoTest generation pipeline |

### 2.2 Agent Definitions

| File | Agent | Role |
|------|-------|------|
| [`01-requirement-analyst.yaml`](../examples/autotest-generation/agents/01-requirement-analyst.yaml) | requirement-analyst | Analyze Confluence requirements → structured dev plan |
| [`02-test-planner.yaml`](../examples/autotest-generation/agents/02-test-planner.yaml) | test-planner | Design test scenarios from dev plan |
| [`03-test-generator.yaml`](../examples/autotest-generation/agents/03-test-generator.yaml) | test-generator | Generate Cucumber feature files |

### 2.3 MCP Tools Used

| MCP Server | Tools | Source |
|------------|-------|--------|
| Confluence | `analyze_requirements`, `plan_tests`, `generate_cucumber` | `examples/autotest-generation/mcps/confluence/` (external) |

> Note: The `examples/autotest-generation/mcps/test/` directory was removed. AutoTest generation uses the
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

1. Create AgentFlow YAML in `examples/{UseCase}/flows/` with numbered prefix (`NN-name.yaml`)
2. Create Agent YAMLs in `examples/{UseCase}/agents/` if introducing new agent roles
3. Add a section in this catalog describing the use case, its architecture, and the
   linked config files
4. For new MCP integrations, add the MCP server source under `examples/{UseCase}/mcps/*/`

Keep use case configs self-contained — all agents and flows referenced by a use case
should exist under `examples/{UseCase}/`.
