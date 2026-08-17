-- Public Namespace and Flow identities are stable URL segments. Namespace
-- names are globally unique; Flow names are unique within one Namespace.
ALTER TABLE iam_namespace
    ADD CONSTRAINT iam_namespace_id_format
    CHECK (id ~ '^[A-Za-z][A-Za-z0-9_-]{0,31}$');

ALTER TABLE iam_namespace
    ADD CONSTRAINT iam_namespace_name_format
    CHECK (name ~ '^[A-Za-z][A-Za-z0-9_-]{0,31}$');

CREATE UNIQUE INDEX iam_namespace_id_ci_key
    ON iam_namespace (LOWER(id));

CREATE UNIQUE INDEX iam_namespace_name_ci_key
    ON iam_namespace (LOWER(name));

CREATE UNIQUE INDEX orh_agentflow_namespace_identity_ci_key
    ON orh_agentflow (LOWER(namespace_id), LOWER(agentflow_id), version);
