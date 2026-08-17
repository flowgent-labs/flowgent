# API Server

[System overview](../overview.md) · [AgentFlow application layer](../agent-flow.md)

The API Server is Flowgent's control-plane gateway and the only process allowed
to connect to PostgreSQL or SQLite. External clients enter through REST or A2A;
all other engine components read and write durable state through
`FlowgentClient` REST calls. Flow lifecycle changes are published to MQTT.

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

**The only component with database access.** All other components read/write state
through the API Server's REST endpoints or via MQTT (real-time scheduling). This
aligns with Kubernetes' apiserver→etcd pattern.

1. **Sole DB connection** — PG/SQLite via a single connection pool (20 max)
2. **Flow definition cache** — in-memory map, invalidated on CRUD, pushed to Controller via watch
3. **MQTT lifecycle events** — publishes flow/run lifecycle events for real-time consumption by Controller, JM, and other components
4. **State write endpoint** — JM/TM/sandbox update run/task status via REST (FlowgentClient), notifier reads channels via API
5. **Multi-tenancy** — Auth (JWT/OIDC/GitHub OAuth) + rate limiting + namespace routing
6. **Scale independence** — stateless, 2+ replicas (JM is stateful, leader-elected)

### REST API (Port 9999)

| Route | Method | Description |
|-------|--------|-------------|
| `/_/healthz` | GET | Health check |
| `/api/v1/webhook/{provider}` | POST | SCM webhook trigger (GitHub/GitLab/Gitea) — per-provider body adapter |
| `/api/v1/{namespace}/agents` | GET/POST | List / Create agent definitions |
| `/api/v1/{namespace}/flows` | GET/POST | List / Create flows |
| `/api/v1/{namespace}/flows/{id}` | GET/PUT/DELETE | Flow CRUD |
| `/api/v1/{namespace}/flows/{id}/runs[...]` | GET/POST/DELETE | Flow-scoped run/task/approval/trace surface for exact-Flow grants |
| `/api/v1/{namespace}/flows/{id}/iam/{options,bindings}` | GET/POST/DELETE | Repository-style Flow Settings access management |
| `/api/v1/{namespace}/runtime-config[/environment\|/secrets]` | GET/PUT | Namespace environment defaults and write-only secrets |
| `/api/v1/{namespace}/flows/{id}/runtime-config[/environment\|/secrets]` | GET/PUT | Flow-local overrides plus redacted effective inheritance |
| `/api/v1/{namespace}/flows/{id}/runtime-config/resolved` | GET | Controller-only effective runtime values for K8s materialization |
| `/api/v1/{namespace}/skills` | GET/POST | List / Create runtime `kind=skill` DAG definitions |
| `/api/v1/{namespace}/skills/{id}` | GET/PUT/DELETE | Runtime Skill CRUD |
| `/api/v1/{namespace}/flows/trigger` | POST | Create PENDING run |
| `/api/v1/{namespace}/runs` | GET/POST | List runs (JM polls this) / Create run |
| `/api/v1/{namespace}/runs/{id}` | GET/PUT | Run status + update |
| `/api/v1/{namespace}/runs/{id}/tasks` | GET/POST | Task list + create (JM/TM write) |
| `/api/v1/{namespace}/runs/{id}/tasks/{tid}` | PUT | Update task status (TM writes) |
| `/api/v1/{namespace}/runs/{id}/trace` | GET | Query normalized OTel trace data for one owned run |
| `/api/v1/{namespace}/runs/{id}/cancel` | POST | Cancel a running flow |
| `/api/v1/{namespace}/runs/{id}/approvals` | GET | List approvals for one owned run |
| `/api/v1/{namespace}/runs/{id}/approvals/{token}/approve` | POST | Approve one owned run gate |
| `/api/v1/{namespace}/runs/{id}/approvals/{token}/reject` | POST | Reject one owned run gate |
| `/api/v1/{namespace}/notifications/channels` | GET/POST | List / Create notification channels |
| `/api/v1/{namespace}/notifications/channels/{id}` | GET/PUT/DELETE | Notification channel CRUD |
| `/api/v1/{namespace}/llm/providers` | GET/POST | List / Create LLM provider definitions |
| `/api/v1/{namespace}/mcp` | GET/POST | List / Create Streamable HTTP MCP definitions |
| `/api/v1/{namespace}/iam/{principals,groups,roles,bindings}` | GET/POST/PUT/DELETE | Namespace organization membership, teams, roles, and grants |
| `/api/v1/{namespace}/iam/api-keys` | GET/POST/DELETE | One-time, revocable namespace machine credentials |
| `/api/v1/{namespace}/flow-releases` | GET/POST | Immutable shared Flow release catalog |
| `/api/v1/{namespace}/flow-releases/{id}/{grants,install}` | GET/POST/DELETE | Producer grants and consumer-owned installation copies |
| `/api/v1/human/approvals` | GET/POST | List pending / Create human approval |
| `/api/v1/human/{token}/approve` | POST | Human approval |
| `/api/v1/human/{token}/reject` | POST | Human rejection |

