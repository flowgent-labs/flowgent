# Flowgent

[![Build & Test](https://github.com/flowgent-labs/flowgent/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/flowgent-labs/flowgent/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/go-1.26%2B-00ADD8?logo=go)](https://go.dev/)
[![Harness](https://img.shields.io/badge/flavor-Harness-purple)](https://your-harness-url.example.com)
[![AuthGuard](https://img.shields.io/badge/secured_by-AuthGuard-00A7E1)](https://github.com/wl4g/authguard)
[![OpenTelemetry](https://img.shields.io/badge/observability-OpenTelemetry-0F0FFF?logo=opentelemetry)](https://opentelemetry.io/)
[![Helm](https://img.shields.io/badge/deploy-Helm-0F1689?logo=helm)](https://helm.sh/)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)

*A distributed enterprise AgentFlow orchestration engine for predictable,
auditable AI automation. Flowgent combines declarative DAG execution with
bounded LLM autonomy and Flink-style runtime clusters, so teams can operate
long-running AI workflows without surrendering deterministic control.*

Flowgent is the execution and application-control plane. It does not implement
login, federation, sessions, roles, policies, or authorization decisions.
[AuthGuard](https://github.com/wl4g/authguard) owns that boundary; its Envoy
integration delivers a signed resource scope that Flowgent applies to every
repository query when enabled.

## Features

- **Declarative AgentFlow execution** — YAML-defined DAGs combine twelve node
  types, deterministic fan-out/fan-in, conditions, human gates, retries, and
  bounded subflow composition.
- **Bounded AI autonomy** — `agent` and `supervisor` nodes add LLM reasoning;
  supervisor actions are restricted to `continue`, `retry`, `inject`, and
  `abort` with explicit quotas.
- **Durable and inspectable state** — Flow, run, task, approval, configuration,
  and artifact metadata are persisted through the API Server. Every state
  transition can be traced and replayed safely.
- **Flink-style runtime isolation** — application mode creates a JobManager per
  active run; each JobManager owns TaskManager and Sandbox workers identified by
  an isolated `runtime_cluster_id`. Session mode shares a long-running runtime.
- **Clear component contracts** — the API Server is the sole database client;
  Controller, JobManager, TaskManager, Sandbox, and Notifier communicate through
  MQTT and the API rather than bypassing durable-state ownership.
- **Enterprise resource control** — namespace-scoped REST APIs, A2A support,
  AuthGuard adapter SDK integration, signed scope verification, resource-level
  SQL predicates, and a fail-closed public gateway path.
- **Operations console and lifecycle tooling** — the React web console, CLI,
  import/export formats, notifications, OpenTelemetry traces, metrics, and
  Kubernetes labels make workflows observable and manageable.
- **Extensible integrations** — OpenAI-compatible LLM providers, HTTP MCP
  services, webhooks, cron triggers, Git providers, SonarQube, and delivery
  channels are configuration-driven.
- **Optional economic boundary** — x402 policy support delegates EIP-712 digest
  signing and all private-key custody to an independently released Wallet service.

## Architecture

```mermaid
flowchart LR
    Client[Browser / REST client / A2A client / Webhook]
    Gateway[Envoy Gateway<br/>JWT verification + ext_authz]
    AuthGuard[AuthGuard<br/>AuthN / AuthZ / signed resource scope]
    Web[Flowgent Web]
    API[API Server<br/>sole durable-state client]
    DB[(PostgreSQL / SQLite)]
    MQ[(MQTT / EMQX)]
    Controller[Controller]
    JM[JobManager]
    TM[TaskManager]
    Sandbox[Sandbox]
    Notifier[Notifier]
    Channels[Slack / Email / Webhook / UI]
    Wallet[External Wallet service<br/>optional]

    Client --> Gateway
    Gateway -->|login and authorization| AuthGuard
    Gateway -->|protected application traffic| Web
    Gateway -->|protected REST traffic| API
    Web --> API
    API --> DB
    API -->|lifecycle events| MQ
    API -->|flow and run API| Controller
    Controller -->|create / remove| JM
    API -->|run and flow API| JM
    JM -->|execution plans| MQ
    MQ --> TM
    TM -->|task state| API
    TM -->|results| MQ
    MQ --> JM
    TM -->|sandbox work| MQ
    MQ --> Sandbox
    Sandbox -->|results| MQ
    MQ --> Notifier
    Notifier --> Channels
    TM -->|bounded digest request| Wallet
```

The architecture has deliberate ownership boundaries:

| Boundary | Owner | Contract |
|---|---|---|
| Durable state | API Server | Other engine components use REST; they do not connect to PostgreSQL or SQLite directly. |
| Scheduling and work | MQTT | Execution plans, results, sandbox work, lifecycle events, and notification events; WebSocket is not an internal scheduler. |
| Runtime lifecycle | Controller → JobManager | Controller owns application JobManagers; each JobManager owns its matching TaskManager and Sandbox workers. |
| Public access | Envoy + AuthGuard | Envoy enforces JWT and external authorization; AuthGuard evaluates identity and policy, while Flowgent verifies and applies the supplied scope. |
| Key custody | External Wallet | Flowgent can request a bounded digest signature but never receives a private key or deploys the Wallet service. |

See the [engine architecture](docs/architecture/overview.md)
([中文](docs/architecture/overview_ZH.md)), [AgentFlow model](docs/architecture/agent-flow.md)
([中文](docs/architecture/agent-flow_ZH.md)), and [AuthGuard integration boundary](docs/architecture/authguard-integration.md)
([中文](docs/architecture/authguard-integration_ZH.md)) for the full contracts.

## Quick Start

### Requirements

- Go 1.26+, Node.js 22+, npm, and Python 3.12 for the complete build and test matrix
- Docker-compatible builder and Helm 3.17+ for images, charts, and E2E deployment
- PostgreSQL, MQTT/EMQX, Kubernetes, and AuthGuard only for the corresponding distributed or protected topology

### Build and run

```bash
git clone --recurse-submodules https://github.com/flowgent-labs/flowgent.git
cd flowgent

# Build the Go runtime, then the two release images.
make build
make build:image

# Run the local all-in-one topology with the reviewed sample configuration.
./bin/flowgent-core all-in-one start -c etc/flowgent.yaml
```

The default local endpoints are:

```bash
curl -fsS http://127.0.0.1:9999/_/healthz
curl -fsS http://127.0.0.1:9999/_/openapi.yaml
curl -fsS http://127.0.0.1:9992/.well-known/agent.json
```

Use the console to import versioned AgentFlow resources; credentials remain in
the deployment secret or runtime environment, never in manifests.

```bash
./bin/flowgent-core console -c etc/flowgent.yaml -- import \
  use-cases/security-autonomy-fixer/e2e/config
```

### Test

```bash
# Go, web, adapter, SQL-scope, Helm-render, and E2E entrypoint checks.
make test

# Complete retained Kubernetes E2E: Flowgent, Web, Envoy, AuthGuard, LDAP,
# GitHub OAuth mock, PostgreSQL, EMQX, Jaeger, and evidence-producing verifiers.
HTTPS_PROXY=http://127.0.0.1:8800 make e2e-security-autonomy-fixer-with-helm

# Functionally equivalent, separately prefixed Docker Compose topology.
HTTPS_PROXY=http://127.0.0.1:8800 make e2e-security-autonomy-fixer-with-docker
```

Both deployment modes execute the same verifier matrix. Kubernetes E2E retains
the successful `e2e-flowgent-*` deployment for manual validation; add
`E2E_ARGS=--clean-after-run` only when it is safe to remove its owned resources.
The reference application and verification evidence contract live in
[`use-cases/security-autonomy-fixer`](use-cases/security-autonomy-fixer/README.md).

## Deployment

### Release artifacts

| Artifact | OCI reference | Purpose |
|---|---|---|
| Runtime | `ghcr.io/flowgent-labs/flowgent:<version>` | API Server, Controller, JobManager, TaskManager, Sandbox, Notifier, A2A, and console commands |
| Web UI | `ghcr.io/flowgent-labs/flowgent-web:<version>` | Nginx-served React operations console |
| Helm chart | `oci://ghcr.io/flowgent-labs/charts/flowgent:<version>` | Flowgent services, Web UI, runtime configuration, optional dependencies, and opt-in AuthGuard middleware |

Merged `refactor:`, `feat:`, and `fix:` pull requests publish exactly the two
production images, an OCI chart, and the matching `flowgent-<version>.tgz`
GitHub release asset. Pull-request builds publish short-lived dirty images and
a verified chart artifact instead. See the [delivery workflow guide](.github/workflows/README.md).

### Install with Helm

Choose an immutable release version, provide the production storage and MQTT
endpoints, and assign an installation-specific runtime owner. The chart defaults
to the two product images above and exposes all operational settings in
[`deploy/helm/flowgent/values.yaml`](deploy/helm/flowgent/values.yaml).

```bash
export FLOWGENT_NAMESPACE=flowgent-system
export FLOWGENT_VERSION=0.1.0

helm upgrade --install flowgent \
  oci://ghcr.io/flowgent-labs/charts/flowgent \
  --version "$FLOWGENT_VERSION" \
  --namespace "$FLOWGENT_NAMESPACE" --create-namespace \
  --set runtime.resourceOwner=flowgent-production \
  --set storage.type=POSTGRE \
  --set storage.postgres.dsn='postgres://flowgent:change-me@postgres:5432/flowgent?sslmode=require' \
  --set messaging.mqtt.broker='tcp://emqx:1883'
```

For development, all-in-one mode uses SQLite and an in-memory queue. For a
distributed deployment, use PostgreSQL and MQTT, configure the namespace and
runtime resource limits, and supply provider, MCP, notifier, and artifact
credentials through the runtime secret contract. Do not place secrets in Helm
values, AgentFlow YAML, or the web application.

### AuthGuard topology

Flowgent's AuthGuard dependency is intentionally opt-in. It supports the same
separation between business application lifecycle and security middleware as
AuthGuard's reference deployment:

| Scenario | `authguard-middleware.enabled` | `adapter.enabled` | Result |
|---|---:|---:|---|
| Local or unprotected development | off | off | No AuthGuard runtime dependency; repositories use the no-op `FlowgentSqlScope`. |
| Production with separately managed AuthGuard | off | on | Flowgent consumes a shared signed-context secret but cannot create, upgrade, or delete AuthGuard. |
| Isolated integration E2E | on | on | One release deploys Flowgent, AuthGuard AuthN/AuthZ/Web, Envoy, and the adapter together. |

For the production topology, deploy AuthGuard independently with
`application.enabled=false` on the middleware release, then enable only the
application chart's adapter and point it to the shared context-key Secret. Both
systems must use the same key. Public API traffic must enter through the
AuthGuard-managed Envoy Gateway; Flowgent's internal `:9990` control-plane
listener must never be exposed on that Gateway.

The adapter verifies each signed context for the requested HTTP action and maps
the matching resource grant to `FlowgentSqlScope`, which embeds AuthGuard's
official `model.SqlScope`. Repository reads, updates, and deletes apply that
predicate; creates and upserts validate the candidate resource in their query or
transaction. See the [integration boundary](docs/architecture/authguard-integration.md)
and the [chart values](deploy/helm/flowgent/values.yaml) for the complete
contract.

## Administration and Integration

### Create and operate an AgentFlow

AgentFlows, agents, MCP connections, LLM providers, skills, notification
channels, and runtime configuration use versioned console resource envelopes.
Import them through the console, observe execution through the Web UI and
OpenTelemetry, and use the API or human gates to drive run lifecycle.

```bash
# Import a directory of reviewed resources.
./bin/flowgent-core console -c etc/flowgent.yaml -- import \
  use-cases/security-autonomy-fixer/e2e/config/agents/

# Export a backup from the configured environment.
./bin/flowgent-core console -c etc/flowgent.yaml -- export --output flowgent-export.yaml
```

Every business integration belongs in its owning AgentFlow configuration. MCP
transport is HTTP-only; TaskManager Pods do not execute arbitrary stdio MCP
binaries. Keep provider tokens, Git credentials, and notification secrets in
the deployment's secret boundary.

### Security boundary

AuthGuard owns authentication, identity federation, hosted login, principal
lifecycle, roles, policy decisions, and authorization audit. Flowgent owns
resource mapping, SDK context verification, and applying the resulting
resource-level scope to its own storage queries. Neither Flowgent Web nor the
engine stores browser tokens or reimplements AuthGuard policy.

The [Security Autonomy Fixer](use-cases/security-autonomy-fixer/README.md) is
the production-shaped reference: LDAP principal discovery and GitHub OAuth are
the only enabled identity features; the policy preset models scoped platform
security, application-security, independent-risk, and regulatory-audit roles
for a multinational financial organization.

## Documentation

- [Documentation index](docs/index.md) · [中文](docs/index_ZH.md)
- [Distributed engine architecture](docs/architecture/overview.md) · [中文](docs/architecture/overview_ZH.md)
- [AgentFlow application architecture](docs/architecture/agent-flow.md) · [中文](docs/architecture/agent-flow_ZH.md)
- [AuthGuard integration boundary](docs/architecture/authguard-integration.md) · [中文](docs/architecture/authguard-integration_ZH.md)
- [Security Autonomy Fixer reference application](use-cases/security-autonomy-fixer/README.md) · [中文](use-cases/security-autonomy-fixer/README_ZH.md)
- [Web console architecture](web/README.md)
- [Helm deployment values](deploy/helm/flowgent/values.yaml)
- [CI and release lifecycle](.github/workflows/README.md)

## License

Flowgent is licensed under the business-friendly [Apache License 2.0](LICENSE).

## Contact

- **Issues:** [github.com/flowgent-labs/flowgent/issues](https://github.com/flowgent-labs/flowgent/issues)
- **Discussions:** [github.com/flowgent-labs/flowgent/discussions](https://github.com/flowgent-labs/flowgent/discussions)
- **Security reports:** [private vulnerability report](https://github.com/flowgent-labs/flowgent/security/advisories/new)

## Acknowledgments

Flowgent builds on several excellent open-source projects and standards:

- [Kubernetes](https://kubernetes.io/) and [Helm](https://helm.sh/) — runtime
  lifecycle, deployment, and package distribution.
- [EMQX](https://www.emqx.io/) and MQTT — execution-plan, result, sandbox, and
  notification transport.
- [PostgreSQL](https://www.postgresql.org/) and [SQLite](https://www.sqlite.org/)
  — durable state backends.
- [AuthGuard](https://github.com/wl4g/authguard) and
  [Envoy Gateway](https://gateway.envoyproxy.io/) — hosted login, identity,
  policy decision, gateway enforcement, and scoped authorization context.
- [OpenTelemetry](https://opentelemetry.io/) and
  [Prometheus](https://prometheus.io/) — traces, metrics, and operational evidence.
- [React](https://react.dev/) and [Vite](https://vite.dev/) — the operations console.

## References

| Area | Specifications and primary documentation |
|---|---|
| Agent interoperability | [Agent2Agent protocol](https://a2a-protocol.org/latest/) |
| Observability | [W3C Trace Context](https://www.w3.org/TR/trace-context/) and [OpenTelemetry specification](https://opentelemetry.io/docs/specs/otel/) |
| Messaging | [MQTT Version 5.0](https://docs.oasis-open.org/mqtt/mqtt/v5.0/mqtt-v5.0.html) |
| Authorization enforcement | [Envoy External Authorization API v3](https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/auth/v3/external_auth.proto) |
| Container orchestration | [Kubernetes documentation](https://kubernetes.io/docs/) and [Helm documentation](https://helm.sh/docs/) |
