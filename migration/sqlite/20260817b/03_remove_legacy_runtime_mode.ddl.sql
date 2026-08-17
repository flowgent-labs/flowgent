-- Resource Pools supersede legacy Flow priority/runtime-mode fields. These
-- columns are present in the initial schema for upgrade compatibility only.
ALTER TABLE orh_agentflow DROP COLUMN priority;
ALTER TABLE orh_agentflow DROP COLUMN mode;
ALTER TABLE orh_flowrun DROP COLUMN priority;
