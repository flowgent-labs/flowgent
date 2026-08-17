-- Namespace and Flow runtime configuration. Secret values are AES-GCM
-- envelopes; public APIs expose only configured key names.
CREATE TABLE IF NOT EXISTS orh_runtime_configuration (
    id             VARCHAR(64) PRIMARY KEY,
    scope_type     VARCHAR(16) NOT NULL CHECK (scope_type IN ('namespace','flow')),
    scope_id       VARCHAR(64) NOT NULL,
    environment    JSONB NOT NULL DEFAULT '{}',
    sealed_secrets JSONB NOT NULL DEFAULT 'null',
    secret_keys    JSONB NOT NULL DEFAULT '[]',
    description    TEXT NOT NULL DEFAULT '',
    namespace_id   VARCHAR(64) NOT NULL,
    status         VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by     VARCHAR(255) NOT NULL DEFAULT '',
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by     VARCHAR(255) NOT NULL DEFAULT '',
    del_flag       BOOLEAN NOT NULL DEFAULT false,
    UNIQUE(namespace_id, scope_type, scope_id)
);
CREATE INDEX IF NOT EXISTS idx_runtime_configuration_scope
    ON orh_runtime_configuration(namespace_id, scope_type, scope_id);
