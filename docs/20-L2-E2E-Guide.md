# Flowgent E2E Test Guide — Full Distributed Mode on K3s

**Date:** 2026-05-23
**Status:** 7 microservices (apiserver, controller, jobmanager, taskmanager, sandbox, wallet, notification) — Helm + K3s

---

## 1. Architecture Summary

Flowgent runs as 7 microservices on K3s. All are deployed via a single Helm chart.

```
kubectl get pods -l 'app.kubernetes.io/name=flowgent'
```

Expected (session mode, 2 replicas per service, 14 pods total):

```
NAME                                    READY   STATUS    RESTARTS   AGE
flowgent-apiserver-xxx                  1/1     Running   0          30s
flowgent-apiserver-yyy                  1/1     Running   0          30s
flowgent-controller-xxx                 1/1     Running   0          30s
flowgent-controller-yyy                 1/1     Running   0          30s
flowgent-jobmanager-xxx                 1/1     Running   0          30s
flowgent-jobmanager-yyy                 1/1     Running   0          30s
flowgent-taskmanager-xxx                1/1     Running   0          30s
flowgent-taskmanager-yyy                1/1     Running   0          30s
flowgent-sandbox-xxx                    1/1     Running   0          30s
flowgent-sandbox-yyy                    1/1     Running   0          30s
flowgent-wallet-xxx                     1/1     Running   0          30s
flowgent-wallet-yyy                     1/1     Running   0          30s
flowgent-notification-xxx               1/1     Running   0          30s
flowgent-notification-yyy               1/1     Running   0          30s
```

### 1.1 Component Responsibilities

| # | Service | Port | Role |
|---|---------|------|------|
| 1 | **API Server** | 9999 (REST), 9992 (A2A), 9991 (mgmt) | Multi-tenant gateway: CRUD, auth, triggers, A2A agent card |
| 2 | **Controller** | — | Polls PG `agentflow_definitions`, hash-mod sharding, inserts PENDING runs, creates K8s JM for application mode |
| 3 | **JobManager** | — | Polls `agentflow_runs` (PENDING), parses flow JSON, builds DAG + ExecutionPlans, calls RM.Schedule() |
| 4 | **TaskManager** | — | Consumes ExecutionPlans from MQTT, executes via router (12 node types) |
| 5 | **Sandbox** | — | Consumes sandbox ExecutionPlans from queue, executes scripts in isolated env with network restrictions |
| 6 | **Wallet** | 9901 | x402 Ed25519 key management, payment signing |
| 7 | **Notifier** | 9993 (WS) | Multi-channel push (Telegram, Slack, DingTalk, Email, Webhook) + WS SSE |

---

## 2. Prerequisites

### 2.1 Infrastructure

| Service | Address | Auth |
|---------|---------|------|
| PostgreSQL | 172.29.235.101:5432 | flowgent/flowgent, db=flowgent |
| EMQX MQTT | 172.29.235.101:1883 | anonymous |
| Redis Cluster | 127.0.0.1:6379-6381 | password=bitnami |
| K3s | — | `kubectl` access |

### 2.2 LLM Providers

| Provider | Key Env | Endpoint |
|----------|---------|----------|
| Bailian (Aliyun) | `BAILIAN_API_KEY` | dashscope.aliyuncs.com/compatible-mode/v1/chat/completions |
| DeepSeek | `DEEPSEEK_API_KEY` | api.deepseek.com/v1/chat/completions |

### 2.3 MCP Binaries (optional, in `/bin/`)

- `/bin/test-mcp` — Test MCP (echo, run_integration, get_report)
- `/bin/github-mcp` — GitHub API
- `/bin/sonarqube-mcp` — SonarQube API
- `/bin/sonatypeiq-mcp` — Sonatype IQ API
- `/bin/nexus3-mcp` — Sonatype Nexus3 API

---

## 3. Build & Deploy

### 3.1 Build Binary & Image

```bash
cd /home/agent/flowgent
go build -o bin/flowgent ./src/cmd/flowgent/

# Build Docker image
podman build -t localhost/flowgent:latest -f deploy/docker/Dockerfile .

# Import into K3s
sudo k3s ctr images import /path/to/flowgent-image.tar
```

### 3.2 Infrastructure Secrets

```bash
kubectl create secret generic flowgent-pg \
  --from-literal=url="postgres://flowgent:flowgent@172.29.235.101:5432/flowgent?sslmode=disable"

kubectl create secret generic flowgent-mqtt \
  --from-literal=broker="tcp://172.29.235.101:1883"
```

