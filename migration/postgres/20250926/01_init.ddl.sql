-- Flowgent PostgreSQL schema -- base tables
-- Applied automatically by the migration runner on first startup.

CREATE TABLE IF NOT EXISTS orh_agentflow (
    agentflow_id VARCHAR(255) NOT NULL,
    version      BIGINT NOT NULL DEFAULT 1,
    definition   JSONB NOT NULL,
    created_by   VARCHAR(255),
    comment      TEXT,
    priority     VARCHAR(16) DEFAULT 'medium',
    tenant_id    VARCHAR(255) DEFAULT 'default',
    namespace    VARCHAR(255) DEFAULT '',
    mode         VARCHAR(32) DEFAULT 'session',
    labels       JSONB DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (agentflow_id, version)
);

CREATE TABLE IF NOT EXISTS orh_flowrun (
    id              VARCHAR(64) PRIMARY KEY,
    agentflow_id    VARCHAR(255) NOT NULL,
    version         BIGINT NOT NULL DEFAULT 1,
    status          VARCHAR(32) NOT NULL DEFAULT 'PENDING',
    vars            JSONB,
    output          JSONB,
    error           TEXT,
    trigger_type    VARCHAR(32),
    trigger_source  VARCHAR(255),
    trigger_payload JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,
    tenant_id       VARCHAR(255) DEFAULT 'default',
    namespace       VARCHAR(255) DEFAULT '',
    priority        VARCHAR(16) DEFAULT 'medium'
);

CREATE INDEX IF NOT EXISTS idx_orhflowrun_agentflow ON orh_flowrun(agentflow_id);
CREATE INDEX IF NOT EXISTS idx_orhflowrun_status ON orh_flowrun(status);

CREATE TABLE IF NOT EXISTS task_runs (
    id                VARCHAR(64) PRIMARY KEY,
    agentflow_run_id   VARCHAR(64) NOT NULL REFERENCES orh_flowrun(id),
    node_id           VARCHAR(255) NOT NULL,
    status            VARCHAR(32) NOT NULL DEFAULT 'PENDING',
    input             JSONB,
    output            JSONB,
    error             TEXT,
    retry_count       INT NOT NULL DEFAULT 0,
    max_retries       INT NOT NULL DEFAULT 0,
    exec_id           VARCHAR(128),
    parent_task_run_id VARCHAR(64),
    sequence          INT NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at        TIMESTAMPTZ,
    finished_at       TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_tasks_run ON task_runs(agentflow_run_id);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON task_runs(status);

CREATE TABLE IF NOT EXISTS human_approvals (
    token            VARCHAR(128) PRIMARY KEY,
    agentflow_run_id  VARCHAR(64) NOT NULL,
    task_run_id      VARCHAR(64),
    status           VARCHAR(32) NOT NULL DEFAULT 'PENDING',
    timeout_at       TIMESTAMPTZ,
    approved_at      TIMESTAMPTZ,
    rejected_at      TIMESTAMPTZ,
    comment          TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_human_run ON human_approvals(agentflow_run_id);

CREATE TABLE IF NOT EXISTS supervisor_log (
    id               BIGSERIAL PRIMARY KEY,
    agentflow_run_id  VARCHAR(64) NOT NULL,
    task_run_id      VARCHAR(64),
    input            JSONB,
    decision         JSONB,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_supervisor_run ON supervisor_log(agentflow_run_id);


CREATE TABLE IF NOT EXISTS schema_migrations (
    version    VARCHAR(16) NOT NULL,
    filename   VARCHAR(255) NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (version, filename)
);

-- RAG agent memory tables
CREATE TABLE IF NOT EXISTS llm_memory (
    id         VARCHAR(64) PRIMARY KEY,
    agent_id   VARCHAR(255) NOT NULL,
    type       VARCHAR(32) NOT NULL DEFAULT 'episodic',
    content    JSONB NOT NULL,
    embedding  JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_llmmemory_agent ON llm_memory(agent_id, type);

CREATE TABLE IF NOT EXISTS knowledge_entries (
    id         VARCHAR(64) PRIMARY KEY,
    category   VARCHAR(255),
    content    JSONB NOT NULL,
    embedding  JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_knowledge_category ON knowledge_entries(category);
