-- Evolve built-in roles for independently controlled environment, write-only
-- secret, and controller-only resolved runtime configuration APIs.
UPDATE iam_role SET permissions=json_insert(permissions,'$[#]','namespace.config.read')
 WHERE id IN ('builtin-reader','builtin-maintainer','builtin-writer','builtin-operator','builtin-secret-manager')
 AND NOT EXISTS (SELECT 1 FROM json_each(iam_role.permissions) WHERE value='namespace.config.read');
UPDATE iam_role SET permissions=json_insert(permissions,'$[#]','namespace.config.manage')
 WHERE id='builtin-maintainer'
 AND NOT EXISTS (SELECT 1 FROM json_each(iam_role.permissions) WHERE value='namespace.config.manage');
UPDATE iam_role SET permissions=json_insert(permissions,'$[#]','namespace.secret.manage')
 WHERE id='builtin-secret-manager'
 AND NOT EXISTS (SELECT 1 FROM json_each(iam_role.permissions) WHERE value='namespace.secret.manage');
UPDATE iam_role SET permissions=json_insert(permissions,'$[#]','flow.config.read')
 WHERE id IN ('builtin-reader','builtin-maintainer','builtin-writer','builtin-operator','builtin-secret-manager')
 AND NOT EXISTS (SELECT 1 FROM json_each(iam_role.permissions) WHERE value='flow.config.read');
UPDATE iam_role SET permissions=json_insert(permissions,'$[#]','flow.config.manage')
 WHERE id IN ('builtin-maintainer','builtin-writer')
 AND NOT EXISTS (SELECT 1 FROM json_each(iam_role.permissions) WHERE value='flow.config.manage');
UPDATE iam_role SET permissions=json_insert(permissions,'$[#]','flow.secret.manage')
 WHERE id='builtin-secret-manager'
 AND NOT EXISTS (SELECT 1 FROM json_each(iam_role.permissions) WHERE value='flow.secret.manage');
UPDATE iam_role SET permissions=json_insert(permissions,'$[#]','flow.runtime.use')
 WHERE id IN ('builtin-system-controller','builtin-system-allinone')
 AND NOT EXISTS (SELECT 1 FROM json_each(iam_role.permissions) WHERE value='flow.runtime.use');

-- Maintainer intentionally excludes secret administration. Its historical
-- flow.* wildcard would otherwise acquire the newly introduced secret right.
UPDATE iam_role SET permissions='["namespace.read","namespace.config.read","namespace.config.manage","agent.*","skill.*","flow.read","flow.use","flow.write","flow.delete","flow.access.manage","flow.config.read","flow.config.manage","flow_release.*","llm_provider.read","llm_provider.use","mcp.read","mcp.use","notification.read","knowledge.*","run.*","approval.*","trace.read"]', updated_at=datetime('now'), updated_by='system'
 WHERE id='builtin-maintainer';