### 3.3 Deploy All Components

```bash
helm install flowgent deploy/helm/flowgent \
  --set global.image.repository=localhost/flowgent \
  --set global.image.tag=latest \
  --set postgresql.host=172.29.235.101 \
  --set postgresql.password=flowgent \
  --set emqx.broker=tcp://172.29.235.101:1883
```

### 3.4 Verify Deployment

```bash
# All 7 components (14 pods with replicas=2)
kubectl get pods -l 'app.kubernetes.io/name=flowgent'

# Component-level verification:
kubectl get deploy -l 'app.kubernetes.io/name=flowgent'
# Expected: apiserver, controller, jobmanager, taskmanager, sandbox, wallet, notification
```

---

## 4. Session Mode — E2E Flow Execution

### 4.1 Trigger a Flow

```bash
# List available flows (loaded from examples/flows/)
curl http://<apiserver-svc>:9999/api/v1/default/agentflows | python3 -m json.tool

# Trigger the simplified security fixer (works with LLM only, no MCP tools needed)
curl -X POST http://<apiserver-svc>:9999/api/v1/default/agentflows/trigger \
  -H 'Content-Type: application/json' \
  -d '{"agentflow_id":"01-security-autonomy-fix-v2","vars":{"repo":"rengine"},"trigger":{"type":"api","source":"e2e-test"}}'
# Response: {"run_id":"<RUN_ID>","status":"PENDING"}
```

### 4.2 Monitor Execution

```bash
RUN_ID="<from-response>"
for i in $(seq 1 60); do
    sleep 5
    STATUS=$(curl -s "http://<apiserver-svc>:9999/api/v1/default/runs/$RUN_ID" | python3 -c "import sys,json; print(json.load(sys.stdin)['status'])")
    echo "[$i] $STATUS"
    [ "$STATUS" = "COMPLETED" ] || [ "$STATUS" = "FAILED" ] && break
done
```

### 4.3 Trace Execution in Logs

```bash
# Controller: should show flow dispatch
kubectl logs -l app.kubernetes.io/component=controller | grep "dispatching"

# JobManager: should show run submission
kubectl logs -l app.kubernetes.io/component=jobmanager | grep "Submit\|JobMaster"

# TaskManager: should show plan execution
kubectl logs -l app.kubernetes.io/component=taskmanager | grep "ExecutePlan\|slot"
```

---

## 5. Application Mode — Dedicated Cluster per VIP Flow

For `priority: grade` flows, the Controller creates a dedicated K8s JM Deployment.

```bash
# Create application namespace
kubectl create namespace flowgent-rengine

# Insert a grade-priority flow definition into PG
psql -h 172.29.235.101 -U flowgent -d flowgent -c "
INSERT INTO agentflow_definitions (agentflow_id, version, definition, priority, namespace)
VALUES ('vip-security-fixer', 1,
  '{\"id\":\"vip-security-fixer\",\"nodes\":[...],\"edges\":[...]}'::jsonb,
  'grade', 'flowgent-rengine')
"

# Controller detects grade priority → creates JM Deployment automatically
kubectl get deploy -n flowgent-rengine flowgent-jm-vip-security-fixer
# Expected: 1 JM pod running in the tenant namespace

# JM picks up PENDING run → dispatches to TM → COMPLETED
```

---

## 6. Flow Versions

Flow definitions live under `examples/flows/`. Use case details: see `docs/10-USE-CASES.md`.

| File | ID | Nodes | Requires |
|------|-----|-------|----------|
| `examples/flows/01-security-autonomy-fix-v1.yaml` | `security-autonomy-fixer` | 21 | SonarQube/Sonatype MCP tools |
| `examples/flows/01-security-autonomy-fix-v2.yaml` | `01-security-autonomy-fix-v2` | 11 | LLM only |
| `examples/flows/00-e2e-sonarqube-real.yaml` | `e2e-sonarqube-real` | — | SonarQube MCP |
| `examples/flows/20-autotest-generation-v1.yaml` | `autotest-generation` | 9 | Confluence MCP |

---

## 7. Sandbox E2E — Script Execution

### 7.1 Sandbox Node in a Flow

```yaml
nodes:
  - id: run-audit
    type: sandbox
    runtime: python3
    script: |
      import json
      print(json.dumps({"status": "ok", "findings": []}))
    timeout: 30s
    resources:
      cpu: "250m"
      memory: "128Mi"
    network_policy:
      mode: none
```

