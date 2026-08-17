# Security Fixer 纯 UI 完整端到端闭环

[English](security-fixer-ui-e2e.md)

**状态：** 已完成——阶段五资源池运行时与 GitHub 风格 Shell 已验收
**开始日期：** 2026-08-15

## 目标

在干净部署中证明真实资源池绑定的 `security-autonomy-fixer` 链路；
资源配置禁止使用 `flowgent console import` 或测试代码直接调用 REST 预置：

```text
清空 PostgreSQL + Helm 重新部署
  -> 浏览器依次创建 LLM -> MCP -> Agents -> 运行时 Skill -> Flows
  -> 浏览器触发 security-autonomy-fixer
  -> Controller -> JM -> MQTT -> TM/Sandbox -> 外部系统
  -> API Server -> Jaeger Query -> 浏览器 Run/Tracking
  -> use-case 场景 11-37 全部通过
```

架构与 use-case 契约仍由
[`architecture/overview.md`](../architecture/overview.md)、
[`architecture/agent-flow.md`](../architecture/agent-flow.md) 和
[`usecase/security-autonomy-fixer/README.md`](../../usecase/security-autonomy-fixer/README.md)
维护；本计划只记录实施进度与验证证据。

## 强制门禁

1. Flowgent 组件、PostgreSQL、MQTT、Kubernetes 运行时、Sandbox、Jaeger 和
   use-case 外部调用 **MUST** 真实；协议 fixture 和预置 Run/TaskRun 不计入。
2. 所有应用资源 **MUST** 通过可见浏览器 UI 提交。测试可以把 canonical
   manifest 载入浏览器内存，但 **MUST NOT** 直接调用资源 REST 或 console import。
3. Playwright **MAY** 仅从进程环境读取 Secret 并填入密码控件。Secret 在 API
   边界 **MUST** 只写不读，且 **MUST NOT** 出现在读取响应、浏览器 trace、报告、
   截图、URL 或 telemetry 中。
4. UI **MUST** 触发运行并检查真实 DAG、全部持久化 TaskRun attempt、重试输入输出
   和 Jaeger Trace 关联。
5. 有效轮次 **MUST** 从清空 PostgreSQL 和完整 Helm redeploy 开始，并按顺序通过
   完整 use-case verifier；失败轮次不计数。
6. 必须连续通过十轮；任何产品修复都会把成功计数归零，并从干净部署重新开始。
7. Notification 密文、JM 生命周期时间戳改造后的阶段二必须额外连续通过五轮；
   A2A 未启用或 verifier 跳过时，不得计为 A2A 验收成功。
8. RBAC/A2A 改造后的阶段三必须再连续完成五轮空库重部署。每轮都必须在可见 UI
   创建 Namespace Service Account 和精确 Flow 授权，验证允许/拒绝/撤销凭据，
   运行不得 SKIP 的标准 A2A JSON-RPC verifier，并保留 GitHub 风格路由截图。
9. `/{namespace}/{flow}` 规范路由和运行时配置继承完成后，阶段四必须重新连续完成
   五轮空库重部署。每轮都必须在可见 Settings UI 配置 Namespace 默认值与 Flow
   覆盖值，证明 Secret 无法回读，并保留 Settings、DAG、attempt I/O、Jaeger、
   RBAC 和 A2A 证据。
10. 移除 UI Mock/运行模式并引入资源池后，阶段五必须连续完成五轮空库重部署。
    每轮必须通过可见 Namespace Settings 创建或确认 Pool、绑定 Flow、证明
    namespace/pool 范围 TM/Sandbox、通过真实 A2A verifier，并保留不含 Secret 的截图。

## 工作状态

