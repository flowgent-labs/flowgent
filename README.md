# Flowgent

**Predictable, auditable, distributed enterprise AI agent orchestration — with native economic layer (x402/MPP).**

Flowgent is an AI-native universal orchestration engine modeled after Apache Flink's session/application architecture. It deeply integrates LLM agent intelligence with deterministic DAG execution — preserving the predictability, reliability, and auditability of traditional workflows while empowering distributed enterprise super-agents with bounded, governable autonomy.

---

## Features

- **DAG Topological Scheduler** — predictable execution with concurrent fan-out. 11 node types compose any orchestration topology.
- **Deterministic + Intelligent** — `agent` and `supervisor` nodes provide LLM intelligence; all others guarantee deterministic execution.
- **Controlled Autonomy** — supervisor constrained to `continue|retry|inject|abort` with configurable quotas.
- **State-Machine Persistence** — every run and task is durable. Pause at `human` gates, resume via API, replay idempotently.
- **Multi-Tenant API** — tenant-scoped REST paths (`/api/v1/{tenant}/...`), JWT/OIDC/GitHub OAuth, A2A protocol server (Google Agent-to-Agent).
- **Session & Application Mode** — priority `grade` → dedicated K8s cluster per tenant; `low|medium|high` → shared pool.
- **Dual-Mode Deployment** — All-in-One (SQLite + memory queue) or Production (PostgreSQL + MQTT/EMQX + Redis + K8s).
- **OTEL Tracing Per Node** — every node span records input, output, and internal state for Jaeger debugging.
- **Cron + Webhook Triggers** — schedule-based and event-driven (GitHub/GitLab webhook) per agentflow.
- **Multi-Provider LLM** — OpenAI-compatible adapter with per-provider rate limiting, SOCKS/HTTP proxy, modalities, and extended thinking.
- **JSON Schema Validation** — agent output validated against `output_schema` with auto-retry on failure.
- **Notification Service** — Telegram, DingTalk, Slack, Email, Webhook channels; WebSocket SSE push for human approvals.
- **Optional Economic Layer** — x402 payment protocol with spending policies, wallet abstraction, and Coinbase facilitator integration.

---

## Quick Start

**Prerequisites:** Go 1.26+ · (optional) PostgreSQL 15+, EMQX 5.x, Redis 7.x

```bash
git clone git@github.com:flowgent-labs/flowgent.git && cd flowgent
make build-flowgent    # core engine
make example-mcps      # example MCP servers (for e2e testing)
```

Run with a config file (required):

```bash
# Development (SQLite + memory queue)
./bin/flowgent daemon start -c etc/flowgent-dev.yaml

# Use the fully annotated sample as a starting point
cp etc/flowgent-dev.yaml my-config.yaml
./bin/flowgent daemon start -c my-config.yaml
```

Verify:

```bash
curl http://localhost:9999/_/healthz             # {"status":"ok"}
curl http://localhost:9999/_/openapi.yaml        # OpenAPI 3.1 spec
curl http://localhost:9992/.well-known/agent.json # A2A agent card
```

REST API on `:9999` · A2A on `:9992` · Wallet on `:9901` · pprof on `:9991`

---

## Architecture

```
API Server ──→ JobManager ──→ Scheduler ──→ TaskManager(s) ──→ MQTT ──→ Executors
  (gateway)     (control)      (pluggable)   (elastic pods)    (bus)     (11 types)
```

| Component | Role | Docs |
|-----------|------|------|
| API Server | Multi-tenant REST + A2A gateway | [01-DESIGN](docs/01-DESIGN-engine-architecture.md#2-api-server--multi-tenant-gateway--operator) |
| JobManager | DAG orchestration, mode routing | [01-DESIGN](docs/01-DESIGN-engine-architecture.md#3-jobmanager--control-plane) |
| ResourceManager | Pluggable dispatch (local/K8s) | [01-DESIGN](docs/01-DESIGN-engine-architecture.md#4-resourcemanager--scheduler--pluggable-dispatch) |
| TaskManager | Persistent slot workers, heartbeat | [01-DESIGN](docs/01-DESIGN-engine-architecture.md#5-taskmanager--persistent-worker) |
| MQTT Event Bus | Distributed JM↔TM messaging | [01-DESIGN](docs/01-DESIGN-engine-architecture.md#6-mqtt-event-bus) |

**Node types (11):** `agent` `tool` `map` `join` `agentflow` `condition` `tribunal` `human` `supervisor` `sandbox` `noop`

Full architecture → [docs/01-DESIGN-engine-architecture.md](docs/01-DESIGN-engine-architecture.md)

---

## Examples

Built-in examples are under `examples/` — agents, flows, MCP servers, and skills.

### AgentFlows (L2)

| Flow | Description | File |
|------|-------------|------|
| Security Autonomy Fixer V1 | Baseline 22-node 12-phase pipeline (all MCPs + skill + re-scan) | `examples/flows/01-security-autonomy-fix-v1.yaml` |
| Security Autonomy Fixer V2 | V1 with GitHub webhook trigger commented out (current deploy target) | `examples/flows/01-security-autonomy-fix-v2.yaml` |
| AutoTest Generation | Confluence → Cucumber pipeline | `examples/flows/20-autotest-generation-v1.yaml` |

### Skills

| Skill | Description | File |
|-------|-------------|------|
| nexus3-maven-versions-retrieve-with-iq-firewall | Nexus3 dependency firewall check via copilot scripts (replaces sonatype-nexus3 MCP) | `examples/skills/nexus3-maven-versions-retrieve-with-iq-firewall.yaml` |

E2E guides:
- [V1 Baseline — Real webhook → SonarQube](docs/10-L2-E2E-security-fixer-v1.md)
- [V2 Current — Webhook simulated, white-box](docs/10-L2-E2E-security-fixer-v2.md)

---

## Developer Quickstart

```bash
make build-flowgent    # core binary
make example-mcps      # example MCP servers
make test              # run all tests
make fmt               # format source
```

### Build Individual Components

```bash
# Core
go build -o bin/flowgent ./src/cmd/flowgent

# Example MCP servers
go build -o bin/mcp-server-github ./examples/mcp-github
go build -o bin/mcp-server-sonarqube ./examples/mcp-sonarqube
```

### Project Layout

```
src/cmd/flowgent/   — core CLI (daemon, apiserver, a2a, wallet, console)
src/api/            — REST API handlers (tenant-scoped CRUD)
src/engine/         — JM, RM (scheduler), TM, executors (11 node types)
src/model/          — domain types (AgentFlowSpec, ExecutionPlan, NodeSpec, etc.)
src/store/          — persistence (SQLite, PostgreSQL)
src/llm/            — LLM client (OpenAI-compatible) + MCP factory
src/notification/   — notification service (Telegram, DingTalk, Slack, Email, Webhook)
examples/           — agents, flows, MCP servers, skills
docs/               — design docs, e2e guide
```

---

## License

See [LICENSE](LICENSE).