### 7.2 Verify Sandbox Execution

```bash
# Sandbox worker log
kubectl logs -l app.kubernetes.io/component=sandbox | grep "sandbox"

# TaskManager log (sandbox plan dispatch)
kubectl logs -l app.kubernetes.io/component=taskmanager | grep "sandbox"

# PG check
psql -h 172.29.235.101 -U flowgent -d flowgent -c \
  "SELECT plan_id, task_type, state FROM execution_plans WHERE task_type='sandbox'"
```

---

## 8. Per-Component Verification Checklist

| # | Service | Command | Expected |
|---|---------|---------|----------|
| 1 | API Server | `curl http://<svc>:9999/_/healthz` | `{"status":"ok"}` |
| 2 | API Server (A2A) | `curl http://<svc>:9992/.well-known/agent.json` | Agent card JSON |
| 3 | Controller | `kubectl logs -l app.kubernetes.io/component=controller` | `shard=X/N, dispatching flow` |
| 4 | JobManager | `kubectl logs -l app.kubernetes.io/component=jobmanager` | `JobManager started` |
| 5 | TaskManager | `kubectl logs -l app.kubernetes.io/component=taskmanager` | `task manager started, slots=N` |
| 6 | Sandbox | `kubectl logs -l app.kubernetes.io/component=sandbox` | `Sandbox worker started` |
| 7 | Wallet | `curl http://<svc>:9901/health` | `200` |
| 8 | Notifier | `kubectl logs -l app.kubernetes.io/component=notification` | `Notifier service started` |

---

## 9. Infrastructure Verification

```bash
# PostgreSQL
psql -h 172.29.235.101 -U flowgent -d flowgent -c "\dt"
# Expected: agentflow_definitions, agentflow_runs, execution_plans, task_runs, ...

# EMQX
curl http://172.29.235.101:18083/api/v5/status

# Redis
redis-cli -h 127.0.0.1 -p 6379 -a bitnami ping
```

---

## 10. AgentFlow Loading: Static YAML vs Dynamic DB

### Static Mode (YAML files)

```yaml
orchestration:
  agentflows:
    static:
      enabled: true
      load-dir: "examples/flows/"
      refresh: 1m
```

### Standard Mode (PostgreSQL)

```yaml
orchestration:
  agentflows:
    standard:
      enabled: true
```

Schema:

```sql
CREATE TABLE agentflow_definitions (
    agentflow_id VARCHAR(255) NOT NULL,
    version      BIGINT NOT NULL DEFAULT 1,
    definition   JSONB NOT NULL,
    priority     VARCHAR(16) DEFAULT 'medium',
    tenant_id    VARCHAR(255) DEFAULT 'default',
    namespace    VARCHAR(255) DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (agentflow_id, version)
);
```

Both modes share `model.AgentFlowSpec` (dual-tagged `json:` + `yaml:`). At startup, static YAML is loaded first, then DB definitions are merged (latest version per `agentflow_id` wins).

---

## 11. Scenarios to Test (Next Iteration)

| # | Test | Components | What It Verifies |
|---|------|-----------|------------------|
| T1 | JM HA Leader Election | JM × 2 | Leader election via IDiscoveryClient, standby takeover within 30s |
| T2 | Controller Hash-Mod Sharding | Controller × 3 | hash(flow_id) % N partition, no overlap, no gaps |
| T3 | TM Distributed Plan Execution | TM × 4 | MQTT dispatch, slot allocation, lease claiming, orphan re-claim |
| T4 | Sandbox Script Execution | Sandbox × 2 + TM | Queue dispatch, policy enforcement, result collection |
| T5 | Notifier Load-Balancing | Notifier × 2 | MQTT shared subscription, dedup, channel delivery |
| T6 | Application Mode | Controller + K8s | Auto-create JM Deployment on grade-priority flow, cleanup on completion |
| T7 | Full E2E | All 7 services | Trigger → Controller → JM → RM → TM/Sandbox → Notifier |

---

## 12. Real E2E: Security Autonomy Fixer V3 — Iterative Fix Loop

This is the core E2E scenario: deploy Flowgent on K3s, let the engine discover
Rengine's SonarQube issues, fix them, re-scan, and iterate until resolved.

### 12.1 Architecture

