# Flowgent E2E Test Guide — Full Production Mode

**Date:** 2026-05-22
**Status:** Helm chart + IDiscoveryClient + Notification queue consumer + Distributed test specs

---

## 1. Prerequisites

### Infrastructure
| Service | Address | Auth |
|---------|---------|------|
| PostgreSQL | 172.29.235.101:5432 | test/test, db=flowgent |
| EMQX MQTT | 172.29.235.101:1883 | anonymous |
| Redis Cluster | 127.0.0.1:6379-6381 | password=bitnami |
| SonarQube | localhost:9000 | admin (token required for scanner) |

### LLM Providers
| Provider | Endpoint | Model | Key Env |
|----------|----------|-------|---------|
| Bailian (Aliyun) | dashscope.aliyuncs.com/compatible-mode/v1/chat/completions | qwen-plus | BAILIAN_API_KEY |
| DeepSeek | api.deepseek.com/v1/chat/completions | deepseek-chat | ANTHROPIC_AUTH_TOKEN |

### MCP Binaries (in `/bin/`)
- `/bin/test-mcp` — Test MCP (echo, run_integration, get_report)
- `/bin/github-mcp` — GitHub API (GH_TOKEN)
- `/bin/sonarqube-mcp` — SonarQube API
- `/bin/sonatypeiq-mcp` — Sonatype IQ API
- `/bin/nexus3-mcp` — Sonatype Nexus3 API

---

## 2. Session Mode (PG + MQTT + Redis — Shared Cluster)

Full distributed deployment on k3s:

```bash
cd /home/agent/flowgent

# Ensure PG schema is current
/usr/local/go/bin/go run -mod=mod scripts/init_pg.go

# Start daemon with production config
./bin/flowgent daemon start -c etc/flowgent-prod.yaml &

# Health check
curl http://localhost:9999/_/healthz

# List available flows
curl http://localhost:9999/api/v1/default/agentflows | python3 -m json.tool

# Trigger 01-sample-security-autonomy-fix-v2 (agent-only, works without SonarQube)
curl -X POST http://localhost:9999/api/v1/default/agentflows/trigger \
  -H 'Content-Type: application/json' \
  -d '{"agentflow_id":"01-sample-security-autonomy-fix-v2","vars":{"repo":"rengine","repo_path":"/home/agent/rengine"},"trigger":{"type":"api","source":"k3s-e2e"}}'

# Monitor execution (takes ~2-5 min for 11 nodes)
RUN_ID="<from-response>"
for i in $(seq 1 60); do
    sleep 5
    STATUS=$(curl -s "http://localhost:9999/api/v1/default/runs/$RUN_ID" | python3 -c "import sys,json; print(json.load(sys.stdin)['status'])")
    echo "[$i] $STATUS"
    [ "$STATUS" = "COMPLETED" ] && break
done
```

---

## 4. Triggering the Full Security-Autonomy-Fixer

The full pipeline (21 nodes, 25 edges) requires SonarQube/Sonatype MCP tools.

### 4.1 SonarQube Scan (Manual Simulation)

Since SonarQube external access is not yet configured, the PR-triggered scan
is simulated via CLI:

```bash
cd ~/rengine

# Run SonarQube scan (requires auth token)
sonar-scanner \
  -Dsonar.host.url=http://localhost:9000 \
  -Dsonar.projectKey=rengine \
  -Dsonar.sources=. \
  -Dsonar.java.binaries="*/target/classes" \
  -Dsonar.token="<SONARQUBE_TOKEN>"

# NOTE: SonarQube admin password was changed from default.
# To reset: podman exec sonarqube ... (see SonarQube docs)
# Without a token, flow nodes that call sonarqube MCP will fail.
# Use the simplified flow (01-sample-security-autonomy-fix-v2) instead.
```

### 4.2 Trigger Full Flow (when MCP tools are available)

```bash
curl -X POST http://localhost:9999/api/v1/default/agentflows/trigger \
  -H 'Content-Type: application/json' \
  -d '{"agentflow_id":"security-autonomy-fixer","vars":{"repos":["rengine"]},"trigger":{"type":"webhook","source":"github","payload":{"repo":"rengine","pr_number":42}}}'
```

---

## 5. Flow Versions

| File | ID | Nodes | Description |
|------|-----|-------|-------------|
| `etc/flows/01-sample-security-autonomy-fix-v1.yaml` | `security-autonomy-fixer` | 21 | **Full original** — 11 phases. Requires SonarQube/Sonatype MCP tools. |
| `etc/flows/01-sample-security-autonomy-fix-v2.yaml` | `security-autonomy-fixer-simple` | 11 | **Simplified** — agent-only + `.cyberbot` metadata. Works with LLM only. |
| `etc/flows/02-sample-autotest-generation-v1.yaml` | `autotest-generation` | 9 | **Auto-test generation** — Confluence-driven test code generation. |

