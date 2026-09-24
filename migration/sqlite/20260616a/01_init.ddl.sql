-- Flowgent SQLite bootstrap schema.
-- This is the complete empty-database schema. Vectors are stored in one
-- sqlite-vec vec0 virtual table per immutable embedding profile; knw_embedding
-- contains the relational metadata and the vec0 row mapping.

PRAGMA foreign_keys = ON;
PRAGMA recursive_triggers = OFF;

CREATE TABLE IF NOT EXISTS schema_migrations (
    version TEXT NOT NULL,
    filename TEXT NOT NULL,
    applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (version, filename)
);

CREATE TABLE orh_namespace (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    instruction_id TEXT,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    UNIQUE (name),
    UNIQUE (id, name),
    FOREIGN KEY (instruction_id, id) REFERENCES llm_instruction(id, namespace_id)
        DEFERRABLE INITIALLY DEFERRED
);
CREATE UNIQUE INDEX uq_orh_namespace_name_ci ON orh_namespace(lower(name));

CREATE TABLE orh_flow (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    current_revision_id TEXT,
    instruction_id TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    FOREIGN KEY (namespace_id) REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    FOREIGN KEY (current_revision_id, id, namespace_id)
        REFERENCES orh_flow_revision(id, flow_id, namespace_id)
        DEFERRABLE INITIALLY DEFERRED,
    FOREIGN KEY (instruction_id, id, namespace_id)
        REFERENCES llm_instruction(id, flow_id, namespace_id)
        DEFERRABLE INITIALLY DEFERRED,
    UNIQUE (namespace_id, name),
    UNIQUE (id, namespace_id)
);
CREATE UNIQUE INDEX uq_orh_flow_name_ci ON orh_flow(namespace_id, lower(name));
CREATE INDEX idx_orh_flow_namespace_status ON orh_flow(namespace_id, status, updated_at DESC);

CREATE TABLE orh_flow_revision (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL,
    flow_id TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK (revision > 0),
    definition TEXT NOT NULL CHECK (json_valid(definition)),
    summarize_enabled INTEGER NOT NULL DEFAULT 0 CHECK (summarize_enabled IN (0, 1)),
    checksum TEXT NOT NULL CHECK (length(checksum) = 64 AND checksum NOT GLOB '*[^0-9a-f]*'),
    comment TEXT,
    description TEXT,
    status TEXT NOT NULL DEFAULT 'PUBLISHED',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    FOREIGN KEY (flow_id, namespace_id) REFERENCES orh_flow(id, namespace_id) ON DELETE RESTRICT,
    UNIQUE (flow_id, revision),
    UNIQUE (id, flow_id, namespace_id),
    UNIQUE (id, namespace_id)
);
CREATE INDEX idx_orh_flow_revision_owner ON orh_flow_revision(flow_id, revision DESC);

CREATE TABLE llm_provider (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    name TEXT NOT NULL CHECK (name <> '' AND name NOT GLOB '*[^A-Za-z0-9_-]*'),
    type TEXT NOT NULL CHECK (type IN ('openai', 'anthropic', 'gemini')),
    base_uri TEXT NOT NULL,
    credential_ref TEXT,
    default_model TEXT,
    models TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(models) AND json_type(models) = 'array'),
    timeout_ms INTEGER NOT NULL DEFAULT 30000 CHECK (timeout_ms > 0),
    env TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(env) AND json_type(env) = 'object'),
    description TEXT,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    UNIQUE (namespace_id, name),
    UNIQUE (id, namespace_id)
);
CREATE UNIQUE INDEX uq_llm_provider_name_ci ON llm_provider(namespace_id, lower(name));

CREATE TABLE llm_mcp (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    name TEXT NOT NULL CHECK (name <> '' AND name NOT GLOB '*[^A-Za-z0-9_-]*'),
    transport TEXT NOT NULL DEFAULT 'http' CHECK (transport IN ('http', 'stdio')),
    rpc_url TEXT,
    command TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(command) AND json_type(command) = 'array'),
    args TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(args) AND json_type(args) = 'array'),
    headers TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(headers) AND json_type(headers) = 'object'),
    env TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(env) AND json_type(env) = 'object'),
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    description TEXT,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    CHECK ((transport = 'http' AND rpc_url IS NOT NULL AND trim(rpc_url) <> '') OR
           (transport = 'stdio' AND json_array_length(command) > 0)),
    UNIQUE (namespace_id, name),
    UNIQUE (id, namespace_id)
);
CREATE UNIQUE INDEX uq_llm_mcp_name_ci ON llm_mcp(namespace_id, lower(name));

CREATE TABLE llm_agent (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    description TEXT,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    current_revision_id TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    FOREIGN KEY (current_revision_id, id, namespace_id)
        REFERENCES llm_agent_revision(id, agent_id, namespace_id)
        DEFERRABLE INITIALLY DEFERRED,
    UNIQUE (namespace_id, name),
    UNIQUE (id, namespace_id)
);
CREATE UNIQUE INDEX uq_llm_agent_name_ci ON llm_agent(namespace_id, lower(name));

CREATE TABLE llm_agent_revision (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK (revision > 0),
    soul TEXT NOT NULL,
    instruction TEXT NOT NULL,
    model_config TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(model_config)),
    input_schema TEXT CHECK (input_schema IS NULL OR json_valid(input_schema)),
    output_schema TEXT CHECK (output_schema IS NULL OR json_valid(output_schema)),
    description TEXT,
    status TEXT NOT NULL DEFAULT 'PUBLISHED',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    FOREIGN KEY (agent_id, namespace_id) REFERENCES llm_agent(id, namespace_id) ON DELETE RESTRICT,
    UNIQUE (agent_id, revision),
    UNIQUE (id, agent_id, namespace_id),
    UNIQUE (id, namespace_id)
);
CREATE INDEX idx_llm_agent_revision_owner ON llm_agent_revision(agent_id, revision DESC);

CREATE TABLE llm_skill (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    description TEXT,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    current_revision_id TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    FOREIGN KEY (current_revision_id, id, namespace_id)
        REFERENCES llm_skill_revision(id, skill_id, namespace_id)
        DEFERRABLE INITIALLY DEFERRED,
    UNIQUE (namespace_id, name),
    UNIQUE (id, namespace_id)
);
CREATE UNIQUE INDEX uq_llm_skill_name_ci ON llm_skill(namespace_id, lower(name));

