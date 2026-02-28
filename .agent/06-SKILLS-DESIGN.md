# Flowgent — Skills, Sandbox & Config Design

**Date:** 2026-05-15
**Status:** Draft for review

---

## Part A: Skills = Sub-AgentFlow (Zero-Friction Migration)

### A.1 Core Insight

**A Skill is a sub-AgentFlow.** Nothing more.

A "skill" accomplishes a task — which inherently means orchestrating multiple tools and/or agents. That's exactly what an AgentFlow is. Users migrating from Claude Code, Codex, Copilot, or any other agent framework can drop their existing skills into Flowgent as AgentFlow YAML files and reference them as `type: agentflow` nodes.

No new abstractions. No new executors. No ReAct loop. No mandatory schema.

### A.2 AgentFlowSpec — New Optional Fields

```go
type AgentFlowSpec struct {
    ID           string         `json:"id" yaml:"id"`
    Kind         string         `json:"kind,omitempty" yaml:"kind,omitempty"`           // NEW: "skill" | "" (empty = regular flow)
    Description  string         `json:"description,omitempty" yaml:"description,omitempty"`
    Summary      string         `json:"summary,omitempty" yaml:"summary,omitempty"`     // NEW: one-liner for A2A card
    InputSchema  *JSONSchema    `json:"input_schema,omitempty" yaml:"input_schema,omitempty"`   // NEW: optional
    OutputSchema *JSONSchema    `json:"output_schema,omitempty" yaml:"output_schema,omitempty"` // NEW: optional
    Vars         map[string]any `json:"vars,omitempty" yaml:"vars,omitempty"`
    Nodes        []Node         `json:"nodes" yaml:"nodes"`
    Edges        []Edge         `json:"edges" yaml:"edges"`
    Triggers     []TriggerDef   `json:"triggers,omitempty" yaml:"triggers,omitempty"`
}
```

**`input_schema` and `output_schema` are entirely optional.** A skill works without them — inputs flow through `${input.*}` JSONPath resolution as they always have, outputs are whatever the flow's final node produces. Schemas are purely for:

- A2A discovery (external systems can see expected parameters)
- Optional input/output validation (nice-to-have, not required)

### A.3 Migration Path — Zero Friction

User has an existing Claude Code / Codex / Copilot skill. They:

**Step 1 — Drop the YAML into the skills directory:**

```yaml
# etc/skills/01-dependency-scan.yaml
id: dependency-scan
kind: skill
summary: "Scan project dependencies for known CVEs"

nodes:
  - id: clone
    type: tool
    tool: github
    input:
      action: clone_repo
      url: "${input.repo_url}"
  - id: scan
    type: tool
    tool: dependency-checker
    input:
      path: "${clone.output.path}"
  - id: normalize
    type: agent
    agent: issue-detector
    input:
      raw_output: "${scan.output}"

edges:
  - { from: clone, to: scan }
  - { from: scan, to: normalize }
```

**Step 2 — Reference in any flow:**

```yaml
nodes:
  - id: dep-scan
    type: agentflow
    agentflow: dependency-scan
    input:
      repo_url: "https://github.com/${item}"
```

That's it. No schema required. No code changes. The existing `SubflowExecutor` handles execution. If user later wants A2A discoverability, they can add `input_schema` / `output_schema` at their own pace.

### A.4 A2A Agent Card

```go
func buildA2ASkills(flows []model.AgentFlowSpec) []map[string]any {
    var skills []map[string]any
    for _, f := range flows {
        if f.Kind != "skill" {
            continue
        }
        entry := map[string]any{
            "id":          f.ID,
            "description": f.Summary,
        }
        if f.InputSchema != nil {
            entry["input_schema"] = f.InputSchema
        }
        if f.OutputSchema != nil {
            entry["output_schema"] = f.OutputSchema
        }
        skills = append(skills, entry)
    }
    return skills
}
```

---

## Part B: Agent Definition — No Toolsets

### B.1 Tools Belong to the DAG, Not the Agent

Tools are deterministic DAG nodes (`type: tool`). The flow designer decides which tool to call, at which step, with which inputs. The agent receives tool output and reasons about it — it never decides to call a tool itself.

This is the architectural line between enterprise orchestration (DAG-controlled) and personal AI assistants (agent-controlled ReAct loop). Flowgent chooses enterprise.

### B.2 AgentDef — What's Missing

The current `AgentDef` is sparse. Two structured additions:

