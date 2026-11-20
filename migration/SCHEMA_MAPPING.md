# Flowgent bootstrap schema mapping

`postgres/20260616a/01_init.ddl.sql` and
`sqlite/20260616a/01_init.ddl.sql` are the complete bootstrap schemas. They are
derived from the effective result of every former incremental migration; they
are not a concatenation of those files. Historical increments are intentionally
removed, so a new deployment has one deterministic migration ledger entry.

The mapping below is semantic. There is no compatibility view and no second
write path for legacy names.

## Common field mapping

| Legacy field | Current field / behavior |
|---|---|
| `namespace_id` free text | `namespace_id` FK to `orh_namespace(id)` |
| `del_flag` | removed; lifecycle is represented only by `status` |
| `version` on content-bearing rows | `revision` on immutable revision tables |
| mutable-record concurrency implicit in timestamps | `row_version` CAS plus database-maintained `updated_at` |
| `labels` and non-relational extensions | `metadata`; core relationships remain typed columns/FKs |
| blank `created_by` / `updated_by` | canonical AuthGuard principal ID, including service principals |
| plaintext API keys or secrets | `credential_ref`, reference-valued `env`, or encrypted `sealed_secrets` |

IDs remain project-generated string IDs. The former `BIGSERIAL` used only by
`supervisor_log` is not propagated to unrelated entities.

## Orchestration and execution

| Legacy table / field | Current target | Notes |
|---|---|---|
| `orh_agentflow` | `orh_flow` + `orh_flow_revision` | Stable identity is separated from immutable DAG content. |
| `orh_agentflow.agentflow_id` | `orh_flow.name` | Name is unique within a Namespace; `orh_flow.id` is the stable FK identity. |
| `orh_agentflow.version` | `orh_flow_revision.revision` | Owner pointer is `orh_flow.current_revision_id` and is changed by CAS. |
| `definition`, `checksum`, `comment` | same fields on `orh_flow_revision` | Definition revisions are append-only. |
| `priority`, `mode` | removed | Already made obsolete by historical migrations; runtime boundary is explicit. |
| `namespace` | definition/runtime deployment data | It is not the authorization Namespace. |
| `orh_flowrun` | `orh_run` | Every Run locks both stable `flow_id` and `flow_revision_id`. |
| `agentflow_id`, `version` | `flow_id`, `flow_revision_id` | Both are composite-FK checked against the same Flow and Namespace. |
| `vars` | `input` | Run input is distinct from `context_snapshot`. |
| text `error` | JSON `error` | Structured failure data is preserved. |
| `labels` | `metadata` | Trigger fields remain first-class columns. |
| `task_runs` | `orh_node_run` | A business retry is a new `(run_id,node_key,attempt)` row. |
| `agentflow_run_id`, `node_id` | `run_id`, `node_key` | Canonical FK names replace transport aliases. |
| `retry_count`, `sequence` | `attempt` | Attempts are never merged across failed branches. |
| `exec_id` | `execution_id` | Namespace-unique dispatch identity. |
| `parent_task_run_id` | `parent_node_run_id` | Parent must belong to the same Run. |
| task transient JSON | `execution_memory`, `checkpoint` plus child tables | Durable checkpoints, messages, workspace version and fencing are not collapsed into one blob. |
| `supervisor_log` | `orh_event` and Run/NodeRun snapshots | The redundant mutable log table is removed; append-only events and immutable context snapshots are authoritative. |
| `subscription_routes` | removed | WebSocket/MQTT subscription routing is ephemeral runtime state, not durable domain state. |
| removed `orh_resource_pool` | no replacement | Runtime application/session placement is represented by Run runtime fields and controller state. |

New execution tables without a legacy equivalent are `orh_node_checkpoint`,
`orh_node_message`, `orh_dispatch`, `orh_event`, `orh_outbox`, and
`orh_effect_execution`. Together they provide restart consistency, fencing,
delivery deduplication and an auditable external-side-effect result.

## Agents, Skills, MCP and model providers

| Legacy table / field | Current target | Notes |
|---|---|---|
| mutable `llm_agent` | `llm_agent` + `llm_agent_revision` | Name/status live on identity; soul/instruction/schemas/model config live on immutable revisions. |
| `model`, `temperature`, `max_tokens` | `llm_agent_revision.model_config` | Provider-specific tuning stays cohesive. |
| `input_schema`, `output_schema` | same typed fields on `llm_agent_revision` | A save creates the next `revision`. |
| mutable `llm_skill` | `llm_skill` + `llm_skill_revision` | Instruction/model/tools are immutable per revision. |
| uploaded Skill files | `llm_skill_file` | Only safe metadata/hash/path are in DB; bytes live under revision `assets/` or `scripts/`. |
| `llm_mcp.url` | `llm_mcp.rpc_url` | `transport` is `http` or `stdio`; command/args remain structured JSON. |
| `llm_mcp.type` | `llm_mcp.transport` | MCP name is limited to `[A-Za-z0-9_-]+`. |
| plural `llm_providers` | singular `llm_provider` | One canonical model-provider table. |
| `provider`, `name` | `name` plus constrained `type` | `type` is exactly `openai`, `anthropic`, or `gemini`. |
| `endpoint` | `base_uri` | Browser/API aliases do not create duplicate DB columns. |
| plaintext `apikey` | `credential_ref` | Plaintext credentials are never persisted. |
| `model` | `default_model` | Available models remain in `models`. |

