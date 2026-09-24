# Project Conventions

## Development (Requirements)

- **All Flowgent Go builds and tests MUST use the repository-root `Makefile`.**
  `make help` shows the supported targets; binaries go to `bin/`.
  - Docker: `make build:image` / `make build:image:core`
  - Host dev: `make build` / `make build:core`
  - Utilities: `make test-ut` / `make test-x402` / `make test-it` / `make fmt` / `make clean`
- **The independent Rust Wallet MUST use only `wallet/Makefile` for its build,
  test, lint, and image lifecycle.** Use `make -C wallet help`. The Flowgent
  root Makefile MUST NOT proxy Wallet targets.
- **NEVER run `go build` directly** outside of `make build:xxx`
- **NEVER run `cargo build`, `cargo test`, or `docker build` directly for Wallet.**
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
- All E2E/integration tests under `tests/`; unit-test mocks/fixtures live
  next to their package (no shared `tests/testutil/` — Go modules under
  `pkg/` don't share a test-only module dependency)
- E2E scenario files named `01-xxx` format

## Documentation Indexing

Start with [docs/index.md](../docs/index.md). Archived plans are low-priority
historical context and are not authoritative for the current implementation.

### Current Architecture & Design

| Doc | Summary |
|-----|---------|
| [docs/architecture/overview.md](../docs/architecture/overview.md) | L1 engine overview: cross-component calls, MQTT/ExecutionPlan contracts, deployment, observability, and physical component links |
| [docs/architecture/agent-flow.md](../docs/architecture/agent-flow.md) | Generic L2 application architecture: DAG model, capabilities, limits, and authoring practices |
| [docs/architecture/engine/wallet.md](../docs/architecture/engine/wallet.md) | Optional external L1 Wallet: process-isolated secp256k1 key custody and EIP-712 digest signing |
| [docs/runbooks/build-dependency-images.md](../docs/runbooks/build-dependency-images.md) | Build and operate x402 facilitator, EVM Anvil, and Solana validator images |

### Use Cases

| Doc | Summary |
|-----|---------|
| [docs/use-cases/overview.md](../docs/use-cases/overview.md) | Concise application catalog and use-case README ownership rules |
| [usecase/security-autonomy-fixer/README.md](../usecase/security-autonomy-fixer/README.md) | Security Autonomy Fixer application design, nodes, edges, resources, limits, and verification entry points |
| [usecase/security-autonomy-fixer/e2e/VERIFICATION.md](../usecase/security-autonomy-fixer/e2e/VERIFICATION.md) | Current E2E environment, scenarios, assertions, and verification results |
| [usecase/security-autonomy-fixer/config/mcps/sonarqube.yaml](../usecase/security-autonomy-fixer/config/mcps/sonarqube.yaml) | SonarQube MCP definition used by the canonical security fixer flow |
| [usecase/autotest-generator/README.md](../usecase/autotest-generator/README.md) | AutoTest Generator draft, owned resources, current execution gaps, and expected behavior |

### Plans

| Doc | Summary |
|-----|---------|
| [docs/plans/e2e-improvements.md](../docs/plans/e2e-improvements.md) | Active integration and distributed E2E improvement requirements |
| [docs/plans/archive/](../docs/plans/archive/) | Low-priority historical drafts and dated progress notes; not current specifications |

### Key Directories

```
pkg/          Flowgent Go modules (api, cache, cmd, config, core, messager, model, notifier, sandbox, store)
wallet/       Git submodule pinned to the independent Rust Wallet repository
deploy/       Docker (docker/) + Helm chart (helm/flowgent/)
etc/          Reference config (flowgent.yaml)
usecase/     Primary use case (security-autonomy-fixer: agents, flows, skills, e2e)
docs/         Current architecture, use cases, runbooks, active plans, and historical archives
tests/        E2E/integration tests
```
