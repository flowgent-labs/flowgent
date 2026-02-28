# Flowgent E2E — Application Mode on K3s

**Date:** 2026-05-24
**Components:** apiserver, a2a, controller, jobmanager, taskmanager, sandbox, notifier (wallet optional)

---

## 1. Deploy Application Mode

```bash
# Build static binary + MCP binaries
CGO_ENABLED=0 go build -o bin/flowgent ./src/cmd/flowgent/
go build -o bin/github-mcp    ./examples/mcp-github/main.go
go build -o bin/sonarqube-mcp ./examples/mcp-sonarqube/main.go

# Build image
podman build -t localhost/flowgent:latest -f deploy/docker/Dockerfile .

# Import to K3s
sudo podman save localhost/flowgent:latest | sudo k3s ctr images import -

# Deploy with A2A + sandbox (wallet disabled)
helm upgrade --install flowgent deploy/helm/flowgent \
  --set global.mode=session \
  --set global.image.repository=localhost/flowgent \
  --set global.image.tag=latest \
  --set a2a.enabled=true \
  --set wallet.enabled=false \
  --set sandbox.enabled=true \
  --set postgresql.host=172.29.235.101 \
  --set postgresql.password=flowgent \
  --set emqx.broker=tcp://172.29.235.101:1883
```

For application mode (grade-priority flow):

```bash
# Insert grade flow into PG
psql -h 172.29.235.101 -U flowgent -d flowgent <<'SQL'
INSERT INTO agentflow_definitions (agentflow_id, version, definition, priority, namespace, tenant_id)
VALUES ('security-autonomy-fixer-v2', 1, '...', 'grade', 'flowgent-rengine', 'default');
SQL

# Controller auto-creates JM Deployment:
kubectl get deploy -n flowgent-rengine
# Expected: flowgent-jm-default-security-autonomy-fixer-v2-<run_id>-<hash>
```

---

## 2. Verification Checklist

### 2.1 REST API — Flow CRUD

```bash
# List flows
curl -s http://<apiserver>:9999/api/v1/default/agentflows | python3 -m json.tool

# Create flow
curl -s -X POST http://<apiserver>:9999/api/v1/default/agentflows \
  -H 'Content-Type: application/json' \
  -d '{"id":"e2e-test","nodes":[{"id":"start","type":"noop"}],"edges":[]}'

# Update flow (change description/instruction)
curl -s -X PUT http://<apiserver>:9999/api/v1/default/agentflows/e2e-test \
  -H 'Content-Type: application/json' \
  -d '{"id":"e2e-test","description":"updated","nodes":[...],"edges":[]}'

# Delete flow
curl -s -X DELETE http://<apiserver>:9999/api/v1/default/agentflows/e2e-test
```

| Check | Expected |
|-------|----------|
| List returns flows | Array with agentflow objects |
| POST returns 200 | New flow persisted |
| PUT updates description | Flow description changed in next GET |
| DELETE removes flow | 200, flow gone from list |

### 2.2 REST API — Run Lifecycle

```bash
# Trigger run
curl -s -X POST http://<apiserver>:9999/api/v1/default/agentflows/trigger \
  -H 'Content-Type: application/json' \
  -d '{"agentflow_id":"security-autonomy-fixer-v2","vars":{},"trigger":{"type":"api"}}'
# → {"run_id":"...","status":"PENDING"}

# Get run status
curl -s http://<apiserver>:9999/api/v1/default/runs/<run_id>
# → {"status":"RUNNING|COMPLETED|FAILED","agentflow_id":"..."}

# Cancel run
curl -s -X POST http://<apiserver>:9999/api/v1/default/runs/<run_id>/cancel

# List tasks for run (all node statuses)
curl -s http://<apiserver>:9999/api/v1/default/runs/<run_id>/tasks
# → [{node_id:"fetch-issues",status:"SUCCESS",output:{...}}, ...]

# Get task detail (includes agent memory)
curl -s http://<apiserver>:9999/api/v1/default/runs/<run_id>/tasks/<task_id>
# → {node_id:"analyze-issues",status:"SUCCESS",output:{...},memory:{...}}
```

| Check | Expected |
|-------|----------|
| Trigger creates PENDING run | `run_id` in response |
| Run progresses to RUNNING then COMPLETED/FAILED | Status changes over time |
| Cancel stops a running flow | Status → CANCELLED |
| Tasks list shows all DAG nodes | One entry per node with status + output |
| Task detail includes agent memory | `memory` field with conversation history |

### 2.3 A2A Protocol

```bash
# Agent card
curl -s http://<apiserver>:9992/.well-known/agent.json | python3 -m json.tool
# → {name, description, url, capabilities, skills:[...]}

# Submit agentflow via A2A
curl -s -X POST http://<apiserver>:9992/a2a/tasks \
  -H 'Content-Type: application/json' \
  -d '{"agentflow_id":"security-autonomy-fixer-v2","input":{"repo":"rengine"}}'
# → {task_id:"...", status:"PENDING"}

# Query task result via A2A
curl -s http://<apiserver>:9992/a2a/tasks/<task_id>
# → {task_id:"...", status:"COMPLETED|RUNNING|FAILED", output:{...}}
```

| Check | Expected |
|-------|----------|
| Agent card returns valid JSON | `name`, `skills[]`, `url` fields present |
| A2A submit creates run | `task_id` returned, run appears in REST list |
| A2A query returns run status | `status` + `output` fields |

