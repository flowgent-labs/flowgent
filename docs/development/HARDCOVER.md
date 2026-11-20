# Coding-Agent Handover

[中文](HARDCOVER_ZH.md)

This is the migration handover for continuing Flowgent development on a new
Codex machine. It records implementation reality, incomplete work, known
environment traps, and the recommended work order. Architecture details remain
in the owning documents; this file links to them instead of redefining them.

## Snapshot

- Audited branch: `dev`
- Audited commit: `cd23a66d71c00ace6c7b30b99fc3233b96e3fece`
- Audit date: 2026-11-01
- Go workspace: Go 1.26, 15 modules in `go.work`
- Web: React/Vite/TypeScript under `web/`
- Database backends: PostgreSQL 16 + pgvector and SQLite + sqlite-vec
- Current E2E evidence:
  [`23/23 passed`](../../use-cases/security-autonomy-fixer/e2e/reports/00_summary.md)

The worktree was clean at the beginning of this audit. This documentation
reconciliation is intentionally left as normal working-tree changes until the
owner reviews and commits it. On the new machine, run `git status --short` and
`git diff --check` before doing new work; do not assume the snapshot commit is
still HEAD after transfer.

## Read First

1. [Repository conventions](../../.agents/AGENTS.md)
2. [Documentation index](../index.md)
3. [Engine overview](../architecture/overview.md)
4. [Durable data lifecycle](../architecture/data-lifecycle.md)
5. [AgentFlow application model](../architecture/agent-flow.md)
6. [AuthGuard boundary](../architecture/authguard-integration.md)
7. [API Server routes/state boundary](../architecture/engine/apiserver.md)
8. [Security Autonomy Fixer](../../use-cases/security-autonomy-fixer/README.md)
9. [K8s manual acceptance](../../use-cases/security-autonomy-fixer/e2e/MANUAL_ACCEPTANCE.md)

For current behavior, source and the documents above are authoritative.
`docs/development/archive/` is historical context only.

## New-Machine Bootstrap

```bash
git clone <flowgent-remote> flowgent
cd flowgent
git checkout dev
git submodule update --init --recursive

go version                 # must satisfy go.work (currently 1.26)
node --version              # CI/build currently uses Node 22
helm version
kubectl version --client

python3 -m venv use-cases/security-autonomy-fixer/e2e/.venv
use-cases/security-autonomy-fixer/e2e/.venv/bin/python -m pip install \
  -r use-cases/security-autonomy-fixer/e2e/requirements.txt
use-cases/security-autonomy-fixer/e2e/.venv/bin/python -m playwright install chromium
```

Inside the GFW, use:

```bash
export HTTPS_PROXY=http://127.0.0.1:8800
export HTTP_PROXY="$HTTPS_PROXY"
export IN_CN_GFW=true
```

For authorized E2E only, credentials may be loaded without inspecting or
printing them:

```bash
source ~/.wl4gshrc.sec
```

Never copy that file, its values, retained acceptance passwords, generated
reports containing sensitive payloads, or Kubernetes Secrets into this
handover or Git.

`agentvault/` is the pinned independent Rust submodule. Use only its own
Makefile. Before a Rust build, check `agentvault/target/`; if it exceeds 10 GiB,
use the repository's Makefile clean target. Do not run raw Cargo commands from
Flowgent automation.

## Current Architecture in One Page

- **Public boundary:** Envoy Gateway + AuthGuard. Port 9999 is the protected
  business listener. Port 9990 is the private control-plane listener and MUST
  NOT be exposed publicly.
- **Business-state boundary:** API Server owns general PostgreSQL/SQLite
  business-table access. Standalone A2A owns only `orh_a2a_task`; JobManager may
  use PostgreSQL/Redis only as a coordination-lock backend.
- **Control plane:** Controller observes active Runs and owns application
  JobManager Deployments. JobManager owns the DAG and reconciles
  TaskManager/Sandbox capacity. Execution plans/results and sandbox work travel
  over MQTT; durable state travels through REST.
- **Stable identity:** Flow, Agent, portable Skill, and Knowledge Document use a
  stable owner plus immutable revisions. Runs reference a concrete Flow
  revision.
