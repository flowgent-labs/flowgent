# 通用企业级独立权限架构设计白皮书

**状态：** Architecture Baseline
**日期：** 2026-08-21
**适用范围：** 通用企业 IAM / App Gateway / API Server / Flowgent / Sigbot Core
**当前落地：** Flowgent 开源项目内置于 Go API Server
**未来复用：** 可按本文模型在 `../sigbot-core` Rust 项目中实现同构权限模块

## 1. 摘要

本文定义一套通用、独立、企业级的身份与授权架构。它不绑定任何具体业务系统，也不把 Flowgent 的 `namespace` 或 `agent flow` 写死为权限系统基础概念。业务系统只需要把自己的对象抽象成受保护资源，通过统一 Resource URN、action、role、grant、condition 与业务资源 adapter 完成权限控制。

核心结论：

- 认证只回答“调用方是谁”。
- 授权只回答“调用方能对哪个资源执行什么动作”。
- 外部 GitHub/OIDC/LDAP identity 只用于绑定，不直接作为业务授权主体。
- 授权主体以 `iam_subject` 表达 user / service account / workload / system，以 `iam_group` 表达团队或集合主体。
- 运行时统一抽象为 `PrincipalSet = subject + groups`。
- 权限动作使用 `iam_action`，角色使用 `iam_role`，授权事实使用 `iam_grant`。
- 资源身份使用基于 RFC 8141 URN 语法风格的 `urn:iam:...` 统一资源名称。
- IAM 核心不维护 `iam_resource` 资源表；资源真相由业务系统自己的表维护。
- request 四元组只用于 route/resource matcher，不作为资源权限模型本身。
- 资源列表查询由业务 Resource Adapter 将 effective grants 编译成业务 SQL scope。

在大型企业内部，该系统通常适合实现为独立 App Gateway / IAM Microservice；在开源项目中，为降低部署复杂度，可以内置在项目 API Server 内。两种形态必须保持相同的模型、判定算法和审计语义。

### 1.1 一个最小授权故事

Alice 通过 GitHub 登录。GitHub 只证明“这是 Alice”，不决定 Alice 能访问什么。系统把 GitHub identity 绑定到内部 subject：

```text
github:29530154
  -> iam_subject:alice
```

Alice 属于 `security-team`：

```text
iam_subject:alice
  -> iam_group_member
  -> iam_group:security-team
```

管理员给 `security-team` 授权：

```text
iam_group:security-team
  -> iam_grant
       urn = urn:iam:prod:flowgent:global:security:agent-flow/*
  -> iam_grant_role
  -> iam_role:reader
  -> iam_role_action
  -> iam_action:agentflow.read
  -> iam_action:agentflow.trace.read
```

当 Alice 请求查看某个 flow 的 trace：

```text
GET /api/v1/security/flows/security-autonomy-fixer/runs/run-123/trace
```

route matcher 生成：

```text
action       = agentflow.trace.read
resourceURN  = urn:iam:prod:flowgent:global:security:agent-flow/security-autonomy-fixer/run/run-123
parentURNs   = [
  urn:iam:prod:flowgent:global:security:agent-flow/security-autonomy-fixer,
  urn:iam:prod:flowgent:global:security:namespace/security,
  urn:iam:prod:flowgent:global:platform:platform/root
]
```

授权器发现 `security-team` 拥有 `reader` role，role 包含 `agentflow.trace.read`，grant pattern 覆盖该 flow，于是允许请求。

## 2. 设计目标

### 2.1 功能目标

- 支持 GitHub / OIDC / LDAP / Password / API Key / Workload Token。
- 支持 subject、group、role、grant、action catalog、route/resource matcher。
- 支持平台级、租户/组织/域级、资源级授权。
- 支持 GitHub 风格的 organization/repository 授权体验，但不绑定 GitHub 领域模型。
- 支持任意业务资源类型，例如 repository、agent-flow、bot、workflow、dataset、channel。
- 支持资源层级继承，例如 namespace owner 可继承访问其下 agent-flow。
- 支持后端 middleware / gateway 强制拦截。
- 支持 UI 读取 auth context 用于菜单、按钮、无权限状态控制。
- 支持审计认证、授权判定和授权管理动作。

### 2.2 工程目标

- 高内聚：authn、identity resolver、matcher、evaluator、audit 模块边界清晰。
- 低耦合：IAM 核心不依赖具体业务表结构。
- 无重复：identity 不拆表重复字段，route matcher 不拆多张半结构化表，资源目录不在 IAM 核心重复维护。
- 可跨语言实现：Go、Rust 等语言共享数据语义、授权算法和 golden test fixtures。
- 可内置也可独立部署：Flowgent 可内置在 API Server，企业内部可抽取为 Gateway/IAM Service。

