# Flowgent

Building the next generation of predictable, auditable, distributed, constrained enterprise-grade super-agents — with native AI economic layer (x402/MPP).

> Flowgent is an AI-native universal orchestration engine. It deeply integrates LLM agent intelligence with deterministic DAG execution — preserving the predictability, reliability, and auditability of traditional workflows while empowering distributed enterprise super-agents with bounded, governable autonomy. Native AI-to-AI payment protocol (x402/MPP) support makes inter-agent service calls measurable, settleable, and governable.

---

## Features

- **DAG Topological Scheduler**
Predictable execution order with concurrent fan-out via `map` nodes. Process 100+ repos in parallel with bounded goroutine pools.

- **9 Node Types**
`agent`, `tool`, `map`, `agentflow`, `condition`, `tribunal`, `human`, `supervisor`, `noop`. Compose any orchestration topology.

- **Deterministic + Intelligent**
`agent` and `supervisor` nodes provide LLM intelligence. All other nodes guarantee deterministic execution. You decide where intelligence lives.

- **Controlled Autonomy**
Supervisor constrained to 5 actions (`continue`, `redirect`, `retry`, `inject`, `abort`) with configurable quotas. LLM-powered but never unbounded.

- **State-Machine Persistence**
Every run and task is durable. Pause at any `human` approval gate, resume via API, replay idempotently.

- **A2A Protocol Server**
External AI systems dynamically call Flowgent via Google A2A protocol. Discover agentflows, trigger runs, query results programmatically.

- **Optional Economic Layer**
x402 payment protocol client-side support with spending policies, wallet abstraction, and human approval governance. Coinbase facilitator integration.

- **Dual-Mode Deployment**
**All-in-One:** SQLite + memory queue (single binary, no dependencies). **Distributed:** PostgreSQL + MQTT (EMQX) + Kubernetes.

- **OTEL Tracing Per Node**
Every node span records input, output, and internal state. Debug any execution path in Jaeger.

- **Cron + Webhook Triggers**
Schedule-based and event-driven (GitHub/GitLab webhook) per agentflow.

- **Multi-Provider LLM**
OpenAI-compatible adapter with per-provider rate limiting, SOCKS/HTTP proxy, modalities, and extended thinking.

- **MCP Ecosystem**
Default built-in 5 stdio MCP servers: GitHub, SonarQube, Sonatype IQ, Nexus3, Test (Maven/Cucumber).

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

Produces one unified binary + 5 MCP servers under `bin/`:

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

# Start individual components
./bin/flowgent apiserver     # REST API only
./bin/flowgent a2a           # A2A protocol only
./bin/flowgent wallet        # Wallet key-management daemon
./bin/flowgent console       # Interactive management console

# With custom config + debug logging
./bin/flowgent -v --config /etc/flowgent/production.yaml daemon start
```

REST API on `:9999` · A2A on `:9992` · Wallet on `:9901` · pprof on `:9991`

### Verify

```bash
curl http://localhost:9999/_/healthz                    # {"status":"ok"}
curl http://localhost:9999/_/openapi.yaml               # OpenAPI 3.1 spec
curl http://localhost:9992/.well-known/agent.json       # A2A agent card
curl http://localhost:9901/health                       # Wallet health
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

### CLI Help

```bash
./bin/flowgent --help              # Top-level commands
./bin/flowgent daemon --help       # Daemon subcommands (start/stop/restart)
./bin/flowgent wallet --help       # Wallet options (--listen, --master-key, --generate-key)
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
go build -o bin/flowgent ./src/cmd/core && ./bin/flowgent daemon start

# Specific components
./bin/flowgent apiserver
./bin/flowgent wallet

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
