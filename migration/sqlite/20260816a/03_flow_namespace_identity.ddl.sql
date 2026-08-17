-- Flow IDs are namespaced identities. Rebuild the table because SQLite cannot
-- replace the legacy UNIQUE(agentflow_id, version) constraint in place.
CREATE TABLE orh_agentflow_namespace_v2 (
    id           TEXT PRIMARY KEY,
    agentflow_id TEXT    NOT NULL,
    version      INTEGER NOT NULL DEFAULT 1,
    definition   TEXT    NOT NULL,
    checksum     TEXT,
    comment      TEXT,
    priority     TEXT    DEFAULT 'medium',
    namespace    TEXT    DEFAULT '',
    mode         TEXT    DEFAULT '',
    labels       TEXT    DEFAULT '{}',
    description  TEXT    NOT NULL DEFAULT '',
    namespace_id TEXT    NOT NULL DEFAULT 'default',
    status       TEXT    NOT NULL DEFAULT 'ACTIVE',
    created_at   TEXT    NOT NULL DEFAULT (datetime('now')),
    created_by   TEXT    NOT NULL DEFAULT '',
    updated_at   TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_by   TEXT    NOT NULL DEFAULT '',
    del_flag     INTEGER NOT NULL DEFAULT 0,
    UNIQUE(namespace_id, agentflow_id, version)
);
INSERT INTO orh_agentflow_namespace_v2
SELECT id,agentflow_id,version,definition,checksum,comment,priority,namespace,mode,labels,
       description,namespace_id,status,created_at,created_by,updated_at,updated_by,del_flag
FROM orh_agentflow;
DROP TABLE orh_agentflow;
ALTER TABLE orh_agentflow_namespace_v2 RENAME TO orh_agentflow;
CREATE INDEX idx_orhagentflow_namespace ON orh_agentflow(namespace_id);
CREATE INDEX idx_orhagentflow_identity ON orh_agentflow(namespace_id, agentflow_id, version DESC);
