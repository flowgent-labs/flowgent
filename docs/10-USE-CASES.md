# Flowgent Use Cases

Real-world use cases demonstrating Flowgent's orchestration capabilities.
Each use case links to its corresponding AgentFlow and Agent definition YAML files under
`examples/`. Structure and naming follow the convention described in the
[architecture doc](01-L1-Engine-Architecture.md#162-directory-layout).

---

## 1. Security Autonomy Fixer

Enterprise security vulnerability remediation with multi-agent team orchestration.

**Flow**: CI trigger → scan repos (SonarQube, Sonatype IQ, Nexus3) → detect issues
→ generate fixes → multi-agent review (security/quality/architecture) → vote →
supervisor check → human approval → commit PR → notify.

### AgentFlow Definitions

| File | Description |
|------|-------------|
| [`examples/flows/01-security-autonomy-fix-v1.yaml`](../examples/flows/01-security-autonomy-fix-v1.yaml) | V1: 11-phase pipeline (discovery → notify) |
| [`examples/flows/01-security-autonomy-fix-v2.yaml`](../examples/flows/01-security-autonomy-fix-v2.yaml) | V2: Updated pipeline with refined review flow |
| [`examples/flows/01-sub-fix.yaml`](../examples/flows/01-sub-fix.yaml) | Sub-flow: analyze → patch → validate for individual issue |

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
  ├── Discovery Agent → parallel fetch (SonarQube / IQ / Nexus3)
  ├── Alpha Agent Group (parallel fix per issue type)
  │     ├── Build/Test Alpha (broken builds)
  │     ├── Cyberflow Alpha (runtime/container vulns)
  │     ├── FOSS Alpha (dependency vulns via IQ)
  │     └── Code Quality Alpha (SonarQube issues)
  ├── Review Board (3-round voting: security + quality + architecture)
  └── CI Verification Loop → Notification
```

**Key features**: 11 DAG node types, parallel map fan-out, deterministic majority vote,
supervisor-controlled autonomy (redirect/retry/inject/abort with quotas),
human-in-the-loop approval gate, multi-channel notification.

### MCP Tools Used

| MCP Server | Tools | Source |
|------------|-------|--------|
| GitHub | `get_latest_commit`, `create_branch`, `commit_and_push`, `create_pull_request` | `examples/mcp-github/` |
| SonarQube | `get_issues` | `examples/mcp-sonarqube/` |
| Sonatype IQ | `get_jobs_by_commit`, `get_foss_solution` | `examples/mcp-sonatypeiq/` |
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
| MCP Test | `analyze_requirements`, `plan_tests`, `generate_cucumber` | `examples/mcp-test/` |

---

## 3. E2E SonarQube Real Scan

End-to-end flow connecting to a real SonarQube instance to fetch and analyze issues.

| File | Description |
|------|-------------|
| [`examples/flows/00-e2e-sonarqube-real.yaml`](../examples/flows/00-e2e-sonarqube-real.yaml) | Direct SonarQube issue fetch + analysis |

---

## Scenario Configs

Quick-start YAML configs for running Flowgent in different modes:

| File | Mode | Backend |
|------|------|---------|
| [`examples/scenario-e2e-allinone.yaml`](../examples/scenario-e2e-allinone.yaml) | All-in-one (single binary) | SQLite + memory queue |
| [`examples/scenario-e2e-production.yaml`](../examples/scenario-e2e-production.yaml) | Distributed (K8s) | PostgreSQL + MQTT + Redis |

---

## Adding New Use Cases

1. Create AgentFlow YAML in `examples/flows/` with numbered prefix (`NN-name.yaml`)
2. Create Agent YAMLs in `examples/agents/` if introducing new agent roles
3. Add a section in this catalog describing the use case, its architecture, and the
   linked config files
4. For new MCP integrations, add the MCP server source under `examples/mcp-*/`

Keep use case configs self-contained — all agents and flows referenced by a use case
should exist under `examples/`.