---

## 6. `.cyberbot` Metadata

The simplified flow includes a `generate-cyberbot` node that produces metadata JSON:

```json
{
  "version": "1.0",
  "project": "rengine",
  "run_id": "<flowgent-run-id>",
  "timestamp": "<ISO8601>",
  "fixes": [{"issue_id":"...","file":"...","severity":"...","status":"patched|reviewed|merged"}],
  "reviewers": [{"name":"...","decision":true|false,"confidence":0-1}],
  "vote_outcome": true|false,
  "status": "completed|needs_review|rejected"
}
```

This file should be committed to the target repo as `.cyberbot` for audit trail.

---

## 7. K3s Full Production Mode Deployment

Deploy all 5 components: Controller, API Server, JobManager, TaskManager, and infrastructure.

### 7.1 Build & Import Image

```bash
cd /home/agent/flowgent
/usr/local/go1.26.1.linux-amd64/bin/go build -o bin/flowgent ./src/cmd/flowgent/
podman build -t localhost/flowgent:latest -f deploy/Dockerfile.jobmanager .
sudo k3s ctr images import /path/to/image.tar
```

### 7.2 Infrastructure (PG, EMQX, Redis)

```bash
# PostgreSQL (required for Controller + Standard mode)
kubectl create secret generic flowgent-pg \
  --from-literal=url="postgres://test:test@172.29.235.101:5432/flowgent?sslmode=disable"

# EMQX MQTT (for JM↔TM dispatch)
kubectl apply -f deploy/emqx/docker-compose.yml  # or deploy via K8s

# Redis Cluster (for distributed cache/lock)
kubectl apply -f deploy/redis/docker-compose.yml  # or deploy via K8s
```

### 7.3 Deploy All Components

**One-shot deploy** (all 6 services):

```bash
kubectl apply -f deploy/kubernetes/flowgent-e2e-production.yaml
```

**Verify all pods running:**

```bash
kubectl get pods -l 'app in (flowgent-apiserver,flowgent-controller,flowgent-jobmanager,flowgent-taskmanager,flowgent-wallet,flowgent-notification)'
```

Expected:
```
NAME                                    READY   STATUS    RESTARTS   AGE
flowgent-apiserver-xxx                  1/1     Running   0          30s
flowgent-controller-xxx                 1/1     Running   0          30s
flowgent-controller-yyy                 1/1     Running   0          30s
flowgent-controller-zzz                 1/1     Running   0          30s
flowgent-jobmanager-xxx                 1/1     Running   0          30s
flowgent-taskmanager-xxx                1/1     Running   0          30s
flowgent-taskmanager-yyy                1/1     Running   0          30s
flowgent-taskmanager-zzz                1/1     Running   0          30s
flowgent-taskmanager-www                1/1     Running   0          30s
flowgent-wallet-xxx                     1/1     Running   0          30s
flowgent-notification-xxx               1/1     Running   0          30s
```

**Individual component deploy** (if not using the all-in-one manifest):

```bash
# API Server (multi-tenant REST + A2A gateway)
kubectl apply -f deploy/kubernetes/flowgent-deployment.yaml

# Controller (sharded flow driver — polls PG, dispatches flows)
kubectl apply -f deploy/kubernetes/flowgent-controller.yaml

# JobManager (session + application mode — same binary, namespace-filtered)
kubectl apply -f deploy/kubernetes/  # uses flowgent-e2e-production.yaml JM section

# TaskManager (elastic worker pool, scale as needed)
kubectl scale deploy/flowgent-taskmanager --replicas=4

# Wallet (x402 payment signing)
kubectl apply -f deploy/kubernetes/wallet-deployment.yaml

# Notification (WS push + multi-channel)
# Included in flowgent-e2e-production.yaml
```

### 7.4 Application Mode (Dedicated Cluster per VIP Flow)

For grade-priority flows, the Controller auto-creates dedicated JM+TM:

```bash
# Create application namespace
kubectl create namespace flowgent-rengine

# The Controller creates JM deployment automatically when a grade flow is found
# Manual deploy (if needed):
kubectl apply -f deploy/kubernetes/flowgent-deployment.yaml -n flowgent-rengine
kubectl scale deploy/flowgent-taskmanager --replicas=4 -n flowgent-rengine
```

