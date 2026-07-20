# 1. Overview

## 1.1 Purpose

> **Core goal**: Verify that Flowgent's `security-autonomy-fixer` agent flow can
**autonomously** discover, analyze, fix, review, and create a PR for real
SonarQube security issues on a target repository.

> The test subject is Flowgent's `security-autonomy-fixer` workflow. This workflow must fix all issues in Rengine's sonarqube scan. You are prohibited from directly intervening in fixing any issues in the Rengine repository. The ultimate goal is that this workflow must be able to autonomously and truly fix and submit to PR#4 without any human intervention.

## 1.2 Test Environment

| Component | Value |
|-----------|-------|
| Platform | K3s single-node |
| Namespace | `default` (infra), `flowgent-{tenant}` (per-tenant JM pods) |
| Mode | Application only |
| Flow Under Test | `security-autonomy-fixer` (24 nodes, 11 phases) |
| MCP GitHub | `https://api.githubcopilot.com/mcp/` (Streamable HTTP) |
| MCP SonarQube | `http://172.29.235.101:18080/mcp` (local, configured the Upstream Bearer Auth, you can call directly.) |
| Test target PR | `https://github.com/wl4g/rengine/pull/4` (Really PR) |
| Test target branch | `fix/flowgent_sec_auto_fix` |
| Sonarqube UI | `http://172.29.235.101:9000` (local deploy) |

## 1.3 Quick Start

```bash
# Run all verification scenarios
python3 runner.py

# Run specific modules (ordered by runtime dependency)
python3 runner.py -s 01  # Infrastructure verification
python3 runner.py -s 02  # API Server CRUD
python3 runner.py -s 03  # A2A protocol validation
python3 runner.py -s 04  # Controller -> JM pod lifecycle
python3 runner.py -s 05  # Messager topics + Sandbox chain
python3 runner.py -s 06  # Notifier connectivity
python3 runner.py -s 07  # Wallet key mgmt + MQTT signing
python3 runner.py -s 08  # OTEL / Jaeger tracing
python3 runner.py -s 09  # E2E security fixer capstone
python3 runner.py -s 10  # PR commit verification

# List all scenarios
python3 runner.py -l
python3 runner.py --api http://10.0.0.1:9999 --pg postgres://u:p@h/db
```

**All scenario execution MUST go through `runner.py`.** Do not manually execute
individual `scenarios/*.py` files.

---

# 2. Pre-Execution Requirements

## 2.1 Environment Reset

Before every real (non-dry-run) execution against a live K3s cluster, reset
shared state to avoid false positives/negatives from stale Deployments, PG rows,
and MQTT messages.

```bash
# 1. Tear down Helm release
helm uninstall flowgent -n default

# 2. Delete leftover JM Deployments (all namespaces)
kubectl delete deployment -A -l flowgent.io/mode=application

# 2b. Delete per-tenant namespaces
kubectl get ns -l flowgent.io/mode=application -o name | xargs -r kubectl delete

# 3. Wipe PostgreSQL data
kubectl exec -it deploy/flowgent-postgres -n default -- \
  psql -U flowgent -d flowgent -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"

# 4. Rebuild and re-import image
make build-image-core
docker save flowgent-core:latest | sudo k3s ctr images import -

# 5. Fresh install (Application mode — no global.mode value)
helm install flowgent deploy/helm/flowgent \
  --set global.image.repository=localhost/flowgent-core \
  --set global.image.tag=latest \
  --set postgresql.enabled=false \
  --set emqx.enabled=true \
  --set redis.enabled=false \
  -n default

# 6. Wait for pods
kubectl wait --for=condition=Ready pod -l app.kubernetes.io/name=flowgent -n default --timeout=120s

# 7. Set GitHub token (required before running scenarios 10/11/12)
source ~/.bashrc && export GITHUB_TOKEN=$GH_TOKEN
```

## 2.2 Config Bootstrapping

After a clean reset (which wipes PG), the database is empty. Every E2E run must
re-seed agents and MCPs. **The GitHub MCP has a `${GITHUB_TOKEN}` placeholder
in its YAML that must be resolved from the environment before seeding.**

### 2.2.1 Set GitHub Token

The canonical token source is `GH_TOKEN` in `~/.bashrc`. Export it as
`GITHUB_TOKEN` so the seed scripts can resolve the YAML placeholder:

```bash
source ~/.bashrc
export GITHUB_TOKEN=$GH_TOKEN
# Verify: echo $GITHUB_TOKEN | head -c 10  # should show token prefix
```

### 2.2.2 Automatic Seeding (via runner.py)

Scenarios 10/11 call `seed_agents_and_mcps()` which reads YAML configs and
resolves `${GITHUB_TOKEN}` from the environment before POSTing to the API server.

| Resource | Method | Source |
|----------|--------|--------|
| MCP (github, sonarqube) | `POST /api/v1/{tenant}/mcp` | `config/mcps/*.yaml` (env vars resolved) |
| Agents (issue-detector, etc.) | `POST /api/v1/{tenant}/agents` | `config/agents/*.yaml` |
| LLM Providers | `POST /api/v1/{tenant}/llm/providers` | Manual (API key required) |

