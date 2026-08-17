-- Namespace-scoped enterprise IAM. A Flowgent deployment is the implicit
-- enterprise boundary; namespace_id is the organization/team boundary.

CREATE TABLE IF NOT EXISTS iam_namespace (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    labels       TEXT NOT NULL DEFAULT '{}',
    description  TEXT NOT NULL DEFAULT '',
    namespace_id TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    created_by   TEXT NOT NULL DEFAULT '',
    updated_at   TEXT NOT NULL DEFAULT (datetime('now')),
    updated_by   TEXT NOT NULL DEFAULT '',
    del_flag     INTEGER NOT NULL DEFAULT 0,
    UNIQUE(name)
);

CREATE TABLE IF NOT EXISTS iam_principal (
    id           TEXT PRIMARY KEY,
    type         TEXT NOT NULL,
    issuer       TEXT NOT NULL DEFAULT '',
    external_id  TEXT NOT NULL,
    username     TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    email        TEXT NOT NULL DEFAULT '',
    attributes   TEXT NOT NULL DEFAULT '{}',
    description  TEXT NOT NULL DEFAULT '',
    namespace_id TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    created_by   TEXT NOT NULL DEFAULT '',
    updated_at   TEXT NOT NULL DEFAULT (datetime('now')),
    updated_by   TEXT NOT NULL DEFAULT '',
    del_flag     INTEGER NOT NULL DEFAULT 0,
    UNIQUE(issuer, external_id)
);
CREATE INDEX IF NOT EXISTS idx_iam_principal_username ON iam_principal(username);

CREATE TABLE IF NOT EXISTS iam_group (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    namespace_id TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    created_by   TEXT NOT NULL DEFAULT '',
    updated_at   TEXT NOT NULL DEFAULT (datetime('now')),
    updated_by   TEXT NOT NULL DEFAULT '',
    del_flag     INTEGER NOT NULL DEFAULT 0,
    UNIQUE(namespace_id, name)
);

CREATE TABLE IF NOT EXISTS iam_group_member (
    id           TEXT PRIMARY KEY,
    group_id     TEXT NOT NULL REFERENCES iam_group(id),
    principal_id TEXT NOT NULL REFERENCES iam_principal(id),
    description  TEXT NOT NULL DEFAULT '',
    namespace_id TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    created_by   TEXT NOT NULL DEFAULT '',
    updated_at   TEXT NOT NULL DEFAULT (datetime('now')),
    updated_by   TEXT NOT NULL DEFAULT '',
    del_flag     INTEGER NOT NULL DEFAULT 0,
    UNIQUE(group_id, principal_id)
);
CREATE INDEX IF NOT EXISTS idx_iam_group_member_principal ON iam_group_member(principal_id);

CREATE TABLE IF NOT EXISTS iam_role (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    builtin      INTEGER NOT NULL DEFAULT 0,
    permissions  TEXT NOT NULL DEFAULT '[]',
    description  TEXT NOT NULL DEFAULT '',
    namespace_id TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    created_by   TEXT NOT NULL DEFAULT '',
    updated_at   TEXT NOT NULL DEFAULT (datetime('now')),
    updated_by   TEXT NOT NULL DEFAULT '',
    del_flag     INTEGER NOT NULL DEFAULT 0,
    UNIQUE(namespace_id, name)
);

CREATE TABLE IF NOT EXISTS iam_role_binding (
    id            TEXT PRIMARY KEY,
    role_id       TEXT NOT NULL REFERENCES iam_role(id),
    subject_type  TEXT NOT NULL,
    subject_id    TEXT NOT NULL,
    resource_type TEXT NOT NULL DEFAULT 'namespace',
    resource_id   TEXT NOT NULL DEFAULT '*',
    effect        TEXT NOT NULL DEFAULT 'ALLOW' CHECK (effect IN ('ALLOW', 'DENY')),
    conditions    TEXT NOT NULL DEFAULT '{}',
    expires_at    TEXT,
    description   TEXT NOT NULL DEFAULT '',
    namespace_id  TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    created_by    TEXT NOT NULL DEFAULT '',
    updated_at    TEXT NOT NULL DEFAULT (datetime('now')),
    updated_by    TEXT NOT NULL DEFAULT '',
    del_flag      INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_iam_binding_subject ON iam_role_binding(subject_type, subject_id);
CREATE INDEX IF NOT EXISTS idx_iam_binding_scope ON iam_role_binding(namespace_id, resource_type, resource_id);

CREATE TABLE IF NOT EXISTS iam_api_key (
    id                 TEXT PRIMARY KEY,
    name               TEXT NOT NULL,
    principal_id       TEXT NOT NULL REFERENCES iam_principal(id),
    secret_hash        TEXT NOT NULL,
    prefix             TEXT NOT NULL,
    suffix             TEXT NOT NULL,
    allowed_namespaces TEXT NOT NULL DEFAULT '[]',
    permissions        TEXT NOT NULL DEFAULT '[]',
    expires_at         TEXT,
    last_used_at       TEXT,
    revoked_at         TEXT,
    description        TEXT NOT NULL DEFAULT '',
    namespace_id       TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at         TEXT NOT NULL DEFAULT (datetime('now')),
    created_by         TEXT NOT NULL DEFAULT '',
    updated_at         TEXT NOT NULL DEFAULT (datetime('now')),
    updated_by         TEXT NOT NULL DEFAULT '',
    del_flag           INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_iam_api_key_principal ON iam_api_key(principal_id);

CREATE TABLE IF NOT EXISTS iam_audit_event (
    id            TEXT PRIMARY KEY,
    actor_id       TEXT NOT NULL DEFAULT '',
    actor_type     TEXT NOT NULL DEFAULT 'user',
    action         TEXT NOT NULL,
    resource_type  TEXT NOT NULL,
    resource_id    TEXT NOT NULL DEFAULT '',
    decision       TEXT NOT NULL,
    reason         TEXT NOT NULL DEFAULT '',
    request_id     TEXT NOT NULL DEFAULT '',
    source_ip      TEXT NOT NULL DEFAULT '',
    metadata       TEXT NOT NULL DEFAULT '{}',
    description    TEXT NOT NULL DEFAULT '',
    namespace_id   TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at     TEXT NOT NULL DEFAULT (datetime('now')),
    created_by     TEXT NOT NULL DEFAULT '',
    updated_at     TEXT NOT NULL DEFAULT (datetime('now')),
    updated_by     TEXT NOT NULL DEFAULT '',
    del_flag       INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_iam_audit_time ON iam_audit_event(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_iam_audit_scope ON iam_audit_event(namespace_id, action);
