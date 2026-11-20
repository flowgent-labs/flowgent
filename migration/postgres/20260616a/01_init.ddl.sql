-- Flowgent PostgreSQL bootstrap schema.
-- This file is the complete schema for a new database; it intentionally does
-- not preserve the pre-1.0 table layout. Identity, immutable revisions,
-- execution state, retrieval knowledge, and approvals are separate domains.

-- Keep database-scoped extension objects outside the application schema. The
-- storage pool searches the selected application schema first and public
-- second, so isolated schemas can share one native pgvector installation.
CREATE EXTENSION IF NOT EXISTS vector WITH SCHEMA public;

-- The migrator creates this table before executing embedded migrations. Keep
-- the declaration here so this DDL can also be executed directly on an empty DB.
CREATE TABLE IF NOT EXISTS schema_migrations (
    version     TEXT NOT NULL,
    filename    TEXT NOT NULL,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (version, filename)
);

CREATE OR REPLACE FUNCTION flowgent_touch_mutable()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at := clock_timestamp();
    NEW.row_version := OLD.row_version + 1;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION flowgent_reject_update_delete()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION '% is append-only', TG_TABLE_NAME USING ERRCODE = '55000';
END;
$$;

-- ---------------------------------------------------------------------------
-- Orchestration identities and immutable definitions
-- ---------------------------------------------------------------------------

CREATE TABLE orh_namespace (
    id              VARCHAR(64) PRIMARY KEY,
    name            TEXT NOT NULL,
    description     TEXT,
    instruction_id  VARCHAR(64),
    status          TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by      VARCHAR(255),
    updated_by      VARCHAR(255),
    row_version     BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata        JSONB,
    UNIQUE (name),
    UNIQUE (id, name)
);
CREATE UNIQUE INDEX uq_orh_namespace_name_ci ON orh_namespace (lower(name));

INSERT INTO orh_namespace (id, name, description, created_by, updated_by)
VALUES ('default', 'default', 'Default namespace', 'system:bootstrap', 'system:bootstrap')
ON CONFLICT (id) DO NOTHING;

CREATE TABLE orh_flow (
    id                   VARCHAR(64) PRIMARY KEY,
    namespace_id         VARCHAR(64) NOT NULL,
    name                 TEXT NOT NULL,
    description          TEXT,
    status               TEXT NOT NULL DEFAULT 'ACTIVE',
    current_revision_id  VARCHAR(64),
    instruction_id       VARCHAR(64),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by           VARCHAR(255),
    updated_by           VARCHAR(255),
    row_version          BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata             JSONB,
    CONSTRAINT fk_orh_flow_namespace FOREIGN KEY (namespace_id)
        REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    UNIQUE (namespace_id, name),
    UNIQUE (id, namespace_id)
);
CREATE UNIQUE INDEX uq_orh_flow_name_ci ON orh_flow (namespace_id, lower(name));
CREATE INDEX idx_orh_flow_namespace_status ON orh_flow (namespace_id, status, updated_at DESC);

CREATE TABLE orh_flow_revision (
    id                  VARCHAR(64) PRIMARY KEY,
    namespace_id        VARCHAR(64) NOT NULL,
    flow_id             VARCHAR(64) NOT NULL,
    revision            BIGINT NOT NULL CHECK (revision > 0),
    definition          JSONB NOT NULL,
    summarize_enabled   BOOLEAN NOT NULL DEFAULT FALSE,
    checksum            CHAR(64) NOT NULL CHECK (checksum ~ '^[0-9a-f]{64}$'),
    comment             TEXT,
    description         TEXT,
    status              TEXT NOT NULL DEFAULT 'PUBLISHED',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by          VARCHAR(255),
    updated_by          VARCHAR(255),
    row_version         BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata            JSONB,
    CONSTRAINT fk_orh_flow_revision_flow FOREIGN KEY (flow_id, namespace_id)
        REFERENCES orh_flow(id, namespace_id) ON DELETE RESTRICT,
    UNIQUE (flow_id, revision),
    UNIQUE (id, flow_id, namespace_id),
    UNIQUE (id, namespace_id)
);
CREATE INDEX idx_orh_flow_revision_owner ON orh_flow_revision (flow_id, revision DESC);

ALTER TABLE orh_flow ADD CONSTRAINT fk_orh_flow_current_revision
    FOREIGN KEY (current_revision_id, id, namespace_id)
    REFERENCES orh_flow_revision(id, flow_id, namespace_id)
    DEFERRABLE INITIALLY DEFERRED;

-- ---------------------------------------------------------------------------
-- LLM resources: providers, MCPs, versioned agents/skills, instructions
-- ---------------------------------------------------------------------------

CREATE TABLE llm_provider (
    id              VARCHAR(64) PRIMARY KEY,
    namespace_id    VARCHAR(64) NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    name            TEXT NOT NULL CHECK (name ~ '^[A-Za-z0-9_-]+$'),
    type            TEXT NOT NULL CHECK (type IN ('openai', 'anthropic', 'gemini')),
    base_uri        TEXT NOT NULL,
    credential_ref TEXT,
    default_model   TEXT,
    models          JSONB NOT NULL DEFAULT '[]'::jsonb,
    timeout_ms      INTEGER NOT NULL DEFAULT 30000 CHECK (timeout_ms > 0),
    env             JSONB NOT NULL DEFAULT '{}'::jsonb,
    description     TEXT,
    status          TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by      VARCHAR(255),
    updated_by      VARCHAR(255),
    row_version     BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata        JSONB,
    UNIQUE (namespace_id, name),
    UNIQUE (id, namespace_id)
);
CREATE UNIQUE INDEX uq_llm_provider_name_ci ON llm_provider (namespace_id, lower(name));

CREATE TABLE llm_mcp (
    id              VARCHAR(64) PRIMARY KEY,
    namespace_id    VARCHAR(64) NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    name            TEXT NOT NULL CHECK (name ~ '^[A-Za-z0-9_-]+$'),
    transport       TEXT NOT NULL DEFAULT 'http' CHECK (transport IN ('http', 'stdio')),
    rpc_url         TEXT,
    command         JSONB NOT NULL DEFAULT '[]'::jsonb,
    args            JSONB NOT NULL DEFAULT '[]'::jsonb,
    headers         JSONB NOT NULL DEFAULT '{}'::jsonb,
    env             JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    description     TEXT,
    status          TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by      VARCHAR(255),
    updated_by      VARCHAR(255),
    row_version     BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata        JSONB,
    CONSTRAINT ck_llm_mcp_transport_config CHECK (
        (transport = 'http' AND rpc_url IS NOT NULL AND btrim(rpc_url) <> '') OR
        (transport = 'stdio' AND jsonb_array_length(command) > 0)
    ),
    UNIQUE (namespace_id, name),
    UNIQUE (id, namespace_id)
);
CREATE UNIQUE INDEX uq_llm_mcp_name_ci ON llm_mcp (namespace_id, lower(name));

CREATE TABLE llm_agent (
    id                   VARCHAR(64) PRIMARY KEY,
    namespace_id         VARCHAR(64) NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    name                 TEXT NOT NULL,
    description          TEXT,
    status               TEXT NOT NULL DEFAULT 'ACTIVE',
    current_revision_id  VARCHAR(64),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by           VARCHAR(255),
    updated_by           VARCHAR(255),
    row_version          BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata             JSONB,
    UNIQUE (namespace_id, name),
    UNIQUE (id, namespace_id)
);
CREATE UNIQUE INDEX uq_llm_agent_name_ci ON llm_agent (namespace_id, lower(name));

