# Security Autonomy Fixer V1 — Distributed E2E (API Trigger)

**Date:** 2026-05-30
**Scope:** K8s ResourceManager + PG + EMQX on K3s — session mode, REST API triggered
**Parent:** [10-L2-USE-CASES.md](10-L2-USE-CASES.md)
**AgentFlow:** [`examples/flows/01-security-autonomy-fix-v1.yaml`](../examples/flows/01-security-autonomy-fix-v1.yaml)

> V1 is the **baseline working version** — deployed on K3s in session mode, triggered
> via REST API. Covers the full 12-phase pipeline: discovery → analyze → fix → review
> → vote → supervisor → condition → human approval → commit & PR → re-scan → report.
> GitHub webhook trigger is available but commented out in the flow definition.
>
> For the real GitHub webhook → SonarQube integration target, see
> [11-L2-E2E-security-fixer-v2.md](11-L2-E2E-security-fixer-v2.md).

---

## 1. Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                        K3s Cluster                               │
│                                                                   │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐         │
│  │apiserver │  │jobmanager│  │taskmgr   │  │ sandbox  │         │
│  │  :9999   │  │ (leader) │  │  ×2 slots│  │  ×2 slots│         │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘         │
│       │             │             │             │                │
│       │    ┌────────┼─────────────┼─────────────┘                │
│       │    │        │             │                              │
│       ▼    ▼        ▼             ▼                              │
│  ┌──────────┐  ┌──────────────────────┐  ┌──────────┐           │
│  │    PG    │  │       EMQX MQTT       │  │ notifier │           │
│  │  :5432   │  │  flowgent/exec/+/+   │  │  :9993   │           │
│  └──────────┘  └──────────────────────┘  └──────────┘           │
│                                                                   │
│  ┌──────────┐  ┌──────────┐                                      │
│  │  Jaeger  │  │   A2A    │ (optional)                            │
│  │  :16686  │  │  :9992   │                                      │
│  └──────────┘  └──────────┘                                      │
└──────────────────────────────────────────────────────────────────┘
         │                    │
         ▼                    ▼
┌──────────────┐   ┌──────────────────┐
│  SonarQube   │   │  GitHub Actions  │
│  :9000       │   │  webhook → fix   │
│  rengine     │   │  → commit → PR   │
│  700+ issues │   │  → re-scan       │
└──────────────┘   └──────────────────┘
```

**Key characteristics:**
- 5 components (session mode): apiserver + jobmanager + taskmanager + sandbox + notifier
- MQTT-based task dispatch: `flowgent/exec/{runID}/{planID}`
- Kubernetes ResourceManager: auto-scales TM replicas by queue depth
- Store: PostgreSQL (external or embedded)
- Queue: MQTT (fail-fast if misconfigured — no silent fallback)

---

## 2. Prerequisites

### 2.1 Infrastructure

| Service | Endpoint | Auth |
|---------|----------|------|
| K3s | localhost | `kubectl` + `helm` |
| PostgreSQL | 172.29.235.101:5432 | flowgent / flowgent, db=flowgent |
| EMQX MQTT | 172.29.235.101:1883 | anonymous |
| SonarQube | http://localhost:9000 | admin / Abcd1234@sonar |
| Jaeger *(optional)* | K3s pod :16686 | none |

### 2.2 External Services

| Service | Credentials | Notes |
|---------|-------------|-------|
| GitHub PAT | `$GITHUB_TOKEN` (repo + PR scope) | For MCP: commit, create PR, comment |
| SonarQube token | `squ_413955...` | For MCP: issue search, re-scan trigger |
| LLM Provider | Bailian / DeepSeek API key | For agent LLM calls |

### 2.3 Registered Agents & MCPs

All agents loaded from `examples/agents/`:
- `supervisor`, `issue-detector`, `fixer-agent`
- `security-reviewer`, `quality-reviewer`, `arch-reviewer`
- `git-agent`

MCP binaries in `/bin/`:
- `github-mcp` — GitHub API (commit, PR, branch)
- `sonarqube-mcp` — SonarQube API (scan, issues, re-analysis)

### 2.4 SonarQube Setup

See [`deploy/docker/sonarqube/docker-compose.yml`](../deploy/docker/sonarqube/docker-compose.yml).

```bash
cd deploy/docker/sonarqube && sudo docker compose up -d