```
SonarQube (localhost:9000, rengine project, 700+ issues)
     │
     ▼ fetch issues (MCP tool)
┌─────────────┐     ┌──────────┐     ┌──────────┐     ┌──────────────┐
│ analyze     │────→│ generate │────→│ review   │────→│ vote         │
│ issues      │     │ fixes    │     │ (3 agents)│    │ (majority)   │
└─────────────┘     └──────────┘     └──────────┘     └──────┬───────┘
                                                             │
                              ┌──────────────────────────────┘
                              ▼
                       ┌────────────┐     ┌──────────────┐
                       │ supervisor │────→│ is-approved? │
                       └────────────┘     └──┬──────┬────┘
                                        true│      │false
                                            │      └──→ back to generate-fixes
                                            ▼
                                     ┌──────────────┐
                                     │ commit       │
                                     │ patches      │
                                     └──────┬───────┘
                                            │
                                            ▼
                                     ┌──────────────┐
                                     │ trigger      │
                                     │ SonarQube    │
                                     │ re-analysis  │
                                     └──────┬───────┘
                                            │
                                            ▼
                                     ┌──────────────┐
                                     │ wait/poll    │ ← sandbox (bash,
                                     │ CE task      │   allowlist network)
                                     └──────┬───────┘
                                            │
                                            ▼
                                     ┌──────────────┐
                                     │ check        │
                                     │ resolved?    │
                                     └──┬──────┬────┘
                               resolved │      │ not resolved
                                        │      │ (iteration < max)
                                        ▼      ▼
                                   ┌─────────┐  ┌──────────────┐
                                   │ human   │  │ back to       │
                                   │ approval│  │ generate-fixes│
                                   └────┬────┘  └──────────────┘
                                        ▼
                                   ┌─────────┐
                                   │ report  │
                                   └─────────┘
```

### 12.2 Prerequisites

```bash
# 1. SonarQube is running with rengine project analyzed
curl -s --noproxy '*' http://localhost:9000/api/system/status
# {"status":"UP"}

# 2. Verify Rengine has analyzable issues
curl -s --noproxy '*' -u "squ_eab3ac9428573619417b0b3bd90ca986e87cbd11:" \
  "http://localhost:9000/api/issues/search?projectKeys=rengine&severities=BLOCKER,CRITICAL,MAJOR&ps=5"
# {"total":700,...}

# 3. Rengine source is accessible
ls /home/agent/rengine/
# pom.xml, src/, ...

# 4. Flowgent deployed on K3s (Section 3)
helm install flowgent deploy/helm/flowgent \
  --set global.image.repository=localhost/flowgent \
  --set global.image.tag=latest \
  --set sandbox.enabled=true \
  --set sandbox.replicas=2
```

### 12.3 Deploy the V3 Flow

```bash
# Copy V3 flow to the examples flows directory (auto-loaded by static loader)
cp examples/flows/01-security-autonomy-fix-v3.yaml examples/flows/

# Or insert into PostgreSQL for Standard mode:
psql -h 172.29.235.101 -U flowgent -d flowgent <<'SQL'
INSERT INTO agentflow_definitions (agentflow_id, version, definition, priority, tenant_id)
VALUES ('security-autonomy-fixer-v3', 1,
  '{"id":"security-autonomy-fixer-v3","nodes":[...]}'::jsonb,
  'high', 'default')
ON CONFLICT (agentflow_id, version) DO NOTHING;
SQL
```

### 12.4 Trigger & Monitor

```bash
# Trigger the V3 flow
curl -X POST http://<apiserver-svc>:9999/api/v1/default/agentflows/trigger \
  -H 'Content-Type: application/json' \
  -d '{
    "agentflow_id": "security-autonomy-fixer-v3",
    "vars": {
      "repo": "rengine",
      "repo_path": "/home/agent/rengine",
      "max_iterations": 3
    },
    "trigger": {"type": "api", "source": "e2e-test"}
  }'

# Response: {"run_id":"<RUN_ID>","status":"PENDING"}
```

### 12.5 Trace Execution (per component)