```go
type AgentDef struct {
    Name         string      `json:"name" yaml:"name"`
    Model        string      `json:"model" yaml:"model"`
    Soul         string      `json:"soul" yaml:"soul"`
    Instruction  string      `json:"instruction" yaml:"instruction"`
    OutputSchema *JSONSchema `json:"output_schema,omitempty" yaml:"output_schema,omitempty"` // NEW
    Temperature  *float64    `json:"temperature,omitempty" yaml:"temperature,omitempty"`       // NEW
    MaxTokens    int         `json:"max_tokens,omitempty" yaml:"max_tokens,omitempty"`         // NEW
}
```

| Field | Why |
|-------|-----|
| `output_schema` | Structured output contract — replaces the "Output STRICT JSON: {...}" prose currently in `instruction`. Enables output validation. Optional. |
| `temperature` | Per-agent override of model default. Supervisor needs 0.2; creative reviewer may want 0.5. |
| `max_tokens` | Output length control per agent role. |

Example:

```yaml
# etc/agents/03-fixer-agent.yaml
name: fixer-agent
model: bailian-codeplan/qwen3.5-coder
temperature: 0.3
max_tokens: 8192
soul: "You are a secure coding expert."
instruction: |-
  Analyze the vulnerability and its surrounding code. Generate the minimal
  secure patch that fixes the root cause without altering business logic.
output_schema:
  type: object
  properties:
    patches:
      type: array
      items:
        type: object
        properties:
          file:  { type: string }
          patch: { type: string }
  required: [patches]
```

---

## Part C: Sandbox — Secure Skill Script Execution

### C.1 Motivation

Skills often contain Python/Shell scripts (e.g., a dependency scanner that runs `pip-audit`, a linter that executes `shellcheck`). Running untrusted code directly in the TaskManager process is unsafe. A sandboxed execution environment is required.

### C.2 CLI: `flowgent sandbox`

A new subcommand that runs a sandbox executor, consuming sandbox execution plans:

```bash
./bin/flowgent sandbox                    # start sandbox worker (local Docker)
./bin/flowgent sandbox --provider k8s     # start sandbox worker (K8s pod per task)
./bin/flowgent sandbox --image flowgent-sandbox:latest
```

### C.3 Execution Model

```
AgentFlow DAG
  └── type: agentflow node (skill-kinded subflow containing scripts)
       │
       ▼
  JobManager creates ExecutionPlan (TaskType: TaskSubflow)
       │
       ├── No scripts in subflow → inline SubflowExecutor (existing path)
       │
       └── Scripts detected in subflow nodes →
           JobManager submits SandboxTask to Scheduler
           Scheduler launches sandbox pod/container
           Sandbox executes scripts, returns results
           JobManager collects, continues DAG
```

The JobManager inspects the subflow's node list during `buildGraph()`. If any node has `type: tool` with a `script:` or `run:` field (or a `type: sandbox` node), the subflow is flagged for sandbox execution.

### C.4 Sandbox Node Type (New)

```yaml
# Inside a skill agentflow
nodes:
  - id: run-audit
    type: sandbox                 # NEW node type
    runtime: python3              # python3 | bash | node
    script: |
      import subprocess, json
      result = subprocess.run(
          ["pip-audit", "--format", "json"],
          capture_output=True, text=True
      )
      print(result.stdout)
    timeout: 120s
    resources:
      cpu: "500m"
      memory: "256Mi"
    output: "${stdout}"           # parsed as JSON if valid, else raw string
```

### C.5 K8s Integration

In Kubernetes mode, the `KubernetesScheduler` creates a `batch/v1 Job` per sandbox task. The sandbox pod:

1. Receives the script + runtime + environment via `FLOWGENT_SANDBOX_TASK` env var or a mounted ConfigMap
2. Executes within resource limits (cpu/memory from plan)
3. Writes stdout/stderr + exit code to a shared volume or streams back via MQTT
4. JobManager watches Job completion, collects output, advances DAG

This is **elastic, on-demand** — sandbox pods only exist while a skill with scripts is executing. No idle resources.

### C.6 Local Mode

In all-in-one (local) mode, the `LocalScheduler` runs the sandbox via `docker run` or direct process execution with `seccomp`/`rlimit`:

```go
type LocalSandboxRunner struct {
    image string
}

func (r *LocalSandboxRunner) Run(ctx context.Context, task *SandboxTask) (*SandboxResult, error) {
    cmd := exec.CommandContext(ctx, "docker", "run", "--rm",
        "--memory="+task.Memory,
        "--cpus="+task.CPU,
        "-e", "SANDBOX_TASK="+task.JSON(),
        r.image,
    )
    out, err := cmd.Output()
    return parseSandboxOutput(out), err
}
```