### 2.4 Agent Memory (PG)

```sql
-- Check memory for a specific node after execution
SELECT node_id, content, metadata
FROM node_memories
WHERE flow_id = 'security-autonomy-fixer-v2'
ORDER BY updated_at DESC;

-- Verify memory accumulates across runs
SELECT flow_id, node_id, COUNT(*) AS entries, MAX(updated_at)
FROM node_memories
WHERE flow_id = 'security-autonomy-fixer-v2'
GROUP BY flow_id, node_id;
```

| Check | Expected |
|-------|----------|
| Memory written for each agent node | One row per (flow_id, node_id) |
| Memory content is LLM conversation | `content` contains system prompt + user + assistant messages |
| Memory persists across runs | `updated_at` advances after each run |

### 2.5 Tribunal (Review + Vote)

```sql
-- Vote results per run
SELECT agentflow_run_id, node_id, output
FROM task_runs
WHERE node_id IN ('vote','tribunal')
ORDER BY created_at DESC;

-- Supervisor decisions
SELECT agentflow_run_id, task_run_id, input, decision
FROM supervisor_log
ORDER BY created_at DESC;
```

| Check | Expected |
|-------|----------|
| Vote node output has `decision: true/false` | Majority vote correctly computed |
| All 3 reviewers participated | `review-security`, `review-quality`, `review-arch` all have outputs |
| Supervisor log records decision | `decision` JSON with `action: "continue"|"retry"|"abort"` |

### 2.6 Human Approval

```bash
# Check pending approvals
curl -s http://<apiserver>:9999/api/v1/default/approvals/pending

# Approve via token
curl -s -X POST http://<apiserver>:9999/api/v1/human/<token>/approve \
  -H 'Content-Type: application/json' \
  -d '{"comment":"LGTM"}'

# Reject via token
curl -s -X POST http://<apiserver>:9999/api/v1/human/<token>/reject \
  -d '{"comment":"needs rework"}'
```

**WebSocket verification:**

```bash
# Connect to notifier WS
websocat ws://<notifier-svc>:9993/ws?tenant=default

# When human approval is created, expect message:
# {"type":"human_approval_created","agentflow_id":"...","run_id":"...","task_id":"..."}
```

| Check | Expected |
|-------|----------|
| Human approval creates PENDING entry | `approvals/pending` returns token |
| Approve advances flow | Next node after human-approval executes |
| WS receives notification | `human_approval_created` message on WS connection |

### 2.7 Commit + PR Nodes

```sql
-- Git operations output
SELECT node_id, output
FROM task_runs
WHERE node_id IN ('create-branch','commit-fixes','create-pr')
  AND agentflow_run_id = '<run_id>';

-- Check memory for git-agent
SELECT content FROM node_memories
WHERE flow_id = 'security-autonomy-fixer-v2'
  AND node_id = 'write-cyberbot';
```

| Check | Expected |
|-------|----------|
| `commit-fixes` output | `modified_files` list with patched files |
| `create-pr` output | `pr_url` or `pr_number` |
| `write-cyberbot` memory | `.cyberbot` metadata JSON in content |

### 2.8 Summary Report

```sql
SELECT node_id, output, status
FROM task_runs
WHERE node_id = 'summary-report'
  AND agentflow_run_id = '<run_id>';
```

| Check | Expected |
|-------|----------|
| Report includes issue count | `total_issues` or `issues_found` in output |
| Report includes vote outcome | `vote_decision` in output |
| Report includes PR links | `pr_links` in output |
| Report persisted as memory | `node_memories` row for `summary-report` |

### 2.9 Notification (EMQX/MQTT)

```bash
# Subscribe to notification topic
mosquitto_sub -h 172.29.235.101 -t 'flowgent/notify/queue/default/+/+' -v

# Trigger a flow with notify nodes → check topic messages
```

| Check | Expected |
|-------|----------|
| MQTT topic receives messages | `flowgent/notify/queue/{tenant}/{flow}` has JSON payload |
| Message contains title + body | `title`, `body`, `agentflow_id`, `run_id` fields |

---

## 3. Security Fixer V2 Full Pipeline

Expected DAG execution order with all verification points:

```
fetch-issues  ──→  analyze-issues  ──→  generate-fixes
                                              │
                    ┌─────────────────────────┘
                    ▼
            review-security  ──┐
            review-quality  ──┤──→  vote  ──→  supervisor
            review-arch     ──┘                    │
                                                   ▼
                                            is-approved?
                                         true│       │false
                                             ▼       └──→ back to generate-fixes
                                     write-cyberbot
                                             │
                                             ▼
                                      summary-report
                                             │
                                             ▼
                                            end
```

Each agent node writes memory to PG. Each tool node writes output to PG.
The vote node aggregates 3 reviews into a majority decision.
The supervisor validates and can abort/retry.

---

## 4. Post-Run Verification

```sql
-- All nodes for a run with status
SELECT tr.node_id, tr.status, tr.output IS NOT NULL AS has_output,
       nm.content IS NOT NULL AS has_memory
FROM task_runs tr
LEFT JOIN node_memories nm ON nm.flow_id = '<definition_id>' AND nm.node_id = tr.node_id
WHERE tr.agentflow_run_id = '<run_id>'
ORDER BY tr.sequence;

-- Verify end-to-end: every agent node has memory, every tool node has output
```
