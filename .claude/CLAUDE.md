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
  # Example: ./deploy/docker/mirror-pull.sh golang:1.26-alpine golang
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
