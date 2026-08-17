CREATE TABLE IF NOT EXISTS orh_resource_pool (
    id                    TEXT PRIMARY KEY,
    name                  TEXT NOT NULL,
    replicas              INTEGER NOT NULL DEFAULT 1 CHECK (replicas > 0),
    slots_per_pod         INTEGER NOT NULL DEFAULT 4 CHECK (slots_per_pod > 0),
    resources             TEXT DEFAULT '{}',
    sandbox_replicas      INTEGER NOT NULL DEFAULT 1 CHECK (sandbox_replicas >= 0),
    sandbox_slots_per_pod INTEGER NOT NULL DEFAULT 4 CHECK (sandbox_slots_per_pod > 0),
    sandbox_resources     TEXT DEFAULT '{}',
    priority_class_name   TEXT NOT NULL DEFAULT '',
    node_selector         TEXT DEFAULT '{}',
    description           TEXT NOT NULL DEFAULT '',
    namespace_id          TEXT NOT NULL,
    status                TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at            TEXT NOT NULL DEFAULT (datetime('now')),
    created_by            TEXT NOT NULL DEFAULT '',
    updated_at            TEXT NOT NULL DEFAULT (datetime('now')),
    updated_by            TEXT NOT NULL DEFAULT '',
    del_flag              INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS orh_resource_pool_namespace_name_ci_key
    ON orh_resource_pool (LOWER(namespace_id), LOWER(name)) WHERE del_flag=0;
CREATE INDEX IF NOT EXISTS orh_resource_pool_namespace_idx
    ON orh_resource_pool (namespace_id) WHERE del_flag=0;

ALTER TABLE orh_flowrun ADD COLUMN resource_pool_id TEXT NOT NULL DEFAULT 'default';
CREATE INDEX IF NOT EXISTS orh_flowrun_resource_pool_idx
    ON orh_flowrun (namespace_id, resource_pool_id);

INSERT OR IGNORE INTO orh_resource_pool
    (id,name,replicas,slots_per_pod,sandbox_replicas,sandbox_slots_per_pod,
     description,namespace_id,status,created_by,updated_by)
SELECT 'builtin-default-resource-pool-' || id,'default',1,4,1,4,
       'Default namespace worker capacity',id,'ACTIVE','system','system'
FROM iam_namespace
WHERE del_flag=0;

UPDATE orh_agentflow
SET definition=json_set(definition, '$.resource_pool_id', 'default'),
    updated_at=datetime('now'), updated_by='system'
WHERE namespace_id='default' AND json_extract(definition, '$.resource_pool_id') IS NULL;