### 2.2.3 Manual Bootstrap (if not using runner.py)

```bash
source ~/.bashrc
export GITHUB_TOKEN=$GH_TOKEN

# Register GitHub MCP with resolved token
curl -s -X POST http://localhost:9999/api/v1/default/mcp \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"github\",
    \"type\": \"http\",
    \"url\": \"https://api.githubcopilot.com/mcp/\",
    \"headers\": {\"Authorization\": \"Bearer ${GITHUB_TOKEN}\"},
    \"enabled\": true
  }"

# Register SonarQube MCP (no auth config needed — upstream bearer auth)
curl -s -X POST http://localhost:9999/api/v1/default/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "name": "sonarqube",
    "type": "http",
    "url": "http://172.29.235.101:18080/mcp",
    "enabled": true
  }'

# Register agents from config/agents/*.yaml (example)
curl -s -X POST http://localhost:9999/api/v1/default/agents \
  -H "Content-Type: application/json" \
  -d @examples/security-autonomy-fixer/config/agents/issue-detector.yaml
```

### 2.2.4 Verify MCP Registration

```bash
curl -s http://localhost:9999/api/v1/default/mcp | python3 -m json.tool
# Both "github" and "sonarqube" should show enabled=true
# GitHub should have a real Bearer token (not ${GITHUB_TOKEN} literal)
```

**IMPORTANT**: After seeding MCPs, the TM pods must be restarted to pick up
the new definitions (TM loads MCPs once at startup):

```bash
kubectl rollout restart deployment/flowgent-taskmanager -n flowgent-default
kubectl rollout status deployment/flowgent-taskmanager -n flowgent-default
```

---

# 3. Verification Architecture

## 3.1 Layer Model

| Layer | Name | Focus | Scenario |
|-------|------|-------|----------|
| L1 | Pre-Deployment | K3s health, image availability, external deps | 01 |
| L2 | Infrastructure | Helm deployment, pod readiness, healthz | 01 |
| L3 | API Server | REST CRUD, lifecycle events, PG persistence | 02 |
| L4 | A2A Protocol | Agent card discovery, task submission | 03 |
| L5 | Controller | Flow lifecycle, K8s JM pod creation | 04 |
| L6 | Messager + Sandbox | MQTT topics, sandbox chain | 05 |
| L7 | Notifier | Multi-channel delivery | 06 |
| L8 | Wallet | x402 key management, MQTT signing | 07 |
| L9 | OTEL Tracing | Jaeger span coverage | 08 |
| L10 | E2E Pipeline | Full security fixer flow (24 nodes) | 09 |
| L11 | PR Verification | Agent-produced fix commits on target PR | 10 |
| L12 | Knowledge RAG | Knowledge retrieval, injection, post-handle | 11 |

## 3.2 Scenario File Index

| # | File | Layer |
|---|------|-------|
| 01 | `01_infra_verifier.py` | L1-L2 |
| 02 | `02_apiserver_verifier.py` | L3 |
| 03 | `03_a2a_protocol_verifier.py` | L4 |
| 04 | `04_controller_verifier.py` | L5 |
| 05 | `05_messager_verifier.py` | L6 |
| 06 | `06_notifier_verifier.py` | L7 |
| 07 | `07_wallet_verifier.py` | L8 |
| 08 | `08_otel_verifier.py` | L9 |
| 09 | `09_e2e_security_fixer.py` | L10 |
| 10 | `10_pr_commit_verifier.py` | L11 |
| 11 | `11_knowledge_verifier.py` | L12 |

---

# 4. Scenario Specifications

## 4.1 Scenario 01 — Infrastructure & Pre-Deployment (L1-L2)

### 4.1.1 Purpose

Validate K3s cluster health, external dependency availability, Helm deployment
state, and pod readiness before running any functional scenarios.

### 4.1.2 Prerequisites

- K3s cluster accessible (`KUBECONFIG` set)
- `kubectl`, `helm`, `docker` on PATH
- Python `requests` library installed

### 4.1.3 Steps

| Step | Action | Expected Input | Expected Output |
|------|--------|---------------|-----------------|
| 1.1 | `kubectl get nodes` | KUBECONFIG pointing to K3s | At least 1 node with `Ready` status |
| 1.2 | `kubectl get pods -n kube-system` | K3s cluster running | All system pods `Running` |
| 1.3 | `GET {SONARQUBE_URL}/api/system/health` | `config.SONARQUBE_URL` | HTTP 200, body contains `health` or `status` field |
| 1.4 | TCP connect to `EMQX_HOST:EMQX_PORT` | `config.EMQX_HOST`, `config.EMQX_PORT` | Connection accepted (non-blocking) |
| 1.5 | `docker images flowgent-core` | Docker daemon running | Image `flowgent-core` listed |
| 2.1 | `helm list -n {namespace} -o json` | Helm binary on PATH | Release `flowgent` with status `deployed` |
| 2.2 | `kubectl get deploy,svc,configmap -l app.kubernetes.io/instance=flowgent` | Helm release deployed | Resources listed (≥1 each of deploy/svc/configmap) |
| 3.1 | `kubectl get pods -l app.kubernetes.io/instance=flowgent -o json` | Pods running | All pods `phase=Running`, all containers `ready=true` |
| 3.2 | `GET {API_URL}/_/healthz` | API server pod Running | HTTP 200 |
| 3.3 | `kubectl logs -l app.kubernetes.io/component={c} --tail=20` for apiserver/controller/notifier | Pod logs accessible | No ERROR/FATAL/PANIC in recent logs |