CREATE TABLE llm_agent_revision (
    id              VARCHAR(64) PRIMARY KEY,
    namespace_id    VARCHAR(64) NOT NULL,
    agent_id        VARCHAR(64) NOT NULL,
    revision        BIGINT NOT NULL CHECK (revision > 0),
    soul            TEXT NOT NULL,
    instruction     TEXT NOT NULL,
    model_config    JSONB NOT NULL DEFAULT '{}'::jsonb,
    input_schema    JSONB,
    output_schema   JSONB,
    description     TEXT,
    status          TEXT NOT NULL DEFAULT 'PUBLISHED',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by      VARCHAR(255),
    updated_by      VARCHAR(255),
    row_version     BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata        JSONB,
    CONSTRAINT fk_llm_agent_revision_agent FOREIGN KEY (agent_id, namespace_id)
        REFERENCES llm_agent(id, namespace_id) ON DELETE RESTRICT,
    UNIQUE (agent_id, revision),
    UNIQUE (id, agent_id, namespace_id),
    UNIQUE (id, namespace_id)
);
CREATE INDEX idx_llm_agent_revision_owner ON llm_agent_revision (agent_id, revision DESC);
ALTER TABLE llm_agent ADD CONSTRAINT fk_llm_agent_current_revision
    FOREIGN KEY (current_revision_id, id, namespace_id)
    REFERENCES llm_agent_revision(id, agent_id, namespace_id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE llm_skill (
    id                   VARCHAR(64) PRIMARY KEY,
    namespace_id         VARCHAR(64) NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    name                 TEXT NOT NULL,
    description          TEXT,
    status               TEXT NOT NULL DEFAULT 'ACTIVE',
    current_revision_id  VARCHAR(64),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by           VARCHAR(255),
    updated_by           VARCHAR(255),
    row_version          BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata             JSONB,
    UNIQUE (namespace_id, name),
    UNIQUE (id, namespace_id)
);
CREATE UNIQUE INDEX uq_llm_skill_name_ci ON llm_skill (namespace_id, lower(name));

CREATE TABLE llm_skill_revision (
    id              VARCHAR(64) PRIMARY KEY,
    namespace_id    VARCHAR(64) NOT NULL,
    skill_id        VARCHAR(64) NOT NULL,
    revision        BIGINT NOT NULL CHECK (revision > 0),
    instruction     TEXT NOT NULL,
    model_config    JSONB NOT NULL DEFAULT '{}'::jsonb,
    tools           JSONB NOT NULL DEFAULT '[]'::jsonb,
    description     TEXT,
    status          TEXT NOT NULL DEFAULT 'PUBLISHED',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by      VARCHAR(255),
    updated_by      VARCHAR(255),
    row_version     BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata        JSONB,
    CONSTRAINT fk_llm_skill_revision_skill FOREIGN KEY (skill_id, namespace_id)
        REFERENCES llm_skill(id, namespace_id) ON DELETE RESTRICT,
    UNIQUE (skill_id, revision),
    UNIQUE (id, skill_id, namespace_id),
    UNIQUE (id, namespace_id)
);
ALTER TABLE llm_skill ADD CONSTRAINT fk_llm_skill_current_revision
    FOREIGN KEY (current_revision_id, id, namespace_id)
    REFERENCES llm_skill_revision(id, skill_id, namespace_id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE llm_skill_file (
    id                  VARCHAR(64) PRIMARY KEY,
    namespace_id        VARCHAR(64) NOT NULL,
    skill_revision_id   VARCHAR(64) NOT NULL,
    kind                TEXT NOT NULL CHECK (kind IN ('asset', 'script')),
    relative_path       TEXT NOT NULL CHECK (
        relative_path !~ '(^|/)\.\.(/|$)' AND relative_path !~ '^/'
    ),
    media_type          TEXT NOT NULL,
    size_bytes          BIGINT NOT NULL CHECK (size_bytes >= 0),
    content_hash        CHAR(64) NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by          VARCHAR(255),
    metadata            JSONB,
    CONSTRAINT fk_llm_skill_file_revision FOREIGN KEY (skill_revision_id, namespace_id)
        REFERENCES llm_skill_revision(id, namespace_id) ON DELETE RESTRICT,
    UNIQUE (skill_revision_id, kind, relative_path)
);

-- Approval is declared after Run/NodeRun below. Instruction owner FKs are
-- added after approval to keep all cycles explicit and deferrable.

-- ---------------------------------------------------------------------------
-- Runs, attempts, recovery, fencing, events and outbox
-- ---------------------------------------------------------------------------

CREATE TABLE orh_run (
    id                  VARCHAR(64) PRIMARY KEY,
    namespace_id        VARCHAR(64) NOT NULL,
    flow_id             VARCHAR(64) NOT NULL,
    flow_revision_id    VARCHAR(64) NOT NULL,
    status              TEXT NOT NULL DEFAULT 'PENDING',
    input               JSONB,
    output              JSONB,
    error               JSONB,
    run_instruction     TEXT,
    summarize_enabled   BOOLEAN NOT NULL DEFAULT FALSE,
    context_snapshot    JSONB NOT NULL,
    trigger_type        TEXT,
    trigger_source      TEXT,
    trigger_payload     JSONB,
    runtime_mode        TEXT NOT NULL DEFAULT 'application'
        CHECK (runtime_mode IN ('application', 'session')),
    runtime_cluster_id  TEXT,
    started_at          TIMESTAMPTZ,
    finished_at         TIMESTAMPTZ,
    description         TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by          VARCHAR(255),
    updated_by          VARCHAR(255),
    row_version         BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata            JSONB,
    CONSTRAINT fk_orh_run_flow FOREIGN KEY (flow_id, namespace_id)
        REFERENCES orh_flow(id, namespace_id) ON DELETE RESTRICT,
    CONSTRAINT fk_orh_run_flow_revision FOREIGN KEY (flow_revision_id, flow_id, namespace_id)
        REFERENCES orh_flow_revision(id, flow_id, namespace_id) ON DELETE RESTRICT,
    CONSTRAINT ck_orh_run_finished CHECK (finished_at IS NULL OR started_at IS NULL OR finished_at >= started_at),
    UNIQUE (id, flow_id, namespace_id),
    UNIQUE (id, namespace_id)
);
CREATE INDEX idx_orh_run_namespace_status ON orh_run (namespace_id, status, created_at DESC);
CREATE INDEX idx_orh_run_flow_created ON orh_run (flow_id, created_at DESC);

CREATE TABLE orh_node_run (
    id                  VARCHAR(64) PRIMARY KEY,
    namespace_id        VARCHAR(64) NOT NULL,
    run_id              VARCHAR(64) NOT NULL,
    node_key            TEXT NOT NULL,
    attempt             INTEGER NOT NULL DEFAULT 1 CHECK (attempt > 0),
    agent_revision_id   VARCHAR(64),
    status              TEXT NOT NULL DEFAULT 'PENDING',
    input               JSONB,
    output              JSONB,
    error               JSONB,
    execution_memory    JSONB,
    checkpoint          JSONB,
    workspace_version   TEXT,
    parent_node_run_id  VARCHAR(64),
    execution_id        TEXT NOT NULL,
    lease_owner         TEXT,
    lease_expires_at    TIMESTAMPTZ,
    fencing_token       BIGINT NOT NULL DEFAULT 0 CHECK (fencing_token >= 0),
    last_heartbeat_at   TIMESTAMPTZ,
    started_at          TIMESTAMPTZ,
    finished_at         TIMESTAMPTZ,
    description         TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by          VARCHAR(255),
    updated_by          VARCHAR(255),
    row_version         BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata            JSONB,
    CONSTRAINT fk_orh_node_run_run FOREIGN KEY (run_id, namespace_id)
        REFERENCES orh_run(id, namespace_id) ON DELETE RESTRICT,
    CONSTRAINT fk_orh_node_run_agent_revision FOREIGN KEY (agent_revision_id, namespace_id)
        REFERENCES llm_agent_revision(id, namespace_id) ON DELETE RESTRICT,
    CONSTRAINT fk_orh_node_run_parent FOREIGN KEY (parent_node_run_id, run_id, namespace_id)
        REFERENCES orh_node_run(id, run_id, namespace_id) ON DELETE RESTRICT,
    CONSTRAINT ck_orh_node_run_finished CHECK (finished_at IS NULL OR started_at IS NULL OR finished_at >= started_at),
    UNIQUE (run_id, node_key, attempt),
    UNIQUE (namespace_id, execution_id),
    UNIQUE (id, run_id, namespace_id),
    UNIQUE (id, namespace_id)
);
CREATE INDEX idx_orh_node_run_run_status ON orh_node_run (run_id, status, node_key, attempt DESC);
CREATE INDEX idx_orh_node_run_lease ON orh_node_run (status, lease_expires_at) WHERE lease_owner IS NOT NULL;

CREATE TABLE orh_node_checkpoint (
    id                  VARCHAR(64) PRIMARY KEY,
    namespace_id        VARCHAR(64) NOT NULL,
    node_run_id         VARCHAR(64) NOT NULL,
    sequence            BIGINT NOT NULL CHECK (sequence > 0),
    execution_memory    JSONB,
    checkpoint          JSONB NOT NULL,
    workspace_version   TEXT NOT NULL,
    fencing_token       BIGINT NOT NULL CHECK (fencing_token >= 0),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by          VARCHAR(255),
    metadata            JSONB,
    CONSTRAINT fk_orh_node_checkpoint_node FOREIGN KEY (node_run_id, namespace_id)
        REFERENCES orh_node_run(id, namespace_id) ON DELETE RESTRICT,
    UNIQUE (node_run_id, sequence),
    UNIQUE (id, node_run_id, namespace_id)
);
CREATE INDEX idx_orh_node_checkpoint_latest ON orh_node_checkpoint (node_run_id, sequence DESC);

CREATE TABLE orh_node_message (
    id                  VARCHAR(64) PRIMARY KEY,
    namespace_id        VARCHAR(64) NOT NULL,
    node_run_id         VARCHAR(64) NOT NULL,
    checkpoint_id       VARCHAR(64),
    ordinal             INTEGER NOT NULL CHECK (ordinal >= 0),
    role                TEXT NOT NULL CHECK (role IN ('system', 'user', 'assistant', 'tool')),
    content             TEXT NOT NULL,
    content_hash        CHAR(64) NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by          VARCHAR(255),
    metadata            JSONB,
    CONSTRAINT fk_orh_node_message_node FOREIGN KEY (node_run_id, namespace_id)
        REFERENCES orh_node_run(id, namespace_id) ON DELETE RESTRICT,
    CONSTRAINT fk_orh_node_message_checkpoint FOREIGN KEY (checkpoint_id, node_run_id, namespace_id)
        REFERENCES orh_node_checkpoint(id, node_run_id, namespace_id) ON DELETE RESTRICT,
    UNIQUE (node_run_id, ordinal)
);

CREATE TABLE orh_dispatch (
    id                  VARCHAR(64) PRIMARY KEY,
    namespace_id        VARCHAR(64) NOT NULL,
    node_run_id         VARCHAR(64) NOT NULL,
    status              TEXT NOT NULL DEFAULT 'pending',
    payload             JSONB NOT NULL,
    available_at        TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    lease_owner         TEXT,
    lease_expires_at    TIMESTAMPTZ,
    fencing_token       BIGINT NOT NULL DEFAULT 0 CHECK (fencing_token >= 0),
    delivery_count      INTEGER NOT NULL DEFAULT 0 CHECK (delivery_count >= 0),
    idempotency_key     TEXT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by          VARCHAR(255),
    updated_by          VARCHAR(255),
    row_version         BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata            JSONB,
    CONSTRAINT fk_orh_dispatch_node FOREIGN KEY (node_run_id, namespace_id)
        REFERENCES orh_node_run(id, namespace_id) ON DELETE RESTRICT,
    UNIQUE (node_run_id),
    UNIQUE (namespace_id, idempotency_key)
);
CREATE INDEX idx_orh_dispatch_claim ON orh_dispatch (status, available_at, lease_expires_at);

CREATE TABLE orh_event (
    id                  VARCHAR(64) PRIMARY KEY,
    namespace_id        VARCHAR(64) NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    run_id              VARCHAR(64),
    node_run_id         VARCHAR(64),
    sequence            BIGINT NOT NULL CHECK (sequence > 0),
    type                TEXT NOT NULL,
    payload             JSONB NOT NULL,
    idempotency_key     TEXT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by          VARCHAR(255),
    metadata            JSONB,
    CONSTRAINT fk_orh_event_run FOREIGN KEY (run_id, namespace_id)
        REFERENCES orh_run(id, namespace_id) ON DELETE RESTRICT,
    CONSTRAINT fk_orh_event_node FOREIGN KEY (node_run_id, run_id, namespace_id)
        REFERENCES orh_node_run(id, run_id, namespace_id) ON DELETE RESTRICT,
    CONSTRAINT ck_orh_event_node_has_run CHECK (node_run_id IS NULL OR run_id IS NOT NULL),
    UNIQUE (namespace_id, idempotency_key),
    UNIQUE (run_id, sequence)
);
CREATE INDEX idx_orh_event_run_sequence ON orh_event (run_id, sequence);

CREATE TABLE orh_outbox (
    id                  VARCHAR(64) PRIMARY KEY,
    namespace_id        VARCHAR(64) NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    aggregate_type      TEXT NOT NULL,
    aggregate_id        VARCHAR(64) NOT NULL,
    event_type          TEXT NOT NULL,
    payload             JSONB NOT NULL,
    status              TEXT NOT NULL DEFAULT 'pending',
    idempotency_key     TEXT NOT NULL,
    available_at        TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    published_at        TIMESTAMPTZ,
    attempts            INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error          TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by          VARCHAR(255),
    updated_by          VARCHAR(255),
    row_version         BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata            JSONB,
    UNIQUE (namespace_id, idempotency_key)
);
CREATE INDEX idx_orh_outbox_pending ON orh_outbox (status, available_at) WHERE status = 'pending';

-- ---------------------------------------------------------------------------
-- Unified approval and external side-effect execution
-- ---------------------------------------------------------------------------

CREATE TABLE orh_approval (
    id                  VARCHAR(64) PRIMARY KEY,
    namespace_id        VARCHAR(64) NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    run_id              VARCHAR(64),
    node_run_id         VARCHAR(64),
    type                TEXT NOT NULL CHECK (type IN (
                            'human_gate', 'tool_call', 'payment',
                            'knowledge_publish', 'instruction_publish')),
    subject_type        TEXT NOT NULL,
    subject_id          TEXT NOT NULL,
    request             JSONB NOT NULL,
    request_hash        CHAR(64) NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    status              TEXT NOT NULL DEFAULT 'pending'
                            CHECK (status IN ('pending', 'approved', 'rejected', 'expired', 'cancelled')),
    decision            JSONB,
    decided_by          VARCHAR(255),
    decided_at          TIMESTAMPTZ,
    expires_at          TIMESTAMPTZ,
    consumed_at         TIMESTAMPTZ,
    idempotency_key     TEXT NOT NULL,
    description         TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by          VARCHAR(255),
    updated_by          VARCHAR(255),
    row_version         BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata            JSONB,
    CONSTRAINT fk_orh_approval_run FOREIGN KEY (run_id, namespace_id)
        REFERENCES orh_run(id, namespace_id) ON DELETE RESTRICT,
    CONSTRAINT fk_orh_approval_node FOREIGN KEY (node_run_id, run_id, namespace_id)
        REFERENCES orh_node_run(id, run_id, namespace_id) ON DELETE RESTRICT,
    CONSTRAINT ck_orh_approval_node_has_run CHECK (node_run_id IS NULL OR run_id IS NOT NULL),
    CONSTRAINT ck_orh_approval_decision CHECK (
        (status = 'pending' AND decision IS NULL AND decided_by IS NULL AND decided_at IS NULL) OR
        (status <> 'pending' AND decided_at IS NOT NULL)
    ),
    CONSTRAINT ck_orh_approval_expiring_effect CHECK (
        type NOT IN ('tool_call', 'payment') OR expires_at IS NOT NULL
    ),
    UNIQUE (namespace_id, idempotency_key),
    UNIQUE (id, namespace_id)
);
CREATE INDEX idx_orh_approval_pending ON orh_approval (namespace_id, expires_at, created_at)
    WHERE status = 'pending';
CREATE INDEX idx_orh_approval_run ON orh_approval (run_id, status, created_at DESC);

CREATE OR REPLACE FUNCTION orh_approval_guard()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.request IS DISTINCT FROM OLD.request
       OR NEW.request_hash IS DISTINCT FROM OLD.request_hash
       OR NEW.type IS DISTINCT FROM OLD.type
       OR NEW.subject_type IS DISTINCT FROM OLD.subject_type
       OR NEW.subject_id IS DISTINCT FROM OLD.subject_id
       OR NEW.idempotency_key IS DISTINCT FROM OLD.idempotency_key
       OR NEW.namespace_id IS DISTINCT FROM OLD.namespace_id
       OR NEW.run_id IS DISTINCT FROM OLD.run_id
       OR NEW.node_run_id IS DISTINCT FROM OLD.node_run_id
       OR NEW.expires_at IS DISTINCT FROM OLD.expires_at THEN
        RAISE EXCEPTION 'approval request is immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status <> 'pending' AND (
       NEW.status IS DISTINCT FROM OLD.status
       OR NEW.decision IS DISTINCT FROM OLD.decision
       OR NEW.decided_by IS DISTINCT FROM OLD.decided_by
       OR NEW.decided_at IS DISTINCT FROM OLD.decided_at) THEN
        RAISE EXCEPTION 'approval decision is final' USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'pending' AND NEW.status NOT IN ('pending', 'approved', 'rejected', 'expired', 'cancelled') THEN
        RAISE EXCEPTION 'invalid approval transition' USING ERRCODE = '23514';
    END IF;
    NEW.updated_at := clock_timestamp();
    NEW.row_version := OLD.row_version + 1;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_orh_approval_guard BEFORE UPDATE ON orh_approval
FOR EACH ROW EXECUTE FUNCTION orh_approval_guard();

CREATE TABLE orh_effect_execution (
    id                  VARCHAR(64) PRIMARY KEY,
    namespace_id        VARCHAR(64) NOT NULL,
    run_id              VARCHAR(64) NOT NULL,
    node_run_id         VARCHAR(64),
    approval_id         VARCHAR(64) NOT NULL,
    type                TEXT NOT NULL CHECK (type IN ('tool_call', 'payment')),
    request             JSONB NOT NULL,
    request_hash        CHAR(64) NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    status              TEXT NOT NULL DEFAULT 'pending',
    result              JSONB,
    attempt             INTEGER NOT NULL DEFAULT 1 CHECK (attempt > 0),
    idempotency_key     TEXT NOT NULL,
    started_at          TIMESTAMPTZ,
    finished_at         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by          VARCHAR(255),
    updated_by          VARCHAR(255),
    row_version         BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata            JSONB,
    CONSTRAINT fk_orh_effect_run FOREIGN KEY (run_id, namespace_id)
        REFERENCES orh_run(id, namespace_id) ON DELETE RESTRICT,
    CONSTRAINT fk_orh_effect_node FOREIGN KEY (node_run_id, run_id, namespace_id)
        REFERENCES orh_node_run(id, run_id, namespace_id) ON DELETE RESTRICT,
    CONSTRAINT fk_orh_effect_approval FOREIGN KEY (approval_id, namespace_id)
        REFERENCES orh_approval(id, namespace_id) ON DELETE RESTRICT,
    UNIQUE (namespace_id, idempotency_key, attempt)
);
CREATE INDEX idx_orh_effect_approval ON orh_effect_execution (approval_id, status);

CREATE TABLE llm_instruction (
    id              VARCHAR(64) PRIMARY KEY,
    namespace_id    VARCHAR(64) NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    scope           TEXT NOT NULL CHECK (scope IN ('namespace', 'flow')),
    flow_id         VARCHAR(64),
    revision        BIGINT NOT NULL CHECK (revision > 0),
    content         TEXT NOT NULL,
    content_hash    CHAR(64) NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    status          TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'retired')),
    approval_id     VARCHAR(64),
    description     TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by      VARCHAR(255),
    updated_by      VARCHAR(255),
    row_version     BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata        JSONB,
    CONSTRAINT fk_llm_instruction_flow FOREIGN KEY (flow_id, namespace_id)
        REFERENCES orh_flow(id, namespace_id) ON DELETE RESTRICT,
    CONSTRAINT fk_llm_instruction_approval FOREIGN KEY (approval_id, namespace_id)
        REFERENCES orh_approval(id, namespace_id) ON DELETE RESTRICT,
    CONSTRAINT ck_llm_instruction_scope CHECK (
        (scope = 'namespace' AND flow_id IS NULL) OR
        (scope = 'flow' AND flow_id IS NOT NULL)
    ),
    CONSTRAINT ck_llm_instruction_publish_approval CHECK (
        status <> 'published' OR approval_id IS NOT NULL
    ),
    UNIQUE (id, namespace_id),
    UNIQUE (id, flow_id, namespace_id)
);
CREATE UNIQUE INDEX uq_llm_instruction_namespace_revision
    ON llm_instruction (namespace_id, revision) WHERE scope = 'namespace';
