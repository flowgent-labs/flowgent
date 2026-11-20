# API Server

[System overview](../overview.md) · [AgentFlow application layer](../agent-flow.md)

The API Server is Flowgent's control-plane gateway and canonical owner of
durable business-state access. External clients enter through REST or A2A;
Controller, JM, TM, Sandbox, and Notifier read and write business state through
`FlowgentClient` REST calls. Flow lifecycle changes are published to MQTT.

The standalone A2A server is a narrow exception: it owns only its protocol task
table (`orh_a2a_task`). JobManager may also use PostgreSQL or Redis as a
coordination-lock backend, but it does not bypass the API for business tables.

```text
UI / REST / A2A / webhook
           │
           ▼
      API Server ──────► PostgreSQL / SQLite
           │
           ├──────────► S3 / GCS task artifacts (optional)
           ├──────────► MQTT lifecycle events
           ├──────────► Jaeger Query API (read-only trace proxy)
           └──────────► HTTP responses / trigger results
```

## API and State Gateway

**The only component with general business-table access.** Other execution
components read/write business state through REST or use MQTT for real-time
scheduling. The A2A task-store and coordination-lock exceptions above have
separate, bounded ownership.

1. **Canonical business DB gateway** — PostgreSQL/SQLite through the storage interfaces
2. **Flow definition cache** — in-memory map, invalidated on CRUD, pushed to Controller via watch
3. **MQTT lifecycle events** — publishes flow/run lifecycle events for real-time consumption by Controller, JM, and other components
4. **State write endpoint** — JM/TM/sandbox update run/task status via REST (FlowgentClient), notifier reads channels via API
5. **Multi-tenancy** — AuthGuard gateway policy + repository SQL scope + namespace routing
6. **Scale independence** — stateless, 2+ replicas (JM is stateful, leader-elected)

### REST API (Ports 9999 and 9990)

Port `9999` is the external business listener. When the AuthGuard adapter is
enabled, every business request must carry a valid signed access context and
its AuthGuard action must match the HTTP method. Port `9990` exposes the same
state API only to trusted Flowgent control-plane components and supplies the
dummy SDK SQL scope needed by Controller/JM/TM/Sandbox/Notifier. It must not be
published through the business Gateway.

| Route | Method | Description |
|-------|--------|-------------|
| `/_/healthz` | GET | Health check |
| `/api/v1/webhook/{provider}` | POST | SCM webhook trigger (GitHub/GitLab/Gitea) — per-provider body adapter |
| `/api/v1/{namespace}/agents` | GET/POST | List / Create agent definitions |
| `/api/v1/{namespace}/flows` | GET/POST | List / Create flows |
| `/api/v1/{namespace}/flows/{id}` | GET/PUT/DELETE | Flow CRUD |
| `/api/v1/{namespace}/flows/{id}/trigger` | POST | Create a `PENDING` Run from one stable Flow identity |
| `/api/v1/{namespace}/flows/{flow_id}/runs[...]` | GET/POST/DELETE | Flow-scoped Run/NodeRun/approval/trace surface for exact-Flow grants |
| `/api/v1/{namespace}/runtime-config[/environment\|/secrets]` | GET/PUT | Namespace environment defaults and write-only secrets |
| `/api/v1/{namespace}/flows/{id}/runtime-config[/environment\|/secrets]` | GET/PUT | Flow-local overrides plus redacted effective inheritance |
| `/api/v1/{namespace}/flows/{id}/runtime-config/resolved` | GET | Controller-only effective runtime values for K8s materialization |
| `/api/v1/{namespace}/skills` | GET/POST | List / Create runtime `kind=skill` DAG definitions |
| `/api/v1/{namespace}/skills/{id}` | GET/PUT/DELETE | Runtime Skill CRUD |
| `/api/v1/{namespace}/skill-definitions` | GET/POST | List / Create portable Skill definitions |
| `/api/v1/{namespace}/skill-definitions/{name}` | GET/PUT/DELETE | Portable Skill revision lifecycle |
| `/api/v1/{namespace}/skill-definitions/{name}/{assets\|scripts}` | POST | Add an allowlisted file and create a new Skill revision |
| `/api/v1/{namespace}/runs` | GET/POST | List runs (JM polls this) / Create run |
| `/api/v1/{namespace}/runs/{run_id}` | GET/PUT/DELETE | Run status, update, and deletion |
| `/api/v1/{namespace}/runs/{run_id}/node-runs` | GET/POST | NodeRun list + create (JM/TM write) |
| `/api/v1/{namespace}/runs/{run_id}/node-runs/{node_run_id}` | GET/PUT | Read/update one attempt |
| `/api/v1/{namespace}/runs/{run_id}/trace` | GET | Query normalized OTel trace data for one owned Run |
| `/api/v1/{namespace}/runs/{run_id}/cancel` | POST | Cancel a running Flow |
| `/api/v1/{namespace}/runs/{run_id}/approvals` | GET/POST | List / Create approvals for one owned Run |
| `/api/v1/{namespace}/runs/{run_id}/approvals/{approval_id}/{decision}` | POST | Resolve with `approve` or `reject` |
| `/api/v1/{namespace}/approvals` | GET | Namespace approval feed for Notifier |
| `/api/v1/{namespace}/notifications/channels` | GET/POST | List / Create notification channels |
| `/api/v1/{namespace}/notifications/channels/{id}` | GET/PUT/DELETE | Notification channel CRUD |
| `/api/v1/{namespace}/llm/providers` | GET/POST | List / Create LLM provider definitions |
| `/api/v1/{namespace}/mcp` | GET/POST | List / Create Streamable HTTP MCP definitions |
| `/api/v1/{namespace}/knowledge` | GET | List published, immutable Knowledge |
| `/api/v1/{namespace}/knowledge/search` | POST | Scoped full-text/vector retrieval |
| `/api/v1/{namespace}/knowledge/candidates` | POST | Create an approval-bound publication candidate |
| `/api/v1/{namespace}/flow-releases` | GET/POST | Immutable shared Flow release catalog |
| `/api/v1/{namespace}/flow-releases/{id}/{grants,install}` | GET/POST/DELETE | Producer grants and consumer-owned installation copies |

