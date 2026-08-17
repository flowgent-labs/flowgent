-- Namespace and Flow runtime configuration. Secret values are AES-GCM
-- envelopes; public APIs expose only configured key names.
CREATE TABLE IF NOT EXISTS orh_runtime_configuration (
    id             TEXT PRIMARY KEY,
    scope_type     TEXT NOT NULL CHECK (scope_type IN ('namespace','flow')),
    scope_id       TEXT NOT NULL,
    environment    TEXT NOT NULL DEFAULT '{}',
    sealed_secrets TEXT NOT NULL DEFAULT 'null',
    secret_keys    TEXT NOT NULL DEFAULT '[]',
    description    TEXT NOT NULL DEFAULT '',
    namespace_id   TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at     TEXT NOT NULL DEFAULT (datetime('now')),
    created_by     TEXT NOT NULL DEFAULT '',
    updated_at     TEXT NOT NULL DEFAULT (datetime('now')),
    updated_by     TEXT NOT NULL DEFAULT '',
    del_flag       INTEGER NOT NULL DEFAULT 0,
    UNIQUE(namespace_id, scope_type, scope_id)
);
CREATE INDEX IF NOT EXISTS idx_runtime_configuration_scope
    ON orh_runtime_configuration(namespace_id, scope_type, scope_id);
