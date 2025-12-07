# Flowgent

Build deterministic workflows powered by autonomous AI agents.

> Flowgent is an AI-native Universality orchestration engine that combines the adaptive intelligence of LLM agents with the predictability, traceability, and reliability of deterministic DAG execution.

---

## Highlights

| Feature | Why It Matters |
|---------|----------------|
| Dynamic + Deterministic | `agent` and `supervisor` nodes provide LLM intelligence; `tool`, `map`, `condition`, `tribunal`, `human`, `noop` nodes guarantee deterministic execution. You decide where intelligence lives. |
| DAG Topological Scheduler | Predictable execution order with concurrent fan-out (`map` nodes) — process 100+ repos in parallel with bounded goroutine pools |
| Controlled Autonomy | Supervisor is constrained to exactly 5 actions (`continue` / `redirect` / `retry` / `inject` / `abort`) with configurable quotas — LLM-powered but never unbounded |
| A2A Protocol Server | External AI systems can dynamically call Flowgent via Google A2A protocol on a dedicated port — discover agentflows, trigger runs, and query results programmatically |
| State-Machine Persistence | Every run and task is durable; pause at any `human` approval gate, resume via API, replay idempotently |
| Dual-Mode Deployment | **All-in-One:** SQLite + memory queue (single binary, zero dependencies). **Distributed:** PostgreSQL + MQTT (EMQX) + Kubernetes |
| OTEL Tracing Per Node | Every node span records input, output, and internal state — debug any execution path in Jaeger |
| 9 Node Types | `agent`, `tool`, `map`, `agentflow`, `condition`, `tribunal`, `human`, `supervisor`, `noop` — compose any orchestration topology |
| Cron + Webhook Triggers | Schedule-based and event-driven (GitHub/GitLab webhook) per agentflow |
| Multi-Provider LLM | OpenAI-compatible adapter with per-provider rate limiting, SOCKS/HTTP proxy, modalities, and extended thinking |
| MCP Ecosystem | 5 stdio MCP servers: GitHub, SonarQube, Sonatype IQ, Nexus3, Test (Maven/Cucumber) |
| OAS 3.1 + Swagger | Full REST API spec and Swagger UI out of the box |

---

## Quick Install

### Prerequisites

- **Go 1.25+**
- (Optional) PostgreSQL 15+ for distributed mode
- (Optional) EMQX 5.x for MQTT distributed queue

### Build

```bash
git clone git@github.com:flowgent-labs/flowgent.git && cd flowgent
make build
```

Produces one main binary + 5 MCP servers under `bin/`:

```
bin/
├── flowgent
├── mcp-server-github
├── mcp-server-sonarqube
├── mcp-server-sonatypeiq
├── mcp-server-nexus3
└── mcp-server-test
```

### Run

```bash
# All-in-one mode (SQLite — zero external dependencies)
./bin/flowgent daemon start

# With custom config + debug logging
export FLOWGENT_CONFIG_FILE=/etc/flowgent/production.yaml
./bin/flowgent -v daemon start
```

REST API on `:9999` · A2A on `:9992` · pprof on `:9991`

### Verify

```bash
curl http://localhost:9999/_/healthz                    # {"status":"ok"}
curl http://localhost:9999/_/openapi.yaml               # OpenAPI 3.1 spec
curl http://localhost:9992/.well-known/agent.json       # A2A agent card
```

### Interactive Console

```bash
./bin/flowgent console

flowgent> list agentflows
flowgent> list runs
flowgent> show run <id>
flowgent> tasks <run-id>
flowgent> exit
```

---

## Developer Quickstart

```bash
make build        # Compile all binaries
make test         # Run all tests
make fmt          # Format source
```

### Run Individual Components

```bash
# Main server
go build -o bin/flowgent ./src/cmd/server && ./bin/flowgent daemon start

# A specific MCP server
go build -o bin/mcp-server-github ./src/cmd/mcp-server-github
GITHUB_TOKEN=xxx ./bin/mcp-server-github
```

### Add a Node Type

1. Add constant in `src/model/node.go`
2. Register in `src/engine/executor.go` → `executeNode` switch
3. LLM node → provide `soul` + `instruction` in YAML; deterministic → pure Go

### Add an MCP Tool

1. Create `src/cmd/mcp-server-<name>/main.go` (stdio MCP pattern)
2. Register in `etc/flowgent.yaml` → `orchestration.mcps`
3. Reference via `type: tool` + `tool: <name>` in any L2 agentflow

### Config Resolution

```
-c/--config flag  >  $FLOWGENT_CONFIG_FILE  >  etc/flowgent.yaml
```

---

## Architecture

```
Trigger Layer (schedule / webhook)
        ↓
Supervisor (Control Plane — LLM, constrained)
        ↓
DAG Executor (topological scheduler + dataflow)
        ↓
Nodes (agent / tool / map / agentflow / condition / tribunal / human / supervisor / noop)
```

| Plane | Nodes | Behaviour |
|-------|-------|-----------|
| **Data Plane** | tool, map, agentflow, condition, tribunal, human, noop | Deterministic |
| **Control Plane** | agent, supervisor | LLM-powered, constrained |

---

## Enterprise Agentflows (L2 Examples)

### Security Autonomy Fixer — 11-phase closed-loop remediation

```
4 parallel scans → LLM triage → nested map fan-out fix → 3-agent review →
majority tribunal → supervisor safety gate → human approval (24h timeout) →
commit & PR → multi-channel notify
```

→ `etc/sample-security-autonomy-fixer.yaml`

### AutoTest Generation — Confluence-to-Cucumber pipeline

```
Confluence fetch → requirement extraction → test planning per project type →
Cucumber .feature + step definitions (Spring Boot / Flask / React) →
review → tribunal → commit & PR → notify
```

→ `etc/sample-autotest-generation.yaml`

---

## Deployment Modes

| Mode | Storage | Queue | Target |
|------|---------|-------|--------|
| **All-in-One** | SQLite | Memory | Local dev, single node |
| **Distributed** | PostgreSQL | MQTT (EMQX) | Kubernetes cluster |

---

## API

| Interface | Port | Spec |
|-----------|------|------|
| REST API | `:9999` | OAS 3.1 (`/_/openapi.yaml`) + Swagger UI |
| A2A Protocol | `:9992` | Google Agent-to-Agent (`/.well-known/agent.json`) |
| Management | `:9991` | pprof (`/debug/pprof/`) |

---

## License

See [LICENSE](LICENSE).
