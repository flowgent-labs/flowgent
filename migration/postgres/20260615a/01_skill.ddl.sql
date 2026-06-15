-- Flowgent PostgreSQL -- Skill definitions table

CREATE TABLE IF NOT EXISTS llm_skill (
    name        VARCHAR(255) PRIMARY KEY,
    description TEXT NOT NULL DEFAULT '',
    instruction TEXT NOT NULL DEFAULT '',
    model       VARCHAR(255) NOT NULL DEFAULT '',
    temperature DOUBLE PRECISION,
    max_tokens  INTEGER DEFAULT 0,
    tools       JSONB DEFAULT '[]',
    tenant_id   VARCHAR(255) NOT NULL DEFAULT 'default',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_llmskill_tenant ON llm_skill(tenant_id);