### 2.3 非目标

- 不把 UI 权限隐藏当作安全边界。
- 不把第三方 provider group 直接作为最终授权主体。
- 不把 request method/path/query 当作资源权限模型。
- 不在 IAM 核心维护业务资源生命周期。
- 不支持任意 regex 资源授权 pattern；v1 只支持可下推查询的有限 wildcard。
- 不保留旧兼容模型。

## 3. 总体模型

统一授权链路：

```text
external identity
  -> iam_subject
  -> PrincipalSet(subject + groups)
  -> iam_grant(urn + conditions)
  -> iam_grant_role
  -> iam_role
  -> iam_role_action
  -> iam_action
  -> authorization decision
```

单请求判定链路：

```text
HTTP request
  -> authn middleware
  -> route/resource matcher
  -> action identifier
  -> resource URN + parent resource URNs
  -> effective grants
  -> role/action check
  -> condition check
  -> ALLOW / DENY
```

资源列表查询链路：

```text
current subject
  -> effective grants for requested action
  -> urns
  -> business Resource Adapter
  -> SQL predicate / query scope
  -> business DB
```

### 3.1 核心关系图

```text
                         ┌─────────────────────┐
external identity ─────▶ │     iam_subject      │
                         └──────────┬──────────┘
                                    │ membership
                                    ▼
                         ┌─────────────────────┐
                         │      iam_group       │
                         └──────────┬──────────┘
                                    │ grantee
                                    ▼
┌──────────────────────────────────────────────────────────────┐
│                         iam_grant                            │
│  grantee_type + grantee_id + urn + condition │
└──────────────────────────────┬───────────────────────────────┘
                               │ grant roles
                               ▼
                         ┌──────────────┐
                         │   iam_role   │
                         └──────┬───────┘
                                │ role actions
                                ▼
                         ┌──────────────┐
request ── matcher ───▶  │  iam_action  │
                         └──────┬───────┘
                                │
                                ▼
                         decision: ALLOW / DENY
```

其中只有业务系统知道资源表结构。IAM 只看 Resource URN 和 action；业务 Resource Adapter 负责把 grant pattern 翻译成业务查询条件。

## 4. 核心概念

### 4.1 Subject

Subject 是可认证、可审计、可直接授权的内部主体。它可以是人工用户、service account、workload identity 或系统主体。

使用 `iam_subject` 而不是 `iam_user`，原因是 `user` 只覆盖 human user，不能完整表达 service account、workload、system 等运行时身份。

外部身份不直接参与业务授权。GitHub/OIDC/LDAP 登录成功后，只用于解析或绑定内部 `iam_subject`。

### 4.2 Identity

Identity 是登录来源。为避免 subject/identity 两张表重复字段，identity 合并到 `iam_subject.identities JSONB`。

示例：

```json
[
  {"provider": "github", "issuer": "github", "subject": "29530154"},
  {"provider": "oidc", "issuer": "https://idp.example.com", "subject": "00u123"},
  {"provider": "ldap", "issuer": "corp-ad", "subject": "CN=james,OU=Users,DC=corp,DC=example"}
]
```

Identity 只保存绑定必要信息，不重复保存 email、name、avatar、last-login 等字段。

### 4.3 Group

`iam_group` 是某个资源域内的主体组/团队。资源域可以是 organization、tenant、namespace、project、business unit 等，由业务系统决定。

不建议把 group 合并进 `iam_subject` 表：

- subject 是可认证实体，group 是集合实体。
- subject 需要 identity resolver、email/service account/workload 约束。
- group 需要 scope/name/membership 约束。
- 合并后会出现大量 nullable 字段，或把 membership 做成自引用关系，增加约束、审计和迁移复杂度。
- 企业 RBAC 中 group 常被授予 role，但请求发起者仍是具体 subject。

因此数据库保留 `iam_subject` 与 `iam_group` 两张表，只在授权评估层统一为：

```text
PrincipalSet = authenticated subject + direct groups + inherited groups
```

v1 不考虑也不支持 group 嵌套。membership 只表达 `iam_subject -> iam_group`。在没有真实业务需求前，不引入 `iam_group_edge`；若未来确实需要，再新增独立边表，并强制 DAG、最大深度和循环检测。

### 4.4 Action

Action 表示“要做什么”，使用点分隔命名：

```text
resource.read
resource.write
resource.delete
resource.access.manage
resource.run.trigger
resource.trace.read
domain.member.manage
platform.admin
```

底层表名使用 `iam_action`，不使用 `iam_permission`。原因是 permission 是最终授权结果，action 是可被 role 组合的原子动作。

资源目标不进入 action 字符串，而由 route/resource matcher 和 grant 解析。

### 4.5 Role

