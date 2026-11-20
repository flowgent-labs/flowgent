# Coding Agent 迁移交接

[English](HARDCOVER.md)

本文用于在新 Codex 机器继续开发 Flowgent，记录当前实现事实、未完成工作、环境陷阱与
推荐顺序。架构细节仍归属对应设计文档，本文只做导航和迁移决策。

## 快照

- 审计分支：`dev`
- 审计 Commit：`cd23a66d71c00ace6c7b30b99fc3233b96e3fece`
- 审计日期：2026-11-01
- Go Workspace：Go 1.26，`go.work` 共 15 个 Module
- Web：`web/` 下 React/Vite/TypeScript
- 数据库：PostgreSQL 16 + pgvector、SQLite + sqlite-vec
- 当前 E2E 证据：[`23/23 passed`](../../use-cases/security-autonomy-fixer/e2e/reports/00_summary.md)

本次审计开始时 Worktree 干净；本次文档对齐在 Owner Review/Commit 前会作为正常未提交
修改保留。迁移后先运行 `git status --short` 与 `git diff --check`，不要假定上述 Commit
仍是最新 HEAD。

## 必读入口

1. [仓库约定](../../.agents/AGENTS.md)
2. [文档索引](../index_ZH.md)
3. [引擎总览](../architecture/overview_ZH.md)
4. [持久数据生命周期](../architecture/data-lifecycle_ZH.md)
5. [AgentFlow 应用模型](../architecture/agent-flow_ZH.md)
6. [AuthGuard 边界](../architecture/authguard-integration_ZH.md)
7. [API Server 路由/状态边界](../architecture/engine/apiserver_ZH.md)
8. [Security Autonomy Fixer](../../use-cases/security-autonomy-fixer/README_ZH.md)
9. [K8s 人工验收](../../use-cases/security-autonomy-fixer/e2e/MANUAL_ACCEPTANCE.md)

现行行为以源码和以上文档为准；`docs/development/archive/` 仅为历史背景。

## 新机器初始化

```bash
git clone <flowgent-remote> flowgent
cd flowgent
git checkout dev
git submodule update --init --recursive

go version                 # 当前 go.work 要求 1.26
node --version              # CI/Build 当前使用 Node 22
helm version
kubectl version --client

python3 -m venv use-cases/security-autonomy-fixer/e2e/.venv
use-cases/security-autonomy-fixer/e2e/.venv/bin/python -m pip install \
  -r use-cases/security-autonomy-fixer/e2e/requirements.txt
use-cases/security-autonomy-fixer/e2e/.venv/bin/python -m playwright install chromium
```

GFW 环境：

```bash
export HTTPS_PROXY=http://127.0.0.1:8800
export HTTP_PROXY="$HTTPS_PROXY"
export IN_CN_GFW=true
```

仅在获准 E2E 时可加载、但不得查看或输出凭据：

```bash
source ~/.wl4gshrc.sec
```

不得把该文件、其值、验收密码、敏感报告或 Kubernetes Secret 写入本文或 Git。

`agentvault/` 是独立 Rust Submodule，只能使用其 Makefile。Rust Build 前检查
`agentvault/target/`；超过 10 GiB 时使用其 Makefile Clean Target，禁止由 Flowgent
自动化直接运行 Cargo。

## 当前架构摘要

- **公网边界：** Envoy Gateway + AuthGuard。9999 是受保护业务监听；9990 是私有控制面，
  **MUST NOT** 公网暴露。
- **业务状态：** API Server 拥有通用 PostgreSQL/SQLite 业务表访问。独立 A2A 只拥有
  `orh_a2a_task`；JobManager 只可把 PostgreSQL/Redis 用作协调锁。
- **控制面：** Controller 根据活跃 Run 管理 Application JM；JM 拥有 DAG 并协调
  TM/Sandbox。Plan/Result/Sandbox Work 走 MQTT，持久状态走 REST。
- **稳定身份：** Flow、Agent、便携式 Skill、Knowledge Document 均使用稳定 Owner +
  不可变 Revision；Run 引用确定 Flow Revision。