The existing k3s deployment (session mode, JM+TM in `default` namespace) handles
all non-application flows via the shared pool.

---

## 8. Known Issues & Workarounds

1. **SonarQube auth**: Admin password changed from default. Generate a token via
   SonarQube UI or reset via podman exec. Until resolved, use simplified flow.

2. **Sonatype IQ/Nexus3**: External enterprise services not available in test env.
   Full flow nodes referencing these tools will fail.

3. **Docker Hub blocked**: Cannot build new k3s images. Use host-based daemon
   for testing until network access is restored.

4. **Supervisor action empty**: LLM may return JSON without `action` field.
   Code now defaults to `"continue"` as defensive fallback. If using Bailian,
   ensure account has sufficient balance.

5. **ALL_PROXY env**: Set to `socks5h://127.0.0.1:1080`. Use `unset ALL_PROXY`
   or `curl --noproxy '*'` for local API calls.

6. **TopK bug (fixed)**: Previously `topk: 5` in model config would override
   temperature to 5.0, causing API error `'temperature' must be Float`.
   Fixed in `src/llm/llm.go` — TopK is now set as separate `top_k` field.

---

## 9. Full Production Mode Verification Checklist

All 6 microservices must be running in K3s with PG/EMQX/Redis backend.

### 9.1 Pod Health

| # | Service | Verify |
|---|---------|--------|
| 1 | API Server (REST) | `curl http://<apiserver-svc>:9999/_/healthz` → `{"status":"ok"}` |
| 2 | API Server (A2A) | `curl http://<apiserver-svc>:9992/.well-known/agent.json` → agent card JSON |
| 3 | Controller | `kubectl logs deploy/flowgent-controller` → `Controller starting ... shard=X/N` |
| 4 | JobManager (session) | `kubectl logs deploy/flowgent-jobmanager` → `JobManager started` |
| 5 | TaskManager | `kubectl logs deploy/flowgent-taskmanager` → `TaskManager ... started` |
| 6 | Wallet | `curl http://<wallet-svc>:9901/health` → 200 |
| 7 | Notification | `kubectl logs deploy/flowgent-notification` → `Notification service started` |

### 9.2 Flow Execution E2E

- [ ] `curl http://<apiserver-svc>:9999/api/v1/default/agentflows` → lists flows (static + DB)
- [ ] Insert flow into PG: `INSERT INTO agentflow_definitions (agentflow_id, version, definition) VALUES ('e2e-prod-test', 1, '{"id":"e2e-prod-test",...}'::jsonb)`
- [ ] Controller picks up flow within poll interval → `kubectl logs deploy/flowgent-controller | grep "dispatching flow"`
- [ ] JM's runPoller picks up pending run → `kubectl logs deploy/flowgent-jobmanager | grep "jobmanager submit"`
- [ ] TaskManager executes → `kubectl logs deploy/flowgent-taskmanager | grep "ExecutePlan"`
- [ ] Run reaches COMPLETED → `SELECT status FROM agentflow_runs WHERE agentflow_id='e2e-prod-test'`

### 9.3 Application Mode (Grade Priority)

- [ ] Insert grade-priority flow into PG
- [ ] Controller creates dedicated JM deployment → `kubectl get deploy flowgent-jm-<flow-id>`
- [ ] Dedicated JM starts with `FLOWGENT_NAMESPACE=<ns>` → runPoller only processes runs in that namespace
- [ ] Run reaches COMPLETED → `SELECT status FROM agentflow_runs WHERE namespace='flowgent-<flow-id>'`

### 9.4 Infrastructure

- [ ] PostgreSQL `agentflow_definitions` + `agentflow_runs` tables exist and are writable
- [ ] MQTT/EMQX accessible at `:1883` (mqtt) + `:18083` (dashboard)
- [ ] Redis cluster `redis-cli -a bitnami cluster info` → `cluster_state:ok`

---

## 10. AgentFlow Loading Modes: Static YAML vs Dynamic DB (JSON)

Flowgent supports two agentflow loading paths, controlled by `orchestration.agentflows` config:

### Static Mode (YAML — File System)

```yaml
orchestration:
  agentflows:
    static:
      enabled: true
      load-dir: "examples/flows/"    # relative to config file
      refresh: 1m                    # hot-reload interval
```

- **Format**: YAML files loaded via `yaml.Unmarshal` → `model.AgentFlowSpec`
- **Storage**: Files on disk in the configured `load-dir`
- **Use case**: GitOps / manifests committed alongside code (like K8s static pod YAML)
- **Hot reload**: `LoadAgentFlows()` is called periodically at the `refresh` interval

