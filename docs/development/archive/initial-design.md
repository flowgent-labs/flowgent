# Flowgent — Initial Design Draft (Original Human-Authored)

**Date:** 2026-05-10
**Status:** Original spec capturing raw requirements and design ideas before implementation.

## 🎯 目标

如下是实现 Flowgent 企业级通用自主灵活可预测的 **AI Agents Orchestration Engine (Layer1)** 核心设计与要求，它包括但不限于：

- DAG Scheduler（并发、依赖、拓扑调度）+ dataflow
- 多 agent 协作（LLM）
- deterministic 控制节点（vote / condition）
- supervisor 控制平面（非确定性但受控）—- 决策（redirect / inject / retry / abort）
- human approval（可恢复）—- human 节点（无 UI，阻塞等待外部事件/API）
- 多 repo 并发处理（100+）
- MCP 工具集成
- webhook + schedule 触发
- 分布式 worker（ (emqx) mqtt queue + k8s）与本地 all-in-one 模式 (memory queue + docker)
- Postgres schema

> 约定：
> 
> - 所有 agent 输出 JSON
> - vote / condition / tool / map / workflow / human 为 deterministic（除 supervisor、agent）
> - Supervisor 输出严格 JSON schema（见下）
> - 变量解析使用简单 JSONPath（`${node.field}`）

其中 layer2 是具体真实应用 yaml orchestration 预定义配置(自主探测github ee pr latest commit scans issues，分析并修复和review投票，最后提交)：

---

## 🧠 核心架构

```
Trigger Layer (schedule/event-driven webhook)
        ↓
Supervisor (Control Plane)
        ↓
Workflow Engine (DAG Executor)
        ↓
Nodes (agent/tool/map/workflow/vote/human/condition)
```

---

## 🧠 核心思想

1. Data Plane（确定性执行）
    - DAG + dataflow
    - 节点：agent/tool/map/workflow/condition/vote/human/noop
    - map = 并发 fan-out（可嵌套）
    - vote/condition 必须 deterministic
2. Control Plane（可控自治）
    - supervisor 节点（LLM）
    - 仅允许：redirect / inject / retry / abort
    - 受限执行（max_injections / max_nodes / allowed_actions）
3. 状态机（Temporal 风格）
    - WorkflowRun / TaskRun 持久化
    - 每个 node 执行 = 一个 TaskRun
    - 所有状态可恢复（resume-safe）
4. 调度模型
    - 拓扑调度（ready queue）
    - 并发 worker（goroutine / 分布式 queue）
    - map = 子任务批量入队
5. Human 节点
    - 挂起（WAITING_HUMAN）
    - 外部 API 唤醒（approve/reject）
    - 无 UI 依赖
6. 分布式
    - Postgres + Queue（pg 或 Redis）
    - 多 worker 横向扩展（k8s）
    - 本地模式：单进程 + 内存队列

---

## ⚙️ Node 类型（必须实现）

```
agent        # LLM节点（唯一有智能）
tool         # MCP调用（无LLM）
map          # fan-out并发
workflow     # 子图（递归）
condition    # 表达式分支（deterministic）
vote         # 投票（deterministic）
human        # 人工审批（阻塞+恢复）
supervisor   # 控制节点（LLM，但受限）
noop         # 空节点
```

---

## ❗关键设计约束（必须遵守）

## 1️⃣ LLM边界

```
只有 agent 和 supervisor 使用 LLM
其他节点必须 deterministic
```

---

## 2️⃣ vote 必须 deterministic

```
LLM负责“判断”，vote负责“裁决”
```

vote 输入示例：

```json
{
  "decision": true,
  "confidence": 0.8,
  "risk_level": "medium",
  "reason": "..."
}
```

vote 逻辑（示例）：

```go
if approveCount >= majority {
    decision = true
}
```

---

## 3️⃣ 所有 agent 输出必须 JSON

禁止自由文本输出。

---

## 4️⃣ human 节点必须支持

- 持久化（DB）
- resume（API / webhook）
- timeout

---

## 5️⃣ supervisor 必须受控

输出必须：

```json
{
  "action": "continue|redirect|retry|inject|abort",
  "target": "node-id",
  "reason": "string"
}
```

禁止：

- 任意执行工具
- 无限扩展 DAG

---

## 6️⃣ map 必须支持

- 并发控制（goroutine pool）
- 嵌套 map（multi-level fan-out）

---

源码目录

```bash
./src/internal/

api
cache/cache.go,memory.go,redis.go
config
engine
memory
model
queue/queue.go,pgqueue.go,memory.go,mqtt.go
store/store.go,sqlite_store.go,postgre_store.go
util
web
worker
```

## src/internal/model/config.go

