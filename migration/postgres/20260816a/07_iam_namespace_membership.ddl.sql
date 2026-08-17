-- Explicit GitHub-organization-style namespace membership. Principals remain
-- deployment identities and can independently belong to multiple namespaces.
CREATE TABLE IF NOT EXISTS iam_namespace_member (
    id           VARCHAR(64) PRIMARY KEY,
    principal_id VARCHAR(64) NOT NULL REFERENCES iam_principal(id),
    membership   VARCHAR(32) NOT NULL DEFAULT 'MEMBER',
    description  TEXT NOT NULL DEFAULT '',
    namespace_id VARCHAR(63) NOT NULL REFERENCES iam_namespace(id),
    status       VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by   VARCHAR(255) NOT NULL DEFAULT '',
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by   VARCHAR(255) NOT NULL DEFAULT '',
    del_flag     BOOLEAN NOT NULL DEFAULT false,
    UNIQUE(namespace_id, principal_id),
    CHECK (membership IN ('MEMBER','OUTSIDE_COLLABORATOR'))
);
CREATE INDEX IF NOT EXISTS idx_iam_namespace_member_principal
    ON iam_namespace_member(principal_id, namespace_id);

INSERT INTO iam_namespace_member
    (id, principal_id, membership, description, namespace_id, status, created_at, created_by, updated_at, updated_by, del_flag)
SELECT 'member-' || md5(random()::text || clock_timestamp()::text || p.id || b.namespace_id),
       p.id, 'MEMBER', 'Backfilled namespace membership', b.namespace_id,
       'ACTIVE', NOW(), 'system', NOW(), 'system', false
FROM iam_role_binding b JOIN iam_principal p ON p.id=b.subject_id
WHERE b.del_flag=false AND b.status='ACTIVE' AND b.namespace_id<>'*'
  AND b.subject_type IN ('user','service_account')
ON CONFLICT (namespace_id, principal_id) DO NOTHING;

INSERT INTO iam_namespace_member
    (id, principal_id, membership, description, namespace_id, status, created_at, created_by, updated_at, updated_by, del_flag)
SELECT 'member-' || md5(random()::text || clock_timestamp()::text || p.id || m.namespace_id),
       p.id, 'MEMBER', 'Backfilled group membership', m.namespace_id,
       'ACTIVE', NOW(), 'system', NOW(), 'system', false
FROM iam_group_member m JOIN iam_principal p ON p.id=m.principal_id
WHERE m.del_flag=false AND m.status='ACTIVE'
ON CONFLICT (namespace_id, principal_id) DO NOTHING;

INSERT INTO iam_namespace_member
    (id, principal_id, membership, description, namespace_id, status, created_at, created_by, updated_at, updated_by, del_flag)
VALUES ('member-default-breakglass', 'breakglass:root', 'MEMBER', 'Default namespace bootstrap owner', 'default',
        'ACTIVE', NOW(), 'system', NOW(), 'system', false)
ON CONFLICT (namespace_id, principal_id) DO NOTHING;
