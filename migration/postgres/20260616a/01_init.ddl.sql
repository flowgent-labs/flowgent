-- Flowgent PostgreSQL — Initial Schema
-- Unified DDL with BaseEntity columns (id, description, tenant_id, status,
-- created_at, created_by, updated_at, updated_by, del_flag) on all tables.

-- ── Agentflow Definitions & Versions ────────────────────────────────────────
CREATE TABLE IF NOT EXISTS orh_agentflow (
    id           VARCHAR(64) PRIMARY KEY,
    agentflow_id VARCHAR(255) NOT NULL,
    version      BIGINT NOT NULL DEFAULT 1,
    definition   JSONB NOT NULL,
    checksum     VARCHAR(255),
    comment      TEXT,
    priority     VARCHAR(16) DEFAULT 'medium',
    namespace    VARCHAR(255) DEFAULT '',
    mode         VARCHAR(32) DEFAULT 'session',
    labels       JSONB DEFAULT '{}',
    description  TEXT NOT NULL DEFAULT '',
    tenant_id    VARCHAR(255) NOT NULL DEFAULT 'default',
    status       VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by   VARCHAR(255) NOT NULL DEFAULT '',
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by   VARCHAR(255) NOT NULL DEFAULT '',
    del_flag     BOOLEAN NOT NULL DEFAULT false,
    UNIQUE(agentflow_id, version)
);
CREATE INDEX IF NOT EXISTS idx_orhagentflow_tenant ON orh_agentflow(tenant_id);

-- ── Flow Runs ───────────────────────────────────────────────────────────────
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
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,
    priority        VARCHAR(16) DEFAULT 'medium',
    namespace       VARCHAR(255) DEFAULT '',
    description     TEXT NOT NULL DEFAULT '',
    tenant_id       VARCHAR(255) NOT NULL DEFAULT 'default',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by      VARCHAR(255) NOT NULL DEFAULT '',
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by      VARCHAR(255) NOT NULL DEFAULT '',
    del_flag        BOOLEAN NOT NULL DEFAULT false
);
CREATE INDEX IF NOT EXISTS idx_orhflowrun_agentflow ON orh_flowrun(agentflow_id);
CREATE INDEX IF NOT EXISTS idx_orhflowrun_status    ON orh_flowrun(status);
CREATE INDEX IF NOT EXISTS idx_orhflowrun_tenant    ON orh_flowrun(tenant_id);

-- ── Task Runs ───────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS task_runs (
    id                VARCHAR(64) PRIMARY KEY,
    agentflow_run_id  VARCHAR(64) NOT NULL REFERENCES orh_flowrun(id),
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
    started_at        TIMESTAMPTZ,
    finished_at       TIMESTAMPTZ,
    description       TEXT NOT NULL DEFAULT '',
    tenant_id         VARCHAR(255) NOT NULL DEFAULT 'default',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by        VARCHAR(255) NOT NULL DEFAULT '',
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by        VARCHAR(255) NOT NULL DEFAULT '',
    del_flag          BOOLEAN NOT NULL DEFAULT false
);
CREATE INDEX IF NOT EXISTS idx_tasks_run    ON task_runs(agentflow_run_id);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON task_runs(status);
CREATE INDEX IF NOT EXISTS idx_tasks_tenant ON task_runs(tenant_id);

-- ── Human Approvals ─────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS human_approvals (
    id               VARCHAR(64) PRIMARY KEY,
    token            VARCHAR(128) NOT NULL UNIQUE,
    agentflow_run_id VARCHAR(64) NOT NULL DEFAULT '',
    task_run_id      VARCHAR(64),
    status           VARCHAR(32) NOT NULL DEFAULT 'PENDING',
    approved         BOOLEAN DEFAULT false,
    comment          TEXT,
    timeout_seconds  INTEGER DEFAULT 0,
    expires_at       TIMESTAMPTZ,
    resolved_at      TIMESTAMPTZ,
    description      TEXT NOT NULL DEFAULT '',
    tenant_id        VARCHAR(255) NOT NULL DEFAULT 'default',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by       VARCHAR(255) NOT NULL DEFAULT '',
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by       VARCHAR(255) NOT NULL DEFAULT '',
    del_flag         BOOLEAN NOT NULL DEFAULT false
);
CREATE INDEX IF NOT EXISTS idx_human_agentflow_run ON human_approvals(agentflow_run_id);
CREATE INDEX IF NOT EXISTS idx_human_token         ON human_approvals(token);
CREATE INDEX IF NOT EXISTS idx_human_tenant        ON human_approvals(tenant_id);

