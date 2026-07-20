-- Flowgent SQLite — Initial Schema
-- Unified DDL with BaseEntity columns (id, description, tenant_id, status,
-- created_at, created_by, updated_at, updated_by, del_flag) on all tables.

-- ── Agentflow Definitions & Versions ────────────────────────────────────────
CREATE TABLE IF NOT EXISTS orh_agentflow (
    id           TEXT PRIMARY KEY,
    agentflow_id TEXT    NOT NULL,
    version      INTEGER NOT NULL DEFAULT 1,
    definition   TEXT    NOT NULL,
    checksum     TEXT,
    comment      TEXT,
    priority     TEXT    DEFAULT 'medium',
    namespace    TEXT    DEFAULT '',
    mode         TEXT    DEFAULT '',
    labels       TEXT    DEFAULT '{}',
    description  TEXT    NOT NULL DEFAULT '',
    tenant_id    TEXT    NOT NULL DEFAULT 'default',
    status       TEXT    NOT NULL DEFAULT 'ACTIVE',
    created_at   TEXT    NOT NULL DEFAULT (datetime('now')),
    created_by   TEXT    NOT NULL DEFAULT '',
    updated_at   TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_by   TEXT    NOT NULL DEFAULT '',
    del_flag     INTEGER NOT NULL DEFAULT 0,
    UNIQUE(agentflow_id, version)
);
CREATE INDEX IF NOT EXISTS idx_orhagentflow_tenant ON orh_agentflow(tenant_id);