Role 是 action 集合。常规授权应授 role，不直接给主体散粒度 action。

通用内置角色建议：

| Role | Scope | 能力 |
|---|---|---|
| owner | resource/domain | 完整管理，包括 access |
| maintainer | resource/domain | 管理资源，不管理 access owner |
| writer | resource | 修改资源和触发执行 |
| operator | resource | 执行、取消、审批，不修改定义 |
| reader | resource/domain | 只读 |
| auditor | platform/domain | 只读审计与证据 |

### 4.6 Grant

Grant 是授权事实：

```text
grantee(subject/group)
  has roles
  on resource(urn)
  under conditions
```

Grant 不做 update，只新增或删除。一个 grant 可绑定一个或多个 role，通过 `iam_grant_role` 表表达。

### 4.7 Resource URN

受保护资源是任意业务对象。IAM 使用 Resource URN 表达资源身份。

本文采用基于 RFC 8141 URN 语法风格的内部统一资源名称：

```text
urn:iam:<partition>:<service>:<region>:<tenant>:<resource-path>
```

字段含义：

| Segment | 含义 |
|---|---|
| `iam` | 内部 URN namespace identifier |
| `partition` | 管理分区或环境，例如 `prod`、`staging`、`corp` |
| `service` | 业务系统或微服务，例如 `flowgent`、`sigbot` |
| `region` | 区域；无区域资源使用 `global` |
| `tenant` | 租户、组织、namespace、account 等稳定隔离边界 |
| `resource-path` | 业务资源路径，格式由业务系统定义，建议 `<type>/<id>[/<subtype>/<id>]` |

示例：

```text
urn:iam:prod:flowgent:global:default:namespace/default
urn:iam:prod:flowgent:global:default:agent-flow/security-autonomy-fixer
urn:iam:prod:flowgent:global:default:agent-flow/security-autonomy-fixer/run/run-123
urn:iam:prod:sigbot:global:sales:bot/customer-support
urn:iam:prod:sigbot:global:sales:dataset/customer-faq
```

Wildcard pattern 示例：

```text
urn:iam:prod:flowgent:global:default:agent-flow/*
urn:iam:prod:flowgent:global:default:**
urn:iam:prod:sigbot:global:sales:bot/*
urn:iam:prod:*:global:*:**
```

v1 wildcard 规则必须可预测、可编译、可下推：

- exact segment：精确匹配。
- `*`：匹配一个 colon segment 或一个 resource-path segment。
- `**`：只允许出现在 resource-path 末尾，表示匹配子树。
- 不支持任意 regex。
- 不支持 segment 中间的模糊匹配，例如 `foo*bar`。

### 4.8 URN、ARN 与 request 四元组

ARN（Amazon Resource Name）是 AWS 的资源命名约定。本文借鉴 ARN 的资源定位思想，但不复用 `arn:` 前缀，避免被误解为 AWS ARN 兼容。

RFC 8141 定义了标准 URN 的外层形式 `urn:<NID>:<NSS>`。本文使用 `iam` 作为内部 NID，并在 NSS 中定义固定分段。若未来需要公开跨组织互操作，应注册正式 NID 或提供明确的 namespace 兼容声明。

request 四元组，例如 method、uri/path、query params、path params，只用于 route/resource matcher：

```text
request tuple
  -> action
  -> resource URN
  -> parent resource URNs
```

它不替代 Resource URN。IP、method、query、MFA、时间等也不进入 URN，而进入 `conditions_json`。

### 4.9 为什么需要 `parentUrns`

一个请求通常命中叶子资源，但授权经常授在父级范围。

示例：

```text
GitHub:     repo 继承 org 授权
Flowgent:   flow/run 继承 flow 与 namespace 授权
S3:         object 继承 bucket 或 access point 授权
```

如果 matcher 只返回叶子 Resource URN，那么 org/namespace/bucket 级授权只能走两种坏设计：

- 把父级 grant 物化复制到所有子资源；
- 让 evaluator 查询业务表，临时推导父资源。

前者会产生海量 grant 与一致性问题，后者会让 IAM core 依赖业务表结构。本文采用更收敛的方式：route/resource matcher 返回确定的父资源链。

```text
resourceUrn = 具体叶子资源
parentUrns  = 具体父资源，按从近到远排序
```

示例：

```text
resourceUrn = urn:iam:prod:flowgent:global:security:agent-flow/fixer/run/run-123
parentUrns  = [
  urn:iam:prod:flowgent:global:security:agent-flow/fixer,
  urn:iam:prod:flowgent:global:security:namespace/security,
  urn:iam:prod:flowgent:global:platform:platform/root
]
```

`parentUrns` 必须是 concrete URN，不能是 wildcard pattern。wildcard 只允许出现在 `iam_grant.urn`。evaluator 使用 grant pattern 去匹配 `[resourceUrn] + parentUrns`。