- **上下文：** Instruction、Knowledge、Execution Memory 分离。公开/持久层规范名是
  NodeRun，`TaskRunInfo` 只是内部兼容名。
- **Knowledge：** Source → Document → Revision → 层级 Content → Embedding；PG 使用
  pgvector，SQLite 使用 sqlite-vec；发布走 Candidate → Approval → 事务 CAS。
- **安全：** AuthGuard 拥有 IAM 与授权决策；Flowgent 只保存 canonical principal
  引用，并在正文进入 LLM 前执行 SQL Scope/ACL。
- **UI：** Overview、Run/Trace/Log/File Preview、可视化 Flow、Agent、两类 Skill 管理、
  MCP、LLM、Published Knowledge、Notification、Runtime Config 与嵌入式账户安全已实现。
  Runtime Skill 管理已实现，但 `kind: skill` DAG Node 执行未实现。
- **部署：** Helm 以分布式模式为主。E2E 把 Flowgent、AuthGuard、Storage、SonarQube、
  Mock 与 Workload 放入独立 `e2e-flowgent-*` Namespace；Namespace 内使用正式资源名。

## 已实现能力

| 领域 | 当前实现 |
|---|---|
| REST CRUD | `pkg/api/pkg/server.go` 规范路由、严格 OpenAPI、AuthGuard Scope |
| Revision | Flow/Agent/Skill/Document 稳定身份、不可变 Revision、CAS 指针 |
| Run/NodeRun | Attempt、输入输出错误、Checkpoint、Message、Lease、Fencing、Workspace Version |
| Runtime | Application/Session，Controller/JM/TM/Sandbox Ownership 与 GC |
| 统一审批 | `orh_approval` 覆盖 Human/Tool/Payment/Knowledge/Instruction |
| Knowledge 发布 | Candidate/Request Hash、审批、Baseline CAS、Outbox/Event 同事务 |
| 检索 | PG FTS + pgvector、SQLite FTS + sqlite-vec、父级/相邻扩展 |
| 安全总结 | 默认关闭；只处理 Completed Run；脱敏后只生成待审批 Candidate |
| Secret | Notification AES-256-GCM 只写字段；运行凭据只保存环境引用 |
| Observability | MQTT W3C Context、Jaeger Proxy、Run/NodeRun 关联、UI Waterfall |
| Flow Release | 不可变目录、Grant、消费方自有安装副本 |
| A2A | A2A 0.3 JSON-RPC/Agent Card 与 Principal 隔离的持久 Task Store |
| Browser | S41–S46 真实 Chromium CRUD/Login/Trace/File Preview |
| Cleanup | Helm/Namespace 正常删除 + 有界 Finalizer 恢复，`/finalize` 最后兜底 |

Schema 事实源：

- [`migration/postgres/20260616a/01_init.ddl.sql`](../../migration/postgres/20260616a/01_init.ddl.sql)
- [`migration/sqlite/20260616a/01_init.ddl.sql`](../../migration/sqlite/20260616a/01_init.ddl.sql)
- [`migration/SCHEMA_MAPPING.md`](../../migration/SCHEMA_MAPPING.md)

## 未完成与已知问题

### P0：迁移可复现性

1. **发布包含账户安全页 Iframe Header 修复的不可变 AuthGuard Web 镜像。** 当前 E2E
   机器曾从相邻 AuthGuard Source Commit `fed51c9` 重建 Web，使
   `/auth/account/security` 使用 `X-Frame-Options: SAMEORIGIN` 和 CSP
   `frame-ancestors 'self'`。Registry `1.0.1` 此前没有该行为，同一 Tag 又在本机被
   Retag；新机器即使拉取 `1.0.1` 也可能令 S46 失败。必须发布新 Tag/Digest、更新
   Chart/E2E Pin，并移除 Mutable Retag 依赖。

   临时恢复：从 `/home/agent/authguard/web` 的 `fed51c9` 构建，设置
   `FLOWGENT_E2E_AUTHGUARD_WEB_IMAGE` 或 Retag/Import，并先验证 nginx Rule。