# Verify
curl -s -u admin:Abcd1234@sonar http://localhost:9000/api/system/status
# → {"status":"UP","version":"26.4.0.121862"}

# Verify rengine project
curl -s -u squ_413955935468a7aacc7977053eda43d585e4da04: \
  "http://localhost:9000/api/issues/search?projectKeys=rengine&severities=BLOCKER,CRITICAL&ps=5"
# → {"total":700,...}
```

---

## 3. Build & Deploy

### 3.1 Build Binary & Image

```bash
cd /home/agent/flowgent

# Build production Docker image (multi-stage, no host Go required)
make docker-build

# Import to K3s
sudo docker save localhost/flowgent:latest | sudo k3s ctr images import -
```

### 3.2 Deploy via Helm

```bash
# Session mode — pre-deploys 5 components (no controller)
helm upgrade --install flowgent deploy/helm/flowgent -n default \
  --set global.mode=session \
  --set global.image.repository=localhost/flowgent \
  --set global.image.tag=latest \
  --set global.image.pullPolicy=IfNotPresent \
  --set emqx.broker=tcp://172.29.235.101:1883 \
  --set postgresql.host=172.29.235.101 \
  --set postgresql.password=flowgent \
  --set a2a.enabled=true \
  --set jaeger.enabled=true

# Wait for all pods
kubectl get pods -l app.kubernetes.io/name=flowgent -w

# Expected (session mode):
#   flowgent-apiserver-*      2/2
#   flowgent-jobmanager-*     2/2
#   flowgent-taskmanager-*    2/2
#   flowgent-sandbox-*        2/2
#   flowgent-notifier-*       2/2
#   flowgent-a2a-*            2/2  (if enabled)
```

### 3.3 Run PG Migrations (if fresh DB)

```bash
kubectl exec -i deploy/flowgent-postgresql -- \
  psql -U flowgent -d flowgent \
  < src/store/src/migration/postgres/20250926/01_init.ddl.sql
```

### 3.4 Verify Deployment

```bash
# Component health
kubectl get deploy -l app.kubernetes.io/name=flowgent

# API health
curl http://<apiserver-svc>:9999/_/healthz
# → {"status":"ok"}

# EMQX dashboard
curl http://172.29.235.101:18083/api/v5/status

# Jaeger UI
kubectl port-forward svc/flowgent-jaeger 16686:16686
# → http://localhost:16686
```

---

## 4. GitHub Actions → SonarQube Fix Pipeline

### 4.1 Pipeline Overview

```
GitHub PR opened/updated on rengine
  │
  ▼
webhook → Flowgent API Server (/_/webhooks/github)
  │
  ▼
JobManager → DAG parsing → 12 nodes dispatched
  │
  ├── [1] fetch-issues (tool: SonarQube MCP)
  │         → GET /api/issues/search?projectKeys=rengine
  │         → returns BLOCKER + CRITICAL issues
  │
  ├── [2] analyze-issues (agent: issue-detector)
  │         → categorizes by type, file, severity
  │         → selects top 5 actionable issues
  │
  ├── [3] generate-fixes (agent: fixer-agent)
  │         → reads source files, generates patches
  │
  ├── [4-6] review-* (3 agents, parallel)
  │         → review-security, review-quality, review-arch
  │         → each returns {decision: true|false, comments}
  │
  ├── [7] vote (tribunal: majority rule)
  │         → ≥2 approve → proceed
  │
  ├── [8] supervisor (agent)
  │         → validates fix plan, can abort/retry
  │
  ├── [9] is-approved? (condition)
  │    true│              │false
  │       ▼              └──→ back to [3] generate-fixes
  ├── [10] commit-fixes (tool: GitHub MCP)
  │         → git checkout -b security-bot/fix-{run_id}
  │         → apply patches, commit
  │
  ├── [11] create-pr (tool: GitHub MCP)
  │         → gh pr create --title "Security fix: ..."
  │
  ├── [12] summary-report (agent)
  │         → markdown report with fix history
  │
  └── [13] notify (notifier → PR comment + Slack/Email)
