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
comment for every revision.

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