-- ── Flow Runs ───────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS orh_flowrun (
    id              TEXT PRIMARY KEY,
    agentflow_id    TEXT    NOT NULL,
    version         INTEGER NOT NULL DEFAULT 1,
    status          TEXT    NOT NULL DEFAULT 'PENDING',
    vars            TEXT,
    output          TEXT,
    error           TEXT,
    trigger_type    TEXT,
    trigger_source  TEXT,
    trigger_payload TEXT,
    labels          TEXT    DEFAULT '{}',
    started_at      TEXT,
    finished_at     TEXT,
    priority        TEXT    DEFAULT 'medium',
    namespace       TEXT    DEFAULT '',
    description     TEXT    NOT NULL DEFAULT '',
    tenant_id       TEXT    NOT NULL DEFAULT 'default',
    created_at      TEXT    NOT NULL DEFAULT (datetime('now')),
    created_by      TEXT    NOT NULL DEFAULT '',
    updated_at      TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_by      TEXT    NOT NULL DEFAULT '',
    del_flag        INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_orhflowrun_agentflow ON orh_flowrun(agentflow_id);
CREATE INDEX IF NOT EXISTS idx_orhflowrun_status    ON orh_flowrun(status);
CREATE INDEX IF NOT EXISTS idx_orhflowrun_tenant    ON orh_flowrun(tenant_id);

-- ── Task Runs ───────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS task_runs (
    id                 TEXT PRIMARY KEY,
    agentflow_run_id   TEXT    NOT NULL REFERENCES orh_flowrun(id),
    node_id            TEXT    NOT NULL,
    status             TEXT    NOT NULL DEFAULT 'PENDING',
    input              TEXT,
    output             TEXT,
    error              TEXT,
    retry_count        INTEGER NOT NULL DEFAULT 0,
    max_retries        INTEGER NOT NULL DEFAULT 0,
    exec_id            TEXT,
    parent_task_run_id TEXT,
    sequence           INTEGER NOT NULL DEFAULT 0,
    started_at         TEXT,
    finished_at        TEXT,
    description        TEXT    NOT NULL DEFAULT '',
    tenant_id          TEXT    NOT NULL DEFAULT 'default',
    created_at         TEXT    NOT NULL DEFAULT (datetime('now')),
    created_by         TEXT    NOT NULL DEFAULT '',
    updated_at         TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_by         TEXT    NOT NULL DEFAULT '',
    del_flag           INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_tasks_run    ON task_runs(agentflow_run_id);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON task_runs(status);
CREATE INDEX IF NOT EXISTS idx_tasks_tenant ON task_runs(tenant_id);

-- ── Human Approvals ─────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS human_approvals (
    id               TEXT PRIMARY KEY,
    token            TEXT    NOT NULL UNIQUE,
    agentflow_run_id TEXT    NOT NULL DEFAULT '',
    task_run_id      TEXT,
    status           TEXT    NOT NULL DEFAULT 'PENDING',
    approved         INTEGER DEFAULT 0,
    comment          TEXT,
    timeout_seconds  INTEGER DEFAULT 0,
    expires_at       TEXT,
    resolved_at      TEXT,
    description      TEXT    NOT NULL DEFAULT '',
    tenant_id        TEXT    NOT NULL DEFAULT 'default',
    created_at       TEXT    NOT NULL DEFAULT (datetime('now')),
    created_by       TEXT    NOT NULL DEFAULT '',
    updated_at       TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_by       TEXT    NOT NULL DEFAULT '',
    del_flag         INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_human_agentflow_run ON human_approvals(agentflow_run_id);
CREATE INDEX IF NOT EXISTS idx_human_token         ON human_approvals(token);
CREATE INDEX IF NOT EXISTS idx_human_tenant        ON human_approvals(tenant_id);

-- ── Supervisor Log ──────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS supervisor_log (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    agentflow_run_id TEXT NOT NULL,
    task_run_id      TEXT,
    input_snapshot   TEXT,
    decision         TEXT,
    description      TEXT    NOT NULL DEFAULT '',
    tenant_id        TEXT    NOT NULL DEFAULT 'default',
    status           TEXT    NOT NULL DEFAULT 'ACTIVE',
    created_at       TEXT    NOT NULL DEFAULT (datetime('now')),
    created_by       TEXT    NOT NULL DEFAULT '',
    updated_at       TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_by       TEXT    NOT NULL DEFAULT '',
    del_flag         INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_supervisor_run    ON supervisor_log(agentflow_run_id);
CREATE INDEX IF NOT EXISTS idx_supervisor_tenant ON supervisor_log(tenant_id);

-- ── LLM Agent Definitions ───────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS llm_agent (
    id            TEXT PRIMARY KEY,
    name          TEXT    NOT NULL UNIQUE,
    model         TEXT    NOT NULL,
    soul          TEXT    NOT NULL,
    instruction   TEXT    NOT NULL,
    output_schema TEXT,
    temperature   REAL,
    max_tokens    INTEGER DEFAULT 0,
    labels        TEXT    DEFAULT '{}',
    description   TEXT    NOT NULL DEFAULT '',
    tenant_id     TEXT    NOT NULL DEFAULT 'default',
    status        TEXT    NOT NULL DEFAULT 'ACTIVE',
    created_at    TEXT    NOT NULL DEFAULT (datetime('now')),
    created_by    TEXT    NOT NULL DEFAULT '',
    updated_at    TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_by    TEXT    NOT NULL DEFAULT '',
    del_flag      INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_llmagent_tenant ON llm_agent(tenant_id);

-- ── Notification Channels ───────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS nfy_channel (
    id           TEXT PRIMARY KEY,
    name         TEXT    NOT NULL,
    channel_type TEXT    NOT NULL,
    config       TEXT    NOT NULL,
    enabled      INTEGER NOT NULL DEFAULT 1,
    labels       TEXT    DEFAULT '{}',
    description  TEXT    NOT NULL DEFAULT '',
    tenant_id    TEXT    NOT NULL DEFAULT 'default',
    status       TEXT    NOT NULL DEFAULT 'ACTIVE',
    created_at   TEXT    NOT NULL DEFAULT (datetime('now')),
    created_by   TEXT    NOT NULL DEFAULT '',
    updated_at   TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_by   TEXT    NOT NULL DEFAULT '',
    del_flag     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_nfychannel_tenant ON nfy_channel(tenant_id);

-- ── Subscription Routes ─────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS subscription_routes (
    id           TEXT PRIMARY KEY,
    agentflow_id TEXT NOT NULL,
    ws_id        TEXT NOT NULL,
    pod_id       TEXT NOT NULL,
    description  TEXT    NOT NULL DEFAULT '',
    tenant_id    TEXT    NOT NULL DEFAULT 'default',
    status       TEXT    NOT NULL DEFAULT 'ACTIVE',
    created_at   TEXT    NOT NULL DEFAULT (datetime('now')),
    created_by   TEXT    NOT NULL DEFAULT '',
    updated_at   TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_by   TEXT    NOT NULL DEFAULT '',
    del_flag     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_subroutes_agentflow ON subscription_routes(agentflow_id);
CREATE INDEX IF NOT EXISTS idx_subroutes_pod       ON subscription_routes(pod_id);

-- ── MCP Definitions ─────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS llm_mcp (
    id          TEXT PRIMARY KEY,
    name        TEXT    NOT NULL UNIQUE,
    enabled     INTEGER NOT NULL DEFAULT 1,
    type        TEXT    NOT NULL DEFAULT '',
    url         TEXT    NOT NULL DEFAULT '',
    headers     TEXT    DEFAULT '{}',
    command     TEXT    DEFAULT '[]',
    args        TEXT    DEFAULT '[]',
    env         TEXT    DEFAULT '{}',
    labels      TEXT    DEFAULT '{}',
    description TEXT    NOT NULL DEFAULT '',
    tenant_id   TEXT    NOT NULL DEFAULT 'default',
    status      TEXT    NOT NULL DEFAULT 'ACTIVE',
    created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    created_by  TEXT    NOT NULL DEFAULT '',
    updated_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_by  TEXT    NOT NULL DEFAULT '',
    del_flag    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_llmmcp_tenant ON llm_mcp(tenant_id);

-- ── LLM Providers ───────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS llm_providers (
    id          TEXT PRIMARY KEY,
    provider    TEXT    NOT NULL,
    endpoint    TEXT    NOT NULL DEFAULT '',
    apikey      TEXT    NOT NULL DEFAULT '',
    model       TEXT    NOT NULL DEFAULT '',
    models      TEXT    DEFAULT '[]',
    timeout_ms  INTEGER DEFAULT 30000,
    labels      TEXT    DEFAULT '{}',
    description TEXT    NOT NULL DEFAULT '',
    tenant_id   TEXT    NOT NULL DEFAULT 'default',
    status      TEXT    NOT NULL DEFAULT 'ACTIVE',
    created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    created_by  TEXT    NOT NULL DEFAULT '',
    updated_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_by  TEXT    NOT NULL DEFAULT '',
    del_flag    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_llmproviders_tenant ON llm_providers(tenant_id);

-- ── Skill Definitions ───────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS llm_skill (
    id          TEXT PRIMARY KEY,
    name        TEXT    NOT NULL UNIQUE,
    instruction TEXT    NOT NULL DEFAULT '',
    model       TEXT    NOT NULL DEFAULT '',
    temperature REAL,
    max_tokens  INTEGER DEFAULT 0,
    tools       TEXT    DEFAULT '[]',
    description TEXT    NOT NULL DEFAULT '',
    tenant_id   TEXT    NOT NULL DEFAULT 'default',
    status      TEXT    NOT NULL DEFAULT 'ACTIVE',
    created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    created_by  TEXT    NOT NULL DEFAULT '',
    updated_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_by  TEXT    NOT NULL DEFAULT '',
    del_flag    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_llmskill_tenant ON llm_skill(tenant_id);

-- ── RAG Memory ──────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS llm_memory (
    id          TEXT PRIMARY KEY,
    flow_id     TEXT NOT NULL,
    node_id     TEXT,
    content     TEXT NOT NULL,
    embedding   TEXT,
    metadata    TEXT,
    description TEXT    NOT NULL DEFAULT '',
    tenant_id   TEXT    NOT NULL DEFAULT 'default',
    status      TEXT    NOT NULL DEFAULT 'ACTIVE',
    created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    created_by  TEXT    NOT NULL DEFAULT '',
    updated_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_by  TEXT    NOT NULL DEFAULT '',
    del_flag    INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_llmmemory_flownode ON llm_memory(flow_id, node_id);
CREATE INDEX IF NOT EXISTS idx_llmmemory_tenant ON llm_memory(tenant_id);

-- ── Knowledge Entries ───────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS knowledge_entries (
    id           TEXT PRIMARY KEY,
    category     TEXT,
    title        TEXT,
    content      TEXT NOT NULL,
    content_type TEXT    NOT NULL DEFAULT 'text',
    source       TEXT,
    source_ref   TEXT,
    tags         TEXT,
    metadata     TEXT    DEFAULT '{}',
    description  TEXT    NOT NULL DEFAULT '',
    tenant_id    TEXT    NOT NULL DEFAULT 'default',
    status       TEXT    NOT NULL DEFAULT 'ACTIVE',
    created_at   TEXT    NOT NULL DEFAULT (datetime('now')),
    created_by   TEXT    NOT NULL DEFAULT '',
    updated_at   TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_by   TEXT    NOT NULL DEFAULT '',
    del_flag     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_knowledge_category ON knowledge_entries(category);
CREATE INDEX IF NOT EXISTS idx_knowledge_tenant    ON knowledge_entries(tenant_id);

-- ── Schema Migrations ───────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    TEXT NOT NULL,
    filename   TEXT NOT NULL,
    applied_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (version, filename)
);