### Run Trace Query

The API Server queries Jaeger's read-only Query API and converts Jaeger storage
JSON into the stable `RunTrace`/`TraceInfo`/`TraceSpan` model. The UI never calls
Jaeger directly. Before querying, the handler loads the run and requires its
namespace to exactly match the route. A missing run returns 404, a disabled
query backend returns 503, and an upstream query failure returns 502. Successful
responses are `private, no-store`.

Jaeger is an observability source, not a recovery database. Exact per-attempt
input/output/error remains in NodeRun rows. Span correlation currently uses the
wire-compatible
`flowgent.task_id`, with `flowgent.node_id` and `flowgent.attempt` available for
diagnostics. The Query API is bounded by timeout, lookback, trace count, and a
16 MiB response limit configured under `mgmt.otel.query_*`.

Run and NodeRun handlers use the same exact namespace-ownership check before
read, mutation, cancellation, or attempt listing. NodeRun update paths override
body-supplied run/namespace identity with the owned route identity and reject a
NodeRun ID already attached to another Run.

### NodeRun Payload Storage

Complete per-attempt input/output is business audit data, not telemetry. The API
Server owns an `ITaskPayloadProvider` and transparently persists/resolves both
fields. The public/persistence term is NodeRun; `TaskRunInfo` remains a temporary
internal Go type name and MUST NOT define a new external contract.

Configuration belongs under `storage.artifacts`, not `mgmt.otel`:

```yaml
storage:
  artifacts:
    provider: default            # default | s3 | gcs
    inline_max_bytes: 262144     # S3/GCS hybrid inline threshold
    max_payload_bytes: 67108864
    compression: gzip            # none | gzip
    prefix: flowgent/task-runs
    put_timeout: 30s
    get_timeout: 30s
    verify_checksum: true
```

`DefaultTaskPayloadProvider` stores complete JSON in the existing DB columns.
`S3TaskPayloadProvider` and `GCSTaskPayloadProvider` keep small values inline
and replace larger values with a versioned reference containing the provider,
object key/URI, encoding, original/stored sizes, and SHA-256. Reads are bounded,
decompressed, checksum-verified, and hydrated before the REST response. DB
write failures clean new objects; Run deletion performs best-effort cleanup.

S3 uses the AWS SDK default credential chain unless explicit static credentials
are configured. GCS uses Application Default Credentials unless a credentials
file is configured; anonymous access requires an explicit emulator or compatible
endpoint. Provider changes require migrating existing references before rollout.

### Browser-Safe Runtime Credential References

