CREATE UNIQUE INDEX orh_agentflow_namespace_identity_ci_key
    ON orh_agentflow (LOWER(namespace_id), LOWER(agentflow_id), version);
