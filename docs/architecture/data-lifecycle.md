# Durable Data Lifecycle

[中文](data-lifecycle_ZH.md)

This document owns the current durable-data contract. The bootstrap schemas are
[`migration/postgres/20260616a/01_init.ddl.sql`](../../migration/postgres/20260616a/01_init.ddl.sql)
and
[`migration/sqlite/20260616a/01_init.ddl.sql`](../../migration/sqlite/20260616a/01_init.ddl.sql).

## Startup Migrations

[`pkg/storage/pkg/migrate.go`](../../pkg/storage/pkg/migrate.go) scans
`migration/{postgres|sqlite}/{version}/*.sql` at startup, ordered by version and
filename. `schema_migrations` records each applied file. A script and its ledger
entry commit in one transaction; failed files roll back and stop startup.
Successful earlier files remain recorded and are skipped on the next start.
PostgreSQL uses a schema-scoped advisory lock; SQLite obtains a writer lock
before checking the ledger, so concurrent startups cannot apply a file twice.

The backend image includes `/app/migration`. Native builds copy SQL beside the
binary under `migration/`; development tests can also find the repository tree.
`FLOWGENT_MIGRATION_DIR` overrides that root. A missing or empty SQL directory is
an error. Migration files contain DDL/DML without outer transaction commands;
add a new version/file instead of editing an applied script.

## Domain Boundaries

| Prefix | Ownership |
|---|---|
| `orh_*` | Namespace, AgentGraph revisions, Runs, NodeRuns, checkpoints, dispatch, events, outbox, approvals, effects, runtime configuration, and releases |
| `llm_*` | Providers, MCPs, Agents and revisions, portable Skill definitions and files, and Instructions |
| `knw_*` | Sources, Documents and revisions, hierarchical content, embedding profiles/vectors, and publication candidates |
| `nfy_*` | Encrypted notification-channel configuration |

Flowgent does not own IAM users, roles, policies, credentials, or sessions.
Audit actors are canonical AuthGuard principal references. Namespace, approval,
audit, event, and migration records are business/control-plane state rather than
IAM replicas.

The API Server is the canonical gateway for durable business state. Two narrow
exceptions are intentional: the standalone A2A server owns only
`orh_a2a_task`, and JobManager can use PostgreSQL or Redis as a coordination-lock
backend without reading or writing business tables directly.

## Stable Identity and Immutable Revisions

AgentGraph, Agent, portable Skill, and Knowledge Document separate stable identity
from immutable content revisions:

- `orh_agent_graph` points to `orh_agent_graph_revision`.
- `llm_agent` points to `llm_agent_revision`.
- `llm_skill` points to `llm_skill_revision`; `llm_skill_file` belongs to a
  revision and stores content-addressed file metadata.
- `knw_document` points to `knw_document_revision`.

Publishing or saving creates a new revision and switches the owner pointer with
compare-and-swap semantics. A Run always references a concrete AgentGraph revision.
The database enforces that current-revision pointers belong to their owner.

The canonical definition/runtime types are AgentGraph, GraphNode, GraphRun and
NodeRun. One NodeRun is one Attempt; node_instance_id remains stable across
retries. Old domain aliases are removed.

## Instruction, Knowledge, and Execution Memory

These concepts MUST remain distinct:

- **Instruction** controls how work is performed. Namespace/AgentGraph Instructions
  are immutable revisions in `llm_instruction`. System instruction is supplied
  by deployment and is not a mutable business record. A Run can archive an
  instruction body or immutable reference.
- **Knowledge** is retrievable fact or experience. It follows
  Source → Document → DocumentRevision → hierarchical Content → Embedding.
- **Execution memory** is state for one Run/NodeRun attempt: messages, tool
  state, workspace version, checkpoints, leases, and fencing tokens. A new Run
  does not inherit a previous Run's execution session.

`knw_content` represents sections, chunks, and summaries with parent/ordinal
relationships. Published Document revisions and their text are immutable.
Cross-run learning MUST enter Knowledge or Instruction through the approval and
publication path; it MUST NOT be copied from a failed session into a later Run.

