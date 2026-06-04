# Project Conventions

## Requirements

- **All builds MUST use `make` from the repository root.** `make help` shows all targets.
  Output goes to `bin/`.
  - Docker (no host Go): `make build` / `make build-all` / `make build-image` / `make build-image-all`
  - Host dev (requires Go): `make build-host` / `make build-host-all`
  - Utilities: `make test` / `make fmt` / `make clean` / `make help`
- **NEVER run `go build` directly** outside of `make build-host` / `make build-host-all`.
  - Forbidden: `cd src/cmd && go build ...`, `cd examples/mcp-* && go build ...`.
- Binaries under `bin/` are git-ignored. Do NOT commit them.
- **Git commit messages must be concise.** Keep subject under 72 chars.
  - Do NOT include Co-authored-by trailers (no `Co-Authored-By: Claude Opus ...`).
  - Do NOT include `via .HAPI` or similar tool attribution in messages.
  - Author identity is always `Flowgent Jaw <developers@flowgent-labs.com>`.
- **If a Docker image cannot be pulled** (e.g. Docker Hub blocked in CN), use the mirror
  script to pull it through the jump host and push to Aliyun CR:
  ```bash
  ./deploy/docker/mirror-pull.sh <docker-image> [aliyun-repo] [jump-host]
  ```
  This pulls from Docker Hub via jump host → pushes to `registry.cn-shenzhen.aliyuncs.com/wl4g/`
  → imports to local k3s. All base images in Dockerfiles MUST use the mirror registry
  (`registry.cn-shenzhen.aliyuncs.com/wl4g/`) to ensure pullability in CN environments.

## Code Quality

- Keep directory and file naming convergent and consistent; avoid ad-hoc new directories or files
- Follow Go naming conventions (lowercase package names, exported symbols capitalized, interfaces ending in er/or, etc.)
- Favor abstract, unified interfaces; program to interfaces to reduce coupling on concrete types
- High cohesion, low coupling: keep related logic within a module; modules interact through interfaces

## Test Code Organization

- **Forbidden**: test files or test helpers under `src/` except for the one allowed exception
- **Exception**: Go standard unit test files named `*_test.go` may sit alongside the source code they test
- All E2E / integration tests (requiring middleware) belong under `tests/`:
  - Shared / base test code goes into `tests/testutil/`, kept abstract and well-structured
  - E2E / scenario test files are named `01-xxx` format
  - Numbering follows the logical execution order of the system modules for readability

## Cost Awareness

- For any large-scale recursive exploration or bulk source-file reading, **always delegate to subagents** (e.g. Explore agent or small-model general-purpose agent)
- These are "dirty reads" that don't need high intelligence — offloading them saves cost and preserves main-context quality

---

# Architecture Overview

Flowgent is a **distributed multi-tenant AI agent orchestration engine** written in Go.
Architecture follows the **Kubernetes apiserver-etcd pattern**: only the API server connects
to PostgreSQL; all other components (Controller, JobManager, TaskManager, Notifier)
communicate exclusively via **MQTT** (pub/sub) or the **apiserver REST API**.

## Component Roles

| Component | Role | DB Access | Communication |
|-----------|------|-----------|---------------|
| **API Server** | REST + A2A gateway, multi-tenant routing | PostgreSQL (sole client) | HTTP REST, WebSocket |
| **Controller** | Sharded flow driver, pod discovery, mode dispatch | none (apiserver REST) | MQTT, K8s API |
| **JobManager** | DAG build, run poller, scheduling dispatch | none (apiserver REST) | MQTT (Publish TopicExec) |
| **TaskManager** | Slot workers, plan execution, inline sandbox | none (apiserver REST) | MQTT (Subscribe TopicExec) |
| **Notifier** | Multi-channel alerts, WebSocket push | none (apiserver REST) | MQTT, HTTP WebSocket |
| **Sandbox** | **Inline in TM pods** — no separate deployment | none | MQTT (via TM) |

## Deployment Modes

- **Session mode** (shared pool): Controller → shared JM → shared TM pool (2+ replicas)
- **Application mode** (dedicated): Controller → per-flow K8s Namespace + JM + TM

## Messaging (Pub/Sub)

All inter-component communication uses `Publish(topic, msg)` / `Subscribe(topic, handler)`.

| Topic | Publisher | Subscriber |
|-------|-----------|------------|
| `flowgent/v1/exec` | JM (via RM) | TM slot workers |
| `flowgent/v1/exec/result` | TM | JM (status tracking) |
| `flowgent/v1/sandbox/trigger` | TM (SandboxExecutor) | TM (inline sandbox runner) |
| `flowgent/v1/sandbox/result` | TM (sandbox runner) | TM (SandboxExecutor) |
| `flowgent/v1/heartbeat` | TM | JM (HeartbeatMonitor) |