CREATE TABLE llm_skill_revision (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL,
    skill_id TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK (revision > 0),
    instruction TEXT NOT NULL,
    model_config TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(model_config)),
    tools TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(tools) AND json_type(tools) = 'array'),
    description TEXT,
    status TEXT NOT NULL DEFAULT 'PUBLISHED',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    FOREIGN KEY (skill_id, namespace_id) REFERENCES llm_skill(id, namespace_id) ON DELETE RESTRICT,
    UNIQUE (skill_id, revision),
    UNIQUE (id, skill_id, namespace_id),
    UNIQUE (id, namespace_id)
);

CREATE TABLE llm_skill_file (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL,
    skill_revision_id TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('asset', 'script')),
    relative_path TEXT NOT NULL CHECK (substr(relative_path, 1, 1) <> '/' AND
        ('/' || relative_path || '/') NOT LIKE '%/../%'),
    media_type TEXT NOT NULL,
    size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
    content_hash TEXT NOT NULL CHECK (length(content_hash) = 64 AND content_hash NOT GLOB '*[^0-9a-f]*'),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    FOREIGN KEY (skill_revision_id, namespace_id)
        REFERENCES llm_skill_revision(id, namespace_id) ON DELETE RESTRICT,
    UNIQUE (skill_revision_id, kind, relative_path)
);

CREATE TABLE orh_run (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL,
    flow_id TEXT NOT NULL,
    flow_revision_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'PENDING',
    input TEXT CHECK (input IS NULL OR json_valid(input)),
    output TEXT CHECK (output IS NULL OR json_valid(output)),
    error TEXT CHECK (error IS NULL OR json_valid(error)),
    run_instruction TEXT,
    summarize_enabled INTEGER NOT NULL DEFAULT 0 CHECK (summarize_enabled IN (0, 1)),
    context_snapshot TEXT NOT NULL CHECK (json_valid(context_snapshot)),
    trigger_type TEXT,
    trigger_source TEXT,
    trigger_payload TEXT CHECK (trigger_payload IS NULL OR json_valid(trigger_payload)),
    runtime_mode TEXT NOT NULL DEFAULT 'application' CHECK (runtime_mode IN ('application', 'session')),
    runtime_cluster_id TEXT,
    started_at TEXT,
    finished_at TEXT,
    description TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    FOREIGN KEY (flow_id, namespace_id) REFERENCES orh_flow(id, namespace_id) ON DELETE RESTRICT,
    FOREIGN KEY (flow_revision_id, flow_id, namespace_id)
        REFERENCES orh_flow_revision(id, flow_id, namespace_id) ON DELETE RESTRICT,
    CHECK (finished_at IS NULL OR started_at IS NULL OR finished_at >= started_at),
    UNIQUE (id, flow_id, namespace_id),
    UNIQUE (id, namespace_id)
);
CREATE INDEX idx_orh_run_namespace_status ON orh_run(namespace_id, status, created_at DESC);
CREATE INDEX idx_orh_run_flow_created ON orh_run(flow_id, created_at DESC);

CREATE TABLE orh_node_run (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    node_key TEXT NOT NULL,
    attempt INTEGER NOT NULL DEFAULT 1 CHECK (attempt > 0),
    agent_revision_id TEXT,
    status TEXT NOT NULL DEFAULT 'PENDING',
    input TEXT CHECK (input IS NULL OR json_valid(input)),
    output TEXT CHECK (output IS NULL OR json_valid(output)),
    error TEXT CHECK (error IS NULL OR json_valid(error)),
    execution_memory TEXT CHECK (execution_memory IS NULL OR json_valid(execution_memory)),
    checkpoint TEXT CHECK (checkpoint IS NULL OR json_valid(checkpoint)),
    workspace_version TEXT,
    parent_node_run_id TEXT,
    execution_id TEXT NOT NULL,
    lease_owner TEXT,
    lease_expires_at TEXT,
    fencing_token INTEGER NOT NULL DEFAULT 0 CHECK (fencing_token >= 0),
    last_heartbeat_at TEXT,
    started_at TEXT,
    finished_at TEXT,
    description TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    FOREIGN KEY (run_id, namespace_id) REFERENCES orh_run(id, namespace_id) ON DELETE RESTRICT,
    FOREIGN KEY (agent_revision_id, namespace_id)
        REFERENCES llm_agent_revision(id, namespace_id) ON DELETE RESTRICT,
    FOREIGN KEY (parent_node_run_id, run_id, namespace_id)
        REFERENCES orh_node_run(id, run_id, namespace_id) ON DELETE RESTRICT,
    CHECK (finished_at IS NULL OR started_at IS NULL OR finished_at >= started_at),
    UNIQUE (run_id, node_key, attempt),
    UNIQUE (namespace_id, execution_id),
    UNIQUE (id, run_id, namespace_id),
    UNIQUE (id, namespace_id)
);
CREATE INDEX idx_orh_node_run_run_status ON orh_node_run(run_id, status, node_key, attempt DESC);
CREATE INDEX idx_orh_node_run_lease ON orh_node_run(status, lease_expires_at) WHERE lease_owner IS NOT NULL;

CREATE TABLE orh_node_checkpoint (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL,
    node_run_id TEXT NOT NULL,
    sequence INTEGER NOT NULL CHECK (sequence > 0),
    execution_memory TEXT CHECK (execution_memory IS NULL OR json_valid(execution_memory)),
    checkpoint TEXT NOT NULL CHECK (json_valid(checkpoint)),
    workspace_version TEXT NOT NULL,
    fencing_token INTEGER NOT NULL CHECK (fencing_token >= 0),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    FOREIGN KEY (node_run_id, namespace_id) REFERENCES orh_node_run(id, namespace_id) ON DELETE RESTRICT,
    UNIQUE (node_run_id, sequence),
    UNIQUE (id, node_run_id, namespace_id)
);
CREATE INDEX idx_orh_node_checkpoint_latest ON orh_node_checkpoint(node_run_id, sequence DESC);