### 4.1.4 Pass/Fail Criteria

- **PASS**: All checks return expected output (WARN for non-critical items like SonarQube unreachable is tolerated)
- **FAIL**: Critical infrastructure missing (no Ready nodes, API server unreachable, all pods CrashLoopBackOff)

---

## 4.2 Scenario 02 — API Server Module (L3)

### 4.2.1 Purpose

Validate complete REST API CRUD operations across all 9 entity types, verify
PostgreSQL persistence, and confirm MQTT lifecycle event publishing.

### 4.2.2 Prerequisites

- Scenario 01 passed (API server running on `:9999`)
- EMQX reachable (MQTT tests)
- PostgreSQL reachable (PG direct tests)

### 4.2.3 Entity CRUD Steps

| Step | Entity | Table | REST Path | Expected Input | Expected Output |
|------|--------|-------|-----------|----------------|-----------------|
| 2.1 | AgentFlow | `orh_agentflow` | `POST /api/v1/{tenant}/flows` | `{id, nodes, edges, priority}` | HTTP 200/201, body contains `id` |
| 2.2 | AgentFlow | `orh_agentflow` | `GET /api/v1/{tenant}/flows` | — | JSON array of flows |
| 2.3 | AgentFlow | `orh_agentflow` | `GET /api/v1/{tenant}/flows/{id}` | Flow ID | Flow object with `version`, `nodes`, `edges` |
| 2.4 | AgentFlow | `orh_agentflow` | `PUT /api/v1/{tenant}/flows/{id}` | `{description, version}` | HTTP 200, `version` incremented |
| 2.5 | AgentFlow | `orh_agentflow` | `DELETE /api/v1/{tenant}/flows/{id}` | Flow ID | HTTP 200/204, soft-deleted (`del_flag=true`) |
| 2.6 | FlowRun | `orh_flowrun` | `POST /api/v1/{tenant}/runs` | `{agentflow_id, priority}` | HTTP 201, body contains `id`, `status=PENDING` |
| 2.7 | FlowRun | `orh_flowrun` | `GET /api/v1/{tenant}/runs/{id}` | Run ID | Run object with `status`, `created_at` |
| 2.8 | TaskRun | `task_runs` | `GET /api/v1/{tenant}/runs/{id}/tasks` | Run ID | JSON array of task runs (may be empty) |
| 2.9 | Agent | `llm_agent` | `POST /api/v1/{tenant}/agents` | `{name, model, instruction}` | HTTP 200/201, body contains `name` |
| 2.10 | Agent | `llm_agent` | `GET /api/v1/{tenant}/agents/{name}` | Agent name | Agent object with `model`, `instruction` |
| 2.11 | MCP | `llm_mcp` | `POST /api/v1/{tenant}/mcp` | `{name, type, url, enabled}` | HTTP 201, body contains `id` |
| 2.12 | MCP | `llm_mcp` | `PUT /api/v1/{tenant}/mcp/{name}` | `{enabled: true/false}` | HTTP 200, `enabled` toggled |
| 2.13 | Provider | `llm_providers` | `POST /api/v1/{tenant}/llm/providers` | `{type, endpoint, models[]}` | HTTP 201, body contains `id` |
| 2.14 | Approval | `human_approvals` | `POST /api/v1/human/approvals` | `{run_id, node_id}` | HTTP 201, body contains `token` |
| 2.15 | Channel | `nfy_channel` | `POST /api/v1/{tenant}/notifications/channels` | `{name, channel_type, config}` | HTTP 201, body contains `id` |

### 4.2.4 MQTT Lifecycle Event Steps

| Step | REST Action | Expected MQTT Topic | Expected Payload |
|------|------------|---------------------|------------------|
| 4.1 | `POST /flows` | `flowgent/v1/{tenant}/flows/{id}/ctrl/flow/updated` | `{action: "created", agentflow_id, version}` |
| 4.2 | `PUT /flows/{id}` | `flowgent/v1/{tenant}/flows/{id}/ctrl/flow/updated` | `{action: "updated", agentflow_id, version++}` |
| 4.3 | `DELETE /flows/{id}` | `flowgent/v1/{tenant}/flows/{id}/ctrl/flow/deleted` | `{action: "deleted", agentflow_id}` |
| 4.4 | `POST /runs` | `flowgent/v1/{tenant}/flows/{id}/runs/{rid}/ctrl/run/created` | `{action: "created", run_id}` |

