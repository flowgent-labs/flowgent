UPDATE iam_role SET permissions=json_insert(permissions, '$[#]', 'resource_pool.read')
 WHERE id IN ('builtin-reader','builtin-writer','builtin-operator','builtin-auditor','builtin-secret-manager')
 AND NOT EXISTS (SELECT 1 FROM json_each(permissions) WHERE value='resource_pool.read');
UPDATE iam_role SET permissions=json_insert(permissions, '$[#]', 'resource_pool.use')
 WHERE id IN ('builtin-writer','builtin-operator')
 AND NOT EXISTS (SELECT 1 FROM json_each(permissions) WHERE value='resource_pool.use');
UPDATE iam_role SET permissions=json_insert(json_insert(json_insert(permissions, '$[#]', 'resource_pool.read'), '$[#]', 'resource_pool.use'), '$[#]', 'resource_pool.manage')
 WHERE id='builtin-namespace-owner'
 AND NOT EXISTS (SELECT 1 FROM json_each(permissions) WHERE value='resource_pool.manage');
UPDATE iam_role SET permissions=json_insert(json_insert(permissions, '$[#]', 'resource_pool.read'), '$[#]', 'resource_pool.use')
 WHERE id IN ('builtin-system-controller','builtin-system-jobmanager','builtin-system-taskmanager','builtin-system-allinone')
 AND NOT EXISTS (SELECT 1 FROM json_each(permissions) WHERE value='resource_pool.use');
UPDATE iam_role SET permissions=json_insert(json_insert(json_insert(permissions, '$[#]', 'resource_pool.read'), '$[#]', 'resource_pool.use'), '$[#]', 'resource_pool.manage')
 WHERE id='builtin-maintainer'
 AND NOT EXISTS (SELECT 1 FROM json_each(permissions) WHERE value='resource_pool.manage');
