ALTER TABLE llm_agent DROP CONSTRAINT IF EXISTS llm_agent_name_key;
ALTER TABLE llm_mcp DROP CONSTRAINT IF EXISTS llm_mcp_name_key;

CREATE UNIQUE INDEX llm_agent_namespace_name_key
    ON llm_agent (LOWER(namespace_id), LOWER(name)) WHERE del_flag=false;

CREATE UNIQUE INDEX llm_mcp_namespace_name_key
    ON llm_mcp (LOWER(namespace_id), LOWER(name)) WHERE del_flag=false;