2. 本次文档修改在 Review/Commit 前必须随 Workspace 一起迁移。

### P1：确定性与恢复

1. `context_snapshot` 已事务采集，但 Agent 仍可能按名称取 latest；Knowledge Search
   未限制到快照 `knowledge_revision_ids`。必须让运行时真正消费锁定 Revision。
2. 默认 System Instruction 只保存 `deployment://system-instruction` 引用；需要不可变
   部署 Revision，或归档有效正文/Hash。
3. NodeRun Checkpoint/Fencing 结构已存在，但并非所有重派路径都会把持久 Checkpoint
   装入 `ExecutionPlan`。需 Kill TM/JM 的恢复测试，证明同 Attempt 恢复且不合并失败分支。
4. 多个已注册 DAG Node 不是端到端可用能力：`SkillExecutor` 与
   `SubflowExecutor` 只返回 Stub Marker；`MapExecutor` 未连接已有 Fan-out Runner；
   `HumanExecutor` 会持久化 Approval，但 JobManager 不会暂停/恢复 DAG；
   Supervisor Action 仅校验，不会应用 Retry/Redirect/Inject/Abort 图变更。
   对外宣称支持前必须补 Contract Test 与定向 E2E。
5. `orh_effect_execution` 只有 Schema、无 Go Runtime Owner。Tool/Payment 副作用必须接入
   Request Hash、Idempotency、Result 与 Reconciliation 后才能宣称 Exactly-once。
6. Bootstrap DDL 的空库验证完善，但下一次不兼容生产变更需要非破坏迁移/回滚路径。

### P1：外部集成

1. **AgentVault：** Flowgent 仍是带 Build Tag 的旧 `wallet.sign.v1` Client 和本地 Policy
   Ledger；需迁移到固定 AgentVault SDK/API，并增加真实 Settlement/Reconciliation E2E。
2. **Gemini：** `pkg/core/pkg/llm/gemini.go` 仍是 Placeholder；UI/API 接受 Gemini Type，
   Runtime 执行未实现。
3. **Embedding Ingestion：** 原生存储/检索已实现，但缺完整生产级 Parser、Chunker、
   Embedding 生成与 Reindex Pipeline。
4. **GraphRAG：** 未实现。未来应通过带 Scope 的 Knowledge Interface 扩展，不能新增
   无治理的第二套 Knowledge Store。

### P2：收敛与增强

1. Go Entity 仍保留 `AgentFlowID`、`Vars`、Provider Endpoint、MCP URL/Type、
   `TaskRunInfo` 等内部兼容 Alias。公开 REST/DB 已规范；只能在 MQTT/A2A 兼容测试保护下移除。
2. Helm `global.mode: all-in-one` 仍标 TODO；Chart 以 Distributed 为主，应实现或移除。
3. 根 Makefile 尚无 PostgreSQL Integration Test Target。新增根级 Target 前，不得用临时
   `go test` 绕过仓库规则。
4. Console Import/Export 覆盖 Runtime Skill，但不覆盖便携式 Skill Definition/File；
   若需要迁移，应定义签名 Archive Contract，不能随意复制 Workspace。
5. Ready Sibling 当前逻辑独立但物理顺序分派；应在恢复语义稳定后实现有界并行。

### 外部/环境限制

- 上次 E2E 时可用 DeepSeek Credential 返回 HTTP 402（余额不足）；23/23 完整编排默认
  使用明确的集群内 deterministic OpenAI-compatible Fixture。真实 DeepSeek Provider
  仍保留，但不是该 23/23 的执行后端。
- K8s E2E 依赖本地 k3s 和 `KUBECONFIG=/etc/rancher/k3s/k3s.yaml`；GFW 阻断 Docker Hub
  时会经镜像源预热 local-path helper。
- `cleanup` 只允许对 Owned `e2e-flowgent-*` Namespace 和白名单 Cluster Resource
  强制移除 Finalizer，绝不能泛化到生产 Namespace。