```bash
RUN_ID="<from-response>"

# Phase 1-2: Controller → JM picks up run
kubectl logs -l app.kubernetes.io/component=controller | grep "$RUN_ID"
kubectl logs -l app.kubernetes.io/component=jobmanager | grep "$RUN_ID"

# Phase 3: TM executes fetch-sq-issues (SonarQube MCP call)
kubectl logs -l app.kubernetes.io/component=taskmanager | grep -A2 "fetch-sq-issues"

# Phase 4-6: Analyze → Fix → Review (agent LLM calls)
kubectl logs -l app.kubernetes.io/component=taskmanager | grep -E "analyze-issues|generate-fixes|review-"

# Phase 7-8: Commit → Re-scan → Wait
kubectl logs -l app.kubernetes.io/component=taskmanager | grep -E "commit-patches|trigger-rescan"

# Phase 9: Sandbox polls SonarQube CE task
kubectl logs -l app.kubernetes.io/component=sandbox | grep "wait-rescan"

# Phase 10-11: Check resolved → Loop or Complete
kubectl logs -l app.kubernetes.io/component=taskmanager | grep -E "check-resolved|compare-results|fix-complete"

# Track iterations via PG
psql -h 172.29.235.101 -U flowgent -d flowgent -c \
  "SELECT task_type, node_id, state, created_at FROM execution_plans
   WHERE agentflow_run_id='$RUN_ID' ORDER BY created_at"
```

### 12.6 Expected Outcomes

| Iteration | Action | Expected |
|-----------|--------|----------|
| 1 | fetch-sq-issues | Returns 700 issues, agent selects top 5 (BLOCKER/CRITICAL) |
| 1 | generate-fixes | Agent reads source files, generates patches |
| 1 | review-* (3 agents) | Each returns `{"decision":true|false,...}` |
| 1 | vote (tribunal) | Majority vote: ≥2 approve → decision=true |
| 1 | commit-patches | Applies patches to /home/agent/rengine/*.java |
| 1 | trigger-rescan | SonarQube begins re-analysis |
| 1 | wait-rescan | Sandbox polls CE task (bash, allowlist: localhost:9000) |
| 1 | check-resolved | Fetches new issue list |
| 1 | compare-results | If resolved_count > 0 → resolution=partial |
| 1 | fix-complete (false) | Loops back to generate-fixes |
| 2 | generate-fixes | Fixes remaining issues from iteration 1 |
| 2 | ... | Repeat review→vote→commit→rescan→check |
| N | compare-results | resolution=complete OR iteration=max_iterations |
| N | human-approval | Token-based async gate (25h timeout) |
| N | summary-report | Final markdown report with fix history |

### 12.7 Verify Fixes in Rengine

```bash
cd /home/agent/rengine

# Check which files were modified by the fixer
git diff --name-only

# Re-run SonarQube scanner to verify fixes persist
sonar-scanner \
  -Dsonar.host.url=http://localhost:9000 \
  -Dsonar.token=squ_eab3ac9428573619417b0b3bd90ca986e87cbd11 \
  -Dsonar.projectKey=rengine \
  -Dsonar.sources=.

# Wait for analysis, then check resolved issues
sleep 30
curl -s --noproxy '*' -u "squ_eab3ac9428573619417b0b3bd90ca986e87cbd11:" \
  "http://localhost:9000/api/issues/search?projectKeys=rengine&resolved=true&ps=10"
```

### 12.8 Flow Completion Verification

```bash
# Final run status
curl -s http://<apiserver-svc>:9999/api/v1/default/runs/$RUN_ID | python3 -c "
import sys,json
r=json.load(sys.stdin)
print(f'Status: {r[\"status\"]}')
print(f'Iterations: check execution_plans for loop count')
"

# All execution plans for this run
psql -h 172.29.235.101 -U flowgent -d flowgent -c \
  "SELECT node_id, task_type, state, retry_count, created_at, finished_at
   FROM execution_plans WHERE agentflow_run_id='$RUN_ID'
   ORDER BY created_at"

# Count iterations (number of times generate-fixes was executed)
psql -h 172.29.235.101 -U flowgent -d flowgent -t -c \
  "SELECT COUNT(*) FROM execution_plans
   WHERE agentflow_run_id='$RUN_ID' AND node_id='generate-fixes'"
```

---

## 13. Known Issues

1. **SonarQube auth**: Token required for full security-fixer flow. Without it, use simplified flow.
2. **Sonatype IQ/Nexus3**: External enterprise services unavailable in test env.
3. **Docker Hub blocked**: Build images locally, import to K3s via `ctr images import`.
4. **Sandbox network**: Default policy is `mode: none` (no egress). Scripts requiring network access need `allowlist` mode with explicit targets.
5. **Wallet key**: Auto-generated via Helm `genPrivateKey`. Printed in NOTES.txt on install.
