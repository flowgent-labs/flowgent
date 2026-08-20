CREATE TABLE llm_agent_namespaced (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    model         TEXT NOT NULL,
    soul          TEXT NOT NULL,
    instruction   TEXT NOT NULL,
    output_schema TEXT,
    temperature   REAL,
    max_tokens    INTEGER DEFAULT 0,
    labels        TEXT DEFAULT '{}',
    description   TEXT NOT NULL DEFAULT '',
    namespace_id  TEXT NOT NULL DEFAULT 'default',
    status        TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    created_by    TEXT NOT NULL DEFAULT '',
    updated_at    TEXT NOT NULL DEFAULT (datetime('now')),
    updated_by    TEXT NOT NULL DEFAULT '',
    del_flag      INTEGER NOT NULL DEFAULT 0
);
INSERT INTO llm_agent_namespaced SELECT * FROM llm_agent;
DROP TABLE llm_agent;
ALTER TABLE llm_agent_namespaced RENAME TO llm_agent;
CREATE INDEX idx_llmagent_namespace ON llm_agent(namespace_id);
CREATE UNIQUE INDEX llm_agent_namespace_name_key
    ON llm_agent (LOWER(namespace_id), LOWER(name)) WHERE del_flag=0;

CREATE TABLE llm_mcp_namespaced (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    enabled      INTEGER NOT NULL DEFAULT 1,
    type         TEXT NOT NULL DEFAULT '',
    url          TEXT NOT NULL DEFAULT '',
    headers      TEXT DEFAULT '{}',
    command      TEXT DEFAULT '[]',
    args         TEXT DEFAULT '[]',
    env          TEXT DEFAULT '{}',
    labels       TEXT DEFAULT '{}',
    description  TEXT NOT NULL DEFAULT '',
    namespace_id TEXT NOT NULL DEFAULT 'default',
    status       TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    created_by   TEXT NOT NULL DEFAULT '',
    updated_at   TEXT NOT NULL DEFAULT (datetime('now')),
    updated_by   TEXT NOT NULL DEFAULT '',
    del_flag     INTEGER NOT NULL DEFAULT 0
);
INSERT INTO llm_mcp_namespaced SELECT * FROM llm_mcp;
DROP TABLE llm_mcp;
ALTER TABLE llm_mcp_namespaced RENAME TO llm_mcp;
CREATE INDEX idx_llmmcp_namespace ON llm_mcp(namespace_id);
CREATE UNIQUE INDEX llm_mcp_namespace_name_key
    ON llm_mcp (LOWER(namespace_id), LOWER(name)) WHERE del_flag=0;
