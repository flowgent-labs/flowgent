-- Namespace-scoped worker capacity. A Flow stores the public pool name while
-- the table keeps an internal UUID identity so names may repeat across tenants.
CREATE TABLE IF NOT EXISTS orh_resource_pool (
    id                    VARCHAR(64) PRIMARY KEY,
    name                  VARCHAR(32) NOT NULL,
    replicas              INTEGER NOT NULL DEFAULT 1 CHECK (replicas > 0),
    slots_per_pod         INTEGER NOT NULL DEFAULT 4 CHECK (slots_per_pod > 0),
    resources             JSONB DEFAULT '{}',
    sandbox_replicas      INTEGER NOT NULL DEFAULT 1 CHECK (sandbox_replicas >= 0),
    sandbox_slots_per_pod INTEGER NOT NULL DEFAULT 4 CHECK (sandbox_slots_per_pod > 0),
    sandbox_resources     JSONB DEFAULT '{}',
    priority_class_name   VARCHAR(253) NOT NULL DEFAULT '',
    node_selector         JSONB DEFAULT '{}',
    description           TEXT NOT NULL DEFAULT '',
    namespace_id          VARCHAR(32) NOT NULL,
    status                VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by            VARCHAR(255) NOT NULL DEFAULT '',
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by            VARCHAR(255) NOT NULL DEFAULT '',
    del_flag              BOOLEAN NOT NULL DEFAULT false,
    CHECK (name ~ '^[A-Za-z][A-Za-z0-9_-]{0,31}$')
);
CREATE UNIQUE INDEX IF NOT EXISTS orh_resource_pool_namespace_name_ci_key
    ON orh_resource_pool (LOWER(namespace_id), LOWER(name)) WHERE del_flag=false;
CREATE INDEX IF NOT EXISTS orh_resource_pool_namespace_idx
    ON orh_resource_pool (namespace_id) WHERE del_flag=false;

ALTER TABLE orh_flowrun ADD COLUMN IF NOT EXISTS resource_pool_id VARCHAR(32) NOT NULL DEFAULT 'default';
CREATE INDEX IF NOT EXISTS orh_flowrun_resource_pool_idx
    ON orh_flowrun (namespace_id, resource_pool_id);

INSERT INTO orh_resource_pool
    (id,name,replicas,slots_per_pod,sandbox_replicas,sandbox_slots_per_pod,
     description,namespace_id,status,created_by,updated_by)
SELECT 'builtin-default-resource-pool-' || id,'default',1,4,1,4,
       'Default namespace worker capacity',id,'ACTIVE','system','system'
FROM iam_namespace
WHERE del_flag=false
ON CONFLICT (id) DO NOTHING;

UPDATE orh_agentflow
SET definition=jsonb_set(definition, '{resource_pool_id}', '"default"'::jsonb, true),
    updated_at=NOW(), updated_by='system'
WHERE namespace_id='default' AND NOT definition ? 'resource_pool_id';