```go
package model

type AgentDef struct {
	Name        string `json:"name" yaml:"name"`
	Model       string `json:"model" yaml:"model"`
	Soul        string `json:"soul" yaml:"soul"`
	Instruction string `json:"instruction" yaml:"instruction"`
}

type LLMProviderDef struct {
	Endpoint    string            `json:"endpoint" yaml:"endpoint"`
	Credentials map[string]string `json:"credentials" yaml:"credentials"`
	Proxy       string            `json:"proxy" yaml:"proxy"` // e.g. "http://proxy:8080" or "socks5h://127.0.0.1:1080"
	Models      []ModelDef        `json:"models" yaml:"models"`
}

type ModelDef struct {
	Name        string          `json:"name" yaml:"name"`
	Temperature float64         `json:"temperature" yaml:"temperature"`
	TopK        int             `json:"topk" yaml:"topk"`
	Modalities  []string        `json:"modalities" yaml:"modalities"` // "text", "audio", "image"
	Thinking    *ThinkingConfig `json:"thinking" yaml:"thinking"`     // Extended thinking / chain-of-thought
}

// ThinkingConfig enables extended chain-of-thought for reasoning models.
type ThinkingConfig struct {
	Type         string `json:"type" yaml:"type"` // "enabled" | "disabled"
	BudgetTokens int    `json:"budget_tokens" yaml:"budget_tokens"`
}

type MCPDef struct {
	Name    string            `json:"name" yaml:"name"`
	Enabled bool              `json:"enabled" yaml:"enabled"`
	Command []string          `json:"command" yaml:"command"`
	Args    []string          `json:"args" yaml:"args"`
	Env     map[string]string `json:"env" yaml:"env"`
}

type ServiceConfig struct {
	ServiceName   string              `json:"service-name" yaml:"service-name"`
	Server        ServerConfig        `json:"server" yaml:"server"`
	Mgmt          MgmtConfig          `json:"mgmt" yaml:"mgmt"`
	Logging       LoggingConfig       `json:"logging" yaml:"logging"`
	Auth          AuthConfig          `json:"auth" yaml:"auth"`
	Cache         CacheConfig         `json:"cache" yaml:"cache"`
	AppDB         AppDBConfig         `json:"appdb" yaml:"appdb"`
	LLM           LLMConfig           `json:"llm" yaml:"llm"`
	Orchestration OrchestrationConfig `json:"orchestration" yaml:"orchestration"`
}

type ServerConfig struct {
	Host            string `json:"host" yaml:"host"`
	Port            int    `json:"port" yaml:"port"`
	ContextPath     string `json:"context-path" yaml:"context-path"`
	ShutdownTimeout string `json:"shutdown-timeout" yaml:"shutdown-timeout"`
	MaxBodyBytes    int    `json:"max-body-bytes" yaml:"max-body-bytes"`
	ReadTimeout     string `json:"read-timeout" yaml:"read-timeout"`
	WriteTimeout    string `json:"write-timeout" yaml:"write-timeout"`
}

type MgmtConfig struct {
	Enabled bool        `json:"enabled" yaml:"enabled"`
	Host    string      `json:"host" yaml:"host"`
	Port    int         `json:"port" yaml:"port"`
	PProf   PProfConfig `json:"pprof" yaml:"pprof"`
	OTEL    OTELConfig  `json:"otel" yaml:"otel"`
}

type PProfConfig struct {
	Enabled    bool   `json:"enabled" yaml:"enabled"`
	ServerBind string `json:"server-bind" yaml:"server-bind"`
}

type OTELConfig struct {
	Enabled  bool   `json:"enabled" yaml:"enabled"`
	Endpoint string `json:"endpoint" yaml:"endpoint"`
	Protocol string `json:"protocol" yaml:"protocol"`
	Timeout  int    `json:"timeout" yaml:"timeout"`
}

type LoggingConfig struct {
	Mode  string `json:"mode" yaml:"mode"`
	Level string `json:"level" yaml:"level"`
}

type AuthConfig struct {
	JWTValidityAK  int              `json:"jwt-validity-ak" yaml:"jwt-validity-ak"`
	JWTValidityRK  int              `json:"jwt-validity-rk" yaml:"jwt-validity-rk"`
	JWTAlgorithm   string           `json:"jwt-algorithm" yaml:"jwt-algorithm"`
	JWTPrivateKey  string           `json:"jwt-private-key" yaml:"jwt-private-key"`
	JWTPublicKey   string           `json:"jwt-public-key" yaml:"jwt-public-key"`
	AnonymousPaths []string         `json:"anonymous-paths" yaml:"anonymous-paths"`
	OIDC           OIDCConfig       `json:"oidc" yaml:"oidc"`
	GitHub         GitHubAuthConfig `json:"github" yaml:"github"`
}

type OIDCConfig struct {
	Enabled     bool   `json:"enabled" yaml:"enabled"`
	ClientID    string `json:"client-id" yaml:"client-id"`
	IssueURL    string `json:"issue-url" yaml:"issue-url"`
	RedirectURL string `json:"redirect-url" yaml:"redirect-url"`
	Scope       string `json:"scope" yaml:"scope"`
}

type GitHubAuthConfig struct {
	Enabled      bool   `json:"enabled" yaml:"enabled"`
	ClientID     string `json:"client-id" yaml:"client-id"`
	ClientSecret string `json:"client-secret" yaml:"client-secret"`
	AuthURL      string `json:"auth-url" yaml:"auth-url"`
	TokenURL     string `json:"token-url" yaml:"token-url"`
	RedirectURL  string `json:"redirect-url" yaml:"redirect-url"`
	Scope        string `json:"scope" yaml:"scope"`
	UserInfoURL  string `json:"user-info-url" yaml:"user-info-url"`
}

type CacheConfig struct {
	Provider string            `json:"provider" yaml:"provider"`
	Memory   MemoryCacheConfig `json:"memory" yaml:"memory"`
	Redis    RedisCacheConfig  `json:"redis" yaml:"redis"`
}

type MemoryCacheConfig struct {
	InitialCapacity int    `json:"initial-capacity" yaml:"initial-capacity"`
	MaxCapacity     int    `json:"max-capacity" yaml:"max-capacity"`
	TTL             int    `json:"ttl" yaml:"ttl"`
	EvictionPolicy  string `json:"eviction-policy" yaml:"eviction-policy"`
}

type RedisCacheConfig struct {
	Nodes             []string `json:"nodes" yaml:"nodes"`
	Username          string   `json:"username" yaml:"username"`
	Password          string   `json:"password" yaml:"password"`
	ConnectionTimeout int      `json:"connection-timeout" yaml:"connection-timeout"`
	ResponseTimeout   int      `json:"response-timeout" yaml:"response-timeout"`
	Retries           int      `json:"retries" yaml:"retries"`
	MaxRetryWait      int      `json:"max-retry-wait" yaml:"max-retry-wait"`
	MinRetryWait      int      `json:"min-retry-wait" yaml:"min-retry-wait"`
	ReadFromReplica   bool     `json:"read-from-replica" yaml:"read-from-replica"`
}

type AppDBConfig struct {
	Type     string         `json:"type" yaml:"type"`
	SQLite   SQLiteConfig   `json:"sqlite" yaml:"sqlite"`
	Postgres PostgresConfig `json:"postgres" yaml:"postgres"`
}

type SQLiteConfig struct {
	Dir string `json:"dir" yaml:"dir"`
}

type PostgresConfig struct {
	Host           string `json:"host" yaml:"host"`
	Port           int    `json:"port" yaml:"port"`
	Database       string `json:"database" yaml:"database"`
	Schema         string `json:"schema" yaml:"schema"`
	Username       string `json:"username" yaml:"username"`
	Password       string `json:"password" yaml:"password"`
	MinConnections int    `json:"min-connections" yaml:"min-connections"`
	MaxConnections int    `json:"max-connections" yaml:"max-connections"`
	UseSSL         bool   `json:"use-ssl" yaml:"use-ssl"`
}

type LLMConfig struct {
	Providers      map[string]LLMProviderDef `json:"providers" yaml:"providers"`
	RequestTimeout string                    `json:"request-timeout" yaml:"request-timeout"`
	RateLimit      map[string]int            `json:"rate-limit" yaml:"rate-limit"` // provider → max requests per minute
}

type OrchestrationConfig struct {
	MCPs                 []MCPDef     `json:"mcps" yaml:"mcps"`
	Agents               []AgentDef   `json:"agents" yaml:"agents"`
	AgentFlows           AgentFlowCfg `json:"agentflows" yaml:"agentflows"`
	MaxConcurrentFlows   int          `json:"max-concurrent-flows" yaml:"max-concurrent-flows"`
	FlowExecutionTimeout string       `json:"flow-execution-timeout" yaml:"flow-execution-timeout"`
	MaxNodeRetries       int          `json:"max-node-retries" yaml:"max-node-retries"`
}

// AgentFlowCfg configures agentflow discovery sources.
type AgentFlowCfg struct {
	Static   StaticAgentFlowCfg   `json:"static" yaml:"static"`
	Standard StandardAgentFlowCfg `json:"standard" yaml:"standard"`
}

// StaticAgentFlowCfg configures file-based agentflow discovery.
type StaticAgentFlowCfg struct {
	Enabled bool     `json:"enabled" yaml:"enabled"`
	Refresh string   `json:"refresh" yaml:"refresh"`
	Paths   []string `json:"paths" yaml:"paths"`
}

// StandardAgentFlowCfg configures DB-backed agentflow discovery (UI drag-and-drop).
type StandardAgentFlowCfg struct {
	Enabled bool `json:"enabled" yaml:"enabled"`
}

type AppConfig struct {
	Service  ServiceConfig
	Flows    []WorkflowSpec
	SubFlows map[string]WorkflowSpec
}

func (c *AppConfig) GetAgent(name string) *AgentDef {
	for i := range c.Service.Orchestration.Agents {
		a := &c.Service.Orchestration.Agents[i]
		if a.Name == name {
			return a
		}
	}
	return nil
}

func (c *AppConfig) GetMCP(name string) *MCPDef {
	for i := range c.Service.Orchestration.MCPs {
		m := &c.Service.Orchestration.MCPs[i]
		if m.Name == name && m.Enabled {
			return m
		}
	}
	return nil
}

func (c *AppConfig) GetFlow(id string) *WorkflowSpec {
	for i := range c.Flows {
		if c.Flows[i].ID == id {
			return &c.Flows[i]
		}
	}
	if sw, ok := c.SubFlows[id]; ok {
		return &sw
	}
	return nil
}

func (c *AppConfig) GetModel(provider string) string {
	if strings, ok := c.Service.LLM.Providers[provider]; ok {
		if len(strings.Models) > 0 {
			return strings.Models[0].Name
		}
	}
	return ""
}

```

