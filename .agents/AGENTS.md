# Project Conventions

## Development (Requirements)

- **All builds MUST use `make` from the repository root.** `make help` shows all targets.
  Output goes to `bin/`.
  - Docker: `make build` / `make build:image:all` / `make build:image:core` / `make build:image:wallet` / `make build:image:all-in-one`
  - Host dev: `make build:all` / `make build:core` / `make build:wallet`
  - Utilities: `make test` / `make fmt` / `make clean` / `make help`
- **NEVER run `go build` directly** outside of `make build:xxx`
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

## Cost Awareness

- Delegate bulk exploration/file reading to Explore subagents
- Offload "dirty reads" to save main-context quality

---

# Documentation Indexing

## Architecture & Design (L1)

| Doc | Summary |
|-----|---------|
| [docs/01-L1-Engine-Architecture.md](../docs/01-L1-Engine-Architecture.md) | Engine architecture: apiserver-DB pattern, MQTT bus, component roles, session/application modes, Helm deployment, sandbox-in-TM design |
| [docs/02-L1-x402-Economic-Support.md](../docs/02-L1-x402-Economic-Support.md) | Optional x402 HTTP 402 payment layer: facilitator settlement, wallet, policy engine |
| [docs/03-L1-Build-Deploy-Deps-Images.md](../docs/03-L1-Build-Deploy-Deps-Images.md) | Docker images for x402 facilitator, EVM Anvil, Solana validator |

## Use Cases (L2)

| Doc | Summary |
|-----|---------|
| [docs/10-L2-USE-CASES.md](../docs/10-L2-USE-CASES.md) | Use case catalog; primary: Security Autonomy Fixer (12-phase CI/CD security pipeline) |
| [usecase/security-autonomy-fixer/e2e-verification/E2E-security-fixer.md](../usecase/security-autonomy-fixer/e2e-verification/E2E-security-fixer.md) | E2E verification context doc for Python scenario suite (runner.py, 8 scenarios) |
| [usecase/security-autonomy-fixer/e2e-verification/Integration-SonarQube.md](../usecase/security-autonomy-fixer/e2e-verification/Integration-SonarQube.md) | SonarQube v26.4.0 Docker Compose deployment guide |

## Skills & MCPs

| Doc | Summary |
|-----|---------|
| [usecase/security-autonomy-fixer/config/skills/nexus3-retrieval/SKILL.md](../usecase/security-autonomy-fixer/config/skills/nexus3-retrieval/SKILL.md) | nexus3-retrieval skill: AI-agent tool catalog for Nexus3 REST API + SonatypeIQ |

## Key Directories

```
pkg/          Go modules (api, cache, cmd, config, core, messager, model, notifier, sandbox, store, wallet)
deploy/       Docker (docker/) + Helm chart (helm/flowgent/)
etc/          Reference config (flowgent.yaml)
usecase/     Primary use case (security-autonomy-fixer: agents, flows, skills, e2e-verification)
docs/         Architecture (01-03 L1) + Use Cases (10 L2)
tests/        E2E/integration tests
```