- **Context:** Instruction, Knowledge, and Execution Memory are separate.
  NodeRun is the canonical public/persistence term; `TaskRunInfo` remains an
  internal compatibility name.
- **Knowledge:** Source → Document → DocumentRevision → hierarchical Content →
  Embedding. PostgreSQL uses pgvector; SQLite uses sqlite-vec. Publication uses
  Candidate → unified Approval → transactional CAS.
- **Security:** AuthGuard owns IAM and authorization decisions. Flowgent stores
  only canonical principal references and applies AuthGuard SQL scope before
  content is returned or sent to an LLM.
- **UI:** Overview, Runs/trace/logs/file previews, visual Flows and revisions,
  Agents, portable/runtime Skill management, MCPs, LLMs, published Knowledge,
  Notifications, runtime configuration, and embedded account security are
  implemented. Runtime Skill management is implemented; execution of a
  `kind: skill` DAG node is not.
- **Deployment:** Helm supports distributed components. E2E isolates Flowgent,
  AuthGuard, storage, SonarQube, mocks, and workload runtime in separate
  `e2e-flowgent-*` namespaces; namespaced resources use production-style names.

## Implemented Capability Matrix

| Area | Implemented evidence / source |
|---|---|
| Canonical REST CRUD | `pkg/api/pkg/server.go`, strict OpenAPI contract, AuthGuard scoped repositories |
| Flow/Agent/Skill/Document revisioning | owner/current-revision pointers, immutable revision tables, CAS updates |
| Run/NodeRun execution | attempts, input/output/error, checkpoints, messages, leases, fencing, workspace version |
| Runtime isolation | application/session modes, Controller/JM/TM/Sandbox ownership and GC |
| Unified approvals | `orh_approval` for human/tool/payment/Knowledge/Instruction decisions |
| Knowledge publication | immutable candidate/request hash, approval, baseline CAS, outbox/event transaction |
| Retrieval | scoped PostgreSQL FTS + pgvector, SQLite FTS + sqlite-vec, parent/neighbor expansion |
| Safe summarization | disabled by default; completed Runs only; secret redaction; candidate creation, never direct publish |
| Notification secrets | AES-256-GCM write-only fields with versioned key ring |
| Runtime credentials | environment references in definitions; Secret resolution inside runtime Pods |
| Observability | W3C context over MQTT, Jaeger query proxy, Run/NodeRun correlation, UI waterfall/details |
| Flow releases | immutable release catalog, grants, consumer-owned installation copies |
| A2A | A2A 0.3 JSON-RPC/Agent Card and principal-partitioned persistent task store |
| Browser console | real Chromium S41–S46 CRUD/login/trace/file-preview coverage |
| E2E cleanup | ownership-scoped Helm/namespace deletion with bounded finalizer recovery and `/finalize` last resort |

The current bootstrap DDL and old-to-new mapping are:

- [`migration/postgres/20260616a/01_init.ddl.sql`](../../migration/postgres/20260616a/01_init.ddl.sql)
- [`migration/sqlite/20260616a/01_init.ddl.sql`](../../migration/sqlite/20260616a/01_init.ddl.sql)
- [`migration/SCHEMA_MAPPING.md`](../../migration/SCHEMA_MAPPING.md)

## Open Work and Known Problems

### P0 — Migration Reproducibility

1. **Publish an immutable AuthGuard Web image containing the embedded account
   security header fix.** The retained E2E machine rebuilt the Web image from
   adjacent AuthGuard source commit `fed51c9` so that exactly
   `/auth/account/security` uses `X-Frame-Options: SAMEORIGIN` and CSP
   `frame-ancestors 'self'`. The registry tag `1.0.1` previously lacked this
   behavior and the same tag was locally retagged. A fresh machine may therefore
   fail S46 even when it pulls `1.0.1`. Publish a new immutable tag/digest,
   update the chart/E2E pin, and remove reliance on mutable local retagging.

   Temporary recovery: build `/home/agent/authguard/web` from commit `fed51c9`,
   set `FLOWGENT_E2E_AUTHGUARD_WEB_IMAGE` or retag/import it, then verify the
   rendered nginx rule before running E2E.