CREATE TABLE orh_node_message (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL,
    node_run_id TEXT NOT NULL,
    checkpoint_id TEXT,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    role TEXT NOT NULL CHECK (role IN ('system', 'user', 'assistant', 'tool')),
    content TEXT NOT NULL,
    content_hash TEXT NOT NULL CHECK (length(content_hash) = 64 AND content_hash NOT GLOB '*[^0-9a-f]*'),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    FOREIGN KEY (node_run_id, namespace_id) REFERENCES orh_node_run(id, namespace_id) ON DELETE RESTRICT,
    FOREIGN KEY (checkpoint_id, node_run_id, namespace_id)
        REFERENCES orh_node_checkpoint(id, node_run_id, namespace_id) ON DELETE RESTRICT,
    UNIQUE (node_run_id, ordinal)
);

CREATE TABLE orh_dispatch (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL,
    node_run_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    payload TEXT NOT NULL CHECK (json_valid(payload)),
    available_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    lease_owner TEXT,
    lease_expires_at TEXT,
    fencing_token INTEGER NOT NULL DEFAULT 0 CHECK (fencing_token >= 0),
    delivery_count INTEGER NOT NULL DEFAULT 0 CHECK (delivery_count >= 0),
    idempotency_key TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    FOREIGN KEY (node_run_id, namespace_id) REFERENCES orh_node_run(id, namespace_id) ON DELETE RESTRICT,
    UNIQUE (node_run_id),
    UNIQUE (namespace_id, idempotency_key)
);
CREATE INDEX idx_orh_dispatch_claim ON orh_dispatch(status, available_at, lease_expires_at);

CREATE TABLE orh_event (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    run_id TEXT,
    node_run_id TEXT,
    sequence INTEGER NOT NULL CHECK (sequence > 0),
    type TEXT NOT NULL,
    payload TEXT NOT NULL CHECK (json_valid(payload)),
    idempotency_key TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    FOREIGN KEY (run_id, namespace_id) REFERENCES orh_run(id, namespace_id) ON DELETE RESTRICT,
    FOREIGN KEY (node_run_id, run_id, namespace_id)
        REFERENCES orh_node_run(id, run_id, namespace_id) ON DELETE RESTRICT,
    CHECK (node_run_id IS NULL OR run_id IS NOT NULL),
    UNIQUE (namespace_id, idempotency_key),
    UNIQUE (run_id, sequence)
);
CREATE INDEX idx_orh_event_run_sequence ON orh_event(run_id, sequence);

CREATE TABLE orh_outbox (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    aggregate_type TEXT NOT NULL,
    aggregate_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload TEXT NOT NULL CHECK (json_valid(payload)),
    status TEXT NOT NULL DEFAULT 'pending',
    idempotency_key TEXT NOT NULL,
    available_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    published_at TEXT,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    UNIQUE (namespace_id, idempotency_key)
);
CREATE INDEX idx_orh_outbox_pending ON orh_outbox(status, available_at) WHERE status = 'pending';

CREATE TABLE orh_approval (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    run_id TEXT,
    node_run_id TEXT,
    type TEXT NOT NULL CHECK (type IN ('human_gate', 'tool_call', 'payment',
        'knowledge_publish', 'instruction_publish')),
    subject_type TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    request TEXT NOT NULL CHECK (json_valid(request)),
    request_hash TEXT NOT NULL CHECK (length(request_hash) = 64 AND request_hash NOT GLOB '*[^0-9a-f]*'),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN
        ('pending', 'approved', 'rejected', 'expired', 'cancelled')),
    decision TEXT CHECK (decision IS NULL OR json_valid(decision)),
    decided_by TEXT,
    decided_at TEXT,
    expires_at TEXT,
    consumed_at TEXT,
    idempotency_key TEXT NOT NULL,
    description TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    FOREIGN KEY (run_id, namespace_id) REFERENCES orh_run(id, namespace_id) ON DELETE RESTRICT,
    FOREIGN KEY (node_run_id, run_id, namespace_id)
        REFERENCES orh_node_run(id, run_id, namespace_id) ON DELETE RESTRICT,
    CHECK (node_run_id IS NULL OR run_id IS NOT NULL),
    CHECK ((status = 'pending' AND decision IS NULL AND decided_by IS NULL AND decided_at IS NULL) OR
           (status <> 'pending' AND decided_at IS NOT NULL)),
    CHECK (type NOT IN ('tool_call', 'payment') OR expires_at IS NOT NULL),
    UNIQUE (namespace_id, idempotency_key),
    UNIQUE (id, namespace_id)
);
CREATE INDEX idx_orh_approval_pending ON orh_approval(namespace_id, expires_at, created_at)
    WHERE status = 'pending';
CREATE INDEX idx_orh_approval_run ON orh_approval(run_id, status, created_at DESC);

CREATE TABLE orh_effect_execution (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    node_run_id TEXT,
    approval_id TEXT NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('tool_call', 'payment')),
    request TEXT NOT NULL CHECK (json_valid(request)),
    request_hash TEXT NOT NULL CHECK (length(request_hash) = 64 AND request_hash NOT GLOB '*[^0-9a-f]*'),
    status TEXT NOT NULL DEFAULT 'pending',
    result TEXT CHECK (result IS NULL OR json_valid(result)),
    attempt INTEGER NOT NULL DEFAULT 1 CHECK (attempt > 0),
    idempotency_key TEXT NOT NULL,
    started_at TEXT,
    finished_at TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    FOREIGN KEY (run_id, namespace_id) REFERENCES orh_run(id, namespace_id) ON DELETE RESTRICT,
    FOREIGN KEY (node_run_id, run_id, namespace_id)
        REFERENCES orh_node_run(id, run_id, namespace_id) ON DELETE RESTRICT,
    FOREIGN KEY (approval_id, namespace_id) REFERENCES orh_approval(id, namespace_id) ON DELETE RESTRICT,
    UNIQUE (namespace_id, idempotency_key, attempt)
);
CREATE INDEX idx_orh_effect_approval ON orh_effect_execution(approval_id, status);

CREATE TABLE llm_instruction (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    scope TEXT NOT NULL CHECK (scope IN ('namespace', 'flow')),
    flow_id TEXT,
    revision INTEGER NOT NULL CHECK (revision > 0),
    content TEXT NOT NULL,
    content_hash TEXT NOT NULL CHECK (length(content_hash) = 64 AND content_hash NOT GLOB '*[^0-9a-f]*'),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'retired')),
    approval_id TEXT,
    description TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    FOREIGN KEY (flow_id, namespace_id) REFERENCES orh_flow(id, namespace_id) ON DELETE RESTRICT,
    FOREIGN KEY (approval_id, namespace_id) REFERENCES orh_approval(id, namespace_id) ON DELETE RESTRICT,
    CHECK ((scope = 'namespace' AND flow_id IS NULL) OR (scope = 'flow' AND flow_id IS NOT NULL)),
    CHECK (status <> 'published' OR approval_id IS NOT NULL),
    UNIQUE (id, namespace_id),
    UNIQUE (id, flow_id, namespace_id)
);
CREATE UNIQUE INDEX uq_llm_instruction_namespace_revision
    ON llm_instruction(namespace_id, revision) WHERE scope = 'namespace';
