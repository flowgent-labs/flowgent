# CI/CD lifecycle

Flowgent follows the same PR-to-release lifecycle as AuthGuard. Delivery files
remain in their owning root directories: workflows and helper scripts under
`.github/`, the product chart under `deploy/helm/flowgent/`, the backend image
under `deploy/docker/`; the frontend source stays under `web/` and its image
recipe is `deploy/docker/Dockerfile.web`.

## Pull requests

`.github/workflows/ci.yml` runs for every PR revision targeting `main`:

1. Derive immutable `dirty-<head-sha-8>` image tags.
2. Build and push `flowgent` and `flowgent-web` dirty images.
3. Build the Go binary and run Go, web, runner, Helm, and integration tests.
4. Package a dirty Flowgent chart as a retained workflow artifact.
5. Run the complete security-autonomy-fixer E2E against those exact images and
   upload its reports and screenshots.

The workflow maintains one CI status comment on the PR rather than adding a new
comment for every revision. E2E summary tables are embedded as Markdown, not
wrapped in a text code block. Reports share a bounded comment budget so the
final status and artifact links remain visible.

GitHub-hosted E2E jobs explicitly use `IN_GFW=false` / `IN_CN_GFW=false`:
runtime images come from their declared upstream registries and do not require
a developer's jump-host loader. Local CN environments retain their existing
mirror-pull policy. SonarQube Compose startup first starts PostgreSQL, SonarQube
and its credential initializer, generates a real API token, then starts MCP.
The interpolation placeholder is never used to start the MCP service.
AuthGuard Web's published package is private, so both E2E jobs build its public
1.0.1 release source at the pinned commit, matching the vendored chart. Kubernetes
persistence is owned by the verifier's pgvector StatefulSet; CI does not start a
second, unused PostgreSQL container. Failed storage startup captures pod, PVC,
event and provisioner logs before cleanup.
The E2E Compose override requires Compose 2.24.4 or newer: `!override` replaces
published ports instead of inheriting production bindings. The formal stack's
EMQX dashboard uses port 18084, leaving 18083 for AuthGuard management. PostgreSQL
uses a startup probe so initialization and recovery are not interrupted by its
liveness probe.

## Merged PR releases

`.github/workflows/release.yml` runs only for merged PRs. The PR title selects
the semantic version bump:

- `refactor:` — major
- `feat:` — minor
- `fix:` — patch

Other titles do not publish a release. A release publishes multi-architecture
`flowgent` and `flowgent-web` images with immutable version and `latest` tags,
publishes `flowgent-<version>.tgz` to
`oci://ghcr.io/flowgent-labs/charts/flowgent`, pull-verifies that chart, and
attaches the same tgz to the GitHub release. Dirty images created by every
revision of the merged PR are removed only after all official artifacts publish
successfully.
