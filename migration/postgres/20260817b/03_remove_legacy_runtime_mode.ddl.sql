-- Flow runtime selection lives in the Flow definition JSON and each Run
-- snapshots runtime_mode. Legacy table-level priority/mode columns are removed.
ALTER TABLE orh_agentflow DROP COLUMN IF EXISTS priority;
ALTER TABLE orh_agentflow DROP COLUMN IF EXISTS mode;
ALTER TABLE orh_flowrun DROP COLUMN IF EXISTS priority;
