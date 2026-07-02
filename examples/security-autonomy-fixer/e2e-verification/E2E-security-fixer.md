# E2E Verification — Security Autonomy Fixer

SonarQube-only remediation pipeline. AI context document for the Python verification suite.

**Run the suite:** `python3 runner.py [-s NN] [-l] [--api URL] [--pg DSN]`
**Cluster:** K3s single-node, namespace `default`
**SonarQube:** `http://172.29.235.101:9000`
**Flow:** `security-autonomy-fixer` (26 nodes, 11 phases, SonarQube-only)

---

## Layer Mapping

| Layer | Name | Verifier |
|-------|------|----------|
| L1 | Pre-Deployment | `python3 runner.py -s 01` |
| L2 | Helm Redeploy | `python3 runner.py -s 01` |
| L3 | Pod Readiness | `python3 runner.py -s 01` |
| L4 | Database | `python3 runner.py -s 05` (PG) + `-s 08` (SQLite via API) |
| L5 | Console Import | `python3 runner.py -s 08` (API-based import verification) |
| L6 | Flow Execution | `python3 runner.py -s 04` (node types) + `-s 08` (full pipeline) |
| L7 | Post-Verification | `python3 runner.py -s 08` (DB state, MQTT messages, task status) |

---

## L1-L3: Infrastructure (Scenario 01)

`python3 runner.py -s 01`

**What it verifies:**
- K3s node Ready, system pods healthy
- SonarQube `/api/system/health` responds
- EMQX port 1883 open
- Docker image `flowgent-core` present
- Helm release `flowgent` deployed
- All flowgent pods Running, no CrashLoopBackOff
- Apiserver `/healthz` returns 200
- Apiserver/JM logs free of ERROR/FATAL/PANIC

**Background:** These are the pre-flight checks that must pass before any flow can execute.
The scenario reads pod state via `kubectl` subprocess calls and health endpoints via HTTP.
It does NOT perform destructive actions (no `helm install`/`uninstall`) — those remain manual steps documented below.

**Manual redeploy steps (when infrastructure is down):**
```bash
# 1. Clean up failed pods
kubectl delete pod --field-selector=status.phase=Failed -n default --force --grace-period=0

# 2. Uninstall old release
helm uninstall flowgent -n default

# 3. Build + import image
make build-image-core
docker save flowgent-core:latest | sudo k3s ctr images import -

# 4. Install (session mode, all values via --set)
helm install flowgent deploy/helm/flowgent \
  --set global.mode=session \
  --set global.image.repository=localhost/flowgent-core \
  --set global.image.tag=latest \
  --set global.image.pullPolicy=IfNotPresent \
  --set postgresql.enabled=false \
  --set emqx.enabled=true \
  --set redis.enabled=false \
  --set messaging.type=mqtt \
  --set storage.type=SQLITE \
  --set lock.provider=memory \
  -n default
```

**Troubleshooting:**
| Symptom | Likely Cause |
|---------|-------------|
| Nodes NotReady | `sudo systemctl restart k3s` |
| ImagePullBackOff | Re-run `docker save \| k3s ctr images import -` |
| CrashLoopBackOff | `kubectl logs <pod> --previous` for startup errors |
| healthz unreachable | `kubectl port-forward svc/flowgent-apiserver 9999:9999` |

---

## L4: Database (Scenarios 05 + 08)

`python3 runner.py -s 05` — PG persistence (direct psycopg2 queries)
`python3 runner.py -s 08` — SQLite tables + rows via API

**What they verify:**
- Scenario 05: Flow definitions and runs persist to PostgreSQL, are retrievable after write
- Scenario 08: `agentflow_definitions`, `agentflow_runs`, `task_runs` tables exist and contain data

**Background:** The storage backend is pluggable (SQLite for dev, PostgreSQL for production).
Scenario 05 skips gracefully if `psycopg2` is not installed or PG is unreachable.
Scenario 08 connects via the REST API and queries DB schema through the apiserver.

**Expected tables:** `agent_definitions`, `agentflow_definitions`, `agentflow_runs`, `task_runs`, `mcp_definitions`, `llm_providers`

---

## L5: Console Import (Scenario 08)

`python3 runner.py -s 08`

**What it verifies:**
- Flow definition POSTed via REST API returns 200/201
- Flow definition persisted in DB (queryable via PG or API)