### 4.2.5 Pass/Fail Criteria

- **PASS**: All CRUD endpoints return correct status codes, PG rows match API responses, MQTT events published for lifecycle changes
- **FAIL**: Any CRUD endpoint returns 5xx, PG/API data mismatch, MQTT events missing

---

## 4.3 Scenario 03 — A2A Protocol Module (L4)

### 4.3.1 Purpose

Validate Google Agent-to-Agent (A2A) protocol: agent card discovery and task
submission.

### 4.3.2 Prerequisites

- Scenario 01 passed (API server and A2A service running on `:9992`)

### 4.3.3 Steps

| Step | Action | Expected Input | Expected Output |
|------|--------|---------------|-----------------|
| 3.1 | `GET /.well-known/agent.json` | A2A port reachable | HTTP 200, JSON with `skills` array |
| 3.2 | `POST /a2a/tasks` | `{skill_id, parameters}` | HTTP 200/201, body contains task `id` |
| 3.3 | `GET /a2a/tasks` | — | HTTP 200 or 404 (list may not be supported) |

### 4.3.4 Pass/Fail Criteria

- **PASS**: Agent card returns valid JSON with skills; task submission returns ID
- **FAIL with grace**: A2A port unreachable → SKIP (not all deployments enable A2A)

---

## 4.4 Scenario 04 — Controller Module (L5)

### 4.4.1 Purpose

Validate Application mode flow lifecycle: Controller creates/updates/deletes K8s
JM Deployments in response to flow CRUD events.

### 4.4.2 Prerequisites

- Scenario 01 passed (Controller pod Running, K8s RBAC configured)

### 4.4.3 Steps — Flow CREATE Lifecycle

| Step | Action | Expected Input | Expected Output |
|------|--------|---------------|-----------------|
| 4.1 | `POST /api/v1/{tenant}/flows` | `{id, nodes: [noop], edges: [], priority: "high"}` | HTTP 200/201, JM Deployment created in `flowgent-{tenant}` namespace |
| 4.2 | Wait for Deployment | — | `flowgent-jobmanager-{tenant}-{flow_id}` exists, 1 replica |
| 4.3 | Verify Deployment spec | Deployment name | Container env includes `FLOWGENT__RUNTIME__AGENT_FLOW_ID={flow_id}` |
| 4.4 | Wait for JM Pod | `app=flowgent-jobmanager,flowgent.io/flow={flow_id}` | Pod reaches `Running` within 60s |
| 4.5 | `DELETE /api/v1/{tenant}/flows/{id}` | Flow ID | HTTP 200/204 |
| 4.6 | Wait for GC | — | JM Deployment deleted within 60s |

### 4.4.4 Steps — Flow UPDATE Lifecycle

| Step | Action | Expected Input | Expected Output |
|------|--------|---------------|-----------------|
| 5.1 | Create flow + wait for Deployment | (same as 4.1-4.2) | JM Deployment running |
| 5.2 | `PUT /api/v1/{tenant}/flows/{id}` | `{description: "updated", version: 2}` | HTTP 200 |
| 5.3 | Check Deployment generation | — | Generation incremented (Controller triggered rolling update) |

### 4.4.5 Steps — MQTT Event Verification

| Step | Action | Expected Input | Expected Output |
|------|--------|---------------|-----------------|
| 6.1 | Subscribe to `ctrl/flow/updated` | MQTT broker reachable | Event received within 5s of flow CREATE with `agentflow_id` matching |
| 6.2 | Subscribe to `ctrl/flow/deleted` | MQTT broker reachable | Event received within 5s of flow DELETE |

### 4.4.6 Pass/Fail Criteria

- **PASS**: Controller creates JM Deployment in correct tenant namespace with
  correct env vars; Deployment deleted on flow delete; MQTT events published
- **FAIL**: JM Deployment not created within 30s, wrong namespace, env vars missing

---

## 4.5 Scenario 05 — Messager Module (L6)

### 4.5.1 Purpose

Validate MQTT topic connectivity and message routing for all 14 inter-component
topics.

### 4.5.2 Prerequisites

- EMQX running (port 1883)
- `paho-mqtt` Python library installed

### 4.5.3 Topic Verification Steps

All topics use the prefix `flowgent/v1/{tenant}/flows/{flowId}/runs/{runId}/`.

