-- Flowgent SQLite — MCP definitions and LLM providers tables

CREATE TABLE IF NOT EXISTS mcps (
    name        TEXT PRIMARY KEY,
    enabled     INTEGER NOT NULL DEFAULT 1,
    type        TEXT NOT NULL DEFAULT '',
    command     TEXT DEFAULT '[]',
    args        TEXT DEFAULT '[]',
    env         TEXT DEFAULT '{}',
    tenant_id   TEXT NOT NULL DEFAULT 'default',
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_mcps_tenant ON mcps(tenant_id);

CREATE TABLE IF NOT EXISTS llm_providers (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    provider    TEXT NOT NULL,
    endpoint    TEXT NOT NULL DEFAULT '',
    apikey      TEXT NOT NULL DEFAULT '',
    model       TEXT NOT NULL DEFAULT '',
    models      TEXT DEFAULT '[]',
    timeout_ms  INTEGER DEFAULT 30000,
    tenant_id   TEXT NOT NULL DEFAULT 'default',
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_llmproviders_tenant ON llm_providers(tenant_id);
