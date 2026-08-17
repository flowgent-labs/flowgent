-- Resource Pools supersede legacy Flow priority/runtime-mode fields. Scheduling
-- configuration lives in orh_resource_pool and every Run snapshots its pool.
ALTER TABLE orh_agentflow DROP COLUMN IF EXISTS priority;
ALTER TABLE orh_agentflow DROP COLUMN IF EXISTS mode;
ALTER TABLE orh_flowrun DROP COLUMN IF EXISTS priority;
