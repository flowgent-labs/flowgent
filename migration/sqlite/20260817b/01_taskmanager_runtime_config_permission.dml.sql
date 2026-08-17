UPDATE iam_role
SET permissions = json_insert(permissions, '$[#]', 'flow.runtime.use'),
    updated_at = CURRENT_TIMESTAMP,
    updated_by = 'migration'
WHERE id = 'builtin-system-taskmanager'
  AND NOT EXISTS (
    SELECT 1 FROM json_each(iam_role.permissions)
    WHERE value = 'flow.runtime.use'
  );