---

## Part D: Directory-Based Configuration

### D.1 Current State → Target State

| Resource | Current (`etc/flowgent.yaml`) | Target |
|----------|-------------------------------|--------|
| Agents | `orchestration.agents: [...]` inline list | `orchestration.agents.static.load-dir: "agents/"` |
| AgentFlows | `orchestration.agentflows.static.paths: [...]` file list | `orchestration.agentflows.static.load-dir: "flows/"` |
| Skills | (none) | `orchestration.skills.static.load-dir: "skills/"` |

### D.2 Main Config (`etc/flowgent.yaml`) — Orchestration Section

```yaml
orchestration:
  max-concurrent-flows: 10
  flow-execution-timeout: 30m
  max-node-retries: 3

  # ── MCP server definitions (unchanged) ──
  mcps:
    - name: github
      enabled: true
      type: local
      command: ["sh", "-c", "/bin/github-mcp"]
      args: ["--transport", "stdio", "-v", "10"]
      env:
        MCP_UPSTREAM_TOKEN_FILE: /path/to/.credentials

  # ── Agent definitions — static dir + future DB ──
  agents:
    static:
      enabled: true
      load-dir: "agents/"       # loads etc/agents/*.yaml
      refresh: 30s               # hot-reload interval
    standard:
      enabled: false             # DB-backed (future Flowgent UI)

  # ── Skill definitions — static dir ──
  skills:
    static:
      enabled: true
      load-dir: "skills/"       # loads etc/skills/*.yaml
      refresh: 30s
    standard:
      enabled: false

  # ── AgentFlow definitions — static dir + future DB ──
  agentflows:
    static:
      enabled: true
      load-dir: "flows/"        # loads etc/flows/*.yaml
      refresh: 1m
    standard:
      enabled: false
```

### D.3 Directory Layout

```
etc/
├── flowgent.yaml                        # main config (thin — no inline agents/flows)
├── agents/                              # agent definition manifests
│   ├── 01-supervisor.yaml
│   ├── 02-issue-detector.yaml
│   ├── 03-fixer-agent.yaml
│   ├── 04-security-reviewer.yaml
│   ├── 05-quality-reviewer.yaml
│   ├── 06-arch-reviewer.yaml
│   ├── 07-requirement-analyst.yaml
│   ├── 08-test-planner.yaml
│   ├── 09-test-generator.yaml
│   └── 10-git-agent.yaml
├── skills/                              # skill-kinded agentflow manifests
│   ├── 01-dependency-scan.yaml
│   └── 02-web-search.yaml
└── flows/                               # L2 agentflow manifests
    ├── 01-security-autonomy-fixer.yaml
    └── 02-autotest-generation.yaml
```

### D.4 Naming Convention

`01-`, `02-` prefix is a convention for human readability and deterministic load order. The loader sorts files alphabetically before loading, so `01-supervisor.yaml` loads before `02-issue-detector.yaml`.

### D.5 Config Struct Changes

```go
// config/config.go

type OrchestrationConfig struct {
    MCPs                 []MCPDef     `json:"mcps" yaml:"mcps"`
    Agents               AgentCfg     `json:"agents" yaml:"agents"`          // was []AgentDef
    Skills               SkillCfg     `json:"skills" yaml:"skills"`          // NEW
    AgentFlows           AgentFlowCfg `json:"agentflows" yaml:"agentflows"`  // updated
    MaxConcurrentFlows   int          `json:"max-concurrent-flows" yaml:"max-concurrent-flows"`
    FlowExecutionTimeout string       `json:"flow-execution-timeout" yaml:"flow-execution-timeout"`
    MaxNodeRetries       int          `json:"max-node-retries" yaml:"max-node-retries"`
}

// AgentCfg replaces inline []AgentDef with dual-source config.
type AgentCfg struct {
    Static   StaticResourceCfg `json:"static" yaml:"static"`
    Standard StandardAgentCfg  `json:"standard" yaml:"standard"`
}

// SkillCfg configures skill discovery.
type SkillCfg struct {
    Static   StaticResourceCfg `json:"static" yaml:"static"`
    Standard StandardAgentCfg  `json:"standard" yaml:"standard"`
}

// StaticResourceCfg is a shared config for directory-based static resource loading.
type StaticResourceCfg struct {
    Enabled bool   `json:"enabled" yaml:"enabled"`
    LoadDir string `json:"load-dir" yaml:"load-dir"`
    Refresh string `json:"refresh" yaml:"refresh"`  // e.g. "30s", "1m"
}

// StandardAgentCfg enables DB-backed agent definitions (future Flowgent UI).
type StandardAgentCfg struct {
    Enabled bool `json:"enabled" yaml:"enabled"`
}

// AgentFlowCfg — updated from paths[] to load-dir.
type AgentFlowCfg struct {
    Static   StaticResourceCfg `json:"static" yaml:"static"`
    Standard StandardAgentCfg  `json:"standard" yaml:"standard"`
}
```