`llm_instruction` is new and deliberately separate from Agent revisions. It
owns Namespace/Flow instructions, enforces per-owner revision uniqueness, and
requires `orh_approval` before a revision can be published.

## Knowledge and execution memory

| Legacy table / field | Current target | Notes |
|---|---|---|
| `knowledge_entries` | `knw_source` → `knw_document` → `knw_document_revision` → `knw_content` | Source, owner, immutable source revision, hierarchy and retrievable content are normalized. |
| `category`, `tags` | document `metadata` | They are filters, not ownership or hierarchy. |
| `title` | `knw_document_revision.title` | Title is versioned with source content. |
| `content` | `knw_content.content` (`TEXT`) | Section/chunk/summary share one hierarchical table. |
| `source`, `source_ref` | `knw_source` and `source_uri`/`source_path` | External version and provenance are retained per revision. |
| `llm_memory.content` used as facts | Knowledge hierarchy above | Only after scope/source classification and human-approved publication. |
| `llm_memory.embedding` JSON array | `knw_embedding` | PostgreSQL uses native `vector`; SQLite maps relational metadata to profile-specific sqlite-vec `vec0 float[D]`. |
| `llm_memory` used as active conversation/tool state | `orh_node_run`, `orh_node_checkpoint`, `orh_node_message` | NodeRun state never becomes cross-Run knowledge implicitly. |

There is intentionally no universal replacement for `llm_memory`: its former
mixed meanings are separated. `knw_embedding_profile.profile_key` immutably
binds provider/model revision/dimensions/distance metric. Profiles never share
similarity calculations. PostgreSQL accepts the native vector limit (16,000
dimensions), using HNSW for supported indexed dimensions and exact scan above
that; sqlite-vec accepts its actual 8,192-dimension maximum.

`knw_candidate` is publication workflow state, not another knowledge store. It
contains the immutable candidate request and only
`unpublished|published|failed`; approval state lives solely in `orh_approval`.

## Approval, Namespace and remaining capabilities

| Legacy table / field | Current target | Notes |
|---|---|---|
| `human_approvals` | `orh_approval` | One fact source covers human gates, tools, payment, knowledge and instruction publication. |
| `token` | `id` | The API token alias is not a second identity column. |
| `approved`, `comment`, `resolved_at` | `status`, immutable `decision`, `decided_at` | Terminal decisions cannot be changed. |
| `timeout_seconds` | `expires_at` | Tool/payment requests require an expiry; expiry is frozen with the request. |
| implicit namespace strings | `orh_namespace` | Namespace instructions and metadata are not IAM records. |
| `orh_runtime_configuration.scope_type/scope_id` | `scope` + nullable `flow_id` | CHECK/partial unique indexes prevent NULL-based duplicate Namespace configs. |
| release `flow_id/flow_version` strings | `flow_id`, `flow_revision_id`, `flow_revision` | Release definitions are tied to a real immutable Flow revision. |
| grant `consumer_namespace` | `consumer_namespace_id` | Both producer and consumer are real Namespace FKs. |
| `a2a_task.state/task_json/version` | `orh_a2a_task.status/task/row_version` | Caller key remains a SHA-256 digest; no bearer credential is stored. |
| `nfy_channel` | `nfy_channel` | Notification capability is retained under its existing domain prefix. |
| `schema_migrations` | `schema_migrations` | Migration ledger is retained and is not an IAM table. |

No Flowgent user, role, permission, membership, session or credential table is
created. Authentication and authorization belong to AuthGuard; Flowgent stores
only canonical principal references in audit fields. Namespace, approval,
audit/event/outbox and the migration ledger are retained because they are not
IAM substitutes.

## Visibility and publication invariants

- System instruction is deployment-owned and has no mutable business table;
  a Run may archive its effective text/reference in `context_snapshot`.
- A Run snapshot stores revision IDs or knowledge index/version references, not
  copied knowledge bodies. Permission revocation is checked again at retrieval.
- Draft knowledge never changes `knw_document.current_revision_id`. The pointer
  can reference only a fully published revision.
- Candidate approval and publication run in one transaction with baseline CAS,
  an outbox idempotency key, and a stable content/request hash.
- Approval is not external-effect success. Tool/payment execution has its own
  idempotent `orh_effect_execution` record and result.
