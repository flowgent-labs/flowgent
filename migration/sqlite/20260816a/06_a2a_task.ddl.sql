-- Shared A2A protocol task state. caller_key is a SHA-256 credential digest;
-- plaintext bearer credentials are never persisted.
CREATE TABLE IF NOT EXISTS a2a_task (
    id          TEXT PRIMARY KEY,
    caller_key  TEXT NOT NULL,
    context_id  TEXT NOT NULL DEFAULT '',
    state       TEXT NOT NULL,
    task_json   TEXT NOT NULL,
    version     INTEGER NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_a2a_task_caller_updated
    ON a2a_task(caller_key, updated_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_a2a_task_caller_context
    ON a2a_task(caller_key, context_id, updated_at DESC);
