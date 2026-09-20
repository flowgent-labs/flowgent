DROP INDEX IF EXISTS orh_flowrun_resource_pool_idx;
ALTER TABLE orh_flowrun ADD COLUMN IF NOT EXISTS runtime_mode VARCHAR(32) NOT NULL DEFAULT 'application';
ALTER TABLE orh_flowrun DROP COLUMN IF EXISTS resource_pool_id;
DROP TABLE IF EXISTS orh_resource_pool;