### Run Trace Query

The API Server queries Jaeger's read-only Query API and converts Jaeger storage
JSON into the stable `RunTrace`/`TraceInfo`/`TraceSpan` model. The UI never calls
Jaeger directly. Before querying, the handler loads the run and requires its
namespace to exactly match the route. A missing run returns 404, a disabled
query backend returns 503, and an upstream query failure returns 502. Successful
responses are `private, no-store`.

Jaeger is an observability source, not a recovery database. Exact per-attempt
input/output/error remains in TaskRun rows. Span correlation uses
`flowgent.task_id`, with `flowgent.node_id` and `flowgent.attempt` available for
diagnostics. The Query API is bounded by timeout, lookback, trace count, and a
16 MiB response limit configured under `mgmt.otel.query_*`.

Run and TaskRun handlers use the same exact namespace-ownership check before
read, mutation, cancellation, or task listing. Task update paths override
body-supplied run/namespace identity with the owned route identity and reject a
task ID already attached to another run.

### TaskRun Payload Storage

Complete per-attempt input/output is business audit data, not telemetry. The API
Server owns an `ITaskPayloadProvider` and transparently persists/resolves both
fields; JobManager, TaskManager, and the UI continue to use the unchanged
`TaskRunInfo` REST contract.

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

Runtime `/skills` routes manage `kind=skill` Flow definitions only. Portable
`SKILL.md` packages, files, archives, and deployment remain a separate future
API lifecycle.

### A2A Protocol (Port 9992)

The standalone and all-in-one compositions use the same A2A 0.3 server and
shared database-backed task store. The card advertises JSON-RPC and a Bearer
security scheme. Every protocol request validates its caller through API Server
`/api/v1/auth/me`, forwards that same credential to REST, and therefore receives
the caller's ordinary RBAC decision. The store partitions tasks by a SHA-256
credential digest; plaintext bearer credentials are never persisted.

| Route | Method | Description |
|-------|--------|-------------|
| `/.well-known/agent.json` | GET | A2A 0.3 Agent Card |
| `/` | POST | Standard JSON-RPC (`message/send`, `tasks/get`, and SDK task methods) |
| `/_/healthz` | GET | Process health; it does not bypass request authentication |

### Auth & Multi-Tenancy

The deployment is the implicit enterprise boundary; there is deliberately no
Enterprise or Workspace database entity. A `namespace_id` is the organization,
business-team, runtime, data, and secret-isolation boundary. Deployment-scoped
principals are mapped by immutable `(issuer, external_id)` identities and join
one or more namespaces through explicit membership edges. Removing membership
revokes only that organization's group memberships and bindings; it does not
delete the enterprise identity.

Authorization is default-deny and has three scopes: platform, namespace, and
exact resource. Groups are namespace-owned teams. Built-in or custom roles bind
users, service accounts, or teams; explicit DENY precedes ALLOW, and bindings
can expire or restrict source CIDRs. A Flow binding's resource is the exact
`namespace/flow` pair. Browser routes use
`/{namespace}/{flow}/...` (namespace-only administration stays under
`/namespaces/{namespace}/settings`), while REST run aliases nest beneath
the Flow, so Definition, runs, tasks, approvals, and traces share one coherent
repository-style boundary. Namespace owners manage membership under namespace
Settings; Flow owners manage direct grants under that Flow's Settings. The last
namespace-owner grant and self-removal are protected.

JWT (ES256/RS256/EdDSA), OIDC, and LDAP identities use the same evaluator.
Internal Controller/JM/TM/Notifier/A2A credentials are distinct rotatable
workload tokens delivered by Kubernetes Secrets. API keys are one-time
plaintext credentials whose database rows contain only a SHA-256 digest,
prefix/suffix metadata, expiry, revocation, namespace attenuation, and explicit
permission attenuation. Namespace owners may issue them for namespace service
accounts; the target's role bindings remain authoritative.

Cross-team reuse publishes an immutable producer-owned `FlowRelease`. A
consumer with an explicit grant installs a copy into its own namespace and then
owns its resource bindings, secrets, runs, traces, and results. Consumers never
execute against the producer's mutable Flow or secret scope.

### Submit Path (API → Store → JM)

The API Server is purely a persistence layer — it never calls the JM directly.
There are two ways runs enter the system:

**Path A: API Trigger (sync write, async execution)**

```
POST /api/v1/{namespace}/flows/trigger
  → agentFlowHandler validates spec exists
  → run := { AgentFlowID, Vars, Status:PENDING, Namespace, Namespace:"" }
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
  → trigger sources create PENDING FlowRun with namespace + resource_pool_id snapshot
  → active-run reconciliation creates the dedicated K8s JM Deployment
  ... (later, asynchronously) ...
  → Dedicated JM picks up only its Flow's namespace runs
  → jm.Submit(run, spec) → JobMaster.Execute()
```

---

## Expected Behavior

1. **Given** any durable resource mutation, **when** it succeeds, **then** API
   Server MUST be the process that commits it to PostgreSQL or SQLite.
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