2. **Preserve this documentation patch during transfer.** It is not part of the
   audited snapshot commit until reviewed and committed.

### P1 — Deterministic Runtime and Recovery

1. `orh_run.context_snapshot` is captured transactionally, but Agent execution
   can still resolve the latest Agent by name and Knowledge search does not
   constrain results to snapshot `knowledge_revision_ids`. Make runtime
   execution consume the locked Agent/Instruction/Knowledge revisions.
2. The default System instruction snapshot is a
   `deployment://system-instruction` reference rather than an archived body.
   Define immutable deployment revisioning or archive the effective body/hash.
3. Durable NodeRun checkpoint/fencing structures exist, but not every
   re-dispatch path reloads the persisted checkpoint into `ExecutionPlan`.
   Implement crash-recovery tests that kill TM/JM and prove same-attempt resume
   without merging a failed branch.
4. Several registered DAG node types are not end-to-end runtime capabilities:
   `SkillExecutor` and `SubflowExecutor` return stub markers; `MapExecutor` is
   not connected to the existing fan-out runner; `HumanExecutor` persists an
   approval but JobManager does not suspend/resume the DAG; and Supervisor
   actions are validated but not applied as retry/redirect/inject/abort graph
   mutations. Add contract tests and focused E2E before advertising these node
   types as supported execution semantics.
5. `orh_effect_execution` exists in both schemas but has no Go runtime owner.
   Wire it to tool/payment side effects with request hash, idempotency, result,
   and reconciliation before claiming exactly-once external effects.
6. Bootstrap DDL is well tested for empty databases, but the next incompatible
   production schema change needs a non-destructive migration/rollback path.

### P1 — External Integrations

1. **AgentVault:** Flowgent still uses the legacy build-tagged
   `wallet.sign.v1` client and local policy ledger. Migrate to a pinned
   AgentVault SDK/API and add real settlement/reconciliation E2E. See
   [AgentVault boundary](../architecture/engine/wallet.md).
2. **Gemini:** `pkg/core/pkg/llm/gemini.go` is a placeholder; the HTTP/API client
   is not implemented. UI/API validation accepts the Gemini provider type, but
   execution is incomplete.
3. **Embedding ingestion:** native storage and retrieval work, but a complete
   production document parser/chunker/embedding generation and reindex pipeline
   is not present.
4. **GraphRAG:** no graph index or graph expansion is implemented. Keep it an
   extension behind the scoped Knowledge interface rather than adding a second
   ungoverned knowledge store.

### P2 — Cleanup and Productization

1. Internal entities retain deprecated aliases (`AgentFlowID`, `Vars`, provider
   endpoint aliases, MCP URL/type, `TaskRunInfo`) for in-process/wire
   compatibility. The public REST/database contract is canonical. Remove aliases
   only with explicit MQTT/A2A compatibility tests; do not perform a blind rename.
2. Helm `global.mode: all-in-one` remains marked TODO. The production chart is
   distributed-first; define or remove the unsupported mode.
3. Root Makefile has no dedicated PostgreSQL integration-test target even
   though integration packages exist under `tests/it`. Add a root-owned target
   before requiring that suite in the standard gate; do not bypass the root
   Makefile with ad-hoc Go commands.
4. The console import/export lifecycle covers runtime Skills but not portable
   Skill definitions/files. Decide whether portable packages need a signed
   archive/export contract rather than casually copying workspace files.
5. Parallel-ready DAG nodes are logically independent but JobManager currently
   dispatches ready siblings sequentially. Bounded parallel scheduling is a
   useful enhancement after recovery semantics are locked down.

### External/Environment Limitations

- The available DeepSeek credentials returned HTTP 402 (insufficient balance)
  during the last E2E. The complete orchestration matrix therefore defaults to
  the explicit in-cluster deterministic OpenAI-compatible fixture. The real
  DeepSeek Provider remains visible but was not the backend proving 23/23.
- Full K8s E2E assumes local k3s and
  `KUBECONFIG=/etc/rancher/k3s/k3s.yaml`. It preheats the local-path helper image
  through the configured mirror when GFW blocks Docker Hub.