LLM and MCP mutations persist environment references, never inline credential
values. LLM reads clear API-key/credential fields and return only `api_key_env`
plus `key_configured`. MCP reads redact sensitive header and env values while
returning safe `header_refs`/`env_refs` metadata. Runtime pods resolve those
references from the Secret configured by `runtime.credential_env_secret`; the
browser never receives the resolved value. Notification channels use a separate
dynamic-secret contract: provider-defined secret fields are encrypted with
AES-256-GCM before their channel row is committed. Reads return only
`configured_secret_fields`; an empty write preserves the existing value and
`clear_secret_fields` is the only way to remove it. The deployment supplies the
versioned master-key ring through `notifier.secret_encryption`; master keys are
never stored in the application database.

Runtime `/skills` routes manage `kind=skill` Flow definitions. Portable
`/skill-definitions` are a separate implemented lifecycle with immutable
revisions and allowlisted `assets/` and `scripts/` uploads.

### A2A Protocol (Port 9992)

The standalone and all-in-one compositions use the same A2A 0.3 server and
database-backed task store. In standalone mode A2A owns only `orh_a2a_task`.
When enabled, the AuthGuard adapter verifies
the signed gateway context and partitions tasks by a SHA-256 digest of the
verified principal ID. Calls from A2A to the API Server use the private
in-cluster control plane.

| Route | Method | Description |
|-------|--------|-------------|
| `/.well-known/agent.json` | GET | A2A 0.3 Agent Card |
| `/` | POST | Standard JSON-RPC (`message/send`, `tasks/get`, and SDK task methods) |
| `/_/healthz` | GET | Process health |

### AuthGuard & Multi-Tenancy

AuthGuard owns authentication, GitHub/LDAP federation, sessions, principals,
roles, policies, and authorization audit. Flowgent only verifies the signed
AuthGuard access context with the official Go adapters SDK and converts grants
to `FlowgentSqlScope` for business queries. When integration is disabled the
scope is a no-op. The API Service is private by default, and public requests
must traverse the AuthGuard-managed Envoy Gateway. See
[AuthGuard integration](../authguard-integration.md).

Cross-team reuse publishes an immutable producer-owned `FlowRelease`. A
consumer with an explicit grant installs a copy into its own namespace and then
owns its resource bindings, secrets, runs, traces, and results. Consumers never
execute against the producer's mutable Flow or secret scope.

### Submit Path (API → Store → JM)

The API Server is purely a persistence layer — it never calls the JM directly.
There are two ways runs enter the system:

**Path A: API Trigger (sync write, async execution)**

```
POST /api/v1/{namespace}/flows/{flow_id}/trigger
  → flow handler resolves the stable Flow ID/name and current revision
  → run := { FlowID, FlowRevisionID, Input, Status:PENDING, ContextSnapshot }
  → persist via apiserver store       // ← sync ends here
  → returns run_id to caller
  ... (later, asynchronously) ...
  → JM's runPoller (2s tick) finds PENDING run via FlowgentClient.ListRuns
  → jm.Submit(run, spec) → JobMaster.Execute()
```

**Path B: Controller Dispatch (fully async)**

```
Controller polls apiserver ListFlows every 10s:
  → Hash-mod shard: only processes owned flows
  → trigger sources create PENDING FlowRun with namespace + runtime_mode snapshot
  → active-run reconciliation creates the dedicated K8s JM Deployment
  ... (later, asynchronously) ...
  → Dedicated JM picks up only its Flow's namespace runs
  → jm.Submit(run, spec) → JobMaster.Execute()
```

---

## Expected Behavior

1. **Given** any Flowgent business-resource mutation other than the bounded A2A
   task-store exception, **when** it succeeds, **then** API Server MUST be the
   process that commits it to PostgreSQL or SQLite.
2. **Given** a valid REST, A2A, or webhook trigger, **when** it is accepted,
   **then** API Server MUST persist one `PENDING` run before returning its ID.
3. **Given** a flow create, update, or delete, **when** persistence succeeds,
   **then** the corresponding MQTT lifecycle event MUST be published for
   Controller reconciliation.
4. **Given** an unauthorized namespace request, **when** authentication or RBAC
   evaluation fails, **then** no resource state may be disclosed or mutated.
5. **Given** a non-API component state update, **when** REST validation fails,
   **then** the database MUST remain unchanged and the caller MUST receive an
   actionable failure.