CREATE UNIQUE INDEX uq_llm_instruction_flow_revision
    ON llm_instruction(flow_id, revision) WHERE scope = 'flow';
CREATE INDEX idx_llm_instruction_owner_status
    ON llm_instruction(namespace_id, flow_id, status, revision DESC);

INSERT OR IGNORE INTO orh_namespace
    (id, name, description, created_by, updated_by)
VALUES ('default', 'default', 'Default namespace', 'system:bootstrap', 'system:bootstrap');

CREATE TABLE knw_source (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    config TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(config)),
    description TEXT,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    UNIQUE (namespace_id, name),
    UNIQUE (id, namespace_id)
);
CREATE UNIQUE INDEX uq_knw_source_name_ci ON knw_source(namespace_id, lower(name));

CREATE TABLE knw_document (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    source_id TEXT NOT NULL,
    document_key TEXT NOT NULL,
    scope TEXT NOT NULL CHECK (scope IN ('namespace', 'flow', 'run')),
    flow_id TEXT,
    run_id TEXT,
    external_id TEXT,
    source_uri TEXT,
    acl_ref TEXT,
    classification TEXT NOT NULL DEFAULT 'internal',
    current_revision_id TEXT,
    description TEXT,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    FOREIGN KEY (source_id, namespace_id) REFERENCES knw_source(id, namespace_id) ON DELETE RESTRICT,
    FOREIGN KEY (flow_id, namespace_id) REFERENCES orh_flow(id, namespace_id) ON DELETE RESTRICT,
    FOREIGN KEY (run_id, flow_id, namespace_id) REFERENCES orh_run(id, flow_id, namespace_id) ON DELETE RESTRICT,
    FOREIGN KEY (current_revision_id, id, namespace_id)
        REFERENCES knw_document_revision(id, document_id, namespace_id)
        DEFERRABLE INITIALLY DEFERRED,
    CHECK ((scope = 'namespace' AND flow_id IS NULL AND run_id IS NULL) OR
           (scope = 'flow' AND flow_id IS NOT NULL AND run_id IS NULL) OR
           (scope = 'run' AND flow_id IS NOT NULL AND run_id IS NOT NULL)),
    UNIQUE (namespace_id, document_key),
    UNIQUE (id, namespace_id)
);
CREATE INDEX idx_knw_document_scope ON knw_document(namespace_id, scope, flow_id, run_id, status);
CREATE INDEX idx_knw_document_source ON knw_document(source_id, status);

CREATE TABLE knw_document_revision (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK (revision > 0),
    source_revision TEXT,
    title TEXT NOT NULL,
    source_path TEXT,
    provenance TEXT NOT NULL CHECK (json_valid(provenance)),
    content_hash TEXT NOT NULL CHECK (length(content_hash) = 64 AND content_hash NOT GLOB '*[^0-9a-f]*'),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'indexing', 'published', 'failed', 'retired')),
    approval_id TEXT,
    description TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    FOREIGN KEY (document_id, namespace_id) REFERENCES knw_document(id, namespace_id) ON DELETE RESTRICT,
    FOREIGN KEY (approval_id, namespace_id) REFERENCES orh_approval(id, namespace_id) ON DELETE RESTRICT,
    CHECK (status <> 'published' OR approval_id IS NOT NULL),
    UNIQUE (document_id, revision),
    UNIQUE (id, document_id, namespace_id),
    UNIQUE (id, namespace_id)
);

CREATE TABLE knw_content (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL,
    document_revision_id TEXT NOT NULL,
    parent_id TEXT,
    type TEXT NOT NULL CHECK (type IN ('section', 'chunk', 'summary')),
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    heading_path TEXT,
    page_start INTEGER,
    page_end INTEGER,
    content TEXT NOT NULL,
    content_hash TEXT NOT NULL CHECK (length(content_hash) = 64 AND content_hash NOT GLOB '*[^0-9a-f]*'),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    FOREIGN KEY (document_revision_id, namespace_id)
        REFERENCES knw_document_revision(id, namespace_id) ON DELETE RESTRICT,
    FOREIGN KEY (parent_id, document_revision_id, namespace_id)
        REFERENCES knw_content(id, document_revision_id, namespace_id) ON DELETE RESTRICT,
    CHECK ((page_start IS NULL AND page_end IS NULL) OR
           (page_start IS NOT NULL AND page_end IS NOT NULL AND page_start > 0 AND page_end >= page_start)),
    UNIQUE (id, document_revision_id, namespace_id),
    UNIQUE (id, namespace_id)
);
CREATE UNIQUE INDEX uq_knw_content_root_ordinal
    ON knw_content(document_revision_id, ordinal) WHERE parent_id IS NULL;
CREATE UNIQUE INDEX uq_knw_content_child_ordinal
    ON knw_content(document_revision_id, parent_id, ordinal) WHERE parent_id IS NOT NULL;
CREATE INDEX idx_knw_content_parent ON knw_content(document_revision_id, parent_id, ordinal);

CREATE VIRTUAL TABLE knw_content_fts USING fts5(
    heading_path, content, content='knw_content', content_rowid='rowid', tokenize='unicode61'
);
CREATE TRIGGER trg_knw_content_fts_insert AFTER INSERT ON knw_content BEGIN
    INSERT INTO knw_content_fts(rowid, heading_path, content)
    VALUES (new.rowid, new.heading_path, new.content);
END;
CREATE TRIGGER trg_knw_content_fts_delete AFTER DELETE ON knw_content BEGIN
    INSERT INTO knw_content_fts(knw_content_fts, rowid, heading_path, content)
    VALUES ('delete', old.rowid, old.heading_path, old.content);
END;
CREATE TRIGGER trg_knw_content_fts_update AFTER UPDATE OF heading_path, content ON knw_content BEGIN
    INSERT INTO knw_content_fts(knw_content_fts, rowid, heading_path, content)
    VALUES ('delete', old.rowid, old.heading_path, old.content);
    INSERT INTO knw_content_fts(rowid, heading_path, content)
    VALUES (new.rowid, new.heading_path, new.content);
