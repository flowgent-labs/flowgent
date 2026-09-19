# API Server

[系统总览](../overview_ZH.md) · [AgentFlow 应用层](../agent-flow_ZH.md)

API Server 是 Flowgent 的控制面网关，也是唯一允许连接 PostgreSQL 或 SQLite
的进程。外部客户端通过 REST 或 A2A 进入系统；其他所有引擎组件都通过
`FlowgentClient` REST 调用读写持久状态。Flow 生命周期变更通过 MQTT 发布。

```text
UI / REST / A2A / webhook
           │
           ▼
      API Server ──────► PostgreSQL / SQLite
           │
           ├──────────► S3 / GCS Task Artifact（可选）
           ├──────────► MQTT 生命周期事件
           ├──────────► Jaeger Query API（只读 Trace 代理）
           └──────────► HTTP 响应 / 触发结果
```

## API 与状态网关

**唯一拥有数据库访问权的组件。** 其他组件只能通过 API Server REST 端点
读写状态，或通过 MQTT 进行实时调度。这与 Kubernetes 的 apiserver→etcd
模式一致。

1. **唯一 DB 连接**——通过单个连接池访问 PG/SQLite，连接上限 20。
2. **Flow 定义缓存**——内存 Map；CRUD 时失效，并通过 watch 推送给 Controller。
3. **MQTT 生命周期事件**——发布 Flow/Run 生命周期事件，供 Controller、JM 等实时消费。
4. **状态写入端点**——JM/TM/Sandbox 通过 REST（FlowgentClient）更新 Run/Task 状态；Notifier 通过 API 读取 Channel。
5. **多租户**——JWT/OIDC/GitHub OAuth 认证、限流与 Namespace 路由。
6. **独立扩缩容**——自身无状态，可部署 2 个以上副本；JM 有状态并选主。

### REST API（端口 9999）

