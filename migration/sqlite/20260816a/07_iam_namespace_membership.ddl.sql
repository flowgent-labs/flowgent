-- Explicit GitHub-organization-style namespace membership. Principals remain
-- deployment identities and can independently belong to multiple namespaces.
CREATE TABLE IF NOT EXISTS iam_namespace_member (
    id           TEXT PRIMARY KEY,
    principal_id TEXT NOT NULL REFERENCES iam_principal(id),
    membership   TEXT NOT NULL DEFAULT 'MEMBER' CHECK (membership IN ('MEMBER','OUTSIDE_COLLABORATOR')),
    description  TEXT NOT NULL DEFAULT '',
    namespace_id TEXT NOT NULL REFERENCES iam_namespace(id),
    status       TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    created_by   TEXT NOT NULL DEFAULT '',
    updated_at   TEXT NOT NULL DEFAULT (datetime('now')),
    updated_by   TEXT NOT NULL DEFAULT '',
    del_flag     INTEGER NOT NULL DEFAULT 0,
    UNIQUE(namespace_id, principal_id)
);
CREATE INDEX IF NOT EXISTS idx_iam_namespace_member_principal
    ON iam_namespace_member(principal_id, namespace_id);

INSERT OR IGNORE INTO iam_namespace_member
    (id, principal_id, membership, description, namespace_id, status, created_at, created_by, updated_at, updated_by, del_flag)
SELECT 'member-' || lower(hex(randomblob(16))), p.id, 'MEMBER', 'Backfilled namespace membership', b.namespace_id,
       'ACTIVE', datetime('now'), 'system', datetime('now'), 'system', 0
FROM iam_role_binding b JOIN iam_principal p ON p.id=b.subject_id
WHERE b.del_flag=0 AND b.status='ACTIVE' AND b.namespace_id<>'*'
  AND b.subject_type IN ('user','service_account');

INSERT OR IGNORE INTO iam_namespace_member
    (id, principal_id, membership, description, namespace_id, status, created_at, created_by, updated_at, updated_by, del_flag)
SELECT 'member-' || lower(hex(randomblob(16))), p.id, 'MEMBER', 'Backfilled group membership', m.namespace_id,
       'ACTIVE', datetime('now'), 'system', datetime('now'), 'system', 0
FROM iam_group_member m JOIN iam_principal p ON p.id=m.principal_id
WHERE m.del_flag=0 AND m.status='ACTIVE';

INSERT OR IGNORE INTO iam_namespace_member
    (id, principal_id, membership, description, namespace_id, status, created_at, created_by, updated_at, updated_by, del_flag)
VALUES ('member-default-breakglass', 'breakglass:root', 'MEMBER', 'Default namespace bootstrap owner', 'default',
        'ACTIVE', datetime('now'), 'system', datetime('now'), 'system', 0);
