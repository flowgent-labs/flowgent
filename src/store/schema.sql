-- Flowgent AgentFlow Engine Schema

CREATE TABLE IF NOT EXISTS agentflow_definitions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agentflow_id    TEXT NOT NULL UNIQUE,
    version         BIGINT NOT NULL DEFAULT 1,
    definition      JSONB NOT NULL,
    checksum        TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by      TEXT,
    comment         TEXT
);
CREATE INDEX IF NOT EXISTS idx_af_def_id ON agentflow_definitions(agentflow_id);
CREATE INDEX IF NOT EXISTS idx_af_def_ver ON agentflow_definitions(agentflow_id, version);

CREATE TABLE IF NOT EXISTS agentflow_runs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agentflow_id    TEXT NOT NULL,
    version         BIGINT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'PENDING',
    vars            JSONB,
    output          JSONB,
    error           TEXT,
    trigger_type    TEXT,
    trigger_source  TEXT,
    trigger_payload JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_af_run_id ON agentflow_runs(agentflow_id);
CREATE INDEX IF NOT EXISTS idx_af_run_status ON agentflow_runs(status);
CREATE INDEX IF NOT EXISTS idx_af_run_created ON agentflow_runs(created_at DESC);

CREATE TABLE IF NOT EXISTS task_runs (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agentflow_run_id  UUID NOT NULL REFERENCES agentflow_runs(id) ON DELETE CASCADE,
    node_id           TEXT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'PENDING',
    input             JSONB,
    output            JSONB,
    error             TEXT,
    retry_count       INT NOT NULL DEFAULT 0,
    max_retries       INT NOT NULL DEFAULT 0,
    exec_id           TEXT NOT NULL,
    parent_task_run_id UUID REFERENCES task_runs(id) ON DELETE SET NULL,
    sequence          INT NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at        TIMESTAMPTZ,
    finished_at       TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_task_run_af ON task_runs(agentflow_run_id);
CREATE INDEX IF NOT EXISTS idx_task_run_status ON task_runs(status);
CREATE INDEX IF NOT EXISTS idx_task_run_node ON task_runs(agentflow_run_id, node_id);
CREATE INDEX IF NOT EXISTS idx_task_run_exec ON task_runs(exec_id);

CREATE TABLE IF NOT EXISTS human_approvals (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_run_id     UUID NOT NULL UNIQUE REFERENCES task_runs(id) ON DELETE CASCADE,
    token           TEXT NOT NULL UNIQUE,
    status          TEXT NOT NULL DEFAULT 'PENDING',
    approved        BOOLEAN,
    comment         TEXT,
    timeout_seconds INT NOT NULL DEFAULT 86400,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ,
    resolved_at     TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_ha_token ON human_approvals(token);
CREATE INDEX IF NOT EXISTS idx_ha_status ON human_approvals(status);

CREATE TABLE IF NOT EXISTS supervisor_log (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agentflow_run_id UUID NOT NULL REFERENCES agentflow_runs(id) ON DELETE CASCADE,
    task_run_id      UUID REFERENCES task_runs(id) ON DELETE SET NULL,
    input_snapshot   JSONB NOT NULL,
    decision         JSONB NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_sup_log_af ON supervisor_log(agentflow_run_id);

CREATE TABLE IF NOT EXISTS idempotency_keys (
    key             TEXT PRIMARY KEY,
    task_run_id     UUID NOT NULL REFERENCES task_runs(id) ON DELETE CASCADE,
    exec_id         TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS agent_memories (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id         TEXT NOT NULL,
    agentflow_run_id UUID REFERENCES agentflow_runs(id) ON DELETE SET NULL,
    type             TEXT NOT NULL DEFAULT 'episodic',
    content          TEXT NOT NULL,
    embedding        vector(1536),
    tags             TEXT[] DEFAULT '{}',
    metadata         JSONB,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_am_agent ON agent_memories(agent_id);
CREATE INDEX IF NOT EXISTS idx_am_type ON agent_memories(type);
CREATE INDEX IF NOT EXISTS idx_am_af ON agent_memories(agentflow_run_id);
