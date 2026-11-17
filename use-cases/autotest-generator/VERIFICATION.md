# Verification status

Five baseline groups are registered: resource/data-scope preflight (01), real
API/deployment/PG readiness (11), persisted-run tracing (13), run-scoped
Prometheus counters (51), and persisted-plan lifecycle logs (52). Every group is
a class with explicit `deploy/init/verifier` methods. The runner uses the shared
report/archive contract and rejects an empty matrix.

Default deployment is gated by preflight. It currently fails before any service
mutation because review agents/MCP manifests are missing, item references escape
map scope, and map/cycle execution is unsupported. `--no-verify` cannot bypass
this gate or turn a failed pipeline into success.

Telemetry checks require a real AutoTest execution identified by
`FLOWGENT_E2E_RUN_ID`, with matching API and PG evidence. Configure the actual
Jaeger (`FLOWGENT_JAEGER_UI`) and Prometheus (`FLOWGENT_E2E_PROMETHEUS_URL`)
endpoints; Prometheus discovery must retain the application's Kubernetes
`flowgent.io/run` label as `run_id`. Logs require the producing workload to still
exist. Missing run evidence or telemetry is a failure, never a successful stub.

Legacy GitHub/SonarQube Go MCP components have full build/Compose/Helm assets
and optional `manual-stdio` profiles. Their programs under `deploy/*/source/` use stdio while
Flowgent requires HTTP-only MCP. Their HTTP deployment and the complete
requirement → generation → approval → delivery scenario remain deferred until
resource/transport/map-join contracts are agreed. No complete application
acceptance result is claimed. See [confirmed scope](../../docs/development/use-cases-review.md) and
[compact runtime gaps](../../docs/development/e2e-runtime-gaps.md).