| 路由 | 方法 | 说明 |
|---|---|---|
| `/_/healthz` | GET | 健康检查 |
| `/api/v1/webhook/{provider}` | POST | SCM Webhook 触发器（GitHub/GitLab/Gitea），按 Provider 适配请求体 |
| `/api/v1/{namespace}/agents` | GET/POST | 列出/创建 Agent 定义 |
| `/api/v1/{namespace}/flows` | GET/POST | 列出/创建 Flow |
| `/api/v1/{namespace}/flows/{id}` | GET/PUT/DELETE | Flow CRUD |
| `/api/v1/{namespace}/flows/{id}/runs[...]` | GET/POST/DELETE | 面向精确 Flow 授权的 Run/Task/Approval/Trace 嵌套接口 |
| `/api/v1/{namespace}/flows/{id}/iam/{options,bindings}` | GET/POST/DELETE | 类 Repository 的 Flow Settings 权限管理 |
| `/api/v1/{namespace}/runtime-config[/environment\|/secrets]` | GET/PUT | Namespace 环境变量默认值与只写 Secret |
| `/api/v1/{namespace}/flows/{id}/runtime-config[/environment\|/secrets]` | GET/PUT | Flow 局部覆盖与脱敏后的有效继承结果 |
| `/api/v1/{namespace}/flows/{id}/runtime-config/resolved` | GET | 仅 Controller 可读的 K8s 运行时物化值 |
| `/api/v1/{namespace}/skills` | GET/POST | 列出/创建运行时 `kind=skill` DAG 定义 |
| `/api/v1/{namespace}/skills/{id}` | GET/PUT/DELETE | 运行时 Skill CRUD |
| `/api/v1/{namespace}/flows/trigger` | POST | 创建 `PENDING` Run |
| `/api/v1/{namespace}/runs` | GET/POST | 列出 Run（JM 轮询）/创建 Run |
| `/api/v1/{namespace}/runs/{id}` | GET/PUT | Run 状态查询与更新 |
| `/api/v1/{namespace}/runs/{id}/tasks` | GET/POST | Task 列表与创建（JM/TM 写入） |
| `/api/v1/{namespace}/runs/{id}/tasks/{tid}` | PUT | 更新 Task 状态（TM 写入） |
| `/api/v1/{namespace}/runs/{id}/trace` | GET | 查询一个归属 Run 的规范化 OTel Trace |
| `/api/v1/{namespace}/runs/{id}/cancel` | POST | 取消运行中的 Flow |
| `/api/v1/{namespace}/runs/{id}/approvals` | GET | 列出一个归属 Run 的审批项 |
| `/api/v1/{namespace}/runs/{id}/approvals/{token}/approve` | POST | 批准一个归属 Run 的 Gate |
| `/api/v1/{namespace}/runs/{id}/approvals/{token}/reject` | POST | 拒绝一个归属 Run 的 Gate |
| `/api/v1/{namespace}/notifications/channels` | GET/POST | 列出/创建 Notifier Channel |
| `/api/v1/{namespace}/notifications/channels/{id}` | GET/PUT/DELETE | Notifier Channel CRUD |
| `/api/v1/{namespace}/llm/providers` | GET/POST | 列出/创建 LLM Provider 定义 |
| `/api/v1/{namespace}/mcp` | GET/POST | 列出/创建 Streamable HTTP MCP 定义 |
| `/api/v1/{namespace}/iam/{principals,groups,roles,bindings}` | GET/POST/PUT/DELETE | Namespace 成员、团队、角色与授权 |
| `/api/v1/{namespace}/iam/api-keys` | GET/POST/DELETE | 仅显示一次、可撤销的 Namespace 机器凭据 |
| `/api/v1/{namespace}/flow-releases` | GET/POST | 不可变共享 Flow Release 目录 |
| `/api/v1/{namespace}/flow-releases/{id}/{grants,install}` | GET/POST/DELETE | 生产方授权与消费方自有安装副本 |
| `/api/v1/human/approvals` | GET/POST | 列出待审批项/创建人工审批 |
| `/api/v1/human/{token}/approve` | POST | 人工批准 |
| `/api/v1/human/{token}/reject` | POST | 人工拒绝 |

### Run Trace 查询

API Server 调用 Jaeger 的只读 Query API，并把 Jaeger 存储格式转换为稳定的
`RunTrace`/`TraceInfo`/`TraceSpan` 模型；UI 不直接访问 Jaeger。查询前 Handler
必须加载 Run 并校验其 Namespace 与路径完全一致。Run 不存在返回 404，未配置
查询后端返回 503，上游查询失败返回 502；成功响应使用 `private, no-store`。

Jaeger 是可观测数据源，不是恢复数据库。每次 attempt 的精确输入/输出/错误
仍保存于 TaskRun。Span 通过 `flowgent.task_id` 关联，
`flowgent.node_id` 与 `flowgent.attempt` 用于辅助诊断。Query API 在
`mgmt.otel.query_*` 下配置 timeout、lookback 与 trace 数量，并限制单次响应为
16 MiB。

Run 与 TaskRun Handler 在读取、修改、取消或列出 Task 前使用相同的 Namespace
归属校验。Task 更新以已校验的路径 Run/Namespace 覆盖 Body 中的身份字段；若
Task ID 已属于另一 Run，则拒绝该请求。

### TaskRun Payload 存储

每次 attempt 的完整输入/输出属于业务审计数据，而不是遥测数据。API Server
持有 `ITaskPayloadProvider`，透明完成两个字段的持久化与还原；JobManager、
TaskManager 和 UI 继续使用不变的 `TaskRunInfo` REST 契约。

配置属于 `storage.artifacts`，不属于 `mgmt.otel`：

```yaml
storage:
  artifacts:
    provider: default            # default | s3 | gcs
    inline_max_bytes: 262144     # S3/GCS 混合内联阈值
    max_payload_bytes: 67108864
    compression: gzip            # none | gzip
    prefix: flowgent/task-runs
    put_timeout: 30s
    get_timeout: 30s
    verify_checksum: true
```