- E2E `cleanup` is intentionally destructive only inside owned
  `e2e-flowgent-*` namespaces and allowlisted cluster-scoped objects. Never
  generalize its finalizer removal to production namespaces.

## Recommended Next Work Order

1. Commit/preserve the architecture and handover documentation update.
2. Publish and pin the fixed AuthGuard Web artifact; prove S46 on a genuinely
   clean machine with no local image cache.
3. Enforce ContextSnapshot revisions end to end, then add kill/restart recovery
   tests for checkpoint, workspace, and fencing consistency.
4. Complete and test Human/Map/Subflow/Skill/Supervisor runtime semantics.
5. Wire `orh_effect_execution`; then implement the AgentVault adapter and real
   payment E2E.
6. Add the production embedding ingestion/reindex pipeline.
7. Implement Gemini or reject it at API/UI creation until the runtime exists.
8. Only then tackle parallel DAG dispatch, GraphRAG, all-in-one Helm, and alias
   removal.

## Validation Gates

All Flowgent Go work uses the repository-root Makefile:

```bash
make fmt
make test-ut
make test-x402
make test-web
make test-e2e-runner
make build
```

For documentation-only changes, at minimum run:

```bash
git diff --check
make test-e2e-runner
```

For the complete retained K8s environment:

```bash
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
export HTTPS_PROXY=http://127.0.0.1:8800
source ~/.wl4gshrc.sec

E2E_ARGS='--timeout 1200' make e2e-security-autonomy-fixer-with-helm
```

The default retains Pods/data. Use the ownership-scoped cleanup only when the
target is confirmed:

```bash
use-cases/security-autonomy-fixer/e2e/.venv/bin/python \
  use-cases/security-autonomy-fixer/e2e/runner.py cleanup --deployer k8s
```

Read
[`MANUAL_ACCEPTANCE.md`](../../use-cases/security-autonomy-fixer/e2e/MANUAL_ACCEPTANCE.md)
for CLI/UI inspection. Retrieve acceptance credentials from the generated
Kubernetes Secret as instructed there; never hard-code them in a document.

## Source-of-Truth Map

| Question | Start here |
|---|---|
| REST routes | `pkg/api/pkg/server.go`, then handler + OpenAPI tests |
| Durable model | bootstrap DDL, `pkg/model/pkg/entities`, `pkg/storage` |
| Run snapshot/revisions | `pkg/storage/pkg/flowrun/flowrun_{postgres,sqlite}.go` |
| Knowledge/vector/publication | `pkg/storage/pkg/knowledge`, `pkg/api/pkg/handler/knowledge.go` |
| Summarizer | `pkg/core/pkg/engine/jobmanager/jobmaster.go` |
| Execution/retry/checkpoint | JobManager, ResourceManager, TaskManager, `orh_node_*` storage |
| AuthGuard scopes | `pkg/api/pkg/authz`, repository scope helpers, Helm adapter values |
| K8s lifecycle | `pkg/controller`, ResourceManager, `deploy/helm/flowgent` |
| E2E topology | `e2e/common/config.py`, `e2e/deploy/`, verifier scenarios |
| Cleanup/finalizers | `e2e/deploy/base/kubernetes_cleanup.py` |
| UI routes/features | `web/src/app/router.tsx`, `web/src/features/` |
| AgentVault | `agentvault/README.md` and its own Makefile; Flowgent transitional client under `pkg/core/pkg/client/signclient` |

## Do Not Regress

- Do not recreate IAM tables or accept client-supplied audit principals.
- Do not merge Instruction, Knowledge, and Execution Memory into a generic
  memory table or JSON blob.
- Do not represent vectors as JSON/TEXT or compare different profiles.
- Do not expose port 9990, resolved credentials, notification plaintext, prompts,
  model responses, or arbitrary payload previews in telemetry.
- Do not bypass Candidate → Approval → CAS publication for reusable Knowledge or
  Instruction.
- Do not use Jaeger as recovery storage.
- Do not rename internal compatibility fields without wire/upgrade tests.
- Do not run raw Go/Cargo builds outside the required Makefiles.
- Do not claim external-provider or AgentVault E2E success when a deterministic
  fixture or legacy client was used.