## Codebase Layout

```
src/
├── api/          REST + A2A server, handlers, middleware
├── cache/        ICache interface, Memory + Redis implementations
├── cmd/          CLI entrypoint (launch.go), controller, console
├── common/       OTEL tracing, logger utilities
├── config/       Viper-based config loading, YAML model
├── core/         Engine: jobmanager, taskmanager, resourcemanager, executor, trigger
├── messaging/    Messager interface, MQTT/Local implementations
├── model/        Shared domain types (DAG, ExecutionPlan, AgentFlowSpec, etc.)
├── notifier/     Notification service + channel senders
├── sandbox/      Inline sandbox runner (executes scripts inside TM pods)
├── store/        Store interface, PostgreSQL + SQLite implementations
└── wallet/       x402 payment approvals
deploy/
├── docker/       Dockerfile, Dockerfile.all-in-one, mirror-pull.sh
└── helm/flowgent/  Helm chart (templates/, values.yaml)
etc/flowgent-dev.yaml  Reference dev config
examples/security-autonomy-fixer/  Primary use case (agents, flows, skills, docs)
```

---

# Documentation Index

## L1 — Architecture & Design

| Doc | Purpose |
|-----|---------|
| [docs/01-L1-Engine-Architecture.md](docs/01-L1-Engine-Architecture.md) | **Core architecture.** Three-phase engine (Design→Schedule→Execute), apiserver-as-sole-DB-gateway, MQTT bus, Component roles, session vs application modes, Helm deployment matrix, seccomp-bpf sandbox |
| [docs/02-L1-x402-Economic-Support.md](docs/02-L1-x402-Economic-Support.md) | **Optional economic layer.** HTTP 402 payment protocol, Coinbase facilitator settlement, `src/payments/` layout, policy engine, wallet management |
| [docs/03-L1-Build-Deploy-Deps-Images.md](docs/03-L1-Build-Deploy-Deps-Images.md) | **Build dependencies.** x402 Facilitator, EVM Anvil, Solana test validator Docker images |

## L2 — Use Cases & Examples

| Doc | Purpose |
|-----|---------|
| [docs/10-L2-USE-CASES.md](docs/10-L2-USE-CASES.md) | **Use cases catalog.** Security Autonomy Fixer as primary example: 12-phase CI/CD security pipeline, multi-agent team, YAML flow definitions |
| [examples/security-autonomy-fixer/docs/E2E-security-fixer-v1.md](examples/security-autonomy-fixer/docs/E2E-security-fixer-v1.md) | **V1 E2E test.** API-triggered 12-phase pipeline on K3s, white-box verification via PG/MQTT/Jaeger |
| [examples/security-autonomy-fixer/docs/E2E-security-fixer-v2.md](examples/security-autonomy-fixer/docs/E2E-security-fixer-v2.md) | **V2 E2E test.** GitHub webhook → real SonarQube fix, 30-node DAG, external service dependencies |
| [examples/security-autonomy-fixer/docs/SonarQube-Integration.md](examples/security-autonomy-fixer/docs/SonarQube-Integration.md) | **SonarQube deployment.** Docker Compose setup for v26.4.0 Community, automated password reset |

## Examples Directory

```
examples/security-autonomy-fixer/
├── agents/        7 agent YAML definitions (supervisor, fixer, reviewers, git-agent)
├── flows/         3 flow YAMLs (v1, v2, sub-fix skill)
├── skills/        nexus3-retrieval skill (Nexus3 API + SonatypeIQ wrapper)
├── docs/          E2E test docs + SonarQube integration guide
└── e2e-verification/  Python E2E test runner + scenarios
```

## Skills

| Skill | Purpose |
|-------|---------|
| [nexus3-retrieval](examples/security-autonomy-fixer/skills/nexus3-retrieval/SKILL.md) | Tool catalog for AI agents to query Nexus3 REST API + SonatypeIQ for non-quarantined Maven dependency versions |

## Key Go Modules

| Module (go.mod) | Purpose |
|-----------------|---------|
| `src/cmd` | CLI entrypoint, controller, component launchers |
| `src/core` | Engine: JM, TM, RM, executor, discovery, trigger |
| `src/messaging` | Messager interface + MQTT/Local/Instrumented impls |
| `src/model` | Shared types: DAG, ExecutionPlan, SandboxPolicy, etc. |
| `src/store` | Store interface + PG/SQLite implementations |
| `src/config` | Viper-based YAML config loading, env var expansion |
| `src/api` | REST + A2A HTTP handlers and middleware |
| `src/cache` | ICache interface + Memory/Redis implementations |
| `src/sandbox` | Inline sandbox script runner (runs inside TM) |