### D.6 Loading Logic

```go
func loadResourceDir[T any](dir string) ([]T, error) {
    entries, err := os.ReadDir(dir)
    if err != nil {
        return nil, fmt.Errorf("read dir %s: %w", dir, err)
    }
    var result []T
    for _, e := range entries {
        if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
            continue
        }
        data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
        var item T
        if err := yaml.Unmarshal(data, &item); err != nil {
            continue
        }
        result = append(result, item)
    }
    return result, nil
}

func LoadAgents(cfg *ServiceConfig, cfgPath string) ([]AgentDef, error) {
    var agents []AgentDef
    if cfg.Orchestration.Agents.Static.Enabled {
        dir := resolveDir(cfgPath, cfg.Orchestration.Agents.Static.LoadDir)
        return loadResourceDir[AgentDef](dir)
    }
    return agents, nil
}
```

---

## Part E: Implementation Plan

### Phase 1: Schema + Config (non-breaking)
1. Add `OutputSchema`, `Temperature`, `MaxTokens` to `AgentDef`
2. Add `Kind`, `Summary`, `InputSchema`, `OutputSchema` to `AgentFlowSpec`
3. Add `AgentCfg`, `SkillCfg`, `StaticResourceCfg`, `StandardAgentCfg` structs
4. Update `OrchestrationConfig`: `Agents` → `AgentCfg`, add `Skills`
5. Update `AgentFlowCfg`: `paths` → `load-dir` (`StaticResourceCfg`)
6. Implement `loadResourceDir[T]()` generic loader
7. Implement static dir loading for agents, skills, flows
8. Hot-reload watcher for all three directories
9. Extract inline agents from `etc/flowgent.yaml` → `etc/agents/*.yaml`
10. Unit tests for all loaders

### Phase 2: A2A Advertisement
1. `buildA2ASkills()` filters by `kind: skill`, includes optional schemas
2. Include agent `output_schema` in A2A agent card
3. E2E test: `GET /.well-known/agent.json`

### Phase 3: Sandbox Subcommand + Node Type
1. Add `SandboxNode` to `model/node.go`
2. Add `TaskSandbox` to `model/execution_plan.go`
3. Implement `SandboxExecutor` in `engine/executor.go`
4. Implement `flowgent sandbox` CLI subcommand
5. Implement `LocalSandboxRunner` (Docker-based)
6. Implement K8s sandbox pod launching in `KubernetesScheduler`
7. JobManager subflow inspection for sandbox detection
8. E2E test: skill with Python script executes in sandbox

### Phase 4: Output Validation (optional enhancement)
1. If `output_schema` is set on agent/skill, validate LLM/flow output
2. Validation errors → structured error messages

---

## Part F: Design Decisions

| Decision | Rationale |
|----------|----------|
| Skills = sub-AgentFlow + `kind: skill` | Zero new abstractions. Reuse `SubflowExecutor`, Store, DAG engine. |
| `input_schema` / `output_schema` optional | Frictionless migration. Users drop existing skills without writing schemas. |
| No toolsets on AgentDef | Tools are deterministic DAG nodes. Agent autonomy over tool selection undermines enterprise auditability. |
| `output_schema` on AgentDef | Structured output contract replaces prose in `instruction`. Machine-readable, validatable, A2A-discoverable. |
| `load-dir` instead of inline/paths | Single pattern for agents, skills, flows. K8s ConfigMap-friendly. Version-control-friendly. |
| `StaticResourceCfg` shared struct | Same `enabled`/`load-dir`/`refresh` for all three resource types. DRY. |
| Sandbox as separate subcommand + K8s pod | Untrusted scripts must be isolated. Elastic scaling — sandbox pods only exist during skill execution. JobManager decides dynamically based on subflow content. |
| `01-` file prefix convention | Human-readable ordering. Loader sorts alphabetically. No semantic meaning beyond ordering. |