-- ── Supervisor Log ──────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS supervisor_log (
    id               BIGSERIAL PRIMARY KEY,
    agentflow_run_id VARCHAR(64) NOT NULL,
    task_run_id      VARCHAR(64),
    input_snapshot   JSONB,
    decision         JSONB,
    description      TEXT NOT NULL DEFAULT '',
    tenant_id        VARCHAR(255) NOT NULL DEFAULT 'default',
    status           VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by       VARCHAR(255) NOT NULL DEFAULT '',
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by       VARCHAR(255) NOT NULL DEFAULT '',
    del_flag         BOOLEAN NOT NULL DEFAULT false
);
CREATE INDEX IF NOT EXISTS idx_supervisor_run    ON supervisor_log(agentflow_run_id);
CREATE INDEX IF NOT EXISTS idx_supervisor_tenant ON supervisor_log(tenant_id);

-- ── LLM Agent Definitions ───────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS llm_agent (
    id            VARCHAR(64) PRIMARY KEY,
    name          VARCHAR(255) NOT NULL UNIQUE,
    model         VARCHAR(255) NOT NULL,
    soul          TEXT NOT NULL,
    instruction   TEXT NOT NULL,
    output_schema JSONB,
    temperature   DOUBLE PRECISION,
    max_tokens    INTEGER DEFAULT 0,
    description   TEXT NOT NULL DEFAULT '',
    tenant_id     VARCHAR(255) NOT NULL DEFAULT 'default',
    status        VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by    VARCHAR(255) NOT NULL DEFAULT '',
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by    VARCHAR(255) NOT NULL DEFAULT '',
    del_flag      BOOLEAN NOT NULL DEFAULT false
);
CREATE INDEX IF NOT EXISTS idx_llmagent_tenant ON llm_agent(tenant_id);

-- ── Notification Channels ───────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS nfy_channel (
    id           VARCHAR(64) PRIMARY KEY,
    name         VARCHAR(255) NOT NULL,
    channel_type VARCHAR(32) NOT NULL,
    config       JSONB NOT NULL,
    enabled      BOOLEAN NOT NULL DEFAULT true,
    description  TEXT NOT NULL DEFAULT '',
    tenant_id    VARCHAR(255) NOT NULL DEFAULT 'default',
    status       VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by   VARCHAR(255) NOT NULL DEFAULT '',
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by   VARCHAR(255) NOT NULL DEFAULT '',
    del_flag     BOOLEAN NOT NULL DEFAULT false
);
CREATE INDEX IF NOT EXISTS idx_nfychannel_tenant ON nfy_channel(tenant_id);

-- ── Subscription Routes ─────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS subscription_routes (
    id           VARCHAR(64) PRIMARY KEY,
    agentflow_id VARCHAR(255) NOT NULL,
    ws_id        VARCHAR(64) NOT NULL,
    pod_id       VARCHAR(64) NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    tenant_id    VARCHAR(255) NOT NULL DEFAULT 'default',
    status       VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by   VARCHAR(255) NOT NULL DEFAULT '',
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by   VARCHAR(255) NOT NULL DEFAULT '',
    del_flag     BOOLEAN NOT NULL DEFAULT false
);
CREATE INDEX IF NOT EXISTS idx_subroutes_agentflow ON subscription_routes(agentflow_id);
CREATE INDEX IF NOT EXISTS idx_subroutes_pod       ON subscription_routes(pod_id);