| 工作流             | 状态            | 证据/下一步                                                                                                                                                             |
| ------------------ | --------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 基线审计           | 已完成          | 浏览器 fixture IT 保留为组件/集成门禁；`test:system` 是真实集群验收门禁。                                                                                               |
| 安全 LLM/MCP 契约  | 已完成          | 浏览器只提交环境变量引用名；读取响应脱敏，运行值来自已配置的 Kubernetes Secret。                                                                                        |
| 运行时 Skill 契约  | 已完成          | 为运行时 `kind=skill` 定义提供 namespace 作用域 CRUD，并与便携式 Skill Studio 草稿分离。                                                                                |
| UI 资源配置        | 已完成          | Playwright 通过可见 UI 依次创建 LLM -> MCP -> Agent -> Runtime Skill -> Flow。                                                                                          |
| UI 触发与 Tracking | 已完成          | UI 触发/审批真实 Run，并检查所有 TaskRun attempt 与 Jaeger trace 关联。                                                                                                 |
| Verifier 兼容      | 已完成          | UI-provisioned 模式复用浏览器 run，禁止 console import、REST 预置或替代触发。                                                                                           |
| 十轮稳定性验证     | 已完成（10/10） | 2026-08-15 连续十轮清库重部署的 UI 系统测试全部通过；每轮均通过 15 个 verifier 场景，并严格匹配 26 个持久化 attempt、26 个 UI 检查 attempt、26 个 Jaeger attempt 链接。 |
| Notification 密文 | 已完成          | Secret 由 UI 动态写入、AES-256-GCM 密文存库、读取脱敏；真实 Notifier 解密并向带鉴权的外部接收器投递。                                                                   |
| JM 生命周期时间戳 | 已完成          | `started_at`/`finished_at` 由 JobMaster 权威写入；UI 系统测试检查非空及时间顺序。                                                                                       |
| 阶段二五轮回归     | 已完成（5/5）   | 2026-08-16 连续五轮空库 + Helm 重部署 + 可见 UI 配置/触发全部通过；每轮 Notification 严格门禁与 15 个现有场景均通过。                                                   |
| 企业 RBAC          | 已完成           | Deployment 为隐式 Enterprise，Namespace 为组织/运行时/数据边界；全局身份、显式 Membership、Team、分层 Role、DENY 优先、审计、受限 Service Account Key、Flow Settings 精确授权、不可变 FlowRelease 安装和 GitHub 风格路由均通过阶段三稳定性验证。 |
| A2A 验收           | 已完成           | A2A 0.3 JSON-RPC、Bearer→API Server RBAC 传播、调用方隔离的共享 Task Store、双副本、真实 FlowRun 场景 25 及严格健康门禁均通过阶段三稳定性验证。 |
| 阶段三五轮回归     | 已完成（5/5）    | 2026-08-16 连续五轮空库 Helm 重部署通过，覆盖可见 UI 配置、精确 Flow RBAC 允许/拒绝/撤销、A2A 2/2 副本、真实 A2A FlowRun、全部 15 个场景及截图留证。 |
| Settings/运行时配置 | 已完成（5/5） | GitHub 风格 Settings 左侧导航、规范 Flow URL、Namespace→Flow 环境变量/Secret 继承、密文只写存储、最小权限 API 及 JM/TM/Sandbox 摘要滚动已连续通过五轮空库重部署验收。 |
| 资源池运行时 | 已完成（5/5） | Namespace CRUD/RBAC、Run 不可变快照、Flow 绑定保护、Pool 范围 MQTT/Ready、共享 TM/Sandbox、完整 Pod 模板协调、按 attempt 解析 Flow 配置及删除 GC 已通过阶段五验收。 |
| GitHub 风格 Shell 清理 | 已完成（5/5） | 固定全局左侧栏、底部模式与全局 `Live API` 部署标记已移除；仅保留顶部导航，Namespace/Flow Settings 各自只有一层局部左侧栏，并已人工复核留存截图。 |
| 阶段五五轮回归 | 已完成（5/5） | 2026-08-17 连续五轮空库 Helm 重部署通过；每轮均由可见 UI 配置 Pool/Flow，浏览器 2/2、verifier 15/15、retry/tracking 截图和独立真实 A2A FlowRun 全部通过。 |

## 十轮矩阵