这样既能表达继承授权，又不需要展开 grant，也不需要 IAM core 理解业务表。

### 4.10 为什么 IAM 核心不需要资源表

IAM 核心不维护 `iam_resource` 表。资源是否存在、资源属性、资源生命周期应由业务系统自己的表维护，例如 Flowgent 的 namespace/flow 表、Sigbot 的 bot/channel/dataset 表。

原因：

- 避免 IAM 资源目录与业务资源表双写不一致。
- 避免 IAM 核心绑定具体业务 schema。
- 避免资源删除后 IAM 表残留导致列表越权。
- 允许不同业务系统用同一套 IAM 模型保护不同资源类型。

IAM 只保存授权目标：

```text
iam_grant.urn
```

如果某项目需要资源搜索、授权选择器、跨服务资源清单、离线审计加速，可以增加可选投影表：

```text
iam_resource_projection
```

该表只是 cache/index，不参与授权正确性；投影失效不能导致越权。

## 5. 数据模型

### 5.1 `iam_subject`

```text
id
kind            -- user / service_account / workload / system
email           -- human user email，非 human subject 可为空
name
avatar_url
identities      -- JSONB array
status
authz_version
created_at
updated_at
```

约束：

- `kind=user` 时，`email` 是平台人工用户主标识，建议做 partial unique index。
- 非 human subject 不强制 email。
- `identities` 中 `(provider, issuer, subject)` 全局唯一。
- 不保存 `last_login_at`；登录事件进入 `iam_audit_event`。
- 不在 identity 中重复保存 email/name/avatar/profile。
- `authz_version` 用于权限变更后刷新或失效旧权限上下文。

JSONB identity 全局唯一无法只靠普通 unique index 完成。resolver 必须使用事务和 advisory lock：

```text
1. identity_key = provider + "\0" + issuer + "\0" + subject
2. pg_advisory_xact_lock(hash(identity_key))
3. SELECT subject WHERE identities @> identity
4. if found return subject
5. else SELECT subject WHERE kind='user' AND email = verified_email
6. append identity or create subject
7. commit
```

### 5.2 `iam_group`

```text
id
scope_type      -- organization / tenant / namespace / project / custom
scope_key
name
description
status
created_at
updated_at
```

约束：

```text
unique(scope_type, scope_key, name)
```

### 5.3 `iam_group_member`

```text
id
group_id
subject_id
created_at
created_by
deleted_at
deleted_by
```

删除即撤销 membership。保留 `deleted_at/deleted_by` 便于审计和恢复分析。

### 5.4 `iam_action`

`iam_action` 保存动作标识符和 HTTP route/resource matcher。

```text
identifier      -- primary key，例如 resource.read
matchers        -- JSONB array
```

`matchers` 元素格式：

```json
{
  "method": "GET",
  "uri": "^/api/v1/([^/]+)/flows/([^/]+)$",
  "path": {"namespace": 1, "flow": 2},
  "queryParams": {},
  "urn": "urn:iam:prod:flowgent:global:{namespace}:agent-flow/{flow}",
  "parentUrns": [
    "urn:iam:prod:flowgent:global:{namespace}:namespace/{namespace}",
    "urn:iam:prod:flowgent:global:root:platform/root"
  ]
}
```

规则：

- `identifier` 是全局唯一 action。
- `matchers` 包含该 action 保护的所有 route matcher。
- `method`、`uri`、`queryParams`、`path` 不拆到独立字段。
- `path` 只保存 regex capture group 名称映射。
- `urn` 支持模板变量。
- `parentUrns` 列出 concrete parent resource URNs，用于继承授权判定。
- `matchers=[]` 表示该 action 只用于内部逻辑或角色组合，不直接匹配 HTTP。

### 5.5 `iam_role`

```text
id
name
scope_type       -- platform / domain / resource
builtin
description
status
created_at
updated_at
```

### 5.6 `iam_role_action`

```text
role_id
action_identifier
```

约束：

```text
unique(role_id, action_identifier)
```

### 5.7 `iam_grant`

```text
id
grantee_type                 -- SUBJECT / GROUP
grantee_id
urn
effect                       -- ALLOW / DENY
conditions_json
created_at
created_by
deleted_at
deleted_by
status
```

约束：

- `grantee_type=SUBJECT` 时，`grantee_id` 指向 `iam_subject.id`。
- `grantee_type=GROUP` 时，`grantee_id` 指向 `iam_group.id`。
- `urn` 是授权目标，可为 exact Resource URN 或有限 Resource URN pattern。字段名故意保持简短，因为表名已经是 `iam_grant`。
- active grant 应避免重复。
- grant 不做 update。
- DENY grant 优先于 ALLOW grant。