---

## 📦 完整配置：etc/flowgent.yaml

```yaml
service-name: flowgent

server:
  host: 0.0.0.0
  port: 9999
  context-path: "/"
  shutdown-timeout: 15s
  max-body-bytes: 10485760
  read-timeout: 30s
  write-timeout: 60s

mgmt:
  enabled: true
  host: 0.0.0.0
  port: 9991
  pperf:
    enabled: true
    server-bind: "0.0.0.0:6669"
  # Notice: More OTEL custom configuration use to env: OTEL_SPAN_xxx
  otel:
    enabled: true
    endpoint: "http://localhost:4317"
    protocol: grpc # Optional: http/protobuf,http/json,grpc
    timeout: 10000

logging:
  mode: JSON # Options: HUMAN|JSON
  level: DEBUG

auth:
  jwt-validity-ak: 3600
  jwt-validity-rk: 86400
  jwt-algorithm: "ES256"
  jwt-private-key: "<YOUR_PRIVATE_KEY>"
  jwt-public-key: "<YOUR_PUBLIC_KEY>"
  anonymous-paths:
    - "/public/**"
    - "/static/**"
    - "/_/healthz"
    - "/_/healthz/**"
    - "/swagger-ui/**"
  oidc:
    enabled: false
    client-id: "<YOUR_OIDC_CLIENT_ID>"
    issue-url: "https://iam.wl4g.com/realms/master"
    redirect-url: "http://wl4g.local:10000/serve/auth/callback/oidc"
    scope: "openid profile email"
  github:
    enabled: true
    auth-url: "https://github.com/login/oauth/authorize"
    token-url: "https://github.com/login/oauth/access_token"
    redirect-url: "http://wl4g.local:10000/serve/auth/callback/github"
    scope: "user"
    user-info-url: "https://api.github.com/user"

cache:
  provider: Redis # Memory|Redis Cluster
  memory:
    initial-capacity: 32
    max-capacity: 65535
    ttl: 3600000
    eviction-policy: LRU
  redis:
    nodes: ["redis://127.0.0.1:6379"]
    username: "default"
    password: "bitnami"
    connection-timeout: 3000
    response-timeout: 6000
    retries: 1
    max-retry-wait: 65536
    min-retry-wait: 1280
    read-from-replica: true

appdb:
  type: "POSTGRE" # SQLITE|POSTGRE
  sqlite:
    dir: "~/.flowgent/sqlite"
  postgres:
    host: "127.0.0.1"
    port: 5432
    database: "flowgent"
    schema: "public"
    username: postgres
    password: "changeit"
    min-connections: 2
    max-connections: 20
    use-ssl: false

llm:
  # Default timeout for LLM API calls
  request-timeout: 120s
  # Rate limiting: max requests per minute per provider
  rate-limit:
    bailian-codeplan: 60
  providers:
    bailian-codeplan:
      endpoint: https://coding.dashscope.aliyuncs.com/apps/xxxx
      credentials:
        api-key: sk-xxxx
        #api-key-file: /path/to/.credentials
      models:
        - name: qwen3.6-plus
          temperature: 0.3
          topk: 5
          modalities:
            input: [text]
            output: [text]
          thinking:
            enabled: true
            budget-tokens: 8192

        - name: qwen3.5-coder
          temperature: 0.3
          topk: 5
          modalities:
            input: [text]
            output: [text]
          thinking:
            enabled: true
            budget-tokens: 8192

orchestration:
  max-concurrent-flows: 10
  flow-execution-timeout: 30m
  max-node-retries: 3
  mcps:
    - name: github
      enabled: true
      type: local
      command: ["sh", "-c", "/bin/github-mcp"]
      args: ["--transport", "stdio", "-v", "10"]
      env:
        #MCP_UPSTREAM_TOKEN: Xxx
        MCP_UPSTREAM_TOKEN_FILE: /path/to/.credentials

    - name: sonarqube
      enabled: true
      type: local
      command: ["sh", "-c", "/bin/sonarqube-mcp"]
      args: ["--transport", "stdio", "-v", "10"]
      env:
        #MCP_UPSTREAM_TOKEN: Xxx
        MCP_UPSTREAM_TOKEN_FILE: /path/to/.credentials

    - name: sonatype-iq
      enabled: true
      type: local
      command: ["sh", "-c", "/bin/sonatype-iq-mcp"]
      args: ["--transport", "stdio", "-v", "10"]
      env:
        #MCP_UPSTREAM_TOKEN: Xxx
        MCP_UPSTREAM_TOKEN_FILE: /path/to/.credentials
        
    - name: sonatype-nexus3
      enabled: true
      type: local
      command: ["sh", "-c", "/bin/sonatype-nexus3-mcp"]
      args: ["--transport", "stdio", "-v", "10"]
      env:
        #MCP_UPSTREAM_TOKEN: Xxx
        MCP_UPSTREAM_TOKEN_FILE: /path/to/.credentials

  agents:
    - name: supervisor
      model: bailian-codeplan/qwen3.6-plus
      soul: |-
        You are a senior autonomous orchestration controller.
        You must optimize workflow execution while ensuring safety, correctness, and auditability.
      instruction: |-
        You may:
        - redirect flow
        - request retry
        - inject additional review
        - abort unsafe execution

        You MUST NOT:
        - execute tools directly
        - modify system outside workflow
        - generate unstructured output

        Output STRICT JSON:
        {
          "action": "continue|redirect|retry|inject|abort",
          "target": "node-id",
          "reason": "string"
        }

    - name: issue-detector
      model: bailian-codeplan/qwen3.6-plus
      soul: |-
        You are a senior DevSecOps expert specializing in SAST/DAST/FOSS analysis.
      instruction: |-
        You MUST:
        - Parse scan results
        - Normalize issues
        - Deduplicate
        - Assign severity

        You MUST NOT:
        - Generate fixes
        - Output free text

        Output STRICT JSON:
        {
          "issues": [
            {
              "id": "string",
              "repo": "string",
              "severity": "high|medium|low",
              "type": "sast|dast|dependency",
              "file": "string",
              "description": "string"
            }
          ]
        }

    - name: fixer-agent
      model: bailian-codeplan/qwen3.5-coder
      soul: |-
        You are a secure coding expert.
      instruction: |-
        You MUST fix vulnerabilities safely.
        You MUST NOT break business logic.

        Output STRICT JSON:
        {
          "patches": [
            {
              "file": "string",
              "patch": "diff content"
            }
          ]
        }

    - name: security-reviewer
      model: bailian-codeplan/qwen3.6-plus
      soul: |-
        You are a strict security reviewer.
      instruction: |-
        Output JSON:
        {
          "decision": true|false,
          "confidence": 0-1,
          "risk_level": "low|medium|high",
          "reason": "string"
        }

    - name: quality-reviewer
      model: bailian-codeplan/qwen3.6-plus
      soul: |-
        You are a code quality reviewer.

    - name: arch-reviewer
      model: bailian-codeplan/qwen3.6-plus
      soul: |-
        You are an architecture reviewer.

    - name: git-agent
      model: bailian-codeplan/qwen3.5-coder
      soul: |-
        You handle git operations.

  agentflows:
    static:
      enabled: true
      refresh: 1m
      paths:
        - sample-security-autonomy-fixer.yaml
    standard:
      enabled: false  # DB-backed agentflows (UI drag-and-drop)
```

