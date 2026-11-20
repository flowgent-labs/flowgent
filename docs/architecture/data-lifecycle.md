# Durable Data Lifecycle

[中文](data-lifecycle_ZH.md)

This document owns the current durable-data contract. The bootstrap schemas are
[`migration/postgres/20260616a/01_init.ddl.sql`](../../migration/postgres/20260616a/01_init.ddl.sql)
and
[`migration/sqlite/20260616a/01_init.ddl.sql`](../../migration/sqlite/20260616a/01_init.ddl.sql);
the legacy-to-current mapping is recorded in
[`migration/SCHEMA_MAPPING.md`](../../migration/SCHEMA_MAPPING.md).

## Domain Boundaries

| Prefix | Ownership |
|---|---|
| `orh_*` | Namespace, Flow revisions, Runs, NodeRuns, checkpoints, dispatch, events, outbox, approvals, effects, runtime configuration, and releases |
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

Flow, Agent, portable Skill, and Knowledge Document separate stable identity
from immutable content revisions:

- `orh_flow` points to `orh_flow_revision`.
- `llm_agent` points to `llm_agent_revision`.
- `llm_skill` points to `llm_skill_revision`; `llm_skill_file` belongs to a
  revision and stores content-addressed file metadata.
- `knw_document` points to `knw_document_revision`.

Publishing or saving creates a new revision and switches the owner pointer with
compare-and-swap semantics. A Run always references a concrete Flow revision.
The database enforces that current-revision pointers belong to their owner.

The public and persistence term for an execution attempt is **NodeRun**. Some
internal Go types still use the legacy name `TaskRunInfo`; this name MUST NOT be
exposed as a new REST or database contract.

## Instruction, Knowledge, and Execution Memory

These concepts MUST remain distinct:

- **Instruction** controls how work is performed. Namespace/Flow Instructions
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

- stable Flow identity and concrete Flow revision/checksum;
- referenced Agent revision identifiers;
- Namespace and Flow Instruction pointers;
- the authorized current Knowledge revision identifiers;
- the effective `summarize_enabled` value.

The result is stored in `orh_run.context_snapshot`. It stores revision lists or
index references, not a copy of the entire knowledge corpus. Permission
revocation remains effective after snapshot creation.

`orh_node_run`, `orh_node_checkpoint`, `orh_node_message`, `orh_dispatch`, and
the lease/fencing fields preserve the physical pieces required for safe retry.
Re-delivery of the same physical attempt continues the same attempt; a business
retry creates a new attempt and may restore a selected checkpoint. Stale
fencing-token writes are rejected.

Current limitation: snapshot capture is implemented, but runtime execution does
not yet enforce every captured revision. Agent execution can still resolve the
latest Agent by name, Knowledge search does not yet constrain retrieval to the
snapshot's `knowledge_revision_ids`, and the default System instruction is a
`deployment://system-instruction` reference rather than an archived body. The
schema supports durable checkpoint recovery, but orchestration does not yet
consistently reload a persisted checkpoint into a newly dispatched plan. These
are correctness gaps, not reserved behavior.

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
uses `knw_candidate` and the unified approval flow:

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

Flow summarization defaults to disabled. Run creation persists the effective
value after applying an explicit Run override or the Flow revision value. Only
a completed Run with an effective value of true can invoke the built-in
summarizer. It redacts obvious secrets, produces Flow-scoped candidates, and
always requires human approval; it never directly publishes Knowledge or
Instruction and does not reopen the completed Run.

## Schema Evolution

The current repository intentionally exposes clean bootstrap schemas instead of
retaining obsolete compatibility tables. This is correct for empty-database
initialization and the E2E environments. Before the next incompatible schema
change reaches persistent production data, the project MUST define and test a
non-destructive upgrade path in addition to updating the bootstrap DDL.