### 5.8 `iam_grant_role`

```text
grant_id
role_id
```

约束：

```text
unique(grant_id, role_id)
```

grant revoke 后，其 role links 随 grant 一起失效。

### 5.9 `iam_api_key`

```text
id
name
owner_subject_id
secret_hash
prefix
suffix
allowed_urns
allowed_actions
expires_at
revoked_at
created_at
created_by
```

API Key 明文只显示一次，数据库只存 hash。API Key 可对 owner subject 的权限做 attenuation，不能放大权限。

### 5.10 `iam_audit_event`

```text
id
actor_type
actor_id
action
resource_urn
decision
reason
grant_id
role_ids
request_id
source_ip
metadata_json
created_at
```

审计事件 append-only。Secret、token、credential 不进入 audit metadata。

## 6. 授权判定

### 6.1 请求判定算法

```text
1. Authn middleware 验证调用方。
2. Resolver 得到 iam_subject。
3. Group resolver 得到 PrincipalSet(subject + groups)。
4. Route matcher 从 iam_action.matchers 找到 action/resourceUrn/parentUrns。
5. Evaluator 加载 subject direct grants。
6. Evaluator 加载 group grants。
7. Evaluator 展开 grant roles 与 role actions。
8. 检查 action match。
9. 检查 urn 是否匹配 resourceUrn 或 parentUrns。
10. 检查 conditions_json。
11. explicit DENY 优先。
12. 默认拒绝。
13. 写 audit event。
```

### 6.2 核心伪代码

单请求授权逻辑可以压缩为如下伪代码：

```text
function Authorize(request):
    subject = Authenticate(request)
    if subject is None:
        return DENY("unauthenticated")

    principals = PrincipalSet(subject, GroupsOf(subject))

    match = MatchAction(request.method, request.path, request.query)
    if match is None:
        return DENY("no matching protected action")

    resource_urns = [match.resourceURN] + match.parentURNs
    grants = LoadActiveGrants(principals)

    decision = DENY("default deny")

    for grant in grants:
        if not AnyPatternMatches(grant.urn, resource_urns):
            continue

        role_actions = ActionsOfGrantRoles(grant.id)
        if match.action not in role_actions:
            continue

        if not ConditionsMatch(grant.conditions_json, request, subject, match.resourceURN):
            continue

        if grant.effect == "DENY":
            return DENY("explicit deny", grant.id)

        decision = ALLOW("matched grant", grant.id)

    return decision
```

这段伪代码刻意不访问业务资源表。业务资源是否存在由业务 handler 或 Resource Adapter 判断；授权器只判断“如果该资源存在，当前主体是否有权访问这个 URN”。

### 6.3 Effective grants

一次请求的有效授权来自：

```text
subject direct grants
+ subject group grants
+ API key attenuation scope
```

resource inheritance 不通过额外 grant 派生，而通过 matcher 返回的 `parentUrns` 与 grant pattern 匹配完成。

### 6.4 Conditions

`conditions_json` 表达 ABAC 条件，不表达资源身份。

示例：

```json
{
  "sourceIp": {"inCidr": ["10.0.0.0/8"]},
  "request": {"methods": ["GET", "POST"]},
  "time": {"before": "2026-12-31T23:59:59Z"},
  "subject": {"mfa": true},
  "resource": {"tags": {"environment": "prod"}}
}
```

条件属性来源：

- request 属性：由 middleware 提供。
- subject 属性：由 `iam_subject` 或认证上下文提供。
- resource 属性：由业务 Resource Adapter 从业务表提供。
- environment 属性：由部署环境或 policy context 提供。

如果 condition 依赖的属性无法可靠获取，默认条件不满足。

## 7. 资源列表查询与 Resource Adapter

单请求拦截只解决“这个请求能不能执行”。企业系统还必须解决“当前用户能看到哪些资源”。

IAM 不直接列资源。业务服务负责列资源，但必须把 IAM 返回的授权范围编译进业务查询条件。

### 7.1 Resource Adapter 接口

每种业务资源类型实现一个很薄的 adapter：

```text
BuildURN(row) -> resource_urn
BuildParentURNs(row) -> []resource_urn
CompileListScope(action, effective_grants) -> SQL predicate / query scope
LoadResourceAttributes(resource_urn) -> attributes
```

职责划分：

- IAM core：计算 subject/group 的 effective grants。
- Resource Adapter：理解业务表结构，将 `urn` 编译成 SQL predicate。
- Business DB：作为资源存在性、资源属性、资源生命周期的唯一真相。

资源列表查询伪代码：