END;

CREATE TABLE knw_embedding_profile (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    profile_key TEXT NOT NULL,
    provider_type TEXT NOT NULL,
    model TEXT NOT NULL,
    model_revision TEXT NOT NULL,
    dimensions INTEGER NOT NULL CHECK (dimensions > 0 AND dimensions <= 8192),
    distance_metric TEXT NOT NULL CHECK (distance_metric IN ('cosine', 'l2', 'l1')),
    vector_table TEXT NOT NULL CHECK (vector_table GLOB 'knw_vec_[0-9a-f]*' AND
        vector_table NOT GLOB '*[^A-Za-z0-9_]*'),
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    UNIQUE (namespace_id, profile_key),
    UNIQUE (profile_key, namespace_id),
    UNIQUE (profile_key, namespace_id, dimensions, distance_metric),
    UNIQUE (vector_table)
);

CREATE TABLE knw_embedding (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL,
    content_id TEXT NOT NULL,
    profile_key TEXT NOT NULL,
    dimensions INTEGER NOT NULL CHECK (dimensions > 0),
    distance_metric TEXT NOT NULL CHECK (distance_metric IN ('cosine', 'l2', 'l1')),
    input_hash TEXT NOT NULL CHECK (length(input_hash) = 64 AND input_hash NOT GLOB '*[^0-9a-f]*'),
    vector_table TEXT NOT NULL,
    vector_rowid INTEGER NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    FOREIGN KEY (content_id, namespace_id) REFERENCES knw_content(id, namespace_id) ON DELETE RESTRICT,
    FOREIGN KEY (profile_key, namespace_id, dimensions, distance_metric)
        REFERENCES knw_embedding_profile(profile_key, namespace_id, dimensions, distance_metric)
        ON DELETE RESTRICT,
    FOREIGN KEY (vector_table) REFERENCES knw_embedding_profile(vector_table) ON DELETE RESTRICT,
    UNIQUE (content_id, profile_key),
    UNIQUE (vector_table, vector_rowid)
);
CREATE INDEX idx_knw_embedding_content ON knw_embedding(content_id, profile_key);

CREATE TABLE knw_candidate (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    source_run_id TEXT NOT NULL,
    target_scope TEXT NOT NULL CHECK (target_scope IN ('namespace', 'flow')),
    target_flow_id TEXT,
    target_document_id TEXT,
    type TEXT NOT NULL CHECK (type IN ('knowledge', 'instruction')),
    content TEXT NOT NULL,
    content_hash TEXT NOT NULL CHECK (length(content_hash) = 64 AND content_hash NOT GLOB '*[^0-9a-f]*'),
    provenance TEXT NOT NULL CHECK (json_valid(provenance)),
    expected_revision INTEGER NOT NULL CHECK (expected_revision >= 0),
    approval_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'unpublished' CHECK (status IN ('unpublished', 'published', 'failed')),
    idempotency_key TEXT NOT NULL,
    description TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    FOREIGN KEY (source_run_id, namespace_id) REFERENCES orh_run(id, namespace_id) ON DELETE RESTRICT,
    FOREIGN KEY (target_flow_id, namespace_id) REFERENCES orh_flow(id, namespace_id) ON DELETE RESTRICT,
    FOREIGN KEY (target_document_id, namespace_id) REFERENCES knw_document(id, namespace_id) ON DELETE RESTRICT,
    FOREIGN KEY (approval_id, namespace_id) REFERENCES orh_approval(id, namespace_id) ON DELETE RESTRICT,
    CHECK ((target_scope = 'namespace' AND target_flow_id IS NULL) OR
           (target_scope = 'flow' AND target_flow_id IS NOT NULL)),
    CHECK ((type = 'instruction' AND target_document_id IS NULL) OR type = 'knowledge'),
    UNIQUE (approval_id),
    UNIQUE (namespace_id, idempotency_key)
);
CREATE INDEX idx_knw_candidate_publish ON knw_candidate(namespace_id, status, created_at);

CREATE TABLE orh_runtime_configuration (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    scope TEXT NOT NULL CHECK (scope IN ('namespace', 'flow')),
    flow_id TEXT,
    environment TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(environment)),
    sealed_secrets TEXT CHECK (sealed_secrets IS NULL OR json_valid(sealed_secrets)),
    secret_keys TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(secret_keys)),
    description TEXT,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    FOREIGN KEY (flow_id, namespace_id) REFERENCES orh_flow(id, namespace_id) ON DELETE CASCADE,
    CHECK ((scope = 'namespace' AND flow_id IS NULL) OR (scope = 'flow' AND flow_id IS NOT NULL)),
    UNIQUE (namespace_id, scope, flow_id)
);
CREATE UNIQUE INDEX uq_orh_runtime_configuration_namespace
    ON orh_runtime_configuration(namespace_id) WHERE scope = 'namespace';
CREATE UNIQUE INDEX uq_orh_runtime_configuration_flow
    ON orh_runtime_configuration(flow_id) WHERE scope = 'flow';

CREATE TABLE orh_flow_release (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL,
    flow_id TEXT NOT NULL,
    flow_revision_id TEXT NOT NULL,
    flow_revision INTEGER NOT NULL CHECK (flow_revision > 0),
    release_version TEXT NOT NULL,
    definition TEXT NOT NULL CHECK (json_valid(definition)),
    checksum TEXT NOT NULL CHECK (length(checksum) = 64 AND checksum NOT GLOB '*[^0-9a-f]*'),
    visibility TEXT NOT NULL CHECK (visibility IN ('PRIVATE', 'SHARED')),
    published_at TEXT NOT NULL,
    description TEXT,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    FOREIGN KEY (flow_revision_id, flow_id, namespace_id)
        REFERENCES orh_flow_revision(id, flow_id, namespace_id) ON DELETE RESTRICT,
    UNIQUE (namespace_id, flow_id, release_version),
    UNIQUE (namespace_id, checksum),
    UNIQUE (id, namespace_id)
);
CREATE INDEX idx_orh_flow_release_catalog ON orh_flow_release(visibility, status, published_at DESC);

