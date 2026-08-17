-- Namespace-scoped enterprise IAM. A Flowgent deployment is the implicit
-- enterprise boundary; namespace_id is the organization/team boundary.

CREATE TABLE IF NOT EXISTS iam_namespace (
    id           VARCHAR(63) PRIMARY KEY,
    name         VARCHAR(255) NOT NULL,
    labels       JSONB NOT NULL DEFAULT '{}',
    description  TEXT NOT NULL DEFAULT '',
    namespace_id VARCHAR(63) NOT NULL,
    status       VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by   VARCHAR(255) NOT NULL DEFAULT '',
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by   VARCHAR(255) NOT NULL DEFAULT '',
    del_flag     BOOLEAN NOT NULL DEFAULT false,
    UNIQUE(name)
);

CREATE TABLE IF NOT EXISTS iam_principal (
    id           VARCHAR(64) PRIMARY KEY,
    type         VARCHAR(32) NOT NULL,
    issuer       VARCHAR(255) NOT NULL DEFAULT '',
    external_id  VARCHAR(255) NOT NULL,
    username     VARCHAR(255) NOT NULL DEFAULT '',
    display_name VARCHAR(255) NOT NULL DEFAULT '',
    email        VARCHAR(320) NOT NULL DEFAULT '',
    attributes   JSONB NOT NULL DEFAULT '{}',
    description  TEXT NOT NULL DEFAULT '',
    namespace_id VARCHAR(255) NOT NULL DEFAULT '',
    status       VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by   VARCHAR(255) NOT NULL DEFAULT '',
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by   VARCHAR(255) NOT NULL DEFAULT '',
    del_flag     BOOLEAN NOT NULL DEFAULT false,
    UNIQUE(issuer, external_id)
);
CREATE INDEX IF NOT EXISTS idx_iam_principal_username ON iam_principal(username);

CREATE TABLE IF NOT EXISTS iam_group (
    id           VARCHAR(64) PRIMARY KEY,
    name         VARCHAR(255) NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    namespace_id VARCHAR(255) NOT NULL,
    status       VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by   VARCHAR(255) NOT NULL DEFAULT '',
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by   VARCHAR(255) NOT NULL DEFAULT '',
    del_flag     BOOLEAN NOT NULL DEFAULT false,
    UNIQUE(namespace_id, name)
);

CREATE TABLE IF NOT EXISTS iam_group_member (
    id           VARCHAR(64) PRIMARY KEY,
    group_id     VARCHAR(64) NOT NULL REFERENCES iam_group(id),
    principal_id VARCHAR(64) NOT NULL REFERENCES iam_principal(id),
    description  TEXT NOT NULL DEFAULT '',
    namespace_id VARCHAR(255) NOT NULL,
    status       VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by   VARCHAR(255) NOT NULL DEFAULT '',
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by   VARCHAR(255) NOT NULL DEFAULT '',
    del_flag     BOOLEAN NOT NULL DEFAULT false,
    UNIQUE(group_id, principal_id)
);
CREATE INDEX IF NOT EXISTS idx_iam_group_member_principal ON iam_group_member(principal_id);

CREATE TABLE IF NOT EXISTS iam_role (
    id           VARCHAR(64) PRIMARY KEY,
    name         VARCHAR(255) NOT NULL,
    builtin      BOOLEAN NOT NULL DEFAULT false,
    permissions  JSONB NOT NULL DEFAULT '[]',
    description  TEXT NOT NULL DEFAULT '',
    namespace_id VARCHAR(255) NOT NULL,
    status       VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by   VARCHAR(255) NOT NULL DEFAULT '',
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by   VARCHAR(255) NOT NULL DEFAULT '',
    del_flag     BOOLEAN NOT NULL DEFAULT false,
    UNIQUE(namespace_id, name)
);

CREATE TABLE IF NOT EXISTS iam_role_binding (
    id            VARCHAR(64) PRIMARY KEY,
    role_id       VARCHAR(64) NOT NULL REFERENCES iam_role(id),
    subject_type  VARCHAR(32) NOT NULL,
    subject_id    VARCHAR(255) NOT NULL,
    resource_type VARCHAR(64) NOT NULL DEFAULT 'namespace',
    resource_id   VARCHAR(255) NOT NULL DEFAULT '*',
    effect        VARCHAR(8) NOT NULL DEFAULT 'ALLOW',
    conditions    JSONB NOT NULL DEFAULT '{}',
    expires_at    TIMESTAMPTZ,
    description   TEXT NOT NULL DEFAULT '',
    namespace_id  VARCHAR(255) NOT NULL,
    status        VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by    VARCHAR(255) NOT NULL DEFAULT '',
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by    VARCHAR(255) NOT NULL DEFAULT '',
    del_flag      BOOLEAN NOT NULL DEFAULT false,
    CHECK (effect IN ('ALLOW', 'DENY'))
);
CREATE INDEX IF NOT EXISTS idx_iam_binding_subject ON iam_role_binding(subject_type, subject_id);
CREATE INDEX IF NOT EXISTS idx_iam_binding_scope ON iam_role_binding(namespace_id, resource_type, resource_id);

CREATE TABLE IF NOT EXISTS iam_api_key (
    id                 VARCHAR(64) PRIMARY KEY,
    name               VARCHAR(255) NOT NULL,
    principal_id       VARCHAR(64) NOT NULL REFERENCES iam_principal(id),
    secret_hash        VARCHAR(128) NOT NULL,
    prefix             VARCHAR(32) NOT NULL,
    suffix             VARCHAR(16) NOT NULL,
    allowed_namespaces JSONB NOT NULL DEFAULT '[]',
    permissions        JSONB NOT NULL DEFAULT '[]',
    expires_at         TIMESTAMPTZ,
    last_used_at       TIMESTAMPTZ,
    revoked_at         TIMESTAMPTZ,
    description        TEXT NOT NULL DEFAULT '',
    namespace_id       VARCHAR(255) NOT NULL DEFAULT '',
    status             VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by         VARCHAR(255) NOT NULL DEFAULT '',
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by         VARCHAR(255) NOT NULL DEFAULT '',
    del_flag           BOOLEAN NOT NULL DEFAULT false
);
CREATE INDEX IF NOT EXISTS idx_iam_api_key_principal ON iam_api_key(principal_id);

CREATE TABLE IF NOT EXISTS iam_audit_event (
    id            VARCHAR(64) PRIMARY KEY,
    actor_id       VARCHAR(255) NOT NULL DEFAULT '',
    actor_type     VARCHAR(32) NOT NULL DEFAULT 'user',
    action         VARCHAR(255) NOT NULL,
    resource_type  VARCHAR(64) NOT NULL,
    resource_id    VARCHAR(255) NOT NULL DEFAULT '',
    decision       VARCHAR(16) NOT NULL,
    reason         VARCHAR(255) NOT NULL DEFAULT '',
    request_id     VARCHAR(128) NOT NULL DEFAULT '',
    source_ip      VARCHAR(128) NOT NULL DEFAULT '',
    metadata       JSONB NOT NULL DEFAULT '{}',
    description    TEXT NOT NULL DEFAULT '',
    namespace_id   VARCHAR(255) NOT NULL DEFAULT '',
    status         VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by     VARCHAR(255) NOT NULL DEFAULT '',
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by     VARCHAR(255) NOT NULL DEFAULT '',
    del_flag       BOOLEAN NOT NULL DEFAULT false
);
CREATE INDEX IF NOT EXISTS idx_iam_audit_time ON iam_audit_event(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_iam_audit_scope ON iam_audit_event(namespace_id, action);
