DROP TRIGGER IF EXISTS iam_namespace_default_resource_pool;
DROP INDEX IF EXISTS orh_flowrun_resource_pool_idx;
ALTER TABLE orh_flowrun ADD COLUMN runtime_mode TEXT NOT NULL DEFAULT 'application';
DROP TABLE IF EXISTS orh_resource_pool;
