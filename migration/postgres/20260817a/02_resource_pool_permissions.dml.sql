UPDATE iam_role SET permissions=(permissions::jsonb || '["resource_pool.read"]'::jsonb)
 WHERE id IN ('builtin-reader','builtin-writer','builtin-operator','builtin-auditor','builtin-secret-manager')
 AND NOT permissions::jsonb ? 'resource_pool.read';
UPDATE iam_role SET permissions=(permissions::jsonb || '["resource_pool.use"]'::jsonb)
 WHERE id IN ('builtin-writer','builtin-operator') AND NOT permissions::jsonb ? 'resource_pool.use';
UPDATE iam_role SET permissions=(permissions::jsonb || '["resource_pool.read","resource_pool.use","resource_pool.manage"]'::jsonb)
 WHERE id='builtin-namespace-owner' AND NOT permissions::jsonb ? 'resource_pool.manage';
UPDATE iam_role SET permissions=(permissions::jsonb || '["resource_pool.read","resource_pool.use"]'::jsonb)
 WHERE id IN ('builtin-system-controller','builtin-system-jobmanager','builtin-system-taskmanager','builtin-system-allinone')
 AND NOT permissions::jsonb ? 'resource_pool.use';

-- Maintainers manage capacity but do not gain secret or IAM administration.
UPDATE iam_role SET permissions=(permissions::jsonb || '["resource_pool.read","resource_pool.use","resource_pool.manage"]'::jsonb)
 WHERE id='builtin-maintainer' AND NOT permissions::jsonb ? 'resource_pool.manage';
