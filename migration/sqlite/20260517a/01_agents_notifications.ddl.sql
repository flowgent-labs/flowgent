-- Flowgent SQLite — agents, notification channels, subscription routes, multi-tenant fields

-- ── Agents table (DB-backed agent definitions) ──────────────
CREATE TABLE IF NOT EXISTS agents (
    name          TEXT PRIMARY KEY,
    model         TEXT NOT NULL,
    soul          TEXT NOT NULL,
    instruction   TEXT NOT NULL,
    output_schema TEXT,
    temperature   REAL,
    max_tokens    INTEGER DEFAULT 0,
    tenant_id     TEXT NOT NULL DEFAULT 'default',
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_agents_tenant ON agents(tenant_id);

-- ── Notification channels ───────────────────────────────────
CREATE TABLE IF NOT EXISTS notification_channels (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    channel_type TEXT NOT NULL,
    config      TEXT NOT NULL,
    enabled     INTEGER NOT NULL DEFAULT 1,
    tenant_id   TEXT NOT NULL DEFAULT 'default',
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_notifchannels_tenant ON notification_channels(tenant_id);

-- ── Subscription routes (clustered WS delivery) ─────────────
CREATE TABLE IF NOT EXISTS subscription_routes (
    id            TEXT PRIMARY KEY,
    agentflow_id  TEXT NOT NULL,
    ws_id         TEXT NOT NULL,
    pod_id        TEXT NOT NULL,
    created_at    TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_subroutes_agentflow ON subscription_routes(agentflow_id);
CREATE INDEX IF NOT EXISTS idx_subroutes_pod ON subscription_routes(pod_id);

-- ── Multi-tenant columns for agentflow definitions ──────────
ALTER TABLE agentflow_definitions ADD COLUMN priority  TEXT DEFAULT 'medium';
ALTER TABLE agentflow_definitions ADD COLUMN tenant_id TEXT DEFAULT 'default';
ALTER TABLE agentflow_definitions ADD COLUMN namespace TEXT DEFAULT '';
ALTER TABLE agentflow_definitions ADD COLUMN mode      TEXT DEFAULT '';
ALTER TABLE agentflow_definitions ADD COLUMN labels    TEXT DEFAULT '{}';

-- ── Multi-tenant columns for agentflow runs ────────────────
ALTER TABLE agentflow_runs ADD COLUMN tenant_id TEXT DEFAULT 'default';
ALTER TABLE agentflow_runs ADD COLUMN namespace TEXT DEFAULT '';
ALTER TABLE agentflow_runs ADD COLUMN priority  TEXT DEFAULT 'medium';

-- ── agentflow_run_id on human_approvals (for efficient lookup) ─
ALTER TABLE human_approvals ADD COLUMN agentflow_run_id TEXT DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_human_afrun ON human_approvals(agentflow_run_id);