```

### 4.2 Configure GitHub Webhook

```bash
# Create webhook on rengine repo (requires admin access)
gh api repos/wl4g/rengine/hooks \
  -f name=web -f active=true \
  -f 'config.url=https://flowgent.wl4g.com/_/webhooks/github' \
  -f config.content_type=json \
  -f events[]=pull_request -f events[]=push
```

```yaml
# flowgent.yaml — webhook config
server:
  webhook:
    enabled: true
    path: "/_/webhooks/github"
    secret: "${GITHUB_WEBHOOK_SECRET}"
```

### 4.3 Register MCP Servers

```yaml
# flowgent.yaml
orchestration:
  mcps:
    - name: github
      enabled: true
      command: ["/bin/github-mcp"]
      env:
        GITHUB_TOKEN: "${GITHUB_TOKEN}"
    - name: sonarqube
      enabled: true
      command: ["/bin/sonarqube-mcp"]
      env:
        SONARQUBE_URL: "http://172.29.235.101:9000"
        SONARQUBE_TOKEN: "squ_413955935468a7aacc7977053eda43d585e4da04"
```

### 4.4 Trigger the Flow

```bash
# Method 1: REST API trigger (manual test)
curl -s -X POST http://<apiserver>:9999/api/v1/default/agentflows/trigger \
  -H 'Content-Type: application/json' \
  -d '{
    "agentflow_id": "security-autonomy-fixer-v1",
    "vars": {"repo": "rengine", "repo_path": "/home/agent/rengine"},
    "trigger": {"type": "api", "source": "e2e-test"}
  }'
# → {"run_id":"<RUN_ID>","status":"PENDING"}

# Method 2: GitHub webhook (production path)
# Push to rengine main → webhook → flowgent → trigger
git push origin main
```

### 4.5 Monitor Execution

```bash
RUN_ID="<from-response>"

# Phase 1-2: Controller → JM picks up run
kubectl logs -l app.kubernetes.io/component=jobmanager | grep "$RUN_ID"

# Phase 3: TM executes fetch-issues (SonarQube MCP call)
kubectl logs -l app.kubernetes.io/component=taskmanager | grep "fetch-issues"

# Phase 4-6: Agent LLM calls
kubectl logs -l app.kubernetes.io/component=taskmanager | grep -E "analyze-issues|generate-fixes|review-"

# Phase 7-8: Commit → PR
kubectl logs -l app.kubernetes.io/component=taskmanager | grep -E "commit-fixes|create-pr"

# Trace via Jaeger
kubectl port-forward svc/flowgent-jaeger 16686:16686
# → Search for run_id in Jaeger UI
```

---

## 5. Verification

Verification proceeds in three layers. Layer 1 and 2 must pass before proceeding
to Layer 3. Run after every fresh Helm deploy.

---

### 5.1 Layer 1 — Pod Status (fresh deploy)

```bash
# All pods must be Running + Ready
kubectl get pods -l app.kubernetes.io/name=flowgent -o wide

# Deployment status
kubectl get deploy -l app.kubernetes.io/name=flowgent

# Check each component log for startup errors
for comp in apiserver jobmanager taskmanager sandbox notifier; do
  echo "=== $comp ==="
  kubectl logs -l app.kubernetes.io/component=$comp --tail=5
done
```

| # | Check | Command | Expected |
|---|-------|---------|----------|
| 1.1 | Pods Running | `kubectl get pods -l app.kubernetes.io/name=flowgent` | 10/10 Ready (5 comps × 2 replicas) |
| 1.2 | Deployments ready | `kubectl get deploy -l app.kubernetes.io/name=flowgent` | All `AVAILABLE` = `DESIRED` |
| 1.3 | JM no MQTT fatal | `kubectl logs -l app.kubernetes.io/component=jobmanager \| grep FATAL` | No output |
| 1.4 | TM no MQTT fatal | `kubectl logs -l app.kubernetes.io/component=taskmanager \| grep FATAL` | No output |
| 1.5 | Sandbox no MQTT fatal | `kubectl logs -l app.kubernetes.io/component=sandbox \| grep FATAL` | No output |

**GATE**: All pods Running + no FATAL log lines before continuing.

---

### 5.2 Layer 2 — Infrastructure (API → EMQX → PG → Jaeger)

#### 5.2.1 API Server

```bash
APISERVER="http://<apiserver-svc>:9999"