| Step | Topic Suffix | Publisher | Subscriber | Expected Payload |
|------|-------------|-----------|------------|------------------|
| 7.1 | `exec/plans` | JM | TM ($share/tm-pool) | `{plan_id, node_id, input, task_type}` |
| 7.2 | `exec/results` | TM | JM | `{plan_id, node_id, state}` (state only, NO output) |
| 7.3 | `sandbox/trigger` | TM | Sandbox ($share/sandbox-pool) | `{plan_id, runtime, script}` |
| 7.4 | `sandbox/result` | Sandbox | TM | `{plan_id, exit_code, stdout}` |
| 7.5 | `notify/event` | Publisher | Notifier ($share/notify-pool) | `{event_type, channel, payload}` |
| 7.6 | `notify/result` | Notifier | Publisher | `{event_id, status, error}` |
| 7.7 | `sign/request` | TM | Wallet ($share/wallet-pool) | `{request_id, unsigned_payload}` |
| 7.8 | `sign/response` | Wallet | TM | `{request_id, signature}` (no x402 fields) |

Additional topics (heartbeat, ctrl/*, cross-pod WS) are validated by their
respective scenarios (04, 08).

### 4.7.4 Skill/Sandbox E2E Chain

```
JM → exec/plans → TM
       ├→ sandbox/trigger → Sandbox (seccomp) → sandbox/result → TM
       ├→ PUT /api/v1/{tenant}/runs/{id}/tasks/{task_id} (persist output via REST)
       └→ exec/results (state only) → JM
```

### 4.7.5 Pass/Fail Criteria

- **PASS**: All topics self-publish and self-subscribe successfully; state-only
  callback constraint verified (`exec/results` contains no `output` field)
- **FAIL**: MQTT broker unreachable, topic routing broken, output data leaked in
  state-only callback

---

## 4.6 Scenario 06 — Notifier Module (L7)

### 4.8.1 Purpose

Validate EMQX broker connectivity and notification topic subscription.

### 4.8.2 Prerequisites

- EMQX running (port 1883, dashboard 18083)

### 4.8.3 Steps

| Step | Action | Expected Input | Expected Output |
|------|--------|---------------|-----------------|
| 8.1 | EMQX dashboard API | `GET http://localhost:18083/api/v5/status` | HTTP 200, EMQX status |
| 8.2 | MQTT connect + subscribe | `$share/notify-pool/flowgent/v1/+/flows/+/runs/+/notify/event` | Subscription confirmed |
| 8.3 | Message receipt (opportunistic) | Pending notification events | Message delivered if any exist |

### 4.8.4 Pass/Fail Criteria

- **PASS**: EMQX reachable, MQTT subscription established
- **FAIL with grace**: Notifier not enabled → SKIP

---

## 4.7 Scenario 07 — Wallet Module (L8)

### 4.9.1 Purpose

Validate wallet key management REST API and async MQTT signing boundary.
Wallet is a signing boundary only — it does NOT parse x402 intents or call
facilitators.

### 4.9.2 Prerequisites

- Wallet enabled in config (`wallet.enabled: true`)
- Secret store configured (master key file)

### 4.9.3 Steps

| Step | Action | Expected Input | Expected Output |
|------|--------|---------------|-----------------|
| 9.1 | `POST /api/v1/wallet/keys` | `{name}` | HTTP 201, `{name, address}` (Ed25519 keypair) |
| 9.2 | `GET /api/v1/wallet/keys` | — | JSON array of wallet names |
| 9.3 | `GET /api/v1/wallet/keys/{name}` | Key name | `{name, address}` |
| 9.4 | `POST /api/v1/wallet/sign` | `{name, payload}` | `{signature}` (hex-encoded) |
| 9.5 | `DELETE /api/v1/wallet/keys/{name}` | Key name | HTTP 204 |
| 9.6 | MQTT `sign/request` → `sign/response` | `{request_id, unsigned_payload}` | `{request_id, signature}` (boundary: no x402 fields) |

### 4.9.4 Pass/Fail Criteria

- **PASS**: All key CRUD operations succeed; MQTT signing round-trip works;
  response contains only signature/error (no x402 policy/facilitator fields)
- **FAIL**: Key operations fail, MQTT signing timeout, boundary violation

---

## 4.8 Scenario 08 — OTEL Tracing (L9)

### 4.10.1 Purpose

Validate complete distributed trace coverage for all 24 nodes of the
`security-autonomy-fixer` flow, including span hierarchy and attribute
completeness.

### 4.10.2 Prerequisites

- Scenarios 01-09 passed
- Jaeger running (port 16686)
- OTEL exporter configured (`OTEL_EXPORTER_OTLP_ENDPOINT`)
- Agents and MCPs seeded (auto-seeded by script)

### 4.10.3 Steps

| Step | Action | Expected Input | Expected Output |
|------|--------|---------------|-----------------|
| 10.1 | Seed agents + MCPs | `config/agents/*.yaml`, `config/mcps/*.yaml` | Agents and MCPs registered in DB |
| 10.2 | Import `security-autonomy-fixer` flow | Flow YAML (24 nodes, 11 phases) | Flow created via API |
| 10.3 | Trigger flow run | `{agentflow_id}` | Run ID returned |
| 10.4 | Wait for completion | Run ID | Flow reaches terminal state |
| 10.5 | Query Jaeger for traces | `flowgent.run_id={run_id}` | ≥1 trace with ≥160 spans |
| 10.6 | Validate per-node spans | Span data | Each node ≥7 spans (dispatch→consume→execute→persist→publish→receive) |
| 10.7 | Validate span attributes | Span tags | `flowgent.node_id`, `flowgent.task_type`, `input`, `output` present |

### 4.10.4 Expected Span Hierarchy per Node

```
JM-DispatchPlan
  └─ TM-ConsumeExecPlan
       └─ SlotWorker-Execute{Type}   (Agent|Tool|Sandbox|Supervisor|Committee|...)
            ├─ LLM-Call / MCP-Call / Sandbox-Execute  (type-specific)
            ├─ API-PUT-/tasks                           (persist output)
            └─ MQTT-Publish-ExecResult                  (state callback)
                 └─ JM-ReceiveExecResult
```

### 4.10.5 Pass/Fail Criteria

- **PASS**: Traces found in Jaeger, ≥160 spans, span hierarchy correct, required
  attributes present on all spans
- **FAIL**: No traces found, span count too low, missing attributes, broken hierarchy

---

## 4.9 Scenario 09 — Security Fixer E2E Capstone (L10)

### 4.11.1 Purpose

Capstone white-box run of the canonical `security-autonomy-fixer` flow (24 nodes,
11 phases). Verifies the complete pipeline from issue discovery through PR creation.

### 4.11.2 Prerequisites

- Scenarios 01-10 passed
- GitHub MCP configured (real `GITHUB_TOKEN`)
- SonarQube MCP reachable (`http://172.29.235.101:18080/mcp`)
- LLM provider registered (e.g. DeepSeek)
- Clean PostgreSQL (no stale flow/runs from previous attempts)

### 4.11.3 Flow Phases

| Phase | Nodes | Description |
|-------|-------|-------------|
| 1. DISCOVERY | `get-commit`, `scan-sonarqube` | Fetch PR commit, scan with SonarQube |
| 2. ANALYZE | `aggregate-issues` | Aggregate and categorize issues |
| 3. FIX | `generate-fixes` | Generate code fixes for each issue |
| 4. REVIEW | `review-security`, `review-quality`, `review-arch` | Parallel review of fixes |
| 5. VOTE | `committee` | Multi-agent vote on fix approval |
| 6. SUPERVISOR | `supervisor-check` | Safety gate with constraints |
| 7. CONDITION | `is-approved` | Branch on approval decision |
| 8. HUMAN | `human-approval` | Async human approval gate |
| 9. COMMIT+PR | `create-branch`, `commit-fixes`, `create-pr` | Git operations via GitHub MCP |
| 10. RE-SCAN | `trigger-rescan`, `wait-rescan`, `check-resolved`, `compare-results`, `fix-complete` | Verify fixes with SonarQube |
| 11. REPORT | `summary-report`, `notify-pr`, `notify-email`, `notify-teams`, `end` | Multi-channel notification |

### 4.11.4 Steps

| Step | Action | Expected Input | Expected Output |
|------|--------|---------------|-----------------|
| 11.1 | Seed agents + MCPs | Agent/MCP YAML files | Agents and MCPs registered (HTTP 201) |
| 11.2 | Import flow definition | `security-autonomy-fixer.yaml` | Flow created with 24 nodes, valid DAG edges |
| 11.3 | Trigger flow | `{agentflow_id: "security-autonomy-fixer"}` | Run ID returned, status transitions to `RUNNING` |
| 11.4 | Poll run status | Run ID, poll interval, timeout | Status reaches `COMPLETED` or `FAILED` within timeout |
| 11.5 | Verify PG: `orh_flowrun` | Run ID | Row exists with correct `status`, `started_at`, `finished_at` |
| 11.6 | Verify PG: `task_runs` | Run ID | ≥1 task rows with `output` populated (UPSERT working) |
| 11.7 | Verify REST: task list | `GET /api/v1/{tenant}/runs/{id}/tasks` | Returns task array with `node_id`, `status`, `output` |
| 11.8 | Verify REST: task detail | `GET /api/v1/{tenant}/runs/{id}/tasks/{task_id}` | Returns task with full `output` JSON |

### 4.11.5 Pass/Fail Criteria

- **PASS**: Flow reaches `COMPLETED`; `task_runs` rows persisted with output;
  REST task APIs return correct data; PG state consistent with API responses
- **FAIL**: Flow hangs (timeout), no task rows persisted, REST/PG mismatch
- **PARTIAL**: Flow runs but some nodes fail (e.g. LLM provider missing) —
  acceptable if infrastructure/plumbing is verified

---

## 4.10 Scenario 10 — PR Commit Verification

### 4.12.1 Purpose

Verify that the security-autonomy-fixer L2 agents produced actual fix commits
on the target repository's pull request. This is the **work product**
verification — it checks what agents delivered, not just pipeline execution.

### 4.12.2 Prerequisites

- GitHub API access (public repo; token optional but recommended)
- PR #4 on `wl4g/rengine` exists
- Branch `fix/flowgent_sec_auto_fix` exists

### 4.12.3 Steps

| Step | Action | Expected Input | Expected Output |
|------|--------|---------------|-----------------|
| 12.1 | `GET /repos/{owner}/{repo}/pulls/{number}` | `wl4g/rengine`, PR #4 | PR metadata: `title`, `state` (open/merged), `changed_files`, `additions`, `deletions` |
| 12.2 | `GET /repos/{owner}/{repo}/commits?sha={branch}&per_page=50` | Branch `fix/flowgent_sec_auto_fix` | Commit list with SHA, author, message |
| 12.3 | Classify commits | Commit messages | Flowgent commits identified by message patterns: `flowgent`, `security-autonomy-fixer`, `auto-fix`, `fix: SonarQube`, `fix: Security`, `remediate`, `security fix`, `vulnerability fix`, `quality gate` |
| 12.4 | `GET /repos/{owner}/{repo}/pulls/{number}/files?per_page=100` | PR #4 | File list with `filename`, `additions`, `deletions`, `status` |
| 12.5 | Classify files | File paths | Test files (match `test`, `_test.go`, `Test.java`) vs source files |

### 4.12.4 Pass/Fail Criteria

- **PASS (full)**: ≥1 Flowgent-authored commit found AND ≥1 test file changed
- **PASS (with caveat)**: PR accessible but no Flowgent commits — indicates MCP
  tool nodes lacked valid credentials (pipeline ran but couldn't push)
- **FAIL**: PR not accessible (critical), branch has no commits (critical)

---

## 4.11 Scenario 11 — Knowledge RAG Verification

### 4.11.1 Purpose

Validate the Knowledge retrieval-augmented generation (RAG) pipeline: knowledge
CRUD via the REST API, keyword and semantic search, pre-LLM knowledge injection,
and asynchronous knowledge extraction after flow runs.

### 4.11.2 Prerequisites

- Scenarios 01-02 passed (API server running, PG accessible)
- Knowledge table created in PostgreSQL
- Flow with at least one LLM (agent) node registered

### 4.11.3 Steps

| Step | Action | Expected Input | Expected Output |
|------|--------|---------------|-----------------|
| 13.1 | `POST /api/v1/{tenant}/knowledge` | `{title, content, tags: ["security", "java"]}` | HTTP 201, knowledge entry with `id` |
| 13.2 | `GET /api/v1/{tenant}/knowledge` | — | JSON array with created entry |
| 13.3 | `GET /api/v1/{tenant}/knowledge/{id}` | Knowledge ID | Entry with correct `title`, `content`, `tags` |
| 13.4 | `PUT /api/v1/{tenant}/knowledge/{id}` | `{content: "updated content"}` | HTTP 200, content updated |
| 13.5 | `POST /api/v1/{tenant}/knowledge/search` | `{query: "SQL injection", top_k: 5}` | Matching entries ranked by relevance |
| 13.6 | `GET /api/v1/{tenant}/knowledge/tags` | — | JSON array with all distinct tags |
| 13.7 | `DELETE /api/v1/{tenant}/knowledge/{id}` | Knowledge ID | HTTP 200/204 |
| 13.8 | Trigger flow with LLM node | Flow with `knowledge_search` config | LLM node system prompt contains injected knowledge |
| 13.9 | Wait for run completion | Run ID | Run reaches `COMPLETED` |
| 13.10 | Verify knowledge updated | `GET /api/v1/{tenant}/knowledge?source=flow_run` | New entries created from node outputs |

### 4.11.4 Knowledge Injection Verification

Before each LLM node executes, the engine retrieves relevant knowledge entries and
injects them into the system prompt. Verification checks:

- **Pre-execution**: Knowledge entries matching the node's domain are retrieved
- **Injection format**: Entries are formatted as context blocks in the system prompt
- **Post-execution**: Node outputs are upserted as new knowledge entries with
  `source=flow_run` and `source_ref={flow_id}:{run_id}:{node_id}`

### 4.11.5 Pass/Fail Criteria

- **PASS**: All CRUD operations return correct responses, search returns relevant
  results, knowledge is injected into LLM prompts, and new knowledge is created
  after flow completion
- **FAIL**: CRUD endpoints return 5xx, search returns empty for known terms,
  knowledge not injected, or post-run knowledge not created

---

# 5. Troubleshooting

## 5.1 Common Issues

| Symptom | Likely Cause | Resolution |
|---------|-------------|------------|
| `Connection refused :9999` | API Server not running | `kubectl logs deploy/flowgent-apiserver` |
| `MQTT publish timeout` | EMQX pod not ready | `kubectl get pods \| grep emqx` |
| `PG connection failed` | PostgreSQL credentials wrong | Check `config.py` PG_* vars |
| `ImagePullBackOff` | flowgent-core image missing | Re-run `docker save \| k3s ctr images import` |
| `JM Deployment not created` | Controller not running or RBAC missing | Check ClusterRoleBinding for tenant namespace |
| `Jaeger trace not found` | OTEL not configured | Verify `OTEL_EXPORTER_OTLP_ENDPOINT` env |
| `Human approval timeout` | WebSocket not connected | Check notifier pod logs |
| `MCP not found: github/sonarqube` | MCPs not registered or TM not restarted | Verify `GET /api/v1/{tenant}/mcp`, restart TM pods |
| `LLM provider not found: default` | No LLM provider registered | `POST /api/v1/{tenant}/llm/providers` |
| `DiskPressure taint` | Node disk >95% | Clean Docker images, journald logs, caches |

## 5.2 Log Collection

```bash
# API Server logs
kubectl logs deploy/flowgent-apiserver -n default --tail=100

# JobManager logs (Application mode — find pod first)
JM_POD=$(kubectl get pods -n flowgent-default -l app=flowgent-jobmanager -o name | head -1)
kubectl logs $JM_POD -n flowgent-default --tail=100

# TaskManager logs
kubectl logs deploy/flowgent-taskmanager -n flowgent-default --tail=100

# EMQX dashboard
open http://localhost:18083  # admin / public

# Jaeger UI
open http://localhost:16686

# PostgreSQL query
kubectl exec -it deploy/flowgent-postgres -- psql -U flowgent -d flowgent -c \
  "SELECT id, status, created_at FROM orh_flowrun ORDER BY created_at DESC LIMIT 5;"
```

---

# 6. Performance Benchmarks

Expected execution times (K3s single-node, 4 CPU, 8GB RAM):

| Scenario | Duration | Bottleneck |
|----------|----------|------------|
| 01 — Infrastructure | 10-15s | K8s API queries |
| 02 — API Server CRUD | 20-30s | DB writes + MQTT events |
| 03 — A2A Protocol | 5-10s | HTTP round-trips |
| 04 — Controller | 40-60s | K8s Deployment creation |
| 05 — Messager Topics | 30-45s | MQTT round-trips |
| 06 — Notifier | 5-10s | MQTT connect + subscribe |
| 07 — Wallet | 15-20s | Key generation + MQTT signing |
| 08 — OTEL Tracing | 300-600s | Full flow execution |
| 09 — Security Fixer E2E | 300-600s | LLM calls + SonarQube API |
| 10 — PR Verification | 5-10s | GitHub API calls |
| 11 — Knowledge RAG | 15-30s | Knowledge CRUD + search + flow run |

**Total suite runtime**: ~15-25 minutes

---

# 7. Architecture Compliance

## 7.1 Key Design Decisions (v3.0)

### 7.1.1 TM → JM Communication
- **State-only callback**: `exec/results` contains `{plan_id, node_id, state}` only
- NO output data in MQTT message
- Output persisted via REST `PUT /api/v1/{tenant}/runs/{id}/tasks/{task_id}`

### 7.1.2 Lifecycle Event Publisher
- Only API Server publishes `ctrl/*` events (`ctrl/flow/updated`, `ctrl/flow/deleted`,
  `ctrl/run/created`, `ctrl/run/status`)
- TM and JM do NOT publish lifecycle events

### 7.1.3 Application Mode Only
- Every flow gets a dedicated JM Deployment in `{prefix}{tenant}` namespace
- Deployment named `flowgent-jobmanager-{tenant}-{flow_id}`
- Controller dispatches at most once per flow-definition version (on-new-definition)

### 7.1.4 DAG Dependency Coordination
- JM outer-loop calls `Ready()` to discover newly-unblocked nodes
- `Schedule()` blocks on per-node Go channel until TM publishes `exec/results`
- Siblings dispatched sequentially (one-at-a-time, blocking)
- Dormant conditional edges skipped in `depsDone()`
- Deadlock detection: `Ready()` empty + `IsComplete()` false → break

### 7.1.5 Table Naming
- `orh_*` prefix: orchestration entities (`orh_agentflow`, `orh_flowrun`)
- `llm_*` prefix: AI entities (`llm_agent`, `llm_mcp`, `llm_providers`)
- `task_runs`: task execution records
- `human_approvals`: human-in-the-loop approvals

### 7.1.6 Wallet Boundary
- TM parses HTTP 402, builds unsigned payloads, evaluates policy, calls facilitator
- Wallet only signs opaque payloads (no x402 parsing, no policy evaluation)

---

# 8. References

- **Architecture**: `docs/01-L1-Engine-Architecture.md`
- **Economic Layer**: `docs/02-L1-x402-Economic-Support.md`
- **Use Cases**: `docs/10-L2-USE-CASES.md`
- **Flow Definition**: `examples/security-autonomy-fixer/config/flows/security-autonomy-fixer.yaml`
- **Agent Definitions**: `examples/security-autonomy-fixer/config/agents/*.yaml`
- **MCP Servers**: `examples/security-autonomy-fixer/config/mcps/`
- **E2E Test Runner**: `examples/security-autonomy-fixer/e2e-verification/runner.py`
- **Scenario Scripts**: `examples/security-autonomy-fixer/e2e-verification/scenarios/*.py`
