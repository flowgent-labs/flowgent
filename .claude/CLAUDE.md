# Project Conventions

## Requirements

- **All builds MUST use `make` from the repository root.** Output goes to `bin/`.
  No host Go or MCP toolchain required — Docker pulls the Go builder image automatically.
  - `make build` — flowgent binary via Docker multi-stage (output: `bin/flowgent`)
  - `make build-all` — flowgent + 4 MCP binaries via Docker (output: `bin/`)
  - `make docker-build` — production Docker image (`flowgent:latest`)
  - `make docker-all-in-one` — all-in-one Docker image (`flowgent:all-in-one`)
  - `make test` — run all tests (requires host Go)
  - `make fmt` — format all modules (requires host Go)
  - `make clean` — remove `bin/`
- **NEVER run `go build` directly on the host** for producing binaries.
  - Forbidden: `cd src/cmd && go build ...`, `go build -o bin/flowgent ./...`
  - Forbidden: `cd examples/mcp-* && go build ...` (MCPs are built in Docker too)
  - Only exception: `go test` and `go fmt` may run on the host for development.
- Binaries under `bin/` are git-ignored. Do NOT commit them.
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