# Health
curl -s $APISERVER/_/healthz
# → {"status":"ok"}

# A2A agent card (if enabled)
curl -s $APISERVER:9992/.well-known/agent.json | jq '{name,url}'

# List flows
curl -s $APISERVER/api/v1/default/agentflows | jq '.[].id'
```

| # | Check | Expected |
|---|-------|----------|
| 2.1 | `/_/healthz` | `{"status":"ok"}` |
| 2.2 | A2A agent card *(if enabled)* | `name`, `url` fields present |
| 2.3 | `GET /agentflows` | Array, contains `security-autonomy-fixer-v1` |

#### 5.2.2 EMQX MQTT

```bash
# Dashboard
curl -s http://172.29.235.101:18083/api/v5/status | jq '.status'

# Subscribe to verify broker is reachable
timeout 5 mosquitto_sub -h 172.29.235.101 -t 'flowgent/healthcheck' -C 1 &
sleep 1
mosquitto_pub -h 172.29.235.101 -t 'flowgent/healthcheck' -m 'ping'
```

| # | Check | Expected |
|---|-------|----------|
| 2.4 | EMQX dashboard | `"status":"Running"` |
| 2.5 | Pub/sub working | `ping` received by subscriber |

#### 5.2.3 PostgreSQL

```bash
# Connectivity
psql -h 172.29.235.101 -U flowgent -d flowgent -c "SELECT 1 AS connectivity"

# Schema
psql -h 172.29.235.101 -U flowgent -d flowgent -c "\dt"
# Expected tables: agentflow_definitions, agentflow_runs, execution_plans, task_runs, node_memories

# Flow definition present
psql -h 172.29.235.101 -U flowgent -d flowgent -c \
  "SELECT agentflow_id, priority, version FROM agentflow_definitions"
```

| # | Check | Expected |
|---|-------|----------|
| 2.6 | PG reachable | `SELECT 1` → `1` |
| 2.7 | Schema exists | 5+ tables |
| 2.8 | Flow registered | `security-autonomy-fixer-v1` in results |

#### 5.2.4 Jaeger Tracing

```bash
kubectl port-forward svc/flowgent-jaeger 16686:16686 &
sleep 2
curl -s http://localhost:16686/api/services | jq '.data'
```

| # | Check | Expected |
|---|-------|----------|
| 2.9 | Jaeger UI | http://localhost:16686 accessible |
| 2.10 | Service registered | `flowgent` in service list |

**GATE**: API healthy + EMQX reachable + PG reachable before Layer 3.

---

### 5.3 Layer 3 — Distributed Flow Execution

Trigger a run and verify every layer of the distributed execution path:
API → PG (run created) → JM (DAG parsed) → MQTT (plans dispatched) → TM (executed) → PG (results persisted).

#### 5.3.1 Trigger & Monitor

```bash
# Trigger
RUN_ID=$(curl -s -X POST $APISERVER/api/v1/default/agentflows/trigger \
  -H 'Content-Type: application/json' \
  -d '{"agentflow_id":"security-autonomy-fixer-v1","vars":{"repo":"rengine"},"trigger":{"type":"api"}}' \
  | jq -r '.run_id')
echo "Run: $RUN_ID"

# Poll
for i in $(seq 1 60); do
  STATUS=$(curl -s $APISERVER/api/v1/default/runs/$RUN_ID | jq -r '.status')
  echo "[$i] $STATUS"
  case $STATUS in COMPLETED|FAILED|CANCELLED) break ;; esac
  sleep 5
done