`DefaultTaskPayloadProvider` 将完整 JSON 保存在现有 DB 列中；
`S3TaskPayloadProvider` 与 `GCSTaskPayloadProvider` 保留较小值内联，并把较大值
替换为带版本的引用，其中包含 Provider、对象 Key/URI、编码、原始/存储大小和
SHA-256。读取会执行大小限制、解压和 Checksum 校验，并在 REST 响应前还原。
DB 写入失败时清理本次新对象；删除 Run 时尽力清理其对象。

S3 默认使用 AWS SDK Credential Chain；GCS 默认使用 Application Default
Credentials。GCS 匿名访问仅允许显式 Emulator/兼容 Endpoint。切换 Provider
前必须迁移历史引用，否则新部署无法读取由旧 Provider 管理的历史对象。

### 浏览器安全的运行时凭据引用

LLM 与 MCP Mutation 只持久化环境变量引用，不接受内联凭据值。LLM 读取会清空
API Key/Credentials，只返回 `api_key_env` 与 `key_configured`；MCP 读取会脱敏
敏感 Header 和 Env 值，同时返回安全的 `header_refs`/`env_refs` 元数据。运行时
Pod 从 `runtime.credential_env_secret` 指定的 Secret 解析这些引用，浏览器不会
获得解析后的值。Notification Channel 使用独立的动态 Secret 契约：由 Provider
Schema 明确定义的敏感字段在提交 Channel 记录前使用 AES-256-GCM 加密；读取只返回
`configured_secret_fields`。空值更新保留原密文，只有 `clear_secret_fields` 能显式
删除。部署通过 `notifier.secret_encryption` 提供带版本的主密钥环，主密钥绝不进入
Flowgent 业务数据库。

运行时 `/skills` 路由只管理 `kind=skill` Flow 定义；便携式 `SKILL.md` Package、
文件、归档与部署属于独立的未来 API 生命周期。

### A2A 协议（端口 9992）

Standalone 与 All-in-one 使用同一个 A2A 0.3 Server 和数据库共享 Task Store。
Agent Card 声明 JSON-RPC 与 Bearer Security Scheme。每个协议请求先通过 API
Server `/api/v1/auth/me` 校验调用方，再把同一个凭据转发给 REST，因此沿用调用方
的正常 RBAC 决策。Task Store 按凭据 SHA-256 摘要隔离，绝不持久化 Bearer 明文。

| 路由 | 方法 | 说明 |
|---|---|---|
| `/.well-known/agent.json` | GET | A2A 0.3 Agent Card |
| `/` | POST | 标准 JSON-RPC（`message/send`、`tasks/get` 及 SDK Task 方法） |
| `/_/healthz` | GET | 进程健康检查；不会绕过业务请求认证 |

### 认证与多租户

一个部署就是隐式 Enterprise 边界，刻意不增加 Enterprise/Workspace 数据实体。
`namespace_id` 同时是 Organization、业务团队、运行时、数据与 Secret 隔离边界。
Principal 是部署级身份，以不可变 `(issuer, external_id)` 映射；身份通过显式
Membership Edge 加入一个或多个 Namespace。移出某个 Namespace 只撤销该组织的
Group Membership 和 Binding，不会删除全局企业身份。

授权默认拒绝，分 Platform、Namespace 与精确资源三级 Scope。Group 是 Namespace
拥有的 Team。内置或自定义 Role 可绑定 User、Service Account 或 Team；显式 DENY
优先于 ALLOW，Binding 可过期或限制来源 CIDR。Flow Binding 的资源是精确的
`namespace/flow` 对。浏览器使用
`/{namespace}/{flow}/...`（仅 Namespace 管理保留在
`/namespaces/{namespace}/settings`）；REST 把 Run 接口嵌套到 Flow 下，令
Definition、Run、Task、Approval 与 Trace 共享一致的 Repository 边界。Namespace
Owner 在 Namespace Settings 管理成员；Flow Owner 在该 Flow Settings 管理直接授权。
系统禁止删除最后一个 Namespace Owner，也禁止当前身份移除自身。