CREATE TABLE orh_flow_release_grant (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    release_id TEXT NOT NULL,
    consumer_namespace_id TEXT NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    expires_at TEXT,
    description TEXT,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    FOREIGN KEY (release_id, namespace_id) REFERENCES orh_flow_release(id, namespace_id) ON DELETE CASCADE,
    UNIQUE (release_id, consumer_namespace_id)
);

CREATE TABLE orh_flow_installation (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    release_id TEXT NOT NULL REFERENCES orh_flow_release(id) ON DELETE RESTRICT,
    release_version TEXT NOT NULL,
    producer_namespace_id TEXT NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    installed_flow_id TEXT NOT NULL,
    release_checksum TEXT NOT NULL,
    applied_checksum TEXT NOT NULL,
    resource_bindings TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(resource_bindings)),
    installed_at TEXT NOT NULL,
    description TEXT,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    FOREIGN KEY (installed_flow_id, namespace_id) REFERENCES orh_flow(id, namespace_id) ON DELETE RESTRICT,
    UNIQUE (namespace_id, installed_flow_id)
);

CREATE TABLE nfy_channel (
    id TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    channel_type TEXT NOT NULL,
    config TEXT NOT NULL CHECK (json_valid(config)),
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    description TEXT,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    UNIQUE (namespace_id, name)
);

CREATE TABLE orh_a2a_task (
    id TEXT PRIMARY KEY,
    caller_key TEXT NOT NULL CHECK (length(caller_key) = 64 AND caller_key NOT GLOB '*[^0-9a-f]*'),
    context_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    task TEXT NOT NULL CHECK (json_valid(task)),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata TEXT CHECK (metadata IS NULL OR json_valid(metadata))
);
CREATE INDEX idx_orh_a2a_task_caller_updated ON orh_a2a_task(caller_key, updated_at DESC, id DESC);
CREATE INDEX idx_orh_a2a_task_caller_context ON orh_a2a_task(caller_key, context_id, updated_at DESC);

-- State-machine and immutability guards.
CREATE TRIGGER trg_orh_namespace_instruction_pointer
BEFORE UPDATE OF instruction_id ON orh_namespace BEGIN
    SELECT CASE WHEN new.instruction_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM llm_instruction i WHERE i.id=new.instruction_id AND
            i.namespace_id=new.id AND i.scope='namespace' AND i.status='published')
        THEN RAISE(ABORT, 'namespace current instruction must be a published namespace instruction') END;
END;
CREATE TRIGGER trg_orh_flow_instruction_pointer
BEFORE UPDATE OF instruction_id ON orh_flow BEGIN
    SELECT CASE WHEN new.instruction_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM llm_instruction i WHERE i.id=new.instruction_id AND
            i.namespace_id=new.namespace_id AND i.flow_id=new.id AND
            i.scope='flow' AND i.status='published')
        THEN RAISE(ABORT, 'flow current instruction must be a published flow instruction') END;
END;
CREATE TRIGGER trg_knw_document_pointer BEFORE UPDATE OF current_revision_id ON knw_document BEGIN
    SELECT CASE WHEN new.current_revision_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM knw_document_revision r WHERE r.id=new.current_revision_id AND
            r.document_id=new.id AND r.namespace_id=new.namespace_id AND r.status='published')
        THEN RAISE(ABORT, 'current knowledge revision must be published') END;
END;

CREATE TRIGGER trg_knw_content_hierarchy_insert BEFORE INSERT ON knw_content
WHEN new.parent_id IS NOT NULL BEGIN
    SELECT CASE WHEN new.parent_id=new.id THEN RAISE(ABORT, 'knowledge content hierarchy is cyclic') END;
    WITH RECURSIVE ancestors(id,parent_id) AS (
        SELECT id,parent_id FROM knw_content WHERE id=new.parent_id
        UNION ALL
        SELECT c.id,c.parent_id FROM knw_content c JOIN ancestors a ON c.id=a.parent_id)
    SELECT CASE WHEN EXISTS(SELECT 1 FROM ancestors WHERE id=new.id)
        THEN RAISE(ABORT, 'knowledge content hierarchy is cyclic') END;
END;
CREATE TRIGGER trg_knw_content_hierarchy_update BEFORE UPDATE OF parent_id ON knw_content
WHEN new.parent_id IS NOT NULL BEGIN
    SELECT CASE WHEN new.parent_id=new.id THEN RAISE(ABORT, 'knowledge content hierarchy is cyclic') END;
    WITH RECURSIVE ancestors(id,parent_id) AS (
        SELECT id,parent_id FROM knw_content WHERE id=new.parent_id
        UNION ALL
        SELECT c.id,c.parent_id FROM knw_content c JOIN ancestors a ON c.id=a.parent_id)
    SELECT CASE WHEN EXISTS(SELECT 1 FROM ancestors WHERE id=new.id)
        THEN RAISE(ABORT, 'knowledge content hierarchy is cyclic') END;
END;

CREATE TRIGGER trg_orh_approval_guard BEFORE UPDATE ON orh_approval BEGIN
    SELECT CASE WHEN new.request <> old.request OR new.request_hash <> old.request_hash OR
        new.type <> old.type OR new.subject_type <> old.subject_type OR new.subject_id <> old.subject_id OR
        new.idempotency_key <> old.idempotency_key OR new.namespace_id <> old.namespace_id OR
        new.run_id IS NOT old.run_id OR new.node_run_id IS NOT old.node_run_id OR
        new.expires_at IS NOT old.expires_at
        THEN RAISE(ABORT, 'approval request is immutable') END;
    SELECT CASE WHEN old.status <> 'pending' AND (new.status <> old.status OR
        new.decision IS NOT old.decision OR new.decided_by IS NOT old.decided_by OR
        new.decided_at IS NOT old.decided_at)
        THEN RAISE(ABORT, 'approval decision is final') END;
END;

CREATE TRIGGER trg_llm_instruction_guard BEFORE UPDATE ON llm_instruction BEGIN
    SELECT CASE WHEN new.namespace_id <> old.namespace_id OR new.scope <> old.scope OR
        new.flow_id IS NOT old.flow_id OR new.revision <> old.revision OR
        new.content <> old.content OR new.content_hash <> old.content_hash
        THEN RAISE(ABORT, 'instruction content and ownership are immutable') END;
    SELECT CASE WHEN old.status = 'published' AND new.approval_id IS NOT old.approval_id
        THEN RAISE(ABORT, 'published instruction approval is immutable') END;
