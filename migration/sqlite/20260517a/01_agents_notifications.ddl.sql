-- Flowgent SQLite -- agents, notification channels, subscription routes, multi-tenant fields

-- -- Agents table (DB-backed agent definitions) ------------------------------
CREATE TABLE IF NOT EXISTS llm_agent (
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
CREATE INDEX IF NOT EXISTS idx_llmagent_tenant ON llm_agent(tenant_id);

-- -- Notification channels --------------------------------------------------
CREATE TABLE IF NOT EXISTS nfy_channel (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    channel_type TEXT NOT NULL,
    config      TEXT NOT NULL,
    enabled     INTEGER NOT NULL DEFAULT 1,
    tenant_id   TEXT NOT NULL DEFAULT 'default',
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_nfychannel_tenant ON nfy_channel(tenant_id);

-- -- Subscription routes (clustered WS delivery) ----------------------------
CREATE TABLE IF NOT EXISTS subscription_routes (
    id            TEXT PRIMARY KEY,
    agentflow_id  TEXT NOT NULL,
    ws_id         TEXT NOT NULL,
    pod_id        TEXT NOT NULL,
    created_at    TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_subroutes_agentflow ON subscription_routes(agentflow_id);
CREATE INDEX IF NOT EXISTS idx_subroutes_pod ON subscription_routes(pod_id);

-- -- Multi-tenant columns for agentflow definitions -------------------------
ALTER TABLE orh_agentflow ADD COLUMN priority  TEXT DEFAULT 'medium';
ALTER TABLE orh_agentflow ADD COLUMN tenant_id TEXT DEFAULT 'default';
ALTER TABLE orh_agentflow ADD COLUMN namespace TEXT DEFAULT '';
ALTER TABLE orh_agentflow ADD COLUMN mode      TEXT DEFAULT '';
ALTER TABLE orh_agentflow ADD COLUMN labels    TEXT DEFAULT '{}';

-- -- Multi-tenant columns for agentflow runs -------------------------------
ALTER TABLE orh_flowrun ADD COLUMN tenant_id TEXT DEFAULT 'default';
ALTER TABLE orh_flowrun ADD COLUMN namespace TEXT DEFAULT '';
ALTER TABLE orh_flowrun ADD COLUMN priority  TEXT DEFAULT 'medium';

-- -- agentflow_run_id on human_approvals (for efficient lookup) -------------
ALTER TABLE human_approvals ADD COLUMN agentflow_run_id TEXT DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_human_orhflowrun ON human_approvals(agentflow_run_id);
