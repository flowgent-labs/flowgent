UPDATE iam_role
SET permissions = permissions::jsonb || '["flow.runtime.use"]'::jsonb,
    updated_at = NOW(),
    updated_by = 'migration'
WHERE id = 'builtin-system-taskmanager'
  AND NOT permissions::jsonb ? 'flow.runtime.use';
