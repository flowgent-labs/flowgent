-- Flowgent PostgreSQL — agents, notification channels, subscription routes, multi-tenant fields

-- ── Agents table (DB-backed agent definitions) ──────────────
CREATE TABLE IF NOT EXISTS agents (
    name          VARCHAR(255) PRIMARY KEY,
    model         VARCHAR(255) NOT NULL,
    soul          TEXT NOT NULL,
    instruction   TEXT NOT NULL,
    output_schema JSONB,
    temperature   DOUBLE PRECISION,
    max_tokens    INTEGER DEFAULT 0,
    tenant_id     VARCHAR(255) NOT NULL DEFAULT 'default',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_agents_tenant ON agents(tenant_id);

-- ── Notification channels ───────────────────────────────────
CREATE TABLE IF NOT EXISTS notification_channels (
    id           VARCHAR(64) PRIMARY KEY,
    name         VARCHAR(255) NOT NULL,
    channel_type VARCHAR(32) NOT NULL,
    config       JSONB NOT NULL,
    enabled      BOOLEAN NOT NULL DEFAULT true,
    tenant_id    VARCHAR(255) NOT NULL DEFAULT 'default',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_notifchannels_tenant ON notification_channels(tenant_id);

-- ── Subscription routes (clustered WS delivery) ─────────────
CREATE TABLE IF NOT EXISTS subscription_routes (
    id            VARCHAR(64) PRIMARY KEY,
    agentflow_id  VARCHAR(255) NOT NULL,
    ws_id         VARCHAR(64) NOT NULL,
    pod_id        VARCHAR(64) NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_subroutes_agentflow ON subscription_routes(agentflow_id);
CREATE INDEX IF NOT EXISTS idx_subroutes_pod ON subscription_routes(pod_id);

-- ── Multi-tenant columns for agentflow definitions ──────────
ALTER TABLE agentflow_definitions ADD COLUMN IF NOT EXISTS priority  VARCHAR(16) DEFAULT 'medium';
ALTER TABLE agentflow_definitions ADD COLUMN IF NOT EXISTS tenant_id VARCHAR(255) DEFAULT 'default';
ALTER TABLE agentflow_definitions ADD COLUMN IF NOT EXISTS namespace VARCHAR(255) DEFAULT '';
ALTER TABLE agentflow_definitions ADD COLUMN IF NOT EXISTS mode      VARCHAR(32) DEFAULT '';
ALTER TABLE agentflow_definitions ADD COLUMN IF NOT EXISTS labels    JSONB DEFAULT '{}';

-- ── Missing columns from original PG migration ──────────
ALTER TABLE agentflow_runs ADD COLUMN IF NOT EXISTS trigger_payload JSONB;

-- ── Multi-tenant columns for agentflow runs ────────────────
ALTER TABLE agentflow_runs ADD COLUMN IF NOT EXISTS tenant_id VARCHAR(255) DEFAULT 'default';
ALTER TABLE agentflow_runs ADD COLUMN IF NOT EXISTS namespace VARCHAR(255) DEFAULT '';
ALTER TABLE agentflow_runs ADD COLUMN IF NOT EXISTS priority  VARCHAR(16) DEFAULT 'medium';

-- ── agentflow_run_id on human_approvals ────────────────────
ALTER TABLE human_approvals ADD COLUMN IF NOT EXISTS agentflow_run_id VARCHAR(64) DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_human_afrun ON human_approvals(agentflow_run_id);