### Standard Mode (JSON — PostgreSQL)

```yaml
orchestration:
  agentflows:
    standard:
      enabled: true   # reads from agentflow_definitions table
```

- **Format**: JSON stored in `agentflow_definitions.definition` (JSONB column) via `json.Marshal` → `json.Unmarshal`
- **Storage**: PostgreSQL table `agentflow_definitions` with versioning `(agentflow_id, version)` composite key
- **Use case**: Future Flowgent UI writes flow definitions; engine reads them at startup
- **Schema** (from `migration/postgres/20250926/01_init.ddl.sql`):

```sql
CREATE TABLE agentflow_definitions (
    agentflow_id VARCHAR(255) NOT NULL,
    version      BIGINT NOT NULL DEFAULT 1,
    definition   JSONB NOT NULL,
    created_by   VARCHAR(255),
    comment      TEXT,
    priority     VARCHAR(16) DEFAULT 'medium',
    tenant_id    VARCHAR(255) DEFAULT 'default',
    namespace    VARCHAR(255) DEFAULT '',
    mode         VARCHAR(32) DEFAULT '',
    labels       JSONB DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (agentflow_id, version)
);
```

### Dual Format — Same Struct

Both modes share the same `model.AgentFlowSpec` struct, which carries both tags:

```go
type AgentFlowSpec struct {
    ID          string         `json:"id" yaml:"id"`
    Nodes       []Node         `json:"nodes" yaml:"nodes"`
    Edges       []Edge         `json:"edges" yaml:"edges"`
    // ... all fields dual-tagged json: + yaml:
}
```

### Startup Merge Flow

In `launch.go → startServer()`:

1. Static YAML loaded via `config.LoadAgentFlows(serviceCfg, cfgPath)`
2. Store initialized (PG or SQLite)
3. If `standard.enabled`: DB definitions loaded via `loadAgentFlowsFromDB(ctx, storeImpl)`, which calls `store.ListAgentFlowDefinitions()`, deduplicates by `agentflow_id` (latest version wins), and unmarshals JSON
4. Merged list passed to `api.NewAgentFlowHandler()`
5. Same for agents: static YAML loaded first, then `store.ListAgents()` merged if `standard.enabled`

### Store Layer (PG CRUD)

- `UpdateAgentFlowSpec(ctx, spec, createdBy, comment)` — `json.Marshal` + INSERT with auto-increment version
- `GetAgentFlowSpec(ctx, agentFlowID)` — `json.Unmarshal` latest version
- `DeleteAgentFlowDefinition(ctx, agentFlowID)` — DELETE all versions
- Agents: `SaveAgent()`, `GetAgent()`, `ListAgents()`, `DeleteAgent()`

Full CRUD is implemented in both `PostgresStore` and `SQLiteStore` (`src/store/store_agents.go`).

---

## 11. Distributed Mode Testing Requirements (Next Iteration)

Tests that verify distributed behavior across multiple pods with real
infrastructure (PG, EMQX, Redis, K3s).

### 11.1 Test Matrix

| # | Test | Components | Replicas | Verifies |
|---|------|-----------|----------|----------|
| T1 | JM HA Leader Election | JM × 2 | 2 | K8s discovery, leader election, standby takeover |
| T2 | Controller Hash-Mod Sharding | Controller × 3 | 3 | hash(flow_id) % N distribution, no overlap, no gaps |
| T3 | TM Distributed Plan Execution | TM × 4 | 4 | MQTT dispatch, slot allocation, lease claiming |
| T4 | Notification Queue Load-Balancing | Notification × 2 | 2 | MQTT shared subscription, dedup, channel delivery |
| T5 | Full End-to-End (All Services) | All 6 | 2 each | Complete flow: UI→API→PG→Controller→JM→TM→Notification |
| T6 | Scale-Up/Down Rebalance | Controller × 3→2 | 3→2 | Shard redistribution on pod removal |

### 11.2 T1: JM HA Leader Election

**Setup:**
```bash
helm install flowgent ./deploy/helm/flowgent --set jobmanager.replicas=2 --set jobmanager.ha.enabled=true
```

**Test steps:**
1. Verify both JM pods Running: `kubectl get pods -l app.kubernetes.io/component=jobmanager`
2. Verify only ONE JM is leader: check logs for `leader elected: true`
3. Kill leader pod: `kubectl delete pod <leader-jm>`
4. Verify standby takes over within `leaseDuration` (30s by default)
5. Verify no runs are lost during failover (check `agentflow_runs` table)

