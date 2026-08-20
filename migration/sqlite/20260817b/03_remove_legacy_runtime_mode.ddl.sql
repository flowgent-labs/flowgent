-- Flow runtime selection lives in the Flow definition JSON and each Run
-- snapshots runtime_mode. These legacy columns came from the initial schema.
ALTER TABLE orh_agentflow DROP COLUMN priority;
ALTER TABLE orh_agentflow DROP COLUMN mode;
ALTER TABLE orh_flowrun DROP COLUMN priority;
