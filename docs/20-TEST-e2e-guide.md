# Flowgent E2E Test Guide — Security Autonomy Fixer

**Date:** 2026-05-20
**Status:** Verified (all-in-one + production modes + Standard DB loading)

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

## 2. All-in-One Mode (SQLite + Memory Queue)

Quick validation with minimal dependencies:

```bash
cd /home/agent/flowgent

# Build
/usr/local/go/bin/go build -o bin/flowgent ./src/cmd/core/

# Create test config (auto-expands env vars)
cat > etc/flowgent-e2e.yaml << 'YEOF'
# ... (copy from etc/flowgent.yaml.fully.sample, set storage.type=SQLITE, cache.provider=Memory)
YEOF

# Start daemon
./bin/flowgent daemon start -c etc/flowgent-e2e.yaml &

# Trigger e2e test flow
curl -X POST http://localhost:9999/api/v1/default/agentflows/trigger \
  -H 'Content-Type: application/json' \
  -d '{"agentflow_id":"e2e-test","trigger":{"type":"api","source":"manual"}}'

# Check result
curl http://localhost:9999/api/v1/default/runs | python3 -m json.tool
```

---

## 3. Production Mode (PG + MQTT + Redis)

Full production simulation on k3s master node:

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

```bash
# 1. Controller (sharded flow driver — polls PG, dispatches flows)
kubectl apply -f deploy/kubernetes/flowgent-controller.yaml
kubectl scale deploy/flowgent-controller --replicas=3

# 2. API Server (multi-tenant REST + A2A gateway)
kubectl apply -f deploy/kubernetes/flowgent-deployment.yaml

# 3. JobManager (standalone session JM — shared pool)
#    Started via: flowgent jobmanager start -c etc/flowgent-prod.yaml

# 4. TaskManager (elastic worker pool)
#    Started via: flowgent taskmanager start -c etc/flowgent-prod.yaml
#    Scale: kubectl scale deploy/flowgent-taskmanager --replicas=4

# 5. (Optional) Facilitator for x402 payments
kubectl apply -f deploy/kubernetes/facilitator-deployment.yaml
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

## 9. Verification Checklist

- [ ] `curl localhost:9999/_/healthz` → `{"status":"ok"}`
- [ ] `curl localhost:9999/api/v1/default/agentflows` → lists flows
- [ ] Trigger `e2e-test` → COMPLETED (all-in-one mode)
- [ ] Trigger `01-sample-security-autonomy-fix-v2` → COMPLETED (production mode)
- [ ] PG has agentflow_runs records
- [ ] MQTT/EMQX dashboard accessible at `:18083`
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