```text
function ListResources(subject, action, resourceType, filters):
    principals = PrincipalSet(subject, GroupsOf(subject))
    grants = LoadActiveGrants(principals)

    allowed_patterns = []
    denied_patterns = []

    for grant in grants:
        if action not in ActionsOfGrantRoles(grant.id):
            continue

        if not ConditionsMatchForList(grant.conditions_json, subject):
            continue

        if grant.effect == "DENY":
            denied_patterns.append(grant.urn)
        else:
            allowed_patterns.append(grant.urn)

    scope = ResourceAdapter(resourceType).CompileListScope(
        allow = allowed_patterns,
        deny  = denied_patterns,
        filters = filters
    )

    return BusinessDB.Query(resourceType, scope)
```

关键点是：IAM 输出 grant patterns，业务 adapter 输出 SQL predicate。不能反过来让 IAM 直接扫描业务表。

### 7.2 Flowgent 查询示例

用户拥有：

```text
reader on urn:iam:prod:flowgent:global:default:agent-flow/security-autonomy-fixer
reader on urn:iam:prod:flowgent:global:security:agent-flow/*
owner  on urn:iam:prod:flowgent:global:platform:platform/root
```

Flowgent flow adapter 可编译为：

```sql
WHERE
  (namespace = 'default' AND name = 'security-autonomy-fixer')
  OR
  (namespace = 'security')
  OR
  (:has_platform_owner = true)
```

如果用户只拥有某个 flow 的直接权限，即使没有 namespace owner 权限，list flows 时也应该能看到该 flow。这与 GitHub 中“被单独授权的 repo 应出现在 repo list 中”的体验一致。

### 7.3 Pattern 下推限制

为了支持稳定 SQL 下推，v1 grant `urn` pattern 只支持有限 wildcard：

```text
exact
*
trailing /*
trailing /**
```

不支持任意 regex。否则只能全表扫描后过滤，无法满足企业级性能、审计和稳定性要求。

### 7.4 一致性策略

资源真相由业务表维护，因此不会出现 IAM 资源目录与业务资源表双写不一致。

授权创建时可选两种策略：

- 强校验：同服务内 grant 创建前调用业务 Resource Adapter 确认资源存在。
- 弱校验：允许预授权未来资源；资源不存在时 list 不返回，请求时业务服务返回 not found。

资源删除后，grant 可以异步清理。由于列表查询来自业务表，已删除资源不会因为 grant 残留被展示；请求也会由业务服务返回 not found。

## 8. 授权场景示例

### 8.1 场景一：团队继承访问某个 namespace 下所有 flow

授权关系：

```text
iam_group:security-team
  -> grant reader
  -> urn:iam:prod:flowgent:global:security:agent-flow/*
```

请求：

```text
GET /api/v1/security/flows/security-autonomy-fixer
```

matcher：

```text
action      = agentflow.read
resourceURN = urn:iam:prod:flowgent:global:security:agent-flow/security-autonomy-fixer
```

结果：

```text
ALLOW
```

原因：grant pattern 覆盖该 flow，reader role 包含 `agentflow.read`。

### 8.2 场景二：用户只被授权一个 flow，但 list flows 必须能看到它

授权关系：

```text
iam_subject:bob
  -> grant reader
  -> urn:iam:prod:flowgent:global:default:agent-flow/payment-risk-fixer
```

Bob 不是 `default` namespace owner。查询列表时，Flowgent Resource Adapter 仍应生成：

```sql
WHERE namespace = 'default'
  AND name = 'payment-risk-fixer'
```

结果：Bob 的 flow 列表中出现 `payment-risk-fixer`，但不会出现同 namespace 下的其他 flow。

### 8.3 场景三：读权限不能触发运行

授权关系：

```text
iam_group:auditors
  -> grant reader
  -> urn:iam:prod:flowgent:global:default:agent-flow/*
```

请求：

```text
POST /api/v1/default/flows/security-autonomy-fixer/runs
```

matcher：

```text
action = agentflow.run.trigger
```

结果：

```text
DENY
```

原因：reader role 不包含 `agentflow.run.trigger`，即使 resource URN 匹配也不能放行。

### 8.4 场景四：显式 DENY 优先

授权关系：

```text
iam_group:dev-team
  -> grant writer
  -> urn:iam:prod:flowgent:global:default:agent-flow/*

iam_subject:carol
  -> grant DENY writer
  -> urn:iam:prod:flowgent:global:default:agent-flow/payroll-fixer
```

结果：Carol 可以写 default namespace 下其他 flow，但不能写 `payroll-fixer`。

### 8.5 场景五：API Key 只能收缩权限

Alice 是 namespace owner。她创建一个 API Key：

```text
allowed_actions = [agentflow.run.trigger]
allowed_urns = [
  urn:iam:prod:flowgent:global:security:agent-flow/security-autonomy-fixer
]
```