| 轮次 | 干净部署 | UI 资源 | UI 触发 | Run/Trace | Verifier | 结果 / Run ID                          |
| ---: | -------- | ------- | ------- | --------- | -------- | -------------------------------------- |
|    1 | 通过     | 通过    | 通过    | 26/26/26  | 15/15    | `f9f75027-ee7a-4a5e-8cd0-8a93abbd1ab8` |
|    2 | 通过     | 通过    | 通过    | 26/26/26  | 15/15    | `3b9bca2e-8236-4276-a69b-9051a02b0797` |
|    3 | 通过     | 通过    | 通过    | 26/26/26  | 15/15    | `c704ae0f-7f7c-4f80-92b6-af1eea849423` |
|    4 | 通过     | 通过    | 通过    | 26/26/26  | 15/15    | `b6946224-9eef-4707-bc5b-9900692edb4b` |
|    5 | 通过     | 通过    | 通过    | 26/26/26  | 15/15    | `5c1587a8-cae1-47fc-8bda-f6d124598d84` |
|    6 | 通过     | 通过    | 通过    | 26/26/26  | 15/15    | `a3a767fd-2cf4-4582-8ed2-a5f7f3ec6b6f` |
|    7 | 通过     | 通过    | 通过    | 26/26/26  | 15/15    | `d7d336cf-a157-428d-9973-374bb1ee421f` |
|    8 | 通过     | 通过    | 通过    | 26/26/26  | 15/15    | `825fabef-d3a3-4b72-84ad-ef22215391b5` |
|    9 | 通过     | 通过    | 通过    | 26/26/26  | 15/15    | `5f087bb2-ed33-4197-97c9-afa4433bea16` |
|   10 | 通过     | 通过    | 通过    | 26/26/26  | 15/15    | `99292d21-23dc-4c78-887b-bc00224abfee` |

## 阶段二五轮矩阵

这五轮验证 Notification 密文动态配置、真实外部投递和 JM 权威 Run 时间戳。
“15/15”是现有场景汇总；其中旧 A2A 场景被跳过，不能作为 A2A 成功证据。

| 轮次 | 干净部署 | UI 资源/触发 | TaskRun/UI/Jaeger | Notification | Verifier | 结果 / Run ID                          |
| ---: | -------- | ----------- | ----------------- | ------------ | -------- | -------------------------------------- |
|    1 | 通过     | 通过        | 26/26/26          | 通过         | 15/15    | `f667ecc7-8fce-4e2e-8614-9e0f2beeae33` |
|    2 | 通过     | 通过        | 26/26/26          | 通过         | 15/15    | `c3d221b6-fbb5-405b-a23f-fa2960c2ee2d` |
|    3 | 通过     | 通过        | 26/26/26          | 通过         | 15/15    | `c53f6950-697d-4081-8e3f-db06afd8fdf1` |
|    4 | 通过     | 通过        | 26/26/26          | 通过         | 15/15    | `7213423a-4fb1-41bf-b094-a7a17fc9de02` |
|    5 | 通过     | 通过        | 26/26/26          | 通过         | 15/15    | `e9ffa166-bfc4-4553-95d1-232a613d8c98` |

## 阶段三五轮矩阵

每轮均清空 PostgreSQL、重新部署 Helm、通过可见 UI 配置全部资源，验证精确 Flow
授权的允许/拒绝/撤销，检查全部 26 个持久化 attempt 及其 Jaeger 链接，通过全部
15 个 verifier，并通过健康的双副本 A2A Deployment 执行真实 FlowRun。

| 轮次 | 主 Run ID                              | Jaeger spans | A2A Run ID                             | 结果 |
| ---: | -------------------------------------- | -----------: | -------------------------------------- | ---- |
|    1 | `d90f56f8-5ae1-40bb-a906-d8d6d5cb7480` |           86 | `8029522d-ba6c-472a-8619-430f27c44ab9` | 通过 |
|    2 | `dbee2a83-369c-43df-8f0b-e1c8f1e37823` |           86 | `04437c23-4f4a-4ff4-9605-15ea060930a5` | 通过 |
|    3 | `e2845a37-65da-406e-a0b1-d2e86a107d04` |           86 | `830f69dc-d0ff-4f99-8026-fad2c9e8266b` | 通过 |
|    4 | `2ae93a96-1e97-45eb-bce4-d7d58bb80a5e` |           86 | `f937f4b4-57cb-4571-ad5a-35c5956a0f95` | 通过 |
|    5 | `b0589199-7593-4cf1-8078-4fee498454a4` |           87 | `69c517b6-3031-49f8-a372-79aef7e3c7f5` | 通过 |