END;

CREATE TRIGGER trg_knw_candidate_guard BEFORE UPDATE ON knw_candidate BEGIN
    SELECT CASE WHEN new.namespace_id <> old.namespace_id OR new.source_run_id <> old.source_run_id OR
        new.target_scope <> old.target_scope OR new.target_flow_id IS NOT old.target_flow_id OR
        new.target_document_id IS NOT old.target_document_id OR new.type <> old.type OR
        new.content <> old.content OR new.content_hash <> old.content_hash OR
        new.provenance <> old.provenance OR new.expected_revision <> old.expected_revision OR
        new.approval_id <> old.approval_id OR new.idempotency_key <> old.idempotency_key
        THEN RAISE(ABORT, 'knowledge candidate request is immutable') END;
    SELECT CASE WHEN old.status <> 'unpublished' AND new.status <> old.status
        THEN RAISE(ABORT, 'knowledge candidate terminal status is final') END;
END;

CREATE TRIGGER trg_knw_document_revision_guard BEFORE UPDATE ON knw_document_revision BEGIN
    SELECT CASE WHEN new.namespace_id <> old.namespace_id OR new.document_id <> old.document_id OR
        new.revision <> old.revision OR new.source_revision IS NOT old.source_revision OR
        new.title <> old.title OR new.source_path IS NOT old.source_path OR
        new.provenance <> old.provenance OR new.content_hash <> old.content_hash
        THEN RAISE(ABORT, 'document revision content and provenance are immutable') END;
    SELECT CASE WHEN old.status = 'published' AND new.approval_id IS NOT old.approval_id
        THEN RAISE(ABORT, 'published document approval is immutable') END;
END;

CREATE TRIGGER trg_knw_content_update_guard BEFORE UPDATE ON knw_content BEGIN
    SELECT CASE WHEN (SELECT status FROM knw_document_revision WHERE id = old.document_revision_id) = 'published'
        THEN RAISE(ABORT, 'published document content is immutable') END;
END;
CREATE TRIGGER trg_knw_content_delete_guard BEFORE DELETE ON knw_content BEGIN
    SELECT CASE WHEN (SELECT status FROM knw_document_revision WHERE id = old.document_revision_id) = 'published'
        THEN RAISE(ABORT, 'published document content is immutable') END;
END;

CREATE TRIGGER trg_orh_flow_revision_update BEFORE UPDATE ON orh_flow_revision BEGIN
    SELECT RAISE(ABORT, 'orh_flow_revision is append-only'); END;
CREATE TRIGGER trg_orh_flow_revision_delete BEFORE DELETE ON orh_flow_revision BEGIN
    SELECT RAISE(ABORT, 'orh_flow_revision is append-only'); END;
CREATE TRIGGER trg_llm_agent_revision_update BEFORE UPDATE ON llm_agent_revision BEGIN
    SELECT RAISE(ABORT, 'llm_agent_revision is append-only'); END;
CREATE TRIGGER trg_llm_agent_revision_delete BEFORE DELETE ON llm_agent_revision BEGIN
    SELECT RAISE(ABORT, 'llm_agent_revision is append-only'); END;
CREATE TRIGGER trg_llm_skill_revision_update BEFORE UPDATE ON llm_skill_revision BEGIN
    SELECT RAISE(ABORT, 'llm_skill_revision is append-only'); END;
CREATE TRIGGER trg_llm_skill_revision_delete BEFORE DELETE ON llm_skill_revision BEGIN
    SELECT RAISE(ABORT, 'llm_skill_revision is append-only'); END;
CREATE TRIGGER trg_llm_skill_file_update BEFORE UPDATE ON llm_skill_file BEGIN
    SELECT RAISE(ABORT, 'llm_skill_file is append-only'); END;
CREATE TRIGGER trg_llm_skill_file_delete BEFORE DELETE ON llm_skill_file BEGIN
    SELECT RAISE(ABORT, 'llm_skill_file is append-only'); END;
CREATE TRIGGER trg_orh_node_checkpoint_update BEFORE UPDATE ON orh_node_checkpoint BEGIN
    SELECT RAISE(ABORT, 'orh_node_checkpoint is append-only'); END;
CREATE TRIGGER trg_orh_node_checkpoint_delete BEFORE DELETE ON orh_node_checkpoint BEGIN
    SELECT RAISE(ABORT, 'orh_node_checkpoint is append-only'); END;
CREATE TRIGGER trg_orh_node_message_update BEFORE UPDATE ON orh_node_message BEGIN
    SELECT RAISE(ABORT, 'orh_node_message is append-only'); END;
CREATE TRIGGER trg_orh_node_message_delete BEFORE DELETE ON orh_node_message BEGIN
    SELECT RAISE(ABORT, 'orh_node_message is append-only'); END;
CREATE TRIGGER trg_orh_event_update BEFORE UPDATE ON orh_event BEGIN
    SELECT RAISE(ABORT, 'orh_event is append-only'); END;
CREATE TRIGGER trg_orh_event_delete BEFORE DELETE ON orh_event BEGIN
    SELECT RAISE(ABORT, 'orh_event is append-only'); END;
CREATE TRIGGER trg_knw_embedding_update BEFORE UPDATE ON knw_embedding BEGIN
    SELECT RAISE(ABORT, 'knw_embedding is append-only'); END;
CREATE TRIGGER trg_knw_embedding_delete BEFORE DELETE ON knw_embedding BEGIN
    SELECT RAISE(ABORT, 'knw_embedding is append-only'); END;
CREATE TRIGGER trg_knw_embedding_profile_identity BEFORE UPDATE ON knw_embedding_profile BEGIN
    SELECT CASE WHEN new.profile_key <> old.profile_key OR new.provider_type <> old.provider_type OR
        new.model <> old.model OR new.model_revision <> old.model_revision OR
        new.dimensions <> old.dimensions OR new.distance_metric <> old.distance_metric OR
        new.vector_table <> old.vector_table
        THEN RAISE(ABORT, 'embedding profile identity is immutable') END;
END;

-- Every ordinary mutable row receives database-maintained updated_at and
-- row_version. The connection keeps recursive_triggers disabled, so these
-- internal UPDATE statements execute exactly once.
CREATE TRIGGER trg_orh_namespace_touch AFTER UPDATE ON orh_namespace BEGIN
    UPDATE orh_namespace SET updated_at=CURRENT_TIMESTAMP,row_version=old.row_version+1 WHERE id=old.id; END;
