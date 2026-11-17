# Project Conventions

## Development (Requirements)

- **All Flowgent Go builds and tests MUST use the repository-root `Makefile`.**
  `make help` shows the supported targets; binaries go to `bin/`.
  - Docker: `make build:image` / `make build:image:core`
  - Host dev: `make build` / `make build:core`
  - Utilities: `make test-ut` / `make test-x402` / `make test-it` / `make fmt` / `make clean`
- **The independent Rust AgentVault submodule MUST use only
  `agentvault/Makefile` for its build, test, lint, and image lifecycle.** Use
  `make -C agentvault help`. The Flowgent root Makefile MUST NOT proxy
  AgentVault targets.
- **NEVER run `go build` directly** outside of `make build:xxx`
- **NEVER run `cargo build`, `cargo test`, or `docker build` directly for
  AgentVault.**
- Binaries under `bin/` are git-ignored. Do NOT commit them.
- **Git commit messages must be concise.** Keep subject under 72 chars.
  - No `Co-Authored-By` trailers, no `via .HAPI` attribution.
  - Author: `Flowgent Jaw <jameswong1376@gmail.com>`.
- **Base images** MUST use mirror registry `registry.cn-shenzhen.aliyuncs.com/wl4g/` for CN pullability.

## Code Quality

- **Any Code, config, or documentation change must follow high cohesion, low coupling. Logical structure must be clear — concise without losing core logic. If related dependent modules exist, they MUST be updated synchronously to remain consistent.**
- Convergent file/directory naming; avoid ad-hoc new directories
- Go conventions: lowercase packages, exported symbols capitalized, `-er`/`-or` interfaces
- Program to interfaces; high cohesion, low coupling

## Build & Test

- **Any code change MUST pass build and tests. Code that doesn't compile or pass tests is not acceptable.**

## Test Code Organization

- Only `*_test.go` files alongside source under `pkg/`
- Application E2E runners, service assets, and verifiers live directly under
  `use-cases/<case>/`; shared runtime helpers and their tests live under
  `tests/e2e_util/`. Unit-test mocks/fixtures live
  next to their package (no shared `tests/testutil/` — Go modules under
  `pkg/` don't share a test-only module dependency)
- The first-party `use-cases/<case>/deploy/flowgent/` directory contains only
  configuration, a Compose override, and links to formal root Docker assets.
  Use cases load `deploy/helm/flowgent` directly; a case-owned `flowgent/helm`
  directory or symlink is forbidden.
  Backend images use the single formal `deploy/docker/Dockerfile`; do not add
  `Dockerfile.core` or an all-in-one variant. Application manifests live under
  `deploy/flowgent/config/`. Formal middleware belongs to `deploy/helm/flowgent/`
  and `deploy/docker/docker-compose.yaml`, with default-on deployment switches.
  Use-case mocks live only in their case's `deploy/mock-xxx-service/` directories;
  never add `deploy/helm/e2e-*` charts or case-owned middleware copies.
  Case initialization and verification belong to the infrastructure verifier;
  AuthGuard uses the formal chart dependency, matching AgentVault integration.
- Workload images use linux/amd64. macOS Solana uses its prescribed arm64 image;
  SonarQube uses the same official version natively on arm64 engines.
  Registry and build arguments come from E2EImagesDefinitions.
- E2E scenario files use `verifier/<scenario>/sNN_description.py`, with class
  `E2ESNNDescriptionVerifier` and `deploy/init/verifier` lifecycle methods.

## Documentation Indexing

Start with [docs/index.md](../docs/index.md). Archived plans are low-priority
historical context and are not authoritative for the current implementation.

### Current Architecture & Design

| Doc | Summary |
|-----|---------|
| [docs/architecture/overview.md](../docs/architecture/overview.md) | L1 engine overview: cross-component calls, MQTT/ExecutionPlan contracts, deployment, observability, and physical component links |
| [docs/architecture/agent-flow.md](../docs/architecture/agent-flow.md) | Generic L2 application architecture: DAG model, capabilities, limits, and authoring practices |
| [docs/architecture/data-lifecycle.md](../docs/architecture/data-lifecycle.md) | Durable identity/revision model, instruction/knowledge/execution-memory separation, approvals, and vector retrieval |
| [docs/architecture/engine/wallet.md](../docs/architecture/engine/wallet.md) | AgentVault boundary and the remaining legacy x402 client integration gap |

### Use Cases

| Doc | Summary |
|-----|---------|
| [docs/index.md#applications](../docs/index.md#applications) | Application catalog and use-case README ownership rules |
| [use-cases/security-autonomy-fixer/README.md](../use-cases/security-autonomy-fixer/README.md) | Security Autonomy Fixer application design, nodes, edges, resources, limits, and verification entry points |
| [use-cases/security-autonomy-fixer/MANUAL_ACCEPTANCE.md](../use-cases/security-autonomy-fixer/MANUAL_ACCEPTANCE.md) | Current K8s E2E topology and reproducible manual acceptance checklist |
| [use-cases/security-autonomy-fixer/deploy/flowgent/config/mcps/sonarqube.yaml](../use-cases/security-autonomy-fixer/deploy/flowgent/config/mcps/sonarqube.yaml) | SonarQube MCP definition used by the canonical security fixer flow |
| [use-cases/autotest-generator/README.md](../use-cases/autotest-generator/README.md) | AutoTest Generator draft, owned resources, current execution gaps, and expected behavior |

### Active Development and History

| Doc | Summary |
|-----|---------|
| [docs/development/HARDCOVER.md](../docs/development/HARDCOVER.md) | Coding-agent migration handover, implemented matrix, known gaps, and next priorities |
| [docs/development/archive/](../docs/development/archive/) | Low-priority historical drafts and dated progress notes; not current specifications |

### Key Directories

```
pkg/          Flowgent Go modules (api, cache, cmd, config, core, messager, model, notifier, sandbox, storage, ...)
agentvault/   Git submodule/symlink to the independent Rust AgentVault repository
deploy/       Docker (docker/) + Helm chart (helm/flowgent/)
etc/          Reference config (flowgent.yaml)
use-cases/    Applications and their self-contained manifests/E2E assets
docs/         Current architecture, use cases, runbooks, active plans, and historical archives
tests/        E2E/integration tests
```