五个主 Run 均为 26 个节点、每节点 1 个 attempt，未自然触发 retry。Retry 持久化与
UI 投影已由确定性单元测试和 fixture 集成测试覆盖。如需真实运行时 retry 证据，应
新增独立的、刻意失败的 E2E 专用 Flow，而不应修改生产用途的 security fixer Flow。

## 阶段四五轮矩阵

每轮均清空 PostgreSQL、完整重新部署 Helm，并通过可见 UI 完成 23 次写操作：LLM、
通知渠道、MCP、Agent、运行时 Skill、Flow、Namespace/Flow RBAC、Namespace 运行时
默认值、Flow 环境变量/Secret 覆盖、API Key 撤销以及真实 Run 触发。每轮均证明
Secret 只写不可回读、Namespace→Flow 继承、26 个持久化/UI/Jaeger attempt 严格
关联，通过全部 15 个 verifier，并额外执行真实 A2A FlowRun。

| 轮次 | 主 Run ID                              | Jaeger spans | A2A Run ID                             | 结果 |
| ---: | -------------------------------------- | -----------: | -------------------------------------- | ---- |
|    1 | `125f809e-bf7f-4e88-984c-50a44ec76008` |           86 | `4320b2ab-48fd-4ea7-bb8a-31f61762d4ce` | 通过 |
|    2 | `4f248755-713a-48b0-953b-c88b2f9da557` |           86 | `7b35fb14-026d-4e65-9862-a0262d4f2d6b` | 通过 |
|    3 | `df99669c-0b83-48d6-bd3a-38d539f8ddc4` |           87 | `783a36d1-fc56-4705-942d-b12eb0ebfefd` | 通过 |
|    4 | `d968f94e-b685-48f3-a922-65de5a3aa5f6` |           87 | `4781485e-148e-4974-97f4-908b5f270cbb` | 通过 |
|    5 | `9dd12442-c98c-4ca6-93a2-e10b46f17b08` |           86 | `442a6493-206a-46c8-999f-e843dc8ac40f` | 通过 |

机器可读汇总结果为 `PASS`，连续成功数为 5。每轮均配置 2 个 Namespace 环境变量
默认值、1 个 Namespace Secret、1 个 Flow 环境变量覆盖和 1 个 Flow Secret 覆盖。
全部 26 个运行节点 attempt 均由 UI 查看并关联至 Jaeger；这些生产 Run 未自然重试。
五轮完成后，额外通过可见 UI 创建并触发一个仅用于 E2E 的确定性失败 Flow。Run
`90766811-9f76-43c5-b389-b6bd6d15872f` 持久化了 3 次 attempt，DAG 显示
`2 retries`，右侧可分别选择三次请求/响应，并将全部 attempt 关联至包含 8 个 span
的真实 Jaeger trace。验证后已删除 probe Flow 及其运行时配置。

## 阶段五五轮矩阵

每轮均清空 PostgreSQL、完整重新部署 Helm、通过可见 UI 创建全部资源，将
`security-autonomy-fixer` 绑定到 `security-critical`，并证明 TM/Sandbox worker 与
MQTT 分发按 Namespace/Pool 隔离。每轮还运行一个真实、确定性的三 attempt retry
probe，在 UI 查看其请求/响应并关联包含 8 个 span 的 Jaeger trace，通过全部 15 个
verifier 场景，并完成独立的鉴权 A2A FlowRun。

