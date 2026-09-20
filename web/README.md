# Flowgent Web

Enterprise web console for the Flowgent distributed AgentFlow orchestration
engine. The application provides an operational dashboard, a visual 12-node DAG
editor, run details and OpenTelemetry-correlated TaskRun tracking, scoped
knowledge, runtime Skills, MCP and LLM configuration, and scoped runtime
configuration. Authentication and authorization are provided by AuthGuard.

## Quick start

Requirements: Node.js 22+ and npm 12+.

```bash
npm ci
npm run dev
```

The development server listens on `http://localhost:5173` and proxies `/api`
and `/_` to `http://127.0.0.1:9999`. Override the target with
`FLOWGENT_API_URL`.

The console always uses the Flowgent API server. Production and development use
the same HTTP repository composition:

```bash
npm run format:check
npm run lint
npm run typecheck
npm test
npm run build
```

## Architecture

```text
src/
  app/          composition, router, shell and application state
  core/
    api/        transport, wire normalization and HTTP repositories
    domain/     stable models and repository interfaces
  features/     vertical business slices
  shared/
    components/ reusable accessible UI primitives
    i18n/       English and Chinese resources
    styles/     tokens, shell, components and feature styles
  test/         shared test setup
```

Pages depend on use-case repository interfaces implemented by HTTP adapters.
Every production query and mutation goes through the Flowgent API server; no
in-browser API or repository emulation exists.

The Go backend is the business API authority. AuthGuard owns login, identity
federation, account security, policy evaluation, and delivery of signed access
context to Flowgent.

## Data and secret safety

- Namespace is application state and the first segment of every query key.
- Flow definitions use the canonical JSON API shape directly.
- Plain-text HTTP errors become a normalized, redacted `ApiError`.
- LLM, MCP, notification, API key and IAM secret material is handled only by
  backend write-only contracts; masking after download is not a security
  boundary.

## Quality gates

All project checks are exposed through the root Makefile:

```bash
make test-web
```

## Container deployment

The production container serves the SPA through Nginx and proxies Flowgent API,
health, and WebSocket paths to `FLOWGENT_API_UPSTREAM`:

```bash
docker build -t flowgent-web:latest -f ../deploy/docker/Dockerfile.web .
docker run --rm -p 8080:8080 \
  -e FLOWGENT_API_UPSTREAM=http://flowgent-apiserver:9999 \
  flowgent-web:latest
```

Use Web, AuthGuard and the Flowgent API on one origin in production. The Helm
Gateway routes authentication endpoints to AuthGuard and business endpoints to
Flowgent.