---

## 📄 Workflow：etc/sample-security-autonomy-fixer.yaml

```yaml
version: "1.0"
id: security-autonomy-fixer
description: |
  Simulates a complete developer remediation workflow for security vulnerabilities:
  1. DISCOVERY — Fetches the latest commit for each target repository and runs
     three independent scans: SonarQube (SAST/DAST), Sonatype IQ (FOSS dependency
     vulnerabilities), and Sonatype Nexus3 (artifact-level compliance). This mirrors
     a developer who runs local scans before opening a PR.
  2. ANALYZE — An LLM agent (issue-detector) parses, normalizes, deduplicates, and
     assigns severity to all findings, just as a developer would triage scan reports.
  3. FIX — For each issue, a sub-workflow (sub-fix) delegates to a fixer-agent to
     analyze the root cause and generate a secure patch, without breaking business
     logic. Nested map nodes allow parallel processing of multiple issues per repo.
  4. REVIEW — Three independent agents (security, quality, architecture) review the
     proposed fixes in parallel, simulating a multi-expert peer-review process.
  5. VOTE — A deterministic majority vote decides whether to proceed, reflecting a
     team consensus before merging risky changes.
  6. SUPERVISOR — A supervisor agent monitors execution for safety, with bounded
     retries, node injection limits, and the ability to abort unsafe runs.
  7. CONDITIONAL — Based on the vote outcome, the workflow either proceeds to human
     approval or loops back to re-generate fixes.
  8. HUMAN APPROVAL — An async approval gate (24h timeout) ensures a real developer
     signs off before any code is pushed.
  9. COMMIT & PR — Creates a dedicated branch, pushes the patches, and opens a
     pull request with a clear security-fix title — exactly what a developer would
     do after completing their remediation work.
  10. REPORT — An agent compiles a final summary report: issues found, fixes applied,
      PR links, and scan results — just as a developer would document their work.
  11. NOTIFY — Multi-channel parallel notification: PR comment on GitHub, email to
      security team, and webhook push to Microsoft Teams — ensuring all stakeholders
      are informed regardless of which channel they monitor.
	
# ─── Triggers ──────────────────────────────────────────────
triggers:
  - type: schedule
    cron: "0 */6 * * *"  # Every 6 hours
	
  - type: webhook
    provider: github
    events: [push, pull_request]
	
  - type: webhook
    provider: gitlab
    events: [push, merge_request]
	
# Runtime variables - populated by trigger or API call
vars:
  repos:
    - "org/repo1"
    - "org/repo2"
  # Notifier targets
  notify_email_to: "security-team@company.com"
  notify_webhook_url: "https://outlook.office.com/webhook/xxxxx"
	
nodes:
  # ┌─────────────────────────────────────────────────┐
  # PHASE 1: DISCOVERY - Get commits + scan repos     │
  # └─────────────────────────────────────────────────┘
  - id: get-commit
    type: tool
    tool: github
    input:
      action: "get_latest_commit"
      repo: "${item}"
      branch: "main"
	
  - id: scan-sonarqube
    type: tool
    tool: sonarqube
    input:
      action: "get_issues"
      project_key: "${item}"
      branch: "main"
	
  - id: scan-sonatypeiq
    type: tool
    tool: sonatype-iq
    input:
      action: "get_jobs_by_commit"
      repo: "${item}"
      commit_sha: "${get-commit.commit_sha}"
	
  - id: scan-nexus3
    type: tool
    tool: sonatype-nexus3
    input:
      action: "get_foss_solution"
      repo: "${item}"
	
  # ┌─────────────────────────────────────────────────┐
  # PHASE 2: ANALYZE - Map scan results to repos      │
  # └─────────────────────────────────────────────────┘
  - id: aggregate-issues
    type: map
    source: "${vars.repos}"
    concurrency: 5
    node:
      type: agent
      agent: issue-detector
      input:
        repo: "${item}"
        sonarqube_issues: "${scan-sonarqube.issues}"
        sonatypeiq_issues: "${scan-sonatypeiq.issues}"
	
  # ┌─────────────────────────────────────────────────┐
  # PHASE 3: FIX - Generate patches for each issue    │
  # └─────────────────────────────────────────────────┘
  - id: generate-fixes
    type: map
    source: "${aggregate-issues.results}"
    concurrency: 5
    node:
      type: map
      source: "${item.issues}"
      node:
        type: workflow
        workflow: sub/sub-fix.yaml
	
  # ┌─────────────────────────────────────────────────┐
  # PHASE 4: REVIEW - Multi-agent review              │
  # └─────────────────────────────────────────────────┘
  - id: review-sec
    type: agent
    agent: security-reviewer
	
  - id: review-quality
    type: agent
    agent: quality-reviewer
	
  - id: review-arch
    type: agent
    agent: arch-reviewer
	
  # ┌─────────────────────────────────────────────────┐
  # PHASE 5: VOTE - Deterministic vote                │
  # └─────────────────────────────────────────────────┘
  - id: vote
    type: vote
    strategy:
      type: majority
    input:
      votes:
        - ${review-sec.decision}
        - ${review-quality.decision}
        - ${review-arch.decision}
	
  # ┌─────────────────────────────────────────────────┐
  # PHASE 6: SUPERVISOR - Controlled autonomy         │
  # └─────────────────────────────────────────────────┘
  - id: supervisor-check
    type: supervisor
    agent: supervisor
    supervisor_config:
      max_retries: 3
      max_nodes: 50
      max_injections: 5
      allowed_actions: ["continue", "retry", "inject", "abort"]
	
  # ┌─────────────────────────────────────────────────┐
  # PHASE 7: CONDITIONAL - Route based on vote        │
  # └─────────────────────────────────────────────────┘
  - id: approved
    type: condition
    expression: "${vote.decision == true}"
	
  # ┌─────────────────────────────────────────────────┐
  # PHASE 8: HUMAN - Approval gate                    │
  # └─────────────────────────────────────────────────┘
  - id: human-approval
    type: human
    approval:
      timeout: 24h
      on_approve: continue
      on_reject: to: fix
	
  # ┌─────────────────────────────────────────────────┐
  # PHASE 9: COMMIT - Create PR                       │
  # └─────────────────────────────────────────────────┘
  - id: create-branch
    type: tool
    tool: github
    input:
      action: "create_branch"
      repo: "${item}"
      branch: "security-bot/fix-${timestamp}"
      base_branch: "main"
	
  - id: commit-fixes
    type: tool
    tool: github
    input:
      action: "commit_and_push"
      repo: "${item}"
      branch: "security-bot/fix-${timestamp}"
      changes: "${generate-fixes.results}"
	
  - id: create-pr
    type: tool
    tool: github
    input:
      action: "create_pull_request"
      repo: "${item}"
      title: "[Security Fix] Auto-generated security fix"
      head: "security-bot/fix-${timestamp}"
      base: "main"
      body: "Automatically generated by Flowgent Security Autonomy Fixer"
	
  # ┌─────────────────────────────────────────────────┐
  # PHASE 10: REPORT - Summary report                 │
  # └─────────────────────────────────────────────────┘
  - id: summary-report
    type: agent
    agent: issue-detector
    input:
      workflow_id: "${vars.workflow_id}"
      run_id: "${vars.run_id}"
      issues_found: "${aggregate-issues.results}"
      pr_links: "${create-pr.pr_url}"
      scan_results:
        sonarqube: "${scan-sonarqube}"
        sonatypeiq: "${scan-sonatypeiq}"
        nexus3: "${scan-nexus3}"
	
  # ┌─────────────────────────────────────────────────┐
  # PHASE 11: NOTIFY - Multi-channel notification     │
  # └─────────────────────────────────────────────────┘
  # PR comment notification
  - id: notify-pr
    type: tool
    tool: github
    input:
      action: "create_issue_comment"
      repo: "${item}"
      pr_number: "${create-pr.pr_number}"
      body: "${summary-report.report}"
	
  # Email notification to security team
  - id: notify-email
    type: tool
    tool: github
    input:
      action: "send_email"
      to: "${vars.notify_email_to}"
      subject: "[Security Fix] Remediation report"
      body: "${summary-report.report}"
	
  # Microsoft Teams notification
  - id: notify-teams
    type: tool
    tool: github
    input:
      action: "webhook_notify"
      webhook_url: "${vars.notify_webhook_url}"
      title: "Security Fix Report"
      body: "${summary-report.report}"
	
  - id: end
    type: noop
	
edges:
  # Discovery → Aggregate
  - { from: get-commit, to: aggregate-issues }
  - { from: scan-sonarqube, to: aggregate-issues }
  - { from: scan-sonatypeiq, to: aggregate-issues }
  - { from: scan-nexus3, to: aggregate-issues }
	
  # Aggregate → Fix
  - { from: aggregate-issues, to: generate-fixes }
	
  # Fix → Review (fan-out)
  - { from: generate-fixes, to: review-sec }
  - { from: generate-fixes, to: review-quality }
  - { from: generate-fixes, to: review-arch }
	
  # Review → Vote
  - { from: review-sec, to: vote }
  - { from: review-quality, to: vote }
  - { from: review-arch, to: vote }
	
  # Vote → Supervisor → Condition
  - { from: vote, to: supervisor-check }
  - { from: supervisor-check, to: approved }
	
  # Condition → Human (if approved) or back to fix
  - { from: approved, to: human-approval, condition: true }
  - { from: approved, to: generate-fixes, condition: false }
	
  # Human approval → Commit → PR → Report → Notify (fan-out) → End
  - { from: human-approval, to: create-branch }
  - { from: create-branch, to: commit-fixes }
  - { from: commit-fixes, to: create-pr }
  - { from: create-pr, to: summary-report }
  # Fan-out to all notification channels in parallel
  - { from: summary-report, to: notify-pr }
  - { from: summary-report, to: notify-email }
  - { from: summary-report, to: notify-teams }
  # All notifications → End
  - { from: notify-pr, to: end }
  - { from: notify-email, to: end }
  - { from: notify-teams, to: end }
	
```

