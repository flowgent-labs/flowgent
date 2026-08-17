# Flowgent

**Predictable, auditable, distributed enterprise AI agent orchestration — with native economic layer (x402/MPP).**

Flowgent is an AI-native universal orchestration engine with namespace-scoped Resource Pools for explicit capacity and SLA isolation. It deeply integrates LLM agent intelligence with deterministic DAG execution — preserving the predictability, reliability, and auditability of traditional workflows while empowering distributed enterprise super-agents with bounded, governable autonomy.

---

## Features

- **DAG Topological Scheduler** — predictable execution with concurrent fan-out. 11 node types compose any orchestration topology.
- **Deterministic + Intelligent** — `agent` and `supervisor` nodes provide LLM intelligence; all others guarantee deterministic execution.
- **Controlled Autonomy** — supervisor constrained to `continue|retry|inject|abort` with configurable quotas.
- **State-Machine Persistence** — every run and task is durable. Pause at `human` gates, resume via API, replay idempotently.
- **Multi-Namespace API** — namespace-scoped REST paths (`/api/v1/{namespace}/...`), JWT/OIDC/GitHub OAuth, A2A protocol server (Google Agent-to-Agent).
- **Resource Pools** — every Flow binds a namespace-scoped TM/Sandbox capacity pool while retaining a dedicated active-run JobManager; pool slots, resources, PriorityClass, and NodeSelector provide explicit SLA isolation.
- **Dual-Mode Deployment** — All-in-One (SQLite + memory queue) or Production (PostgreSQL + MQTT/EMQX + Redis + K8s).
- **OTEL Tracing Per Node** — every node span records input, output, and internal state for Jaeger debugging.
- **Cron + Webhook Triggers** — schedule-based and event-driven (GitHub/GitLab webhook) per agentflow.
- **Multi-Provider LLM** — OpenAI-compatible adapter with per-provider rate limiting, SOCKS/HTTP proxy, modalities, and extended thinking.
- **JSON Schema Validation** — agent output validated against `output_schema` with auto-retry on failure.
- **Notification Service** — Telegram, DingTalk, Slack, Email, Webhook channels; WebSocket SSE push for human approvals.
- **Optional Economic Layer** — x402 policy and signed resource retries with digest signing delegated to a process-isolated external Wallet service.

---

## Quick Start

**Prerequisites:** Go 1.26+ · (optional) PostgreSQL 15+, EMQX 5.x, Redis 7.x

```bash
git clone --recurse-submodules git@github.com:flowgent-labs/flowgent.git && cd flowgent
make build:image      # build the Flowgent core image
make build            # build the Flowgent core binary
```

- Run with a config file (required):

```bash
# Development (SQLite + memory queue)
./bin/flowgent daemon start -c etc/flowgent.yaml

# Use the fully annotated sample as a starting point
cp etc/flowgent.yaml my-config.yaml
./bin/flowgent daemon start -c my-config.yaml
```

- Verify

```bash
curl http://localhost:9999/_/healthz             # {"status":"ok"}
curl http://localhost:9999/_/openapi.yaml        # OpenAPI 3.1 spec
curl http://localhost:9992/.well-known/agent.json # A2A agent card
```

REST API on `:9999` · A2A on `:9992` · pprof on `:9991`

---

## Architectures

- [Documentation index](docs/index.md)
- [L1 engine architecture](docs/architecture/overview.md)
- [External Wallet and x402 boundary](docs/architecture/engine/wallet.md)

## Examples

- [L2 AgentFlow architecture](docs/architecture/agent-flow.md)
- [Use-case catalog](docs/use-cases/overview.md)

---

## License

See [Apache License](LICENSE).
