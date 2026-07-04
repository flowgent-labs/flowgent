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
| [`security-autonomy-fixer.yaml`](../examples/security-autonomy-fixer/config/flows/security-autonomy-fixer.yaml) | **Canonical** — 11-phase SonarQube-only remediation pipeline (discovery → notify). No SonatypeIQ dependency. |
| [`sub-fix.yaml`](../examples/security-autonomy-fixer/config/flows/sub-fix.yaml) | Sub-flow: analyze → patch → validate for individual issue |

> **E2E testing:**
> - [VERIFICATION.md](../examples/security-autonomy-fixer/e2e-verification/VERIFICATION.md) — White-box PG/EMQX/Jaeger verification checklist, including wallet x402 signing checks

### 1.2 Agent Definitions

| File | Agent | Role |
|------|-------|------|
| [`agents/01-supervisor.yaml`](../examples/security-autonomy-fixer/config/agents/01-supervisor.yaml) | supervisor | Orchestration controller, enforces safety |
| [`agents/02-issue-detector.yaml`](../examples/security-autonomy-fixer/config/agents/02-issue-detector.yaml) | issue-detector | Parses scan results, normalizes findings |
| [`agents/03-fixer-agent.yaml`](../examples/security-autonomy-fixer/config/agents/03-fixer-agent.yaml) | fixer-agent | Generates minimal secure patches |
| [`agents/04-security-reviewer.yaml`](../examples/security-autonomy-fixer/config/agents/04-security-reviewer.yaml) | security-reviewer | Reviews fixes for security correctness |
| [`agents/05-quality-reviewer.yaml`](../examples/security-autonomy-fixer/config/agents/05-quality-reviewer.yaml) | quality-reviewer | Reviews code quality |
| [`agents/06-arch-reviewer.yaml`](../examples/security-autonomy-fixer/config/agents/06-arch-reviewer.yaml) | arch-reviewer | Reviews architectural impact |
| [`agents/07-git-agent.yaml`](../examples/security-autonomy-fixer/config/agents/07-git-agent.yaml) | git-agent | Git operations (branch, commit, PR) |

### 1.3 MCP Tools & Skills Used

| Server/Skill | Tools | Source |
|--------------|-------|--------|
| GitHub | `get_latest_commit`, `create_branch`, `commit_and_push`, `create_pull_request` | `examples/security-autonomy-fixer/config/mcps/github/` |
| SonarQube | `scan/get_issues`, `scan/trigger_analysis`, `scan/get_status` | `examples/security-autonomy-fixer/config/mcps/sonarqube/` |


### 1.3 Architecture

**Multi-Agent Team** with deterministic control plane:

```tree
Supervisor (per-repo)
  ├── Discovery Phase
  │     └── SonarQube MCP (SAST — get_issues)
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
| [`autotest-generator-v1.yaml`](../examples/autotest-generator/config/flows/autotest-generator-v1.yaml) | V1: AutoTest generation pipeline |

### 2.2 Agent Definitions

| File | Agent | Role |
|------|-------|------|
| [`01-requirement-analyst.yaml`](../examples/autotest-generator/config/agents/01-requirement-analyst.yaml) | requirement-analyst | Analyze Confluence requirements → structured dev plan |
| [`02-test-planner.yaml`](../examples/autotest-generator/config/agents/02-test-planner.yaml) | test-planner | Design test scenarios from dev plan |
| [`03-test-generator.yaml`](../examples/autotest-generator/config/agents/03-test-generator.yaml) | test-generator | Generate Cucumber feature files |

### 2.3 MCP Tools Used

| MCP Server | Tools | Source |
|------------|-------|--------|
| Confluence | `analyze_requirements`, `plan_tests`, `generate_cucumber` | `examples/autotest-generator/config/mcps/confluence/` (external) |

> Note: The `examples/autotest-generator/config/mcps/test/` directory was removed. AutoTest generation uses the
> Confluence MCP for requirement fetching. Test planning and Cucumber generation are
> handled by the `test-planner` and `test-generator` agents directly.

---

## Config Files

| File | Mode | Backend | Use |
|------|------|---------|-----|
| `etc/flowgent.yaml` | Reference (annotated) | Configurable | Dev / CI / custom configs |
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