# Tasks
curl -s $APISERVER/api/v1/default/runs/$RUN_ID/tasks | jq '.[] | {node_id, status}'
```

#### 5.3.2 MQTT — Verify Plan Dispatch

```bash
# In a separate terminal, subscribe before triggering
mosquitto_sub -h 172.29.235.101 -t 'flowgent/exec/+/+' -v

# Each ExecutionPlan dispatched should produce a message:
# flowgent/exec/{runID}/{planID} { "plan_id":"...", "node_id":"fetch-issues", ... }
```

| # | Check | Expected |
|---|-------|----------|
| 3.1 | Run created | `RUN_ID` non-empty, status PENDING |
| 3.2 | Status progresses | PENDING → RUNNING → COMPLETED/FAILED |
| 3.3 | Tasks list | 12+ nodes with status |
| 3.4 | MQTT messages | One per ExecutionPlan dispatched |

#### 5.3.3 PG — Verify Persistence

```sql
-- Run lifecycle
SELECT id, agentflow_id, status, created_at, started_at, finished_at
FROM agentflow_runs WHERE id = '<run_id>';

-- Task execution (all nodes)
SELECT node_id, task_type, status, retry_count
FROM task_runs WHERE agentflow_run_id = '<run_id>'
ORDER BY sequence;

-- Agent memory
SELECT flow_id, node_id, length(content) AS bytes, updated_at
FROM node_memories WHERE flow_id = 'security-autonomy-fixer-v1'
ORDER BY updated_at DESC;
```

| # | Check | Expected |
|---|-------|----------|
| 3.5 | `agentflow_runs` | Status COMPLETED, timestamps set |
| 3.6 | `task_runs` | 12+ rows, one per DAG node |
| 3.7 | `node_memories` | Content per agent node, persists across runs |

#### 5.3.4 Tribunal + Vote

```sql
SELECT node_id, output FROM task_runs
WHERE node_id IN ('vote','tribunal') AND agentflow_run_id = '<run_id>';
```

| # | Check | Expected |
|---|-------|----------|
| 3.8 | Vote output | `decision: true/false` |
| 3.9 | 3 reviewers | `review-security`, `review-quality`, `review-arch` executed |

#### 5.3.5 Jaeger — Trace Verification

```bash
# Open Jaeger UI
kubectl port-forward svc/flowgent-jaeger 16686:16686
# → http://localhost:16686 → search service=flowgent → find trace for run_id
```

| # | Check | Expected |
|---|-------|----------|
| 3.10 | Service registered | `flowgent` in dropdown |
| 3.11 | Trace spans | `trigger` → `execute` → `schedule` spans |
| 3.12 | Span attributes | `agentflow_id`, `run_id`, `node_count` tags |

---

## 6. Post-Run SonarQube Verification

```bash
# Check pre-fix issues
curl -s -u admin:Abcd1234@sonar \
  "http://localhost:9000/api/issues/search?componentKeys=rengine&statuses=OPEN&ps=1" \
  | jq '.total'

# Check quality gate
curl -s -u admin:Abcd1234@sonar \
  "http://localhost:9000/api/qualitygates/project_status?projectKey=rengine" \
  | jq '.projectStatus.status'

# Verify PR is on GitHub
gh pr list --repo wl4g/rengine --head security-bot/fix-*
```

---

## 7. Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| Pods stuck in `ContainerCreating` | Image not imported to K3s | `sudo k3s ctr images import` |
| JM logs: `FATAL: MQTT broker not configured` | EMQX broker empty | `--set emqx.broker=tcp://<host>:1883` |
| Tasks all FAILED: `agent not found` | Agent defs not registered | Load from `examples/agents/` into PG |
| Tasks all FAILED: `MCP client not found` | MCP binary missing or config wrong | Check `mcps:` section in flowgent.yaml |
| `task_runs` table empty | `SaveExecutionPlan` not called | PG reachable? Check JM logs |
| SonarQube 401 | Wrong token | Verify `SONARQUBE_TOKEN` env var |
| GitHub MCP 401 | PAT expired or wrong scope | Regenerate PAT with `repo` + `pull_requests` |
| Webhook not received | Firewall / DNS / wrong URL | Check apiserver logs for webhook requests |
