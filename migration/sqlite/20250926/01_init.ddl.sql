-- Flowgent SQLite schema
-- Column types use DATETIME for date fields so the Go sqlite3 driver
-- scans them as time.Time (not string).

CREATE TABLE IF NOT EXISTS orh_agentflow (
    agentflow_id TEXT    NOT NULL,
    version      INTEGER NOT NULL DEFAULT 1,
    definition   TEXT    NOT NULL,
    checksum     TEXT,
    created_by   TEXT,
    comment      TEXT,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (agentflow_id, version)
);

CREATE TABLE IF NOT EXISTS orh_flowrun (
    id              TEXT PRIMARY KEY,
    agentflow_id     TEXT NOT NULL,
    version         INTEGER NOT NULL DEFAULT 1,
    status          TEXT NOT NULL DEFAULT 'PENDING',
    vars            TEXT,
    output          TEXT,
    error           TEXT,
    trigger_type    TEXT,
    trigger_source  TEXT,
    trigger_payload TEXT,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at      DATETIME,
    finished_at     DATETIME
);
CREATE INDEX IF NOT EXISTS idx_orhflowrun_agentflow ON orh_flowrun(agentflow_id);
CREATE INDEX IF NOT EXISTS idx_orhflowrun_status    ON orh_flowrun(status);

CREATE TABLE IF NOT EXISTS task_runs (
    id                 TEXT PRIMARY KEY,
    agentflow_run_id    TEXT NOT NULL REFERENCES orh_flowrun(id),
    node_id            TEXT NOT NULL,
    status             TEXT NOT NULL DEFAULT 'PENDING',
    input              TEXT,
    output             TEXT,
    error              TEXT,
    retry_count        INTEGER NOT NULL DEFAULT 0,
    max_retries        INTEGER NOT NULL DEFAULT 0,
    exec_id            TEXT,
    parent_task_run_id  TEXT,
    sequence           INTEGER NOT NULL DEFAULT 0,
    created_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at         DATETIME,
    finished_at        DATETIME
);
CREATE INDEX IF NOT EXISTS idx_tasks_run    ON task_runs(agentflow_run_id);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON task_runs(status);

CREATE TABLE IF NOT EXISTS human_approvals (
    task_run_id     TEXT,
    token           TEXT PRIMARY KEY,
    status          TEXT NOT NULL DEFAULT 'PENDING',
    approved        INTEGER DEFAULT 0,
    comment         TEXT,
    timeout_seconds INTEGER DEFAULT 0,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at      DATETIME,
    resolved_at     DATETIME
);

CREATE TABLE IF NOT EXISTS supervisor_log (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    agentflow_run_id  TEXT NOT NULL,
    task_run_id      TEXT,
    input_snapshot   TEXT,
    decision         TEXT,
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_supervisor_run ON supervisor_log(agentflow_run_id);

CREATE TABLE IF NOT EXISTS schema_migrations (
    version    TEXT NOT NULL,
    filename   TEXT NOT NULL,
    applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (version, filename)
);

-- RAG agent memory
CREATE TABLE IF NOT EXISTS llm_memory (
    id               TEXT PRIMARY KEY,
    agent_id         TEXT NOT NULL,
    agentflow_run_id  TEXT,
    type             TEXT NOT NULL DEFAULT 'episodic',
    content          TEXT NOT NULL,
    embedding        TEXT,
    tags             TEXT,
    metadata         TEXT,
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_llmmemory_agent ON llm_memory(agent_id, type);

CREATE TABLE IF NOT EXISTS knowledge_entries (
    id         TEXT PRIMARY KEY,
    category   TEXT,
    title      TEXT,
    content    TEXT NOT NULL,
    embedding  TEXT,
    tags       TEXT,
    source     TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_knowledge_category ON knowledge_entries(category);