JWT（ES256/RS256/EdDSA）、GitHub 浏览器 SSO、通用 OIDC 与 LDAP 身份共用相同
Evaluator。GitHub SSO 使用标准 OAuth2 authorization-code flow；通用 OIDC 使用
Provider Discovery、ID Token 验签和 Nonce 校验。浏览器登录成功后，Callback
设置 HttpOnly `flowgent_session` Cookie 并返回 `303 /dashboard`；Callback
不会把 Token 写入 HTML 或浏览器存储，因此 CSP 可保持 `script-src 'self'`。
手动 Bearer 输入仅用于 API Key 与 break-glass Token。LDAP 密码登录成功后也遵循
同一个浏览器 Session Cookie 契约。`POST /auth/logout` 清理 Session Cookie。

Controller、JM、TM、Notifier 与 A2A 分别使用 Kubernetes Secret 投递的可轮换
Workload Token。API Key 明文只显示一次；数据库只保存 SHA-256 摘要、前后缀、
有效期、撤销状态、Namespace 限制和显式 Permission 限制。Namespace Owner 可为
该组织 Service Account 签发 Key，但最终权限仍受目标 Principal 的 Role Binding
限制。

跨团队复用通过生产方拥有的不可变 `FlowRelease` 完成。消费方获得显式 Grant 后，
把副本安装到自己的 Namespace，并自行拥有资源绑定、Secret、Run、Trace 与结果；
消费方不会直接运行生产方可变 Flow，也不会继承生产方 Secret Scope。

### 提交路径（API → Store → JM）

API Server 仅负责持久化，不直接调用 JM。Run 有两条进入系统的路径。

**路径 A：API 触发（同步写入、异步执行）**

```text
POST /api/v1/{namespace}/flows/trigger
  → agentFlowHandler 验证 Spec 存在
  → run := { AgentFlowID, Vars, Status:PENDING, Namespace, Namespace:"" }
  → 通过 API Server Store 持久化       // ← 同步阶段在此结束
  → 向调用方返回 run_id
  ...（稍后异步执行）...
  → JM 的 runPoller（2 秒周期）通过 FlowgentClient.ListRuns 找到 PENDING Run
  → jm.Submit(run, spec) → JobMaster.Execute()
```

**路径 B：Controller 分发（完全异步）**

```text
Controller 每 10 秒轮询 API Server ListFlows：
  → Hash-mod 分片：只处理本副本拥有的 Flow
  → 触发入口创建带 namespace + runtime_mode 快照的 PENDING FlowRun
  → 活跃 Run 协调创建专用 K8s JM Deployment
  ...（稍后异步执行）...
  → 专属 JM 只获取自己的 Flow Run
  → jm.Submit(run, spec) → JobMaster.Execute()
```

---

## 预期行为

1. **给定**任意持久资源变更，**当**变更成功时，**则** API Server **MUST** 是将其提交到 PostgreSQL 或 SQLite 的进程。
2. **给定**有效的 REST、A2A 或 Webhook 触发，**当**请求被接受时，**则** API Server **MUST** 在返回 ID 前持久化一个 `PENDING` Run。
3. **给定** Flow 创建、更新或删除，**当**持久化成功时，**则 MUST** 发布对应 MQTT 生命周期事件供 Controller 协调。
4. **给定**未授权的 Namespace 请求，**当**认证或 RBAC 失败时，**则 MUST NOT** 泄露或修改任何资源状态。
5. **给定**非 API 组件提交状态更新，**当** REST 校验失败时，**则**数据库 **MUST** 保持不变，且调用方 **MUST** 收到可处理的错误。
