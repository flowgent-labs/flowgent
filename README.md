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
make build-all         # build for all images on docker
make build-host-all    # build for all binaries on host
```

- Run with a config file (required):

```bash
# Development (SQLite + memory queue)
./bin/flowgent daemon start -c etc/flowgent-dev.yaml

# Use the fully annotated sample as a starting point
cp etc/flowgent-dev.yaml my-config.yaml
./bin/flowgent daemon start -c my-config.yaml
```

- Verify

```bash
curl http://localhost:9999/_/healthz             # {"status":"ok"}
curl http://localhost:9999/_/openapi.yaml        # OpenAPI 3.1 spec
curl http://localhost:9992/.well-known/agent.json # A2A agent card
```

REST API on `:9999` · A2A on `:9992` · Wallet on `:9901` · pprof on `:9991`

---

## Architectures

- [docs/01-L1-Engine-Architecture.md](docs/01-L1-Engine-Architecture.md)
- [docs/02-L1-x402-Economic-Support.md](docs/02-L1-x402-Economic-Support.md)

## Examples

- [docs/10-L2-USE-CASES.md](docs/10-L2-USE-CASES.md)

---

## License

See [Apache License](LICENSE).