## 推荐顺序

1. Review/Commit 本次架构与交接文档。
2. 发布并 Pin AuthGuard Web 固定镜像，在无本地 Cache 的新机器证明 S46。
3. 端到端强制 ContextSnapshot Revision，并增加 Checkpoint/Workspace/Fencing Kill Test。
4. 完成 Human/Map/Subflow/Skill/Supervisor Runtime 语义与定向测试。
5. 接入 `orh_effect_execution`，随后实现 AgentVault Adapter 与真实支付 E2E。
6. 实现生产 Embedding Ingestion/Reindex。
7. 实现 Gemini，或在 Runtime 可用前禁止创建 Gemini Provider。
8. 最后再做并行 DAG、GraphRAG、All-in-one Helm 与 Alias 清理。

## 验证门禁

Flowgent Go 操作只走根 Makefile：

```bash
make fmt
make test-ut
make test-x402
make test-web
make test-e2e-runner
make build
```

纯文档至少运行：

```bash
git diff --check
make test-e2e-runner
```

完整 K8s：

```bash
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
export HTTPS_PROXY=http://127.0.0.1:8800
source ~/.wl4gshrc.sec
E2E_ARGS='--timeout 1200' make e2e-security-autonomy-fixer-with-helm
```

默认保留 Pod/Data。确认目标后才执行 Owned Cleanup：

```bash
use-cases/security-autonomy-fixer/e2e/.venv/bin/python \
  use-cases/security-autonomy-fixer/e2e/runner.py cleanup --deployer k8s
```

CLI/UI 验收见
[`MANUAL_ACCEPTANCE.md`](../../use-cases/security-autonomy-fixer/e2e/MANUAL_ACCEPTANCE.md)。
按其中命令从 Kubernetes Secret 获取临时验收账号，禁止硬编码进文档。

## 事实源导航

| 问题 | 入口 |
|---|---|
| REST Route | `pkg/api/pkg/server.go`、Handler、OpenAPI Test |
| 持久模型 | Bootstrap DDL、`pkg/model/pkg/entities`、`pkg/storage` |
| Run Snapshot | `pkg/storage/pkg/flowrun/flowrun_{postgres,sqlite}.go` |
| Knowledge/Vector/Publish | `pkg/storage/pkg/knowledge`、Knowledge Handler |
| Summarizer | `pkg/core/pkg/engine/jobmanager/jobmaster.go` |
| Retry/Checkpoint | JobManager、ResourceManager、TaskManager、`orh_node_*` Storage |
| AuthGuard Scope | `pkg/api/pkg/authz`、Repository Scope、Helm Adapter Values |
| K8s 生命周期 | `pkg/controller`、ResourceManager、`deploy/helm/flowgent` |
| E2E 拓扑 | `e2e/common/config.py`、`e2e/deploy/`、Verifier |
| Cleanup | `e2e/deploy/base/kubernetes_cleanup.py` |
| UI | `web/src/app/router.tsx`、`web/src/features/` |
| AgentVault | `agentvault/README.md` 与其 Makefile；旧 Client 在 `pkg/core/pkg/client/signclient` |

## 禁止回退

- 不得重建 IAM 表或接受客户端伪造审计 Principal。
- 不得把 Instruction、Knowledge、Execution Memory 合并成万能表/JSON。
- 不得以 JSON/TEXT 存 Vector 或混算不同 Profile。
- 不得公网暴露 9990、解析后的 Credential、Notification 明文、Prompt/Model Response 或
  任意 Payload Preview 到 Telemetry。
- 不得绕过 Candidate → Approval → CAS 发布可复用 Knowledge/Instruction。
- 不得把 Jaeger 当恢复存储。
- 不得在无 Wire/Upgrade Test 时机械重命名内部兼容字段。
- 不得绕过指定 Makefile 直接运行 Go/Cargo Build。
- 使用 Fixture 或旧 Client 时，不得宣称外部 Provider/AgentVault E2E 已通过。