该 API Key 只能触发这一个 flow。即使 Alice 本人拥有更多权限，API Key 也不能访问其他 flow，不能读取 secret，不能管理授权。

### 8.6 场景六：资源已删除但 grant 残留

授权关系中仍有：

```text
grant reader on urn:iam:prod:flowgent:global:default:agent-flow/old-flow
```

但业务 flow 表中 `old-flow` 已删除。

结果：

- list flows 不展示 `old-flow`，因为列表来自业务表。
- 直接请求 `old-flow` 返回 not found。
- grant 可由异步清理任务删除，但残留 grant 不会导致越权。

### 8.7 场景七：GitHub 风格 organization / repository 授权

GitHub org/repo 是典型层级资源授权：organization 拥有 repository，team 或 user 被授予 repository role，organization 级设置可提供更宽的默认授权。它可以直接映射到本文模型：

```text
GitHub org       -> tenant/domain
GitHub team      -> iam_group
GitHub repo      -> Resource URN
GitHub repo role -> iam_role
```

示例 URN：

```text
urn:iam:prod:github:global:flowgent-labs:org/flowgent-labs
urn:iam:prod:github:global:flowgent-labs:repo/flowgent
urn:iam:prod:github:global:flowgent-labs:repo/flowgent-ui
```

给某个 team 授权单个 repo：

```text
iam_group:platform-team
  -> grant maintainer
  -> urn:iam:prod:github:global:flowgent-labs:repo/flowgent
```

给某个 team 授权 org 下所有 repo 只读：

```text
iam_group:security-reviewers
  -> grant reader
  -> urn:iam:prod:github:global:flowgent-labs:repo/*
```

list repositories 可编译为：

```sql
WHERE org = 'flowgent-labs'
  AND (
    repo = 'flowgent'
    OR :has_all_org_repo_read = true
  )
```

这与 Flowgent namespace/flow 是同一个模型。资源名称不同，但 subject/group/role/grant/action 语义不变。

### 8.8 场景八：AWS S3 风格跨地域资源授权

S3 与 GitHub 的资源形态不同：bucket name 是全局命名，object key 是路径，access point 可以带 region，Multi-Region Access Point 是跨多个 region bucket 的全局入口。本文模型仍能统一表达，因为 region 和 resource-path 都是 Resource URN 的一等分段。

Bucket 与 object：

```text
urn:iam:prod:s3:global:111122223333:bucket/company-audit-logs
urn:iam:prod:s3:global:111122223333:bucket/company-audit-logs/object/2026/08/22/report.json
```

Regional access point：

```text
urn:iam:prod:s3:us-west-2:111122223333:access-point/audit-reader
urn:iam:prod:s3:us-west-2:111122223333:access-point/audit-reader/object/*
```

Multi-region access point：

```text
urn:iam:prod:s3:global:111122223333:multi-region-access-point/audit-global/object/*
```

授权：

```text
iam_group:global-auditors
  -> grant reader
  -> urn:iam:prod:s3:*:111122223333:access-point/audit-reader/object/**
```

条件：

```json
{
  "sourceIp": {"inCidr": ["10.0.0.0/8"]},
  "request": {"tls": true}
}
```

关键点：region 只是 URN 的一个 segment。GitHub 这类全局资源可使用 `global`，S3 这类资源可使用具体 region 或 `global` 表达全局入口。授权 evaluator 不需要变化。

## 9. 认证与身份解析

### 9.1 GitHub OAuth

```text
/auth/login/github
  -> GitHub OAuth authorize
/auth/callback/github
  -> oauth2.Exchange
  -> GitHub /user + /user/emails
  -> resolve iam_subject by identities[github/github/user_id]
  -> 未命中则按 verified primary email resolve/create iam_subject(kind=user)
  -> append github identity if needed
  -> compute auth context
  -> issue internal JWT/session
  -> Set-Cookie HttpOnly
  -> redirect
```

### 9.2 OIDC

```text
OIDC discovery
  -> authorization code
  -> token exchange
  -> ID token verify
  -> nonce validate
  -> resolve iam_subject by identities[oidc/issuer/sub]
  -> 未命中则按 verified email resolve/create iam_subject(kind=user)
  -> append oidc identity if needed
```

### 9.3 LDAP

```text
service bind
  -> user search
  -> user password bind
  -> resolve iam_subject by identities[ldap/domain/subject]
  -> 未命中则按 LDAP email resolve/create iam_subject(kind=user)
  -> append ldap identity if needed
```

LDAP subject 应可配置，默认可选：

```text
dn
uid
sAMAccountName
userPrincipalName
```

## 10. JWT、Cookie 与 UI Auth Context

浏览器登录默认使用 HttpOnly session cookie，不把 JWT 暴露给 JavaScript。

