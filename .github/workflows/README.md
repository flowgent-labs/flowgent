# CI/CD Architecture

Two workflows covering the full lifecycle from PR to release.

## Workflows

| File | Purpose | Trigger | Jobs |
|------|---------|---------|------|
| `ci.yml` | PR validation + daily/weekly regression | `pull_request: opened, synchronize, reopened` + `schedule` | `build-and-test` → `make build` + `make test-ut`; `integration-it` → `make test-it` |
| `release.yml` | Semver tag, cross-platform binaries, Docker images | `pull_request: closed + merged` + `push: main` | DAG: `release` (bump → tag) → `build-binaries` ∥ `build-images` → `upload-release` |

## Trigger Matrix

| Event | ci.yml | release.yml | What happens |
|-------|--------|-------------|--------------|
| PR opened / new commits pushed | ✅ | — | build + unit tests + integration tests |
| PR merged to main | — | ✅ | semver bump (feat:/fix:/refactor:) → git tag → cross-compile binaries → docker build & push to ghcr.io → GitHub Release |
| Direct push to main | — | ✅ | same as merge (commit message parsed for bump level) |
| Daily 03:07 UTC | ✅ | — | scheduled full test against main |
| Weekly Mon 08:37 UTC | ✅ | — | same as daily, plus deploy E2E if k8s available |

## Key Design Decisions

**PR merge does NOT run tests.** Tests ran on every push to the PR branch already; re-running on main is redundant and doubles CI spend.

**`release.yml` handles both `pull_request:closed+merged` AND `push:main`.** Dual trigger ensures the green check appears on the main-branch commit status. `concurrency: release-${{ github.ref }}` serialises the two events for a single merge so they don't race (second arrival is a no-op).

**Semver from commit type, not label.** `refactor:` → major, `feat:` → minor, `fix:` → patch. On squash-merge the original PR title becomes HEAD's message; on merge-commit the feature-branch tip's first line is used.

**Integration tests run in their own job with a longer timeout.** `integration-it` needs a PostgreSQL service container (pgvector on port 15432). If neither is available the affected tests skip gracefully rather than failing the build.

## `IN_CN_GFW` Environment Variable

| Value | Behaviour |
|-------|-----------|
| `"false"` (CI default) | No Go proxy/CDN — direct network (GitHub Actions has unthrottled access) |
| `"true"` (local behind GFW) | Routes `go mod download` through `goproxy.cn` inside Docker build; if `HTTPS_PROXY` is set it is used instead |
