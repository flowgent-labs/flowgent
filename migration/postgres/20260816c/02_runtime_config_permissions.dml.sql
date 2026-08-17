-- Evolve built-in roles for independently controlled environment, write-only
-- secret, and controller-only resolved runtime configuration APIs.
UPDATE iam_role SET permissions=(permissions::jsonb || '["namespace.config.read"]'::jsonb)
 WHERE id IN ('builtin-reader','builtin-maintainer','builtin-writer','builtin-operator','builtin-secret-manager')
 AND NOT permissions::jsonb ? 'namespace.config.read';
UPDATE iam_role SET permissions=(permissions::jsonb || '["namespace.config.manage"]'::jsonb)
 WHERE id='builtin-maintainer' AND NOT permissions::jsonb ? 'namespace.config.manage';
UPDATE iam_role SET permissions=(permissions::jsonb || '["namespace.secret.manage"]'::jsonb)
 WHERE id='builtin-secret-manager' AND NOT permissions::jsonb ? 'namespace.secret.manage';
UPDATE iam_role SET permissions=(permissions::jsonb || '["flow.config.read"]'::jsonb)
 WHERE id IN ('builtin-reader','builtin-maintainer','builtin-writer','builtin-operator','builtin-secret-manager')
 AND NOT permissions::jsonb ? 'flow.config.read';
UPDATE iam_role SET permissions=(permissions::jsonb || '["flow.config.manage"]'::jsonb)
 WHERE id IN ('builtin-maintainer','builtin-writer') AND NOT permissions::jsonb ? 'flow.config.manage';
UPDATE iam_role SET permissions=(permissions::jsonb || '["flow.secret.manage"]'::jsonb)
 WHERE id='builtin-secret-manager' AND NOT permissions::jsonb ? 'flow.secret.manage';
UPDATE iam_role SET permissions=(permissions::jsonb || '["flow.runtime.use"]'::jsonb)
 WHERE id IN ('builtin-system-controller','builtin-system-allinone') AND NOT permissions::jsonb ? 'flow.runtime.use';

-- Maintainer intentionally excludes secret administration. Its historical
-- flow.* wildcard would otherwise acquire the newly introduced secret right.
UPDATE iam_role SET permissions='["namespace.read","namespace.config.read","namespace.config.manage","agent.*","skill.*","flow.read","flow.use","flow.write","flow.delete","flow.access.manage","flow.config.read","flow.config.manage","flow_release.*","llm_provider.read","llm_provider.use","mcp.read","mcp.use","notification.read","knowledge.*","run.*","approval.*","trace.read"]', updated_at=NOW(), updated_by='system'
 WHERE id='builtin-maintainer';
