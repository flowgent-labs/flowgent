-- SQLite cannot add CHECK constraints in place. API/store validation owns the
-- format rule; these indexes make case-insensitive uniqueness race-safe.
CREATE UNIQUE INDEX iam_namespace_id_ci_key
    ON iam_namespace (LOWER(id));

CREATE UNIQUE INDEX iam_namespace_name_ci_key
    ON iam_namespace (LOWER(name));

CREATE UNIQUE INDEX orh_agentflow_namespace_identity_ci_key
    ON orh_agentflow (LOWER(namespace_id), LOWER(agentflow_id), version);