| 轮次 | 主 Run ID                              | Jaeger spans | A2A Run ID                             | 结果 |
| ---: | -------------------------------------- | -----------: | -------------------------------------- | ---- |
|    1 | `3ce6bbe8-5486-4dbc-80a9-de1430d0f4a3` |           86 | `abb56adf-c1c1-4a17-a42c-2828c3d77afd` | 通过 |
|    2 | `bef7b197-71d3-42e4-afdc-1a52a05e844c` |           87 | `f7fe7071-f534-456c-bf4d-b22dd7128c34` | 通过 |
|    3 | `33f1d3d0-626a-455e-bd2d-db776684f157` |           87 | `ef28d58f-1714-4241-9b95-7ca9246b67a2` | 通过 |
|    4 | `08f9f386-082f-4ef4-87df-2562f898780d` |           87 | `9fe90fb3-7867-4e44-9671-b66c1700d0a1` | 通过 |
|    5 | `2b2c7ae8-ecfe-4157-af74-377519419da9` |           87 | `2caddddb-a8e7-4e89-86cf-55efb7d5c20e` | 通过 |

机器可读汇总为 `PASS`，连续成功数为 `5/5`。每个主 Run 均有 26 个持久化 attempt、
26 个 UI 检查 attempt、26 个 Jaeger 链接及完整的 `exec/plans`/`exec/results` MQTT
覆盖。Pool 生命周期 verifier 还独立证明了 JM、TM、Sandbox、ConfigMap 和 Secret
的扩容、取消、删除与垃圾回收。

## 已发现缺口

| 缺口                           | 判断                                                                         | 解决状态                                                                                                          |
| ------------------------------ | ---------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| LLM/MCP UI 曾为 mock/blocked   | 后端需要不透明环境变量引用与脱敏读取。                                       | 已解决                                                                                                            |
| 运行时 Skill UI 曾不可用       | 运行时 Skill 需要独立的 `kind=skill` REST 生命周期。                         | 已解决                                                                                                            |
| 现有 `e2e-real` 表述过强       | 它预置执行记录并使用 Jaeger fixture。                                        | 已重分类为 fixture IT；`e2e-system` 才是验收门禁。                                                                |
| verifier 曾假设 console import | 场景 12/31 需要 UI-provisioned 模式且不削弱断言。                            | 已解决                                                                                                            |
| 通知渠道 Secret                | 外部通知 Secret 必须支持 UI 动态变更且不能明文存储或回读。                   | 已解决：schema 驱动的 Secret 字段、AES-256-GCM 版本信封、空值保留/显式清除、真实解密投递及 UI/E2E 门禁。          |
| 稳定性测试期间 k3s 本地镜像 GC | 卸载后镜像暂时无引用，下一次安装前被回收。                                   | 已修复 E2E 部署生命周期：teardown 后、Helm install 前重新导入镜像。                                               |
| 终态 Run/Task 轮询竞态         | Run 先观察到 `COMPLETED` 时可能停止 Task 轮询，而缓存尚未包含最后 attempts。 | 已解决：每个终态 Run revision 强制刷新一次 TaskRun；系统 E2E 要求持久化 attempt 与 UI 实际检查集合/数量严格相等。 |
| 终态运行时证据 GC 竞态         | UI 检查较慢且先执行独立 verifier 时，可能超过应用运行时的预期保留窗口。      | 已解决：Jaeger 后立即执行依赖本次运行的场景 31–37，再执行独立的引擎场景 21–25，且未削弱任何断言。                 |
| Run 生命周期时间戳             | 已完成的真实 Run 曾返回空 `started_at`/`finished_at`。                       | 已解决：JM 是唯一生命周期时间权威；API 持久化 JM 更新，UI 系统测试验证开始/完成时间非空且有序。                   |
| 企业 RBAC 资源模型             | 已确认 Deployment → Namespace/Org → 精确资源，不增加 Enterprise/Workspace 表。 | 已在实现层解决：显式 Namespace Membership、Team/Role Binding、Flow Settings、生产方不可变 Release 与消费方自有安装。 |
| A2A 旧 verifier                | 曾使用过时 REST 路径并把不可达当成 SKIP/PASS，Deployment 健康检查也未对齐。 | 已在实现层解决：标准 A2A 0.3 JSON-RPC、严格真实执行 verifier、共享 Task DB、Bearer RBAC 传播及强制 2/2 副本健康。 |
| 空运行时配置响应               | 空库时 Secret key 集合被序列化为 `null`，不符合 UI 集合契约。                 | 已在 API 序列化与 UI repository 边界双重规范化，并增加前后端回归测试。                                      |
| 运行时工作负载协调权限         | Flow JM 会协调绑定 Pool 的 worker 模板，但工作负载 `flowgent-runtime` Role 缺少 Deployment `update`。 | 已只为同 Namespace runtime Role 补充所需 Deployment verb；回归测试、Helm lint 和五轮真实重部署均通过。 |
| Pool 路由 MQTT 审计            | observer 仍订阅 Pool 改造前的 `exec/plans` 与 `sandbox/trigger` 路径，可能把成功 Run 误判失败。 | 已改为观察非共享的 `/{namespace}/pools/+/{flow}/...` 分发主题，点对点 callback 仍保留 Flow 路径；五轮均达到 plan/result 26/26 覆盖。 |
| MQTT 证据 Secret 落盘          | Sandbox trigger 审计 payload 包含 attempt 范围环境值，即使应用 API 和截图已经脱敏。 | 已在证据序列化边界解决：所有环境值，以及递归命名的 secret/token/password/auth/cookie/API-key 字段均在写盘前替换为 `<redacted>`；当前产物已在不输出值的前提下清洗。 |
| Notifier verifier Pod 选择     | 重部署后，无序 Pod 列表首项可能是旧的 Failed Pod。                            | 已改为只查询 Running 且 Ready 的 Pod 并选择最新项；加密真实投递断言未削弱。                                 |
| 冷启动核心镜像构建超时         | 受限资源下的全新 core image 构建可能合理超过旧的通用 10 分钟限制。            | 已增加仅适用于 core build 的 20 分钟上限；其他命令超时不变。                                               |
| 运行时配置孤儿                 | 删除 Flow 后，按 Flow 物化的运行时 ConfigMap/Secret 可能在运行 Deployment 删除后残留。 | 已增加 Controller 范围内的垃圾回收，只删除 Flow 已不存在且由 Controller 管理的配置；场景 23 同时验证物化与删除。 |
| Broker 订阅就绪竞态            | Kubernetes `AvailableReplicas` 不能证明新扩容 TM/Sandbox 已完成 MQTT 订阅，第一条计划可能丢失。 | 已增加 Namespace/Flow/角色精确作用域的 `RuntimeReady` 租约，仅在订阅注册后发布；ResourceManager 在首个 plan/trigger 前等待新鲜就绪信号。 |

