-- Flowgent PostgreSQL — MCP definitions and LLM providers tables

CREATE TABLE IF NOT EXISTS mcps (
    name        VARCHAR(255) PRIMARY KEY,
    enabled     BOOLEAN NOT NULL DEFAULT true,
    type        VARCHAR(32) NOT NULL DEFAULT '',
    command     JSONB DEFAULT '[]',
    args        JSONB DEFAULT '[]',
    env         JSONB DEFAULT '{}',
    tenant_id   VARCHAR(255) NOT NULL DEFAULT 'default',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_mcps_tenant ON mcps(tenant_id);

CREATE TABLE IF NOT EXISTS llm_providers (
    id          VARCHAR(64) PRIMARY KEY,
    name        VARCHAR(255) NOT NULL,
    provider    VARCHAR(255) NOT NULL,
    endpoint    VARCHAR(512) NOT NULL DEFAULT '',
    apikey      VARCHAR(512) NOT NULL DEFAULT '',
    model       VARCHAR(255) NOT NULL DEFAULT '',
    models      JSONB DEFAULT '[]',
    timeout_ms  INTEGER DEFAULT 30000,
    tenant_id   VARCHAR(255) NOT NULL DEFAULT 'default',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_llmproviders_tenant ON llm_providers(tenant_id);