---

## 🧩 子流程：sub-fix.yaml

```yaml
workflow:
  id: sub-fix

  nodes:
    - id: analyze
      type: agent
      agent: fixer-agent
      instruction: |
        Analyze the issue only. DO NOT patch.

    - id: patch
      type: agent
      agent: fixer-agent
      instruction: |
        Generate secure patch.

    - id: validate
      type: tool
      tool: sonarqube

  edges:
    - { from: analyze, to: patch }
    - { from: patch, to: validate }
```

---

## 🚀 实现优先级

必须优先实现：

1. DAG executor（拓扑调度）
2. map 并发（goroutine pool）
3. JSONPath 变量解析
4. vote 引擎（deterministic）
5. supervisor hook
6. human 持久化 + resume
7. trigger dispatcher

---

## ✔️ 交付标准

系统必须：

- 可并发处理 ≥100 repos
- 所有节点可追踪（日志 + OTEL）
- workflow 可恢复执行
- 所有 decision 可审计
- UI 可映射 nodes + edges

---

请你完整 review 并深度思考理解实现是否满足需求，共 10 次迭代，同时确保代码高内聚低耦合；
另外我的最终目标不仅仅是 build + e2e 成功，而是要确保 security-autonomy-fixer 真实场景应用，即：[**会真实扫描 github ee pr latest commit 并连接到 sonarqube/sonatypeiq/sonatype nexus3 获取 issues details 并修复然后提交整个闭环跑通**]。 这个真实场景没跑通则禁止停止告诉我做好了，禁止偷懒。