## 证据规则

- 每轮 summary 保留在 use-case 的 `e2e/reports/` 证据目录。
- Review 截图和机器可读 summary 按轮归档在
  `usecase/security-autonomy-fixer/e2e/reports/ui-rounds/`。
- 阶段二五轮证据归档在
  `usecase/security-autonomy-fixer/e2e/reports/ui-rounds-phase2/`；每轮保留
  `round.json`、`ui-provision.json`、15 份场景报告、Playwright HTML 报告和 UI 截图。
- 阶段三 RBAC/A2A 证据独立归档在
  `usecase/security-autonomy-fixer/e2e/reports/ui-rounds-phase3-rbac-a2a/`。
- 阶段四 Settings/运行时配置证据归档在
  `usecase/security-autonomy-fixer/e2e/reports/ui-rounds-phase4-settings-runtime-config/`；
  每轮均保留 `round.json`、`ui-provision.json`、15 份场景报告、Playwright HTML
  报告和已脱敏 UI 截图。
- 真实三 attempt retry probe 截图保留在阶段四证据目录的 `retry-probe/` 下。
- 阶段五资源池证据归档在
  `usecase/security-autonomy-fixer/e2e/reports/ui-rounds-phase5-resource-pools/`；
  每轮均保留 `round.json`、`ui-provision.json`、15 份场景报告，以及 Pool 配置、
  GitHub 风格 Settings、DAG、attempt I/O、RBAC、脱敏 Secret 和 Jaeger 关联截图。
- 本计划和任何生成证据都不得写入 Secret。