JWT 可包含：

```json
{
  "sub": "iam-subject-id",
  "email": "user@example.com",
  "preferred_username": "user@example.com",
  "identity_provider": "github",
  "actions": ["resource.read", "resource.run.trigger"],
  "authz_version": 12
}
```

UI 不直接解析 JWT。UI 调用：

```text
GET /api/v1/auth/me
```

返回：

```json
{
  "subject": {},
  "identity": {},
  "actions": [],
  "accessible_urns": [],
  "authz_version": 12
}
```

UI 用该结果控制菜单与按钮。后端 middleware / gateway 是唯一可信安全边界。

## 11. Bootstrap 与首次管理员

为避免首次 SSO 登录后无权限，支持 bootstrap admin email：

```yaml
auth:
  bootstrap_admin_emails:
    - admin@example.com
```

登录成功后，如果 verified email 命中：

```text
resolve/create iam_subject(kind=user)
append identity
grant platform owner
```

该过程必须幂等。生产环境完成初始授权后，应关闭或收敛 bootstrap 列表。

## 12. Flowgent 映射示例

| Flowgent 概念 | 通用 IAM 概念 |
|---|---|
| namespace | tenant / resource domain |
| agent flow | protected resource |
| flow run / task / trace | agent-flow 的子资源或执行证据 |
| LLM provider / MCP / skill / notification channel | 其他 protected resource |

示例 route matcher：

```json
{
  "method": "GET",
  "uri": "^/api/v1/([^/]+)/flows/([^/]+)/runs/([^/]+)/trace$",
  "path": {"namespace": 1, "flow": 2, "run": 3},
  "queryParams": {},
  "urn": "urn:iam:prod:flowgent:global:{namespace}:agent-flow/{flow}/run/{run}",
  "parentUrns": [
    "urn:iam:prod:flowgent:global:{namespace}:agent-flow/{flow}",
    "urn:iam:prod:flowgent:global:{namespace}:namespace/{namespace}",
    "urn:iam:prod:flowgent:global:platform:platform/root"
  ]
}
```

这里 `agent-flow` 只是受保护资源示例，不是 IAM 模型内置概念。

## 13. Sigbot Core 映射示例

| Sigbot 概念 | 通用 IAM 概念 |
|---|---|
| organization / team | tenant / resource domain |
| bot | protected resource |
| skill / tool / channel / memory | protected resource 或 bot 子资源 |
| conversation / evidence | bot 或 channel 的证据资源 |

示例：

```text
urn:iam:prod:sigbot:global:sales:bot/customer-support
urn:iam:prod:sigbot:global:sales:bot/customer-support/memory/customer-faq
urn:iam:prod:sigbot:global:sales:channel/slack-main
```

## 14. 模块边界

### 14.1 Go / Flowgent

```text
auth    -- OAuth/OIDC/LDAP/session/JWT/API key
iam     -- subject/group/role/grant management
authz   -- matcher/evaluator/middleware/audit
store   -- persistence
```

### 14.2 Rust / Sigbot Core

```text
sigbot_iam::model
sigbot_iam::resolver
sigbot_iam::matcher
sigbot_iam::evaluator
sigbot_iam::audit
sigbot_iam::middleware
```

跨语言共享：

- Resource URN grammar。
- action identifier 命名规范。
- `iam_action.matchers` JSON schema。
- grant evaluation algorithm。
- auth context API contract。
- golden test fixtures。

## 15. 安全原则

1. 默认拒绝。
2. DENY 优先于 ALLOW。
3. 第三方 identity 不直接授权。
4. 浏览器不读取 HttpOnly JWT。
5. API Key 明文只显示一次。
6. API Key 只能 attenuate owner subject 权限，不能放大权限。
7. Secret 不进入 IAM audit metadata。
8. Grant 不可修改，只能新增/删除。
9. 登录记录进入 audit，不写用户主档冗余字段。
10. UI 权限控制只是体验，middleware / gateway 才是安全边界。
11. 资源真相由业务表维护，IAM 不维护核心资源目录。
12. 所有授权判定必须可审计。

## 16. 参考

- RFC 8141: Uniform Resource Names (URNs): <https://www.rfc-editor.org/rfc/rfc8141.html>
- AWS IAM Amazon Resource Names (ARNs): <https://docs.aws.amazon.com/IAM/latest/UserGuide/reference-arns.html>
- GitHub organization repository roles: <https://docs.github.com/en/organizations/managing-user-access-to-your-organizations-repositories/managing-repository-roles/repository-roles-for-an-organization>
- Amazon S3 IAM resource types and policy resources: <https://docs.aws.amazon.com/AmazonS3/latest/userguide/security_iam_service-with-iam.html>
