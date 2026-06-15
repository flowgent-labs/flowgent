-- Flowgent SQLite -- Skill definitions table

CREATE TABLE IF NOT EXISTS llm_skill (
    name        TEXT PRIMARY KEY,
    description TEXT NOT NULL DEFAULT '',
    instruction TEXT NOT NULL DEFAULT '',
    model       TEXT NOT NULL DEFAULT '',
    temperature REAL,
    max_tokens  INTEGER DEFAULT 0,
    tools       TEXT DEFAULT '[]',
    tenant_id   TEXT NOT NULL DEFAULT 'default',
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_llmskill_tenant ON llm_skill(tenant_id);
