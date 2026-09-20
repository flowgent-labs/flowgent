-- Immutable deployment-internal Flow release catalog and consumer installs.
CREATE TABLE IF NOT EXISTS orh_flow_release (
    id                 VARCHAR(64) PRIMARY KEY,
    flow_id            VARCHAR(255) NOT NULL,
    flow_version       BIGINT NOT NULL,
    release_version    VARCHAR(128) NOT NULL,
    definition         JSONB NOT NULL,
    checksum           VARCHAR(64) NOT NULL,
    visibility         VARCHAR(16) NOT NULL CHECK (visibility IN ('PRIVATE','SHARED')),
    published_at       TIMESTAMPTZ NOT NULL,
    description        TEXT NOT NULL DEFAULT '',
    namespace_id       VARCHAR(255) NOT NULL,
    status             VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by         VARCHAR(255) NOT NULL DEFAULT '',
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by         VARCHAR(255) NOT NULL DEFAULT '',
    del_flag           BOOLEAN NOT NULL DEFAULT false,
    UNIQUE(namespace_id, flow_id, release_version),
    UNIQUE(namespace_id, checksum)
);
CREATE INDEX IF NOT EXISTS idx_flow_release_catalog
    ON orh_flow_release(visibility, status, published_at DESC);

CREATE TABLE IF NOT EXISTS orh_flow_release_grant (
    id                    VARCHAR(64) PRIMARY KEY,
    release_id            VARCHAR(64) NOT NULL REFERENCES orh_flow_release(id),
    consumer_namespace    VARCHAR(255) NOT NULL,
    expires_at            TIMESTAMPTZ,
    description           TEXT NOT NULL DEFAULT '',
    namespace_id          VARCHAR(255) NOT NULL,
    status                VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by            VARCHAR(255) NOT NULL DEFAULT '',
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by            VARCHAR(255) NOT NULL DEFAULT '',
    del_flag              BOOLEAN NOT NULL DEFAULT false,
    UNIQUE(release_id, consumer_namespace)
);
CREATE INDEX IF NOT EXISTS idx_flow_release_grant_consumer
    ON orh_flow_release_grant(consumer_namespace, status);

CREATE TABLE IF NOT EXISTS orh_flow_installation (
    id                    VARCHAR(64) PRIMARY KEY,
    release_id            VARCHAR(64) NOT NULL REFERENCES orh_flow_release(id),
    release_version       VARCHAR(128) NOT NULL,
    producer_namespace    VARCHAR(255) NOT NULL,
    installed_flow_id     VARCHAR(255) NOT NULL,
    release_checksum      VARCHAR(64) NOT NULL,
    applied_checksum      VARCHAR(64) NOT NULL,
    resource_bindings     JSONB NOT NULL DEFAULT '{}',
    installed_at          TIMESTAMPTZ NOT NULL,
    description           TEXT NOT NULL DEFAULT '',
    namespace_id          VARCHAR(255) NOT NULL,
    status                VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by            VARCHAR(255) NOT NULL DEFAULT '',
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by            VARCHAR(255) NOT NULL DEFAULT '',
    del_flag              BOOLEAN NOT NULL DEFAULT false,
    UNIQUE(namespace_id, installed_flow_id)
);
CREATE INDEX IF NOT EXISTS idx_flow_installation_release
    ON orh_flow_installation(release_id, namespace_id);
