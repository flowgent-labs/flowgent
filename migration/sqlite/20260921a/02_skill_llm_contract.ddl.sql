-- SQLite needs a table rebuild to replace the legacy global UNIQUE(name)
-- constraint with a namespace-aware partial unique index.
CREATE TABLE llm_skill_namespaced (
    id           TEXT PRIMARY KEY,
    name         TEXT    NOT NULL,
    instruction  TEXT    NOT NULL DEFAULT '',
    model        TEXT    NOT NULL DEFAULT '',
    temperature  REAL,
    max_tokens   INTEGER DEFAULT 0,
    tools        TEXT    DEFAULT '[]',
    version      INTEGER NOT NULL DEFAULT 1,
    description  TEXT    NOT NULL DEFAULT '',
    namespace_id TEXT    NOT NULL DEFAULT 'default',
    status       TEXT    NOT NULL DEFAULT 'ACTIVE',
    created_at   TEXT    NOT NULL DEFAULT (datetime('now')),
    created_by   TEXT    NOT NULL DEFAULT '',
    updated_at   TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_by   TEXT    NOT NULL DEFAULT '',
    del_flag     INTEGER NOT NULL DEFAULT 0
);
INSERT INTO llm_skill_namespaced (
    id, name, instruction, model, temperature, max_tokens, tools, version,
    description, namespace_id, status, created_at, created_by, updated_at,
    updated_by, del_flag
)
SELECT id, name, instruction, model, temperature, max_tokens, tools, 1,
       description, namespace_id, status, created_at, created_by, updated_at,
       updated_by, del_flag
FROM llm_skill;
DROP TABLE llm_skill;
ALTER TABLE llm_skill_namespaced RENAME TO llm_skill;
CREATE INDEX idx_llmskill_namespace ON llm_skill(namespace_id);
CREATE UNIQUE INDEX llm_skill_namespace_name_key
    ON llm_skill (LOWER(namespace_id), LOWER(name)) WHERE del_flag=0;

ALTER TABLE llm_providers ADD COLUMN name TEXT NOT NULL DEFAULT '';
ALTER TABLE llm_providers ADD COLUMN type TEXT NOT NULL DEFAULT 'openai';
ALTER TABLE llm_providers ADD COLUMN env TEXT DEFAULT '{}';
UPDATE llm_providers SET name=provider WHERE name='';
UPDATE llm_providers
SET type=CASE
    WHEN LOWER(provider) IN ('anthropic', 'gemini') THEN LOWER(provider)
    ELSE 'openai'
END
WHERE type='' OR type IS NULL;
