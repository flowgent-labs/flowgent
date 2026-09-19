# Flowgent

**Predictable, auditable, distributed enterprise AI agent orchestration — with native economic layer (x402/MPP).**

Flowgent is an AI-native universal orchestration engine with Flink-style runtime clusters for explicit execution isolation. It deeply integrates LLM agent intelligence with deterministic DAG execution — preserving the predictability, reliability, and auditability of traditional workflows while empowering distributed enterprise super-agents with bounded, governable autonomy.

---

## Features

- **DAG Topological Scheduler** — predictable execution with concurrent fan-out. 11 node types compose any orchestration topology.
- **Deterministic + Intelligent** — `agent` and `supervisor` nodes provide LLM intelligence; all others guarantee deterministic execution.
- **Controlled Autonomy** — supervisor constrained to `continue|retry|inject|abort` with configurable quotas.
- **State-Machine Persistence** — every run and task is durable. Pause at `human` gates, resume via API, replay idempotently.
- **Multi-Namespace API** — namespace-scoped REST paths (`/api/v1/{namespace}/...`), JWT/OIDC/GitHub OAuth, A2A protocol server (Google Agent-to-Agent).
- **Runtime Clusters** — application-mode runs get isolated per-run JM/TM/Sandbox clusters, while session-mode runs share the Helm-deployed session cluster. Runtime cluster IDs keep JobManager ownership, workers, MQTT dispatch, and Kubernetes labels isolated.
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

### One-command Security Autonomy Fixer E2E

This is the production-shaped k3s path: Flowgent, Envoy Gateway, AuthGuard,
LDAP principal discovery, GitHub OAuth authentication, PostgreSQL, EMQX,
Jaeger, and the Security Autonomy Fixer are deployed and verified together.
The runner silently loads `~/.wl4gshrc.sec` when present; it never prints its
secret values.

```bash
HTTPS_PROXY=http://127.0.0.1:8800 make e2e-security-fixer
```

The successful Helm deployment is intentionally retained. It is isolated from
AuthGuard's own E2E by the `e2e-flowgent-*` namespace/release/resource prefix,
a dedicated Envoy Gateway controller name, workload namespaces, PostgreSQL
schemas `e2e_flowgent` / `e2e_flowgent_authguard`, and special ports. Start
persistent local tunnels for manual exploration with:

```bash
make e2e-security-fixer-access
```

| Endpoint | Address |
| --- | --- |
| Flowgent UI | `http://127.0.0.1:31080` |
| Direct E2E API tunnel | `http://127.0.0.1:29999` |
| AuthGuard-protected API tunnel | `http://127.0.0.1:18089` |
| AuthGuard AuthN tunnel | `http://127.0.0.1:18082` |
| AuthGuard management tunnel | `http://127.0.0.1:19091` |

The E2E intentionally enables only the identity features used here: GitHub
OAuth and LDAP federated principal discovery. A dedicated Redis cluster stores
the mandatory one-time OAuth challenges; OIDC, Keycloak, SCIM, and custom
discovery connectors remain disabled. See the
[use-case security deployment](use-cases/security-autonomy-fixer/README.md#enterprise-authorization-preset)
for the multinational financial-company role model.

The Helm chart vendors the locked official AuthGuard dependency behind
`authguard-middleware.enabled=false`. Its reviewed Application contract binds
Hosted Login and AuthN to `flowgent.wl4g.com` with HTTPS-only return URIs.
Production operators deploy middleware independently with
`application.enabled=false`; ordinary Flowgent upgrades therefore cannot
remove it. Set `authguard-middleware.adapter.enabled=true` on the application
release to consume its signed context. When both flags are false, no AuthGuard
credentials are required and repositories use the native-IAM no-op
`FlowgentSqlScope`. The designated E2E may enable application and middleware in
one isolated release.

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

Licensed under the business-friendly [Apache License 2.0](LICENSE), including
commercial use, modification, distribution, and patent grants subject to its
terms and notices.
