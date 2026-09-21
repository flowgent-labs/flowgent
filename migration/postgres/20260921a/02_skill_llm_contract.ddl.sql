-- Reusable Skills are namespace-scoped definitions, distinct from runtime
-- kind=skill AgentFlows. Names are unique only inside their namespace.
ALTER TABLE llm_skill DROP CONSTRAINT IF EXISTS llm_skill_name_key;
ALTER TABLE llm_skill ADD COLUMN IF NOT EXISTS version BIGINT NOT NULL DEFAULT 1;
CREATE UNIQUE INDEX IF NOT EXISTS llm_skill_namespace_name_key
    ON llm_skill (LOWER(namespace_id), LOWER(name)) WHERE del_flag=false;

-- Name is the operator-facing identifier; type is the wire protocol adapter.
-- provider remains an alias for compatibility with existing clients.
ALTER TABLE llm_providers ADD COLUMN IF NOT EXISTS name VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE llm_providers ADD COLUMN IF NOT EXISTS type VARCHAR(32) NOT NULL DEFAULT 'openai';
ALTER TABLE llm_providers ADD COLUMN IF NOT EXISTS env JSONB DEFAULT '{}';
UPDATE llm_providers SET name=provider WHERE name='';
UPDATE llm_providers
SET type=CASE
    WHEN LOWER(provider) IN ('anthropic', 'gemini') THEN LOWER(provider)
    ELSE 'openai'
END
WHERE type='' OR type IS NULL;