**Expected:**
- Only one JM runs runPoller at any time
- Leader election uses `IDiscoveryClient.IsLeader()` → lexicographic name ordering
- Standby JM polls `agentflow_runs` but skips if not leader
- Failover time < leaseDuration + 2×pollInterval

### 11.3 T2: Controller Hash-Mod Sharding

**Setup:**
```bash
# Insert test flows with known IDs into PG
for i in $(seq 1 20); do
  psql -c "INSERT INTO agentflow_definitions (agentflow_id, version, definition)
    VALUES ('test-flow-$i', 1, '{\"id\":\"test-flow-$i\",\"nodes\":[...]}'::jsonb)"
done

helm install flowgent ./deploy/helm/flowgent --set controller.replicas=3
```

**Test steps:**
1. Wait for all 3 controller pods to discover each other
2. Check each pod's log for `shard=X/3` message
3. Verify each pod only dispatches flows in its shard:
   - Pod 0: `hash(flow_id) % 3 == 0`
   - Pod 1: `hash(flow_id) % 3 == 1`
   - Pod 2: `hash(flow_id) % 3 == 2`
4. Verify NO flow is dispatched by two pods (no overlap)
5. Verify ALL 20 flows are dispatched (no gaps)

**Expected:**
- Flows partitioned uniformly (±1) across controller pods
- No duplicate dispatches (each flow handled by exactly one pod)
- Discovery rebalances on scale events (within pollInterval)

### 11.4 T3: TM Distributed Plan Execution

**Setup:**
```bash
helm install flowgent ./deploy/helm/flowgent --set taskmanager.replicas=4 --set taskmanager.slots=4
```

**Test steps:**
1. Submit a flow with 16 map items (parallel tasks)
2. Verify all 4 TMs receive execution plans via MQTT
3. Check each TM's slot utilization: `kubectl logs <tm-pod> | grep "slot"`
4. Verify execution plans are evenly distributed
5. Kill one TM pod mid-execution
6. Verify remaining TMs pick up orphaned plans (lease expiry)

**Expected:**
- 4 TMs × 4 slots = 16 concurrent executions
- MQTT topics per TM: `flowgent/exec/tm/{tmID}`
- Lease-based plan claiming (PG `claim_lease`)
- Orphaned plans automatically re-claimed after lease timeout

### 11.5 T4: Notification Queue Load-Balancing

**Setup:**
```bash
helm install flowgent ./deploy/helm/flowgent --set notification.replicas=2
```

**Test steps:**
1. Publish 10 notification messages to `/flowgent/notify/queue/default/test-flow`
2. Verify each notification pod receives ~5 messages (load-balanced)
3. Check external channel delivery logs
4. Verify no duplicate deliveries

**Expected:**
- MQTT shared subscription distributes messages across pods
- Each message processed exactly once
- Channel delivery logged per message

### 11.6 T5: Full End-to-End

**Setup:**
```bash
helm install flowgent ./deploy/helm/flowgent \
  --set apiserver.replicas=2 \
  --set controller.replicas=2 \
  --set jobmanager.replicas=2 \
  --set taskmanager.replicas=2 \
  --set wallet.replicas=2 \
  --set notification.replicas=2
```

**Test steps:**
1. Build and import image to K3s
2. Deploy all 6 services via Helm (12 pods total)
3. Verify all pods Running: `kubectl get pods | grep flowgent | wc -l` == 12
4. Insert test flow via API: `curl -X POST http://<apiserver>/api/v1/default/agentflows/definitions ...`
5. Controller picks up flow → creates pending run
6. JM runPoller picks up run → dispatches to TM
7. TM executes plans → run COMPLETED
8. Notification service detects completion → posts to configured channels

**Expected:**
- All 6 services discover peers via IDiscoveryClient (K8s label selector)
- Wallet auto-generates key, printed in Helm NOTES.txt
- Flow completes end-to-end within timeout
- PG has `agentflow_runs` record with status=COMPLETED
- Notification queue messages consumed and dispatched

### 11.7 Infrastructure Requirements

| Service | Version | Access |
|---------|---------|--------|
| PostgreSQL | 16+ | `172.29.235.101:5432`, test/test, db=flowgent |
| EMQX MQTT | 5.5+ | `172.29.235.101:1883` (mqtt), `:18083` (dashboard) |
| Redis Cluster | 7.0+ | `127.0.0.1:6379-6381`, password=bitnami |
| K3s | 1.35+ | `kubectl` access to cluster |
| Go | 1.26+ | `CGO_ENABLED=0 go build` (static binary) |
