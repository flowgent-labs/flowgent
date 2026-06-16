# Project Conventions

## Requirements

- **All builds MUST use `make` from the repository root.** `make help` shows all targets.
  Output goes to `bin/`.
  - Docker: `make build` / `make build-all` / `make build-image` / `make build-image-all`
  - Host dev: `make build-host` / `make build-host-all`
  - Utilities: `make test` / `make fmt` / `make clean` / `make help`
- **NEVER run `go build` directly** outside of `make build-host` / `make build-host-all`.
- Binaries under `bin/` are git-ignored. Do NOT commit them.
- **Git commit messages must be concise.** Keep subject under 72 chars.
  - No `Co-Authored-By` trailers, no `via .HAPI` attribution.
  - Author: `Flowgent Jaw <developers@flowgent-labs.com>`.
- **Base images** MUST use mirror registry `registry.cn-shenzhen.aliyuncs.com/wl4g/` for CN pullability.

## Code Quality

- **Any code, config, or documentation change must follow high cohesion, low coupling. Logical structure must be clear — concise without losing core logic. If related dependent modules exist, they MUST be updated synchronously to remain consistent.**
- Convergent file/directory naming; avoid ad-hoc new directories
- Go conventions: lowercase packages, exported symbols capitalized, `-er`/`-or` interfaces
- Program to interfaces; high cohesion, low coupling

## Build & Test

- **Any code change MUST pass build and tests. Code that doesn't compile or pass tests is not acceptable.**

## Test Code Organization

- Only `*_test.go` files alongside source under `pkg/`
- All E2E/integration tests under `tests/`; shared code in `tests/testutil/`
- E2E scenario files named `01-xxx` format

## Cost Awareness

- Delegate bulk exploration/file reading to Explore subagents
- Offload "dirty reads" to save main-context quality

---

# Documentation Index

## Architecture & Design (L1)

| Doc | Summary |
|-----|---------|
| [docs/01-L1-Engine-Architecture.md](docs/01-L1-Engine-Architecture.md) | Engine architecture: apiserver-DB pattern, MQTT bus, component roles, session/application modes, Helm deployment, sandbox-in-TM design |
| [docs/02-L1-x402-Economic-Support.md](docs/02-L1-x402-Economic-Support.md) | Optional x402 HTTP 402 payment layer: facilitator settlement, wallet, policy engine |
| [docs/03-L1-Build-Deploy-Deps-Images.md](docs/03-L1-Build-Deploy-Deps-Images.md) | Docker images for x402 facilitator, EVM Anvil, Solana validator |

## Use Cases (L2)

| Doc | Summary |
|-----|---------|
| [docs/10-L2-USE-CASES.md](docs/10-L2-USE-CASES.md) | Use case catalog; primary: Security Autonomy Fixer (12-phase CI/CD security pipeline) |
| [examples/security-autonomy-fixer/docs/E2E-security-fixer-v1.md](examples/security-autonomy-fixer/docs/E2E-security-fixer-v1.md) | V1 E2E: API-triggered pipeline on K3s, white-box PG/MQTT/Jaeger verification |
| [examples/security-autonomy-fixer/docs/E2E-security-fixer-v2.md](examples/security-autonomy-fixer/docs/E2E-security-fixer-v2.md) | V2 E2E: GitHub webhook → SonarQube fix, 30-node DAG, external services |
| [examples/security-autonomy-fixer/docs/SonarQube-Integration.md](examples/security-autonomy-fixer/docs/SonarQube-Integration.md) | SonarQube v26.4.0 Docker Compose deployment guide |

## Skills & MCPs

| Doc | Summary |
|-----|---------|
| [examples/security-autonomy-fixer/skills/nexus3-retrieval/SKILL.md](examples/security-autonomy-fixer/skills/nexus3-retrieval/SKILL.md) | nexus3-retrieval skill: AI-agent tool catalog for Nexus3 REST API + SonatypeIQ |

## Key Directories

```
pkg/          Go modules (api, cache, cmd, config, core, messager, model, notifier, sandbox, store, wallet)
deploy/       Docker (docker/) + Helm chart (helm/flowgent/)
etc/          Reference config (flowgent-dev.yaml)
examples/     Primary use case (security-autonomy-fixer: agents, flows, skills, docs, e2e-verification)
docs/         Architecture (01-03 L1) + Use Cases (10 L2)
tests/        E2E/integration tests
```
