-- Flow IDs are scoped by namespace (the Organization/team boundary).
ALTER TABLE orh_agentflow DROP CONSTRAINT IF EXISTS orh_agentflow_agentflow_id_version_key;
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'orh_agentflow'::regclass
          AND conname = 'orh_agentflow_namespace_identity_key'
    ) THEN
        ALTER TABLE orh_agentflow
            ADD CONSTRAINT orh_agentflow_namespace_identity_key UNIQUE(namespace_id, agentflow_id, version);
    END IF;
END $$;
CREATE INDEX IF NOT EXISTS idx_orhagentflow_identity ON orh_agentflow(namespace_id, agentflow_id, version DESC);
