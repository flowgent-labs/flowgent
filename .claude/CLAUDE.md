# Project Conventions

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
