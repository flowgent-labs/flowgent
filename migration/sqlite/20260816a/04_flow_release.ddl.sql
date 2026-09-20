-- Immutable deployment-internal Flow release catalog and consumer installs.
CREATE TABLE IF NOT EXISTS orh_flow_release (
    id              TEXT PRIMARY KEY,
    flow_id         TEXT NOT NULL,
    flow_version    INTEGER NOT NULL,
    release_version TEXT NOT NULL,
    definition      TEXT NOT NULL,
    checksum        TEXT NOT NULL,
    visibility      TEXT NOT NULL CHECK (visibility IN ('PRIVATE','SHARED')),
    published_at    TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    namespace_id    TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    created_by      TEXT NOT NULL DEFAULT '',
    updated_at      TEXT NOT NULL DEFAULT (datetime('now')),
    updated_by      TEXT NOT NULL DEFAULT '',
    del_flag        INTEGER NOT NULL DEFAULT 0,
    UNIQUE(namespace_id, flow_id, release_version),
    UNIQUE(namespace_id, checksum)
);
CREATE INDEX IF NOT EXISTS idx_flow_release_catalog
    ON orh_flow_release(visibility, status, published_at DESC);

CREATE TABLE IF NOT EXISTS orh_flow_release_grant (
    id                 TEXT PRIMARY KEY,
    release_id         TEXT NOT NULL REFERENCES orh_flow_release(id),
    consumer_namespace TEXT NOT NULL,
    expires_at         TEXT,
    description        TEXT NOT NULL DEFAULT '',
    namespace_id       TEXT NOT NULL,
    status             TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at         TEXT NOT NULL DEFAULT (datetime('now')),
    created_by         TEXT NOT NULL DEFAULT '',
    updated_at         TEXT NOT NULL DEFAULT (datetime('now')),
    updated_by         TEXT NOT NULL DEFAULT '',
    del_flag           INTEGER NOT NULL DEFAULT 0,
    UNIQUE(release_id, consumer_namespace)
);
CREATE INDEX IF NOT EXISTS idx_flow_release_grant_consumer
    ON orh_flow_release_grant(consumer_namespace, status);

CREATE TABLE IF NOT EXISTS orh_flow_installation (
    id                   TEXT PRIMARY KEY,
    release_id           TEXT NOT NULL REFERENCES orh_flow_release(id),
    release_version      TEXT NOT NULL,
    producer_namespace   TEXT NOT NULL,
    installed_flow_id    TEXT NOT NULL,
    release_checksum     TEXT NOT NULL,
    applied_checksum     TEXT NOT NULL,
    resource_bindings    TEXT NOT NULL DEFAULT '{}',
    installed_at         TEXT NOT NULL,
    description          TEXT NOT NULL DEFAULT '',
    namespace_id         TEXT NOT NULL,
    status               TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at           TEXT NOT NULL DEFAULT (datetime('now')),
    created_by           TEXT NOT NULL DEFAULT '',
    updated_at           TEXT NOT NULL DEFAULT (datetime('now')),
    updated_by           TEXT NOT NULL DEFAULT '',
    del_flag             INTEGER NOT NULL DEFAULT 0,
    UNIQUE(namespace_id, installed_flow_id)
);
CREATE INDEX IF NOT EXISTS idx_flow_installation_release
    ON orh_flow_installation(release_id, namespace_id);