CREATE UNIQUE INDEX uq_llm_instruction_flow_revision
    ON llm_instruction (flow_id, revision) WHERE scope = 'flow';
CREATE INDEX idx_llm_instruction_owner_status ON llm_instruction (namespace_id, flow_id, status, revision DESC);

ALTER TABLE orh_namespace ADD CONSTRAINT fk_orh_namespace_instruction
    FOREIGN KEY (instruction_id, id)
    REFERENCES llm_instruction(id, namespace_id)
    DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE orh_flow ADD CONSTRAINT fk_orh_flow_instruction
    FOREIGN KEY (instruction_id, id, namespace_id)
    REFERENCES llm_instruction(id, flow_id, namespace_id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE OR REPLACE FUNCTION llm_instruction_guard()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.namespace_id IS DISTINCT FROM OLD.namespace_id
       OR NEW.scope IS DISTINCT FROM OLD.scope
       OR NEW.flow_id IS DISTINCT FROM OLD.flow_id
       OR NEW.revision IS DISTINCT FROM OLD.revision
       OR NEW.content IS DISTINCT FROM OLD.content
       OR NEW.content_hash IS DISTINCT FROM OLD.content_hash THEN
        RAISE EXCEPTION 'instruction content and ownership are immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'published' AND NEW.approval_id IS DISTINCT FROM OLD.approval_id THEN
        RAISE EXCEPTION 'published instruction approval is immutable' USING ERRCODE = '55000';
    END IF;
    NEW.updated_at := clock_timestamp();
    NEW.row_version := OLD.row_version + 1;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_llm_instruction_guard BEFORE UPDATE ON llm_instruction
FOR EACH ROW EXECUTE FUNCTION llm_instruction_guard();

CREATE OR REPLACE FUNCTION llm_instruction_pointer_guard()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    pointed_scope TEXT;
    pointed_status TEXT;
BEGIN
    IF NEW.instruction_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT scope,status INTO pointed_scope,pointed_status
      FROM llm_instruction WHERE id=NEW.instruction_id;
    IF pointed_status IS DISTINCT FROM 'published' THEN
        RAISE EXCEPTION 'current instruction must be published' USING ERRCODE = '23514';
    END IF;
    IF TG_TABLE_NAME='orh_namespace' AND pointed_scope IS DISTINCT FROM 'namespace' THEN
        RAISE EXCEPTION 'namespace instruction pointer has the wrong scope' USING ERRCODE = '23514';
    END IF;
    IF TG_TABLE_NAME='orh_flow' AND pointed_scope IS DISTINCT FROM 'flow' THEN
        RAISE EXCEPTION 'flow instruction pointer has the wrong scope' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_orh_namespace_instruction_pointer
BEFORE INSERT OR UPDATE OF instruction_id ON orh_namespace
FOR EACH ROW EXECUTE FUNCTION llm_instruction_pointer_guard();
CREATE TRIGGER trg_orh_flow_instruction_pointer
BEFORE INSERT OR UPDATE OF instruction_id ON orh_flow
FOR EACH ROW EXECUTE FUNCTION llm_instruction_pointer_guard();

-- ---------------------------------------------------------------------------
-- Knowledge: source -> document -> revision -> hierarchy -> embedding
-- ---------------------------------------------------------------------------

CREATE TABLE knw_source (
    id              VARCHAR(64) PRIMARY KEY,
    namespace_id    VARCHAR(64) NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    name            TEXT NOT NULL,
    type            TEXT NOT NULL,
    config          JSONB NOT NULL DEFAULT '{}'::jsonb,
    description     TEXT,
    status          TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by      VARCHAR(255),
    updated_by      VARCHAR(255),
    row_version     BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata        JSONB,
    UNIQUE (namespace_id, name),
    UNIQUE (id, namespace_id)
);
CREATE UNIQUE INDEX uq_knw_source_name_ci ON knw_source (namespace_id, lower(name));

CREATE TABLE knw_document (
    id                   VARCHAR(64) PRIMARY KEY,
    namespace_id         VARCHAR(64) NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    source_id            VARCHAR(64) NOT NULL,
    document_key         TEXT NOT NULL,
    scope                TEXT NOT NULL CHECK (scope IN ('namespace', 'flow', 'run')),
    flow_id              VARCHAR(64),
    run_id               VARCHAR(64),
    external_id          TEXT,
    source_uri           TEXT,
    acl_ref              TEXT,
    classification       TEXT NOT NULL DEFAULT 'internal',
    current_revision_id  VARCHAR(64),
    description          TEXT,
    status               TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by           VARCHAR(255),
    updated_by           VARCHAR(255),
    row_version          BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata             JSONB,
    CONSTRAINT fk_knw_document_source FOREIGN KEY (source_id, namespace_id)
        REFERENCES knw_source(id, namespace_id) ON DELETE RESTRICT,
    CONSTRAINT fk_knw_document_flow FOREIGN KEY (flow_id, namespace_id)
        REFERENCES orh_flow(id, namespace_id) ON DELETE RESTRICT,
    CONSTRAINT fk_knw_document_run FOREIGN KEY (run_id, flow_id, namespace_id)
        REFERENCES orh_run(id, flow_id, namespace_id) ON DELETE RESTRICT,
    CONSTRAINT ck_knw_document_scope CHECK (
        (scope = 'namespace' AND flow_id IS NULL AND run_id IS NULL) OR
        (scope = 'flow' AND flow_id IS NOT NULL AND run_id IS NULL) OR
        (scope = 'run' AND flow_id IS NOT NULL AND run_id IS NOT NULL)
    ),
    UNIQUE (namespace_id, document_key),
    UNIQUE (id, namespace_id)
);
CREATE INDEX idx_knw_document_scope ON knw_document (namespace_id, scope, flow_id, run_id, status);
CREATE INDEX idx_knw_document_source ON knw_document (source_id, status);

CREATE TABLE knw_document_revision (
    id              VARCHAR(64) PRIMARY KEY,
    namespace_id    VARCHAR(64) NOT NULL,
    document_id     VARCHAR(64) NOT NULL,
    revision        BIGINT NOT NULL CHECK (revision > 0),
    source_revision TEXT,
    title           TEXT NOT NULL,
    source_path     TEXT,
    provenance      JSONB NOT NULL,
    content_hash    CHAR(64) NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    status          TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'indexing', 'published', 'failed', 'retired')),
    approval_id     VARCHAR(64),
    description     TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by      VARCHAR(255),
    updated_by      VARCHAR(255),
    row_version     BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata        JSONB,
    CONSTRAINT fk_knw_document_revision_document FOREIGN KEY (document_id, namespace_id)
        REFERENCES knw_document(id, namespace_id) ON DELETE RESTRICT,
    CONSTRAINT fk_knw_document_revision_approval FOREIGN KEY (approval_id, namespace_id)
        REFERENCES orh_approval(id, namespace_id) ON DELETE RESTRICT,
    CONSTRAINT ck_knw_document_revision_approval CHECK (status <> 'published' OR approval_id IS NOT NULL),
    UNIQUE (document_id, revision),
    UNIQUE (id, document_id, namespace_id),
    UNIQUE (id, namespace_id)
);
ALTER TABLE knw_document ADD CONSTRAINT fk_knw_document_current_revision
    FOREIGN KEY (current_revision_id, id, namespace_id)
    REFERENCES knw_document_revision(id, document_id, namespace_id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE OR REPLACE FUNCTION knw_document_pointer_guard()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE pointed_status TEXT;
BEGIN
    IF NEW.current_revision_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT status INTO pointed_status FROM knw_document_revision
     WHERE id=NEW.current_revision_id AND document_id=NEW.id AND namespace_id=NEW.namespace_id;
    IF pointed_status IS DISTINCT FROM 'published' THEN
        RAISE EXCEPTION 'current knowledge revision must be published' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_knw_document_pointer
BEFORE INSERT OR UPDATE OF current_revision_id ON knw_document
FOR EACH ROW EXECUTE FUNCTION knw_document_pointer_guard();

CREATE TABLE knw_content (
    id                    VARCHAR(64) PRIMARY KEY,
    namespace_id          VARCHAR(64) NOT NULL,
    document_revision_id  VARCHAR(64) NOT NULL,
    parent_id             VARCHAR(64),
    type                  TEXT NOT NULL CHECK (type IN ('section', 'chunk', 'summary')),
    ordinal               INTEGER NOT NULL CHECK (ordinal >= 0),
    heading_path          TEXT,
    page_start            INTEGER,
    page_end              INTEGER,
    content               TEXT NOT NULL,
    content_hash          CHAR(64) NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    search_vector         TSVECTOR GENERATED ALWAYS AS (to_tsvector('simple', content)) STORED,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by            VARCHAR(255),
    updated_by            VARCHAR(255),
    row_version           BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata              JSONB,
    CONSTRAINT fk_knw_content_revision FOREIGN KEY (document_revision_id, namespace_id)
        REFERENCES knw_document_revision(id, namespace_id) ON DELETE RESTRICT,
    CONSTRAINT fk_knw_content_parent FOREIGN KEY (parent_id, document_revision_id, namespace_id)
        REFERENCES knw_content(id, document_revision_id, namespace_id) ON DELETE RESTRICT,
    CONSTRAINT ck_knw_content_pages CHECK (
        (page_start IS NULL AND page_end IS NULL) OR
        (page_start IS NOT NULL AND page_end IS NOT NULL AND page_start > 0 AND page_end >= page_start)
    ),
    UNIQUE (id, document_revision_id, namespace_id),
    UNIQUE (id, namespace_id)
);
CREATE UNIQUE INDEX uq_knw_content_root_ordinal
    ON knw_content (document_revision_id, ordinal) WHERE parent_id IS NULL;
CREATE UNIQUE INDEX uq_knw_content_child_ordinal
    ON knw_content (document_revision_id, parent_id, ordinal) WHERE parent_id IS NOT NULL;
CREATE INDEX idx_knw_content_parent ON knw_content (document_revision_id, parent_id, ordinal);
CREATE INDEX idx_knw_content_fts ON knw_content USING GIN (search_vector);

CREATE OR REPLACE FUNCTION knw_content_hierarchy_guard()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    cyclic BOOLEAN;
BEGIN
    IF NEW.parent_id IS NULL THEN
        RETURN NEW;
    END IF;
    IF NEW.parent_id=NEW.id THEN
        RAISE EXCEPTION 'knowledge content hierarchy is cyclic' USING ERRCODE = '23514';
    END IF;
    WITH RECURSIVE ancestors(id,parent_id) AS (
        SELECT id,parent_id FROM knw_content WHERE id=NEW.parent_id
        UNION ALL
        SELECT c.id,c.parent_id FROM knw_content c JOIN ancestors a ON c.id=a.parent_id
    ) SELECT EXISTS(SELECT 1 FROM ancestors WHERE id=NEW.id) INTO cyclic;
    IF cyclic THEN
        RAISE EXCEPTION 'knowledge content hierarchy is cyclic' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_knw_content_hierarchy
BEFORE INSERT OR UPDATE OF parent_id ON knw_content
FOR EACH ROW EXECUTE FUNCTION knw_content_hierarchy_guard();

CREATE TABLE knw_embedding_profile (
    id               VARCHAR(64) PRIMARY KEY,
    namespace_id     VARCHAR(64) NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    profile_key      TEXT NOT NULL,
    provider_type    TEXT NOT NULL,
    model            TEXT NOT NULL,
    model_revision   TEXT NOT NULL,
    dimensions       INTEGER NOT NULL CHECK (dimensions > 0 AND dimensions <= 16000),
    distance_metric  TEXT NOT NULL CHECK (distance_metric IN ('cosine', 'l2', 'l1')),
    status           TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by       VARCHAR(255),
    updated_by       VARCHAR(255),
    row_version      BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata         JSONB,
    UNIQUE (namespace_id, profile_key),
    UNIQUE (profile_key, namespace_id)
);

CREATE TABLE knw_embedding (
    id              VARCHAR(64) PRIMARY KEY,
    namespace_id    VARCHAR(64) NOT NULL,
    content_id      VARCHAR(64) NOT NULL,
    profile_key     TEXT NOT NULL,
    dimensions      INTEGER NOT NULL CHECK (dimensions > 0),
    distance_metric TEXT NOT NULL CHECK (distance_metric IN ('cosine', 'l2', 'l1')),
    input_hash      CHAR(64) NOT NULL CHECK (input_hash ~ '^[0-9a-f]{64}$'),
    embedding       VECTOR NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by      VARCHAR(255),
    updated_by      VARCHAR(255),
    row_version     BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata        JSONB,
    CONSTRAINT fk_knw_embedding_content FOREIGN KEY (content_id, namespace_id)
        REFERENCES knw_content(id, namespace_id) ON DELETE RESTRICT,
    CONSTRAINT fk_knw_embedding_profile FOREIGN KEY (profile_key, namespace_id)
        REFERENCES knw_embedding_profile(profile_key, namespace_id) ON DELETE RESTRICT,
    CONSTRAINT ck_knw_embedding_dimensions CHECK (vector_dims(embedding) = dimensions),
    UNIQUE (content_id, profile_key)
);
CREATE INDEX idx_knw_embedding_content ON knw_embedding (content_id, profile_key);

CREATE OR REPLACE FUNCTION knw_embedding_profile_guard()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.namespace_id IS DISTINCT FROM OLD.namespace_id
       OR NEW.profile_key IS DISTINCT FROM OLD.profile_key
       OR NEW.provider_type IS DISTINCT FROM OLD.provider_type
       OR NEW.model IS DISTINCT FROM OLD.model
       OR NEW.model_revision IS DISTINCT FROM OLD.model_revision
       OR NEW.dimensions IS DISTINCT FROM OLD.dimensions
       OR NEW.distance_metric IS DISTINCT FROM OLD.distance_metric THEN
        RAISE EXCEPTION 'embedding profile identity is immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_knw_embedding_profile_guard BEFORE UPDATE ON knw_embedding_profile
FOR EACH ROW EXECUTE FUNCTION knw_embedding_profile_guard();

CREATE OR REPLACE FUNCTION knw_embedding_validate_profile()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    expected_dimensions INTEGER;
    expected_metric TEXT;
BEGIN
    SELECT dimensions, distance_metric
      INTO expected_dimensions, expected_metric
      FROM knw_embedding_profile
     WHERE namespace_id = NEW.namespace_id AND profile_key = NEW.profile_key;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'unknown embedding profile %', NEW.profile_key USING ERRCODE = '23503';
    END IF;
    IF NEW.dimensions <> expected_dimensions OR vector_dims(NEW.embedding) <> expected_dimensions THEN
        RAISE EXCEPTION 'embedding dimension does not match profile %', NEW.profile_key USING ERRCODE = '22000';
    END IF;
    IF NEW.distance_metric <> expected_metric THEN
        RAISE EXCEPTION 'embedding distance metric does not match profile %', NEW.profile_key USING ERRCODE = '22000';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_knw_embedding_validate BEFORE INSERT OR UPDATE ON knw_embedding
FOR EACH ROW EXECUTE FUNCTION knw_embedding_validate_profile();

CREATE OR REPLACE FUNCTION knw_ensure_embedding_profile_index(p_namespace_id TEXT, p_profile_key TEXT)
RETURNS TEXT LANGUAGE plpgsql AS $$
DECLARE
    p_dimensions INTEGER;
    p_metric TEXT;
    opclass TEXT;
    storage_type TEXT;
    index_name TEXT;
BEGIN
    SELECT dimensions, distance_metric INTO p_dimensions, p_metric
      FROM knw_embedding_profile
     WHERE namespace_id = p_namespace_id AND profile_key = p_profile_key;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'unknown embedding profile %/%', p_namespace_id, p_profile_key USING ERRCODE = '23503';
    END IF;

    -- pgvector HNSW supports vector up to 2,000 dimensions and halfvec up
    -- to 4,000. Larger valid profiles remain searchable by an exact scan;
    -- rejecting them would incorrectly hard-code one embedding model family.
    IF p_dimensions <= 2000 THEN
        storage_type := 'vector';
        opclass := CASE p_metric
            WHEN 'cosine' THEN 'vector_cosine_ops'
            WHEN 'l1' THEN 'vector_l1_ops'
            ELSE 'vector_l2_ops'
        END;
    ELSIF p_dimensions <= 4000 THEN
        storage_type := 'halfvec';
        opclass := CASE p_metric
            WHEN 'cosine' THEN 'halfvec_cosine_ops'
            WHEN 'l1' THEN 'halfvec_l1_ops'
            ELSE 'halfvec_l2_ops'
        END;
    ELSE
        RETURN 'exact_scan';
    END IF;
    index_name := 'idx_knw_embedding_vec_' || substr(md5(p_namespace_id || chr(1) || p_profile_key), 1, 20);
    EXECUTE format(
        'CREATE INDEX IF NOT EXISTS %I ON knw_embedding USING hnsw ((embedding::%s(%s)) %s) WHERE namespace_id = %L AND profile_key = %L',
        index_name, storage_type, p_dimensions, opclass, p_namespace_id, p_profile_key
    );
    RETURN index_name;
END;
$$;

CREATE OR REPLACE FUNCTION knw_embedding_profile_after_insert()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    PERFORM knw_ensure_embedding_profile_index(NEW.namespace_id, NEW.profile_key);
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_knw_embedding_profile_index AFTER INSERT ON knw_embedding_profile
FOR EACH ROW EXECUTE FUNCTION knw_embedding_profile_after_insert();

CREATE TABLE knw_candidate (
    id                  VARCHAR(64) PRIMARY KEY,
    namespace_id        VARCHAR(64) NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    source_run_id       VARCHAR(64) NOT NULL,
    target_scope        TEXT NOT NULL CHECK (target_scope IN ('namespace', 'flow')),
    target_flow_id      VARCHAR(64),
    target_document_id  VARCHAR(64),
    type                TEXT NOT NULL CHECK (type IN ('knowledge', 'instruction')),
    content             TEXT NOT NULL,
    content_hash        CHAR(64) NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    provenance          JSONB NOT NULL,
    expected_revision   BIGINT NOT NULL CHECK (expected_revision >= 0),
    approval_id         VARCHAR(64) NOT NULL,
    status              TEXT NOT NULL DEFAULT 'unpublished'
                            CHECK (status IN ('unpublished', 'published', 'failed')),
    idempotency_key     TEXT NOT NULL,
    description         TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by          VARCHAR(255),
    updated_by          VARCHAR(255),
    row_version         BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata            JSONB,
    CONSTRAINT fk_knw_candidate_run FOREIGN KEY (source_run_id, namespace_id)
        REFERENCES orh_run(id, namespace_id) ON DELETE RESTRICT,
    CONSTRAINT fk_knw_candidate_flow FOREIGN KEY (target_flow_id, namespace_id)
        REFERENCES orh_flow(id, namespace_id) ON DELETE RESTRICT,
    CONSTRAINT fk_knw_candidate_document FOREIGN KEY (target_document_id, namespace_id)
        REFERENCES knw_document(id, namespace_id) ON DELETE RESTRICT,
    CONSTRAINT fk_knw_candidate_approval FOREIGN KEY (approval_id, namespace_id)
        REFERENCES orh_approval(id, namespace_id) ON DELETE RESTRICT,
    CONSTRAINT ck_knw_candidate_scope CHECK (
        (target_scope = 'namespace' AND target_flow_id IS NULL) OR
        (target_scope = 'flow' AND target_flow_id IS NOT NULL)
    ),
    CONSTRAINT ck_knw_candidate_target CHECK (
        (type = 'instruction' AND target_document_id IS NULL) OR type = 'knowledge'
    ),
    UNIQUE (approval_id),
    UNIQUE (namespace_id, idempotency_key)
);
CREATE INDEX idx_knw_candidate_publish ON knw_candidate (namespace_id, status, created_at);

CREATE OR REPLACE FUNCTION knw_candidate_guard()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.namespace_id IS DISTINCT FROM OLD.namespace_id
       OR NEW.source_run_id IS DISTINCT FROM OLD.source_run_id
       OR NEW.target_scope IS DISTINCT FROM OLD.target_scope
       OR NEW.target_flow_id IS DISTINCT FROM OLD.target_flow_id
       OR NEW.target_document_id IS DISTINCT FROM OLD.target_document_id
       OR NEW.type IS DISTINCT FROM OLD.type
       OR NEW.content IS DISTINCT FROM OLD.content
       OR NEW.content_hash IS DISTINCT FROM OLD.content_hash
       OR NEW.provenance IS DISTINCT FROM OLD.provenance
       OR NEW.expected_revision IS DISTINCT FROM OLD.expected_revision
       OR NEW.approval_id IS DISTINCT FROM OLD.approval_id
       OR NEW.idempotency_key IS DISTINCT FROM OLD.idempotency_key THEN
        RAISE EXCEPTION 'knowledge candidate request is immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status <> 'unpublished' AND NEW.status IS DISTINCT FROM OLD.status THEN
        RAISE EXCEPTION 'knowledge candidate terminal status is final' USING ERRCODE = '55000';
    END IF;
    NEW.updated_at := clock_timestamp();
    NEW.row_version := OLD.row_version + 1;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_knw_candidate_guard BEFORE UPDATE ON knw_candidate
FOR EACH ROW EXECUTE FUNCTION knw_candidate_guard();

CREATE OR REPLACE FUNCTION knw_document_revision_guard()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.namespace_id IS DISTINCT FROM OLD.namespace_id
       OR NEW.document_id IS DISTINCT FROM OLD.document_id
       OR NEW.revision IS DISTINCT FROM OLD.revision
       OR NEW.source_revision IS DISTINCT FROM OLD.source_revision
       OR NEW.title IS DISTINCT FROM OLD.title
       OR NEW.source_path IS DISTINCT FROM OLD.source_path
       OR NEW.provenance IS DISTINCT FROM OLD.provenance
       OR NEW.content_hash IS DISTINCT FROM OLD.content_hash THEN
        RAISE EXCEPTION 'document revision content and provenance are immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'published' AND NEW.approval_id IS DISTINCT FROM OLD.approval_id THEN
        RAISE EXCEPTION 'published document approval is immutable' USING ERRCODE = '55000';
    END IF;
    NEW.updated_at := clock_timestamp();
    NEW.row_version := OLD.row_version + 1;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_knw_document_revision_guard BEFORE UPDATE ON knw_document_revision
FOR EACH ROW EXECUTE FUNCTION knw_document_revision_guard();

CREATE OR REPLACE FUNCTION knw_content_guard()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE revision_status TEXT;
BEGIN
    SELECT status INTO revision_status FROM knw_document_revision
     WHERE id = COALESCE(OLD.document_revision_id, NEW.document_revision_id);
    IF revision_status = 'published' THEN
        RAISE EXCEPTION 'published document content is immutable' USING ERRCODE = '55000';
    END IF;
    IF TG_OP = 'UPDATE' THEN
        NEW.updated_at := clock_timestamp();
        NEW.row_version := OLD.row_version + 1;
        RETURN NEW;
    END IF;
    RETURN OLD;
END;
$$;
CREATE TRIGGER trg_knw_content_guard BEFORE UPDATE OR DELETE ON knw_content
FOR EACH ROW EXECUTE FUNCTION knw_content_guard();

-- ---------------------------------------------------------------------------
-- Runtime configuration, release catalog, notifications and A2A
-- ---------------------------------------------------------------------------

CREATE TABLE orh_runtime_configuration (
    id              VARCHAR(64) PRIMARY KEY,
    namespace_id    VARCHAR(64) NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    scope           TEXT NOT NULL CHECK (scope IN ('namespace', 'flow')),
    flow_id         VARCHAR(64),
    environment     JSONB NOT NULL DEFAULT '{}'::jsonb,
    sealed_secrets  JSONB,
    secret_keys     JSONB NOT NULL DEFAULT '[]'::jsonb,
    description     TEXT,
    status          TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by      VARCHAR(255),
    updated_by      VARCHAR(255),
    row_version     BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata        JSONB,
    CONSTRAINT fk_orh_runtime_configuration_flow FOREIGN KEY (flow_id, namespace_id)
        REFERENCES orh_flow(id, namespace_id) ON DELETE CASCADE,
    CONSTRAINT ck_orh_runtime_configuration_scope CHECK (
        (scope = 'namespace' AND flow_id IS NULL) OR
        (scope = 'flow' AND flow_id IS NOT NULL)
    ),
    UNIQUE (namespace_id, scope, flow_id)
);
CREATE UNIQUE INDEX uq_orh_runtime_configuration_namespace
    ON orh_runtime_configuration (namespace_id) WHERE scope = 'namespace';
CREATE UNIQUE INDEX uq_orh_runtime_configuration_flow
    ON orh_runtime_configuration (flow_id) WHERE scope = 'flow';

CREATE TABLE orh_flow_release (
    id                  VARCHAR(64) PRIMARY KEY,
    namespace_id        VARCHAR(64) NOT NULL,
    flow_id             VARCHAR(64) NOT NULL,
    flow_revision_id    VARCHAR(64) NOT NULL,
    flow_revision       BIGINT NOT NULL,
    release_version     TEXT NOT NULL,
    definition          JSONB NOT NULL,
    checksum            CHAR(64) NOT NULL CHECK (checksum ~ '^[0-9a-f]{64}$'),
    visibility          TEXT NOT NULL CHECK (visibility IN ('PRIVATE', 'SHARED')),
    published_at        TIMESTAMPTZ NOT NULL,
    description         TEXT,
    status              TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by          VARCHAR(255),
    updated_by          VARCHAR(255),
    row_version         BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata            JSONB,
    CONSTRAINT fk_orh_flow_release_revision FOREIGN KEY (flow_revision_id, flow_id, namespace_id)
        REFERENCES orh_flow_revision(id, flow_id, namespace_id) ON DELETE RESTRICT,
    UNIQUE (namespace_id, flow_id, release_version),
    UNIQUE (namespace_id, checksum),
    UNIQUE (id, namespace_id)
);
CREATE INDEX idx_orh_flow_release_catalog ON orh_flow_release (visibility, status, published_at DESC);

CREATE TABLE orh_flow_release_grant (
    id                  VARCHAR(64) PRIMARY KEY,
    namespace_id        VARCHAR(64) NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    release_id          VARCHAR(64) NOT NULL,
    consumer_namespace_id VARCHAR(64) NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    expires_at          TIMESTAMPTZ,
    description         TEXT,
    status              TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by          VARCHAR(255),
    updated_by          VARCHAR(255),
    row_version         BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata            JSONB,
    CONSTRAINT fk_orh_flow_release_grant_release FOREIGN KEY (release_id, namespace_id)
        REFERENCES orh_flow_release(id, namespace_id) ON DELETE CASCADE,
    UNIQUE (release_id, consumer_namespace_id)
);

CREATE TABLE orh_flow_installation (
    id                    VARCHAR(64) PRIMARY KEY,
    namespace_id          VARCHAR(64) NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    release_id            VARCHAR(64) NOT NULL REFERENCES orh_flow_release(id) ON DELETE RESTRICT,
    release_version       TEXT NOT NULL,
    producer_namespace_id VARCHAR(64) NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    installed_flow_id     VARCHAR(64) NOT NULL,
    release_checksum      CHAR(64) NOT NULL,
    applied_checksum      CHAR(64) NOT NULL,
    resource_bindings     JSONB NOT NULL DEFAULT '{}'::jsonb,
    installed_at          TIMESTAMPTZ NOT NULL,
    description           TEXT,
    status                TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by            VARCHAR(255),
    updated_by            VARCHAR(255),
    row_version           BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata              JSONB,
    CONSTRAINT fk_orh_flow_installation_flow FOREIGN KEY (installed_flow_id, namespace_id)
        REFERENCES orh_flow(id, namespace_id) ON DELETE RESTRICT,
    UNIQUE (namespace_id, installed_flow_id)
);

CREATE TABLE nfy_channel (
    id              VARCHAR(64) PRIMARY KEY,
    namespace_id    VARCHAR(64) NOT NULL REFERENCES orh_namespace(id) ON DELETE RESTRICT,
    name            TEXT NOT NULL,
    channel_type    TEXT NOT NULL,
    config          JSONB NOT NULL,
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    description     TEXT,
    status          TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by      VARCHAR(255),
    updated_by      VARCHAR(255),
    row_version     BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata        JSONB,
    UNIQUE (namespace_id, name)
);

CREATE TABLE orh_a2a_task (
    id              VARCHAR(64) PRIMARY KEY,
    caller_key      CHAR(64) NOT NULL CHECK (caller_key ~ '^[0-9a-f]{64}$'),
    context_id      TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL,
    task            JSONB NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by      VARCHAR(255),
    updated_by      VARCHAR(255),
    row_version     BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0),
    metadata        JSONB
);
CREATE INDEX idx_orh_a2a_task_caller_updated ON orh_a2a_task (caller_key, updated_at DESC, id DESC);
CREATE INDEX idx_orh_a2a_task_caller_context ON orh_a2a_task (caller_key, context_id, updated_at DESC);