另外 k8s 分布式模式依赖的 mqtt 中间件已启动好，请确保真实跑通如上场景（分别在 memory queue 和 emqx mqtt queue 模式下）

root@k8sm1:~/flowgent/deploy/emqx# dcp logs -f

> Executing external compose provider "/usr/bin/docker-compose". Please refer to the documentation for details. <<<<
> 

Attaching to emqx1
emqx1    | WARNING: Default (insecure) Erlang cookie is in use.
emqx1    | WARNING: Configure node.cookie in /opt/emqx/etc/emqx.conf or override from environment variable EMQX_NODE__COOKIE
emqx1    | WARNING: NOTE: Use the same cookie for all nodes in the cluster.
emqx1    | EMQX_RPC__PORT_DISCOVERY [rpc.port_discovery]: manual
emqx1    | EMQX_NODE__NAME [[node.name](http://node.name/)]: [emqx@10.89.20.5](mailto:emqx@10.89.20.5)
emqx1    | Listener ssl:default on 0.0.0.0:8883 started.
emqx1    | Listener tcp:default on 0.0.0.0:1883 started.
emqx1    | Listener ws:default on 0.0.0.0:8083 started.
emqx1    | Listener wss:default on 0.0.0.0:8084 started.
emqx1    | Listener http:dashboard on :18083 started.
emqx1    | EMQX 5.5.0 is running now!

## 附加要求

1，其中 pg和 SQLite也是分别对应生产分布式模式和all in one 模式，且除了系统user role等数据存储，还要包括 rag agents的memory记忆，请按这个要求具体实现(同样代码要求还是高内聚低耦合)

2，请当前flowgent 系统本身，也应该支持传统 swagger oas3.1 APIs，以及应支持使用 google 官方 a2a  go sdk 构建 server接口，以供企业内部其他 AI Agents系统自主调用，请按要求具体实现(同样代码要求还是高内聚低耦合)

3，请你继续按要求仔细review并迭代10次，然后生成本次代码实现的工作进度md，以用于后续其他agent审查和持续工作其他模块

4，另外请导包路径请干掉 cve-auto-fix，这是我要跑的真实应用案例orchestration 配置(layer2)，而这里要你优先实现的是通用layer1

6，请review迭代100次充分理解透，确保etc/CyberBot.yaml中所有配置项真实且正确实现(这是经过精心设计过的配置(基础设置都是通用web相关的，其实些基础配置来自另一个rust项目的基础通用web部分框架代码，你可参考 /root/sigbot-core/src 翻译为go通用基础代码)，不应该存在未使用的配置)

7，请重命名为 flowgent，同时请结合目前你所深入思考及实现的理解，来总结生成 readme/readme_zh，要务必保持逻辑简洁逻辑清晰但不丢失任何重要信息。如至少包括一句话介绍"Build deterministic workflows powered by autonomous agents."，核心架构及前瞻性(这是一套极度通用agents编排底层引擎，既保留传统workflow可预测性可靠性，又保留ReAct agent动态高度自主性)，安装或部署，企业级真实案例配置，全量可配置表，开发者二次开发指南(请增加以各种新node 或 hook插件开发为主)

8，cron trigger 功能肯定要的啊，请确保如在l2配置sample-security-autonomy-fixer-v1.0.yaml中 trigger 定义。  

9，请确保flowgent.yaml系统主配置里只导入如sample-security-autonomy-fixer-v1.0.yaml这种layer2应用主配置，而在应用主配置里才显示导入 sub workflow yaml

10，若 provider 里注释的 modalities和thinking等如果能实现则放开注释并真实合理实现它们。   

11，请对每种不同 llm.provider 支持配置操作代理 http:// socks5h:// 这种

12，另外请再次彻底review是否所有l1/l2配置都已正确合理高内聚低耦合使用了，以及核心流程e2e测试OK？      

13，企业级分布式追踪监控排障能力：请问是否可以使用google adk-go sdk 抽象的trace能力(或使用otel)，确保layer1 引擎层的任何node流转都可追踪，最终目标是企业级可靠性(任何节点执行路由错误或异常都应该可轻易debug troubleshooting)，确保最终能在如jasper ui上可看到完整的每一步node执行(且包含此时的上下文输入输出内部状态计算等详情哦)

14，另外请将所有配置和代码里使用 workflow关键字的地方都改为 agentflow 以统一术语(保持与传统workflow见名知其区分)

15，请对照 layer2 sample-security-autonomy-fixer.yaml  再增加一个企业级内部 的auto test 代码生成 agents(可根据 confluence 上需求，分析/提炼/整理出高度结构化且详细的开发规划文档，然后基于在每个如 spring boot/flask/react js 等不同前后端类型项目下统一使用 cucumber 实现完整集成测试代码)

---

请确保代码结构高内聚低耦合且逻辑清晰，注释统一用英文，草稿讨论可用中文，每次完成任务后必须总结生成当前进度文档并写入.agent/目录，以供后续其他 agent 可持续迭代开发

---