## Context Snapshot and Recovery

Run creation uses a serializable transaction to lock:

- stable AgentGraph identity and concrete AgentGraph revision/checksum;
- referenced Agent revision identifiers;
- Namespace and AgentGraph Instruction pointers;
- the authorized current Knowledge revision identifiers;
- the effective `summarize_enabled` value.

The result is stored in `orh_graph_run.context_snapshot`. It stores revision lists or
index references, not a copy of the entire knowledge corpus. Permission
revocation remains effective after snapshot creation.

`orh_node_run`, `orh_node_checkpoint`, `orh_node_message`, `orh_dispatch`, and
the lease/fencing fields preserve the physical pieces required for safe retry.
Re-delivery of the same physical attempt continues the same attempt; a business
retry creates a new attempt and may restore a selected checkpoint. Stale
fencing-token writes are rejected.

GraphRun snapshots now close nested definitions and Agent revisions, and
execution reads the pinned agent revision. GraphCheckpoint recovery overlays the
committed NodeRun ledger; Sandbox retries restore a verified baseline. Knowledge
search still needs to constrain retrieval to knowledge_revision_ids; the default
System instruction remains a deployment reference. Unknown external effects and
unresolved live executors require reconciliation before execution continues.

## Knowledge Retrieval and Publication

PostgreSQL stores embeddings in native `pgvector` columns. SQLite uses the
`sqlite-vec` extension and maps metadata rows to dimension-specific
`vec0 float[D]` virtual tables. Vector data MUST NOT be represented as JSON or
text. An immutable profile key identifies model, model revision, dimensions,
and distance metric; different profiles are never compared together.

Implemented retrieval includes scoped full-text search, native vector search,
ACL/AuthGuard filtering, and parent/adjacent-content expansion. Authorization
is applied before content reaches an LLM. The current implementation does not
provide GraphRAG, an external graph/vector database, or a complete production
embedding-generation/ingestion pipeline.

Published Knowledge is read-only through the direct Knowledge API. New content
uses `knw_candidate` and the unified approval graph:

1. Create an immutable candidate with provenance, target scope, baseline
   revision, content hash, and idempotency key.
2. Create `orh_approval` with a frozen request and SHA-256 request hash.
3. Decide the approval once; expiry, cancellation, rejection, or request drift
   fails closed.
4. Publish in one transaction with baseline CAS, immutable revision creation,
   owner-pointer CAS, candidate transition, and outbox/event writes.

## Unified Approval and Summarization

`orh_approval` is the only approval fact source for `human_gate`, `tool_call`,
`payment`, `knowledge_publish`, and `instruction_publish`. The state machine is
`pending` → one of `approved`, `rejected`, `expired`, or `cancelled`; a terminal
decision cannot be changed. Approval proves authorization only. External side
effects still require idempotent execution and result verification.

AgentGraph summarization defaults to disabled. Run creation persists the effective
value after applying an explicit Run override or the AgentGraph revision value. Only
a completed Run with an effective value of true can invoke the built-in
summarizer. It redacts obvious secrets, produces AgentGraph-scoped candidates, and
always requires human approval; it never directly publishes Knowledge or
Instruction and does not reopen the completed Run.

## Schema Evolution

Version `20260616a` initializes an empty database; it does not convert the old
pre-1.0 layout. Further DDL/DML changes MUST append versioned files rather than
rewrite an applied bootstrap or migration. The pre-release canonical domain reset
in this project explicitly rebuilds the empty-database bootstrap; it does not
automatically migrate or delete an existing database. For released deployments, incompatible changes affecting
persisted data MUST include a tested, non-destructive upgrade path.

The current execution model is AgentGraph → GraphRun → NodeRun, with scoped
SubGraph / GraphRevision / GraphPatch. GraphRun creation closes nested definition
and agent-revision references; the snapshot is immutable. Checkpoint and workspace
commit/recovery semantics are owned by [AgentGraph durable recovery](agent-graph.md#agentgraph-domain-and-durable-recovery).