-- ---------------------------------------------------------------------------
-- Immutability and automatic mutable-entity maintenance
-- ---------------------------------------------------------------------------

CREATE TRIGGER trg_orh_flow_revision_immutable BEFORE UPDATE OR DELETE ON orh_flow_revision
FOR EACH ROW EXECUTE FUNCTION flowgent_reject_update_delete();
CREATE TRIGGER trg_llm_agent_revision_immutable BEFORE UPDATE OR DELETE ON llm_agent_revision
FOR EACH ROW EXECUTE FUNCTION flowgent_reject_update_delete();
CREATE TRIGGER trg_llm_skill_revision_immutable BEFORE UPDATE OR DELETE ON llm_skill_revision
FOR EACH ROW EXECUTE FUNCTION flowgent_reject_update_delete();
CREATE TRIGGER trg_llm_skill_file_immutable BEFORE UPDATE OR DELETE ON llm_skill_file
FOR EACH ROW EXECUTE FUNCTION flowgent_reject_update_delete();
CREATE TRIGGER trg_orh_node_checkpoint_immutable BEFORE UPDATE OR DELETE ON orh_node_checkpoint
FOR EACH ROW EXECUTE FUNCTION flowgent_reject_update_delete();
CREATE TRIGGER trg_orh_node_message_immutable BEFORE UPDATE OR DELETE ON orh_node_message
FOR EACH ROW EXECUTE FUNCTION flowgent_reject_update_delete();
CREATE TRIGGER trg_orh_event_immutable BEFORE UPDATE OR DELETE ON orh_event
FOR EACH ROW EXECUTE FUNCTION flowgent_reject_update_delete();

DO $$
DECLARE table_name TEXT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'orh_namespace', 'orh_flow', 'llm_provider', 'llm_mcp', 'llm_agent',
        'llm_skill', 'orh_run', 'orh_node_run', 'orh_dispatch', 'orh_outbox',
        'orh_effect_execution', 'knw_source', 'knw_document',
        'knw_embedding_profile', 'orh_runtime_configuration',
        'orh_flow_release', 'orh_flow_release_grant', 'orh_flow_installation',
        'nfy_channel', 'orh_a2a_task'
    ] LOOP
        EXECUTE format(
            'CREATE TRIGGER %I BEFORE UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION flowgent_touch_mutable()',
            'trg_' || table_name || '_touch', table_name
        );
    END LOOP;
END;
$$;

-- knw_embedding rows are immutable for a (content, profile) pair. Re-embedding
-- requires a new immutable profile_key, never an in-place vector overwrite.
CREATE TRIGGER trg_knw_embedding_immutable BEFORE UPDATE OR DELETE ON knw_embedding
FOR EACH ROW EXECUTE FUNCTION flowgent_reject_update_delete();