**Background:** In production, resources (LLMs, MCPs, Agents, Flows) are imported via `flowgent console import`.
Scenario 08 tests the same underlying API that the console uses, creating the flow definition and verifying persistence.
For the full console workflow, the data import file is at `/tmp/flowgent-seed-v2.yaml` (generated during deploy).

**Manual console import (for interactive debugging):**
```bash
kubectl exec -it deploy/flowgent-apiserver -n default -- \
  /app/flowgent console --api-server-url http://localhost:9999
# > import /tmp/flowgent-seed-v2.yaml
# > flow list
# > agent list
# > mcp list
```

---

## L6: Flow Execution (Scenarios 04 + 08)

`python3 runner.py -s 04` — Node type verification (agent, tribunal, supervisor)
`python3 runner.py -s 08` — Full 26-node security fixer pipeline

**What they verify:**
- Scenario 04: Creates a minimal flow with agent/tribunal/supervisor nodes, triggers it, waits for completion
- Scenario 08: Loads the canonical flow from `flows/security-autonomy-fixer.yaml`, creates it via API, triggers, monitors

**Background — Pipeline phases:**
1. DISCOVERY — `get-commit` → `scan-sonarqube`
2. ANALYZE — `aggregate-issues` (issue-detector agent)
3. FIX — `generate-fixes` (fixer-agent)
4. REVIEW — `review-security` / `review-quality` / `review-arch` (parallel fan-out)
5. VOTE — `tribunal` (majority vote)
6. SUPERVISOR — `supervisor-check` (safety gate)
7. CONDITION — `is-approved` routes to human-approval (true) or loop to fix (false)
8. HUMAN — `human-approval` (async gate, 24h timeout)
9. COMMIT+PR — `create-branch` → `commit-fixes` → `create-pr`
10. RE-SCAN — `trigger-rescan` → `wait-rescan` → `check-resolved` → `compare-results` → `fix-complete`
11. REPORT — `summary-report` → `notify-pr` / `notify-email` / `notify-teams`

**Expected failure modes (non-blocking for e2e):**
- MCP tools (`get-commit`, `scan-sonarqube`, `create-branch`) fail if MCP binaries not deployed in TM pod
- Agent nodes fail if LLM API key not configured
- These failures prove the pipeline works — API → MQTT → JM → TM execution is verified

**MQTT topic pattern:** `flowgent/notify/queue/{tenant}/{flow_id}/+`

---

## L7: Post-Verification (Scenario 08)

`python3 runner.py -s 08`

**What it verifies:**
- Flow status transitions from PENDING → RUNNING → COMPLETED/FAILED
- Task runs recorded in DB with status and output
- MQTT notification messages published
- All related DB tables populated

**Critical success criteria:**
1. Flow status = `COMPLETED`
2. All node tasks executed (no stuck PENDING tasks)
3. At least one MQTT notification per phase transition
4. Summary report generated with phase outputs

---

## Scenario Quick Reference

| # | Scenario | Covers | Key Dependency |
|---|----------|--------|---------------|
| 01 | Preflight | L1-L3 | kubectl, docker, helm CLIs |
| 02 | REST API | L6 (API CRUD) | Apiserver reachable |
| 03 | A2A Protocol | (independent) | A2A port 9992 |
| 04 | Flow Execution | L6 (node types) | Agents seeded in DB |
| 05 | PG Storage | L4 | PostgreSQL reachable |
| 06 | Jaeger OTEL | L3.7 (traces) | Jaeger reachable |
| 07 | Notifier MQTT | L1.5 (messaging) | EMQX reachable |
| 08 | Security Fixer | L5+L6+L7 | Flow YAML, all agents, MCPs, LLM |

**Run all:** `python3 runner.py`
**Run single:** `python3 runner.py -s 08`

---

## Non-Deterministic Checkpoints (AI-Assisted)

The Python scenarios cover deterministic verification. The following require human or AI judgment:

- **SonarQube quality gate on PR branch** — depends on external SonarQube CE task completion
- **GitHub PR actually created** — requires `gh` CLI with repo access
- **PR comment posted** — requires GitHub token with PR comment scope
- **Email/Teams notifications delivered** — depends on external service configuration

These are described here as context for AI-driven additional verification during interactive debugging sessions, not as automated assertions.