CREATE TRIGGER trg_orh_flow_touch AFTER UPDATE ON orh_flow BEGIN
    UPDATE orh_flow SET updated_at=CURRENT_TIMESTAMP,row_version=old.row_version+1 WHERE id=old.id; END;
CREATE TRIGGER trg_llm_provider_touch AFTER UPDATE ON llm_provider BEGIN
    UPDATE llm_provider SET updated_at=CURRENT_TIMESTAMP,row_version=old.row_version+1 WHERE id=old.id; END;
CREATE TRIGGER trg_llm_mcp_touch AFTER UPDATE ON llm_mcp BEGIN
    UPDATE llm_mcp SET updated_at=CURRENT_TIMESTAMP,row_version=old.row_version+1 WHERE id=old.id; END;
CREATE TRIGGER trg_llm_agent_touch AFTER UPDATE ON llm_agent BEGIN
    UPDATE llm_agent SET updated_at=CURRENT_TIMESTAMP,row_version=old.row_version+1 WHERE id=old.id; END;
CREATE TRIGGER trg_llm_skill_touch AFTER UPDATE ON llm_skill BEGIN
    UPDATE llm_skill SET updated_at=CURRENT_TIMESTAMP,row_version=old.row_version+1 WHERE id=old.id; END;
CREATE TRIGGER trg_orh_run_touch AFTER UPDATE ON orh_run BEGIN
    UPDATE orh_run SET updated_at=CURRENT_TIMESTAMP,row_version=old.row_version+1 WHERE id=old.id; END;
CREATE TRIGGER trg_orh_node_run_touch AFTER UPDATE ON orh_node_run BEGIN
    UPDATE orh_node_run SET updated_at=CURRENT_TIMESTAMP,row_version=old.row_version+1 WHERE id=old.id; END;
CREATE TRIGGER trg_orh_dispatch_touch AFTER UPDATE ON orh_dispatch BEGIN
    UPDATE orh_dispatch SET updated_at=CURRENT_TIMESTAMP,row_version=old.row_version+1 WHERE id=old.id; END;
CREATE TRIGGER trg_orh_outbox_touch AFTER UPDATE ON orh_outbox BEGIN
    UPDATE orh_outbox SET updated_at=CURRENT_TIMESTAMP,row_version=old.row_version+1 WHERE id=old.id; END;
CREATE TRIGGER trg_orh_approval_touch AFTER UPDATE ON orh_approval BEGIN
    UPDATE orh_approval SET updated_at=CURRENT_TIMESTAMP,row_version=old.row_version+1 WHERE id=old.id; END;
CREATE TRIGGER trg_orh_effect_execution_touch AFTER UPDATE ON orh_effect_execution BEGIN
    UPDATE orh_effect_execution SET updated_at=CURRENT_TIMESTAMP,row_version=old.row_version+1 WHERE id=old.id; END;
CREATE TRIGGER trg_llm_instruction_touch AFTER UPDATE ON llm_instruction BEGIN
    UPDATE llm_instruction SET updated_at=CURRENT_TIMESTAMP,row_version=old.row_version+1 WHERE id=old.id; END;
CREATE TRIGGER trg_knw_source_touch AFTER UPDATE ON knw_source BEGIN
    UPDATE knw_source SET updated_at=CURRENT_TIMESTAMP,row_version=old.row_version+1 WHERE id=old.id; END;
CREATE TRIGGER trg_knw_document_touch AFTER UPDATE ON knw_document BEGIN
    UPDATE knw_document SET updated_at=CURRENT_TIMESTAMP,row_version=old.row_version+1 WHERE id=old.id; END;
CREATE TRIGGER trg_knw_document_revision_touch AFTER UPDATE ON knw_document_revision BEGIN
    UPDATE knw_document_revision SET updated_at=CURRENT_TIMESTAMP,row_version=old.row_version+1 WHERE id=old.id; END;
CREATE TRIGGER trg_knw_content_touch AFTER UPDATE ON knw_content BEGIN
    UPDATE knw_content SET updated_at=CURRENT_TIMESTAMP,row_version=old.row_version+1 WHERE id=old.id; END;
CREATE TRIGGER trg_knw_embedding_profile_touch AFTER UPDATE ON knw_embedding_profile BEGIN
    UPDATE knw_embedding_profile SET updated_at=CURRENT_TIMESTAMP,row_version=old.row_version+1 WHERE id=old.id; END;
CREATE TRIGGER trg_knw_candidate_touch AFTER UPDATE ON knw_candidate BEGIN
    UPDATE knw_candidate SET updated_at=CURRENT_TIMESTAMP,row_version=old.row_version+1 WHERE id=old.id; END;
CREATE TRIGGER trg_orh_runtime_configuration_touch AFTER UPDATE ON orh_runtime_configuration BEGIN
    UPDATE orh_runtime_configuration SET updated_at=CURRENT_TIMESTAMP,row_version=old.row_version+1 WHERE id=old.id; END;
CREATE TRIGGER trg_orh_flow_release_touch AFTER UPDATE ON orh_flow_release BEGIN
    UPDATE orh_flow_release SET updated_at=CURRENT_TIMESTAMP,row_version=old.row_version+1 WHERE id=old.id; END;
CREATE TRIGGER trg_orh_flow_release_grant_touch AFTER UPDATE ON orh_flow_release_grant BEGIN
    UPDATE orh_flow_release_grant SET updated_at=CURRENT_TIMESTAMP,row_version=old.row_version+1 WHERE id=old.id; END;
CREATE TRIGGER trg_orh_flow_installation_touch AFTER UPDATE ON orh_flow_installation BEGIN
    UPDATE orh_flow_installation SET updated_at=CURRENT_TIMESTAMP,row_version=old.row_version+1 WHERE id=old.id; END;
CREATE TRIGGER trg_nfy_channel_touch AFTER UPDATE ON nfy_channel BEGIN
    UPDATE nfy_channel SET updated_at=CURRENT_TIMESTAMP,row_version=old.row_version+1 WHERE id=old.id; END;
CREATE TRIGGER trg_orh_a2a_task_touch AFTER UPDATE ON orh_a2a_task BEGIN
    UPDATE orh_a2a_task SET updated_at=CURRENT_TIMESTAMP,row_version=old.row_version+1 WHERE id=old.id; END;