-- ── MCP Definitions ─────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS llm_mcp (
    id          VARCHAR(64) PRIMARY KEY,
    name        VARCHAR(255) NOT NULL UNIQUE,
    enabled     BOOLEAN NOT NULL DEFAULT true,
    type        VARCHAR(32) NOT NULL DEFAULT '',
    command     JSONB DEFAULT '[]',
    args        JSONB DEFAULT '[]',
    env         JSONB DEFAULT '{}',
    description TEXT NOT NULL DEFAULT '',
    tenant_id   VARCHAR(255) NOT NULL DEFAULT 'default',
    status      VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by  VARCHAR(255) NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by  VARCHAR(255) NOT NULL DEFAULT '',
    del_flag    BOOLEAN NOT NULL DEFAULT false
);
CREATE INDEX IF NOT EXISTS idx_llmmcp_tenant ON llm_mcp(tenant_id);

-- ── LLM Providers ───────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS llm_providers (
    id          VARCHAR(64) PRIMARY KEY,
    provider    VARCHAR(255) NOT NULL,
    endpoint    VARCHAR(512) NOT NULL DEFAULT '',
    apikey      VARCHAR(512) NOT NULL DEFAULT '',
    model       VARCHAR(255) NOT NULL DEFAULT '',
    models      JSONB DEFAULT '[]',
    timeout_ms  INTEGER DEFAULT 30000,
    description TEXT NOT NULL DEFAULT '',
    tenant_id   VARCHAR(255) NOT NULL DEFAULT 'default',
    status      VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by  VARCHAR(255) NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by  VARCHAR(255) NOT NULL DEFAULT '',
    del_flag    BOOLEAN NOT NULL DEFAULT false
);
CREATE INDEX IF NOT EXISTS idx_llmproviders_tenant ON llm_providers(tenant_id);

-- ── Skill Definitions ───────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS llm_skill (
    id          VARCHAR(64) PRIMARY KEY,
    name        VARCHAR(255) NOT NULL UNIQUE,
    instruction TEXT NOT NULL DEFAULT '',
    model       VARCHAR(255) NOT NULL DEFAULT '',
    temperature DOUBLE PRECISION,
    max_tokens  INTEGER DEFAULT 0,
    tools       JSONB DEFAULT '[]',
    description TEXT NOT NULL DEFAULT '',
    tenant_id   VARCHAR(255) NOT NULL DEFAULT 'default',
    status      VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by  VARCHAR(255) NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by  VARCHAR(255) NOT NULL DEFAULT '',
    del_flag    BOOLEAN NOT NULL DEFAULT false
);
CREATE INDEX IF NOT EXISTS idx_llmskill_tenant ON llm_skill(tenant_id);

-- ── RAG Memory ──────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS llm_memory (
    id          VARCHAR(64) PRIMARY KEY,
    flow_id     VARCHAR(255) NOT NULL,
    node_id     VARCHAR(64),
    content     JSONB NOT NULL,
    embedding   JSONB,
    metadata    JSONB,
    description TEXT NOT NULL DEFAULT '',
    tenant_id   VARCHAR(255) NOT NULL DEFAULT 'default',
    status      VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by  VARCHAR(255) NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by  VARCHAR(255) NOT NULL DEFAULT '',
    del_flag    BOOLEAN NOT NULL DEFAULT false
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_llmmemory_flownode ON llm_memory(flow_id, node_id);
CREATE INDEX IF NOT EXISTS idx_llmmemory_tenant ON llm_memory(tenant_id);

-- ── Knowledge Entries ───────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS knowledge_entries (
    id          VARCHAR(64) PRIMARY KEY,
    category    VARCHAR(255),
    title       VARCHAR(255),
    content     JSONB NOT NULL,
    embedding   JSONB,
    tags        JSONB,
    source      VARCHAR(255),
    description TEXT NOT NULL DEFAULT '',
    tenant_id   VARCHAR(255) NOT NULL DEFAULT 'default',
    status      VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by  VARCHAR(255) NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by  VARCHAR(255) NOT NULL DEFAULT '',
    del_flag    BOOLEAN NOT NULL DEFAULT false
);
CREATE INDEX IF NOT EXISTS idx_knowledge_category ON knowledge_entries(category);
CREATE INDEX IF NOT EXISTS idx_knowledge_tenant    ON knowledge_entries(tenant_id);

-- ── Schema Migrations ───────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    VARCHAR(16) NOT NULL,
    filename   VARCHAR(255) NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (version, filename)
);
