# IT E2E 用例改进清单

[English](e2e-improvements.md)

## 硬性目标

`tests/it` 必须严格对齐 `docs/architecture/overview.md` 的真实 E2E。唯一允许 mock 的是外部系统 API：LLM provider、GitHub、SonarQube、Telegram、LDAP/OIDC provider 等。除此之外，数据库、中间件、消息队列、Flowgent 运行组件、部署形态必须使用本地真实实例，包括 PostgreSQL、MQTT broker、apiserver、controller、jobmanager、taskmanager、sandbox、notifier、Kubernetes session runtime cluster 和 per-run application runtime cluster。

严禁用 in-memory、fake、stub、mock 替代核心组件；除非测试明确标记为 standalone/local smoke，且不得声称覆盖真实 E2E。fresh clone 后必须能按文档启动依赖并稳定运行。

## 强制约束

| 约束 | 要求 |
|---|---|
| 架构一致 | 任何实现必须遵循 `docs/architecture/overview.md`，严禁测试捷径违背架构。 |
| 结构稳定 | 当前 `tests/it` 目录和 test 文件结构已基本满意；无明确必要收益时，严禁移动、重命名或大规模重排。 |
| 设计质量 | 必须高内聚、低耦合、模块化；严禁冗余复制大段 flow/helper。 |
| 命名 | 测试名必须准确描述真实覆盖行为；严禁用未启动的组件命名。 |
| 断言 | 严禁只用 `run completed` 证明核心行为；必须验证状态、事件、持久化、路由或输出。 |
| 交付 | 未满足本清单的实现一律不合格，不得标记完成。 |

## 当前事实

当前 runner 启动真实 apiserver、PostgreSQL-backed store、本地 JobManager poller 和 `ProviderStandalone`；未启动 Controller、KubernetesResourceManager、MQTT broker、独立 TM、独立 Sandbox、Notifier consumer。因此现有套件只能算 standalone/local smoke，不得标记为真实分布式 E2E。

## 必改问题

| # | 问题 | 必须修改 | 验收标准 |
|---|---|---|---|
| 1 | Harness 定位不准 | 保留现有目录结构；在 README/Makefile/CI 中明确区分 standalone smoke 与 distributed E2E；新增或扩展 distributed harness。 | 文档不再夸大覆盖；需要 PostgreSQL/MQTT/K8s 时必须写清；无必要结构重排。 |
| 2 | 缺真实 MQTT 往返 | 启动本地真实 MQTT broker；JM 使用 MQTT 分布式路径；真实 TM 消费 cluster-routed `$share/tm-{namespace}-{clusterId}/.../exec/plans`。 | 断言 JM publish -> TM consume -> REST SaveTask -> TM publish result -> JM unblock；路由或订阅顺序错误必须失败。 |
| 3 | `exec/results` 契约未验证 | 修复并测试 state-only result；输出必须通过 REST/task state 持久化。 | `exec/results` 只含 JM 所需路由/状态字段；重输出泄漏必须失败；output 仍可查询。 |
| 4 | Controller 用例不真实 | 启动真实 Controller；验证 `ListFlows`、hash-mod sharding、活跃 Flow JM 创建、run 路由、cleanup。 | Controller 未运行、shard 错误、JM 创建/清理缺失必须失败；测试名不得误导。 |
| 5 | 缺 runtime cluster K8s E2E | 提供 k3s/kind/Helm E2E profile；启动真实 apiserver/controller/notifier/MQTT/PostgreSQL/session JM/application runtime 组件。 | 验证 namespace 隔离、runtime-cluster labels、per-run application JM、session JM 路由、TM/Sandbox 容量与 Controller cleanup。 |
| 6 | DAG 断言过弱 | 为线性、fan-out/fan-in、condition、committee、map、join、supervisor、subflow 增加强断言。 | 必须验证 task rows、顺序、skip、输出；错误 branch/decision/持久化必须失败。 |
| 7 | 缺失败路径 | 增加 node failure、timeout、deadlock、invalid dependency、retry exhaustion、cancel。 | 每个边界场景必须确定性 setup；不得依赖任意长 sleep；错误必须可观测。 |
| 8 | Sandbox 不真实 | distributed 测试必须启动真实 sandbox worker；验证 workspace、trigger/result、stdout/stderr/exit code。 | 同一场景严禁同时接受成功和失败；网络 deny、seccomp 能力支持时必须测，不支持必须显式 skip。 |
| 9 | Notifier 不真实 | 启动真实 Notifier consumer；用本地 mock Telegram/webhook/email；验证 event delivery 与 `notify/result`。 | Notifier 未运行或 mock endpoint 未收到预期 payload 必须失败。 |
| 10 | Trigger 覆盖不足 | 覆盖 REST trigger、已实现 webhook provider 的正向/unmatched、A2A、已实现 cron/interval/controller trigger。 | 每条路径必须验证 run 创建、vars 传递、JM pickup；未实现能力必须明确记录，不得静默遗漏。 |
| 11 | Apiserver-only DB 契约缺失 | DB 访问只允许测试验证；非 apiserver 组件必须通过 REST/MQTT 写状态。 | distributed E2E 不得给 JM/TM/Sandbox/Notifier 注入直接 DB store；状态可经 REST 和 DB 验证。 |
| 12 | Fixtures 重复 | 合并重复 `securityFixerFlow()` 等 fixture；场景覆盖用小而明确的 override。 | flow fixture 高内聚、无共享可变泄漏；更新一次即可影响所有复用点。 |
| 13 | 隔离性不足 | 清理 namespace、flows、runs、tasks、providers、MCPs、knowledge、approvals、channels；固定端口和 `-p 1` 必须说明原因。 | fresh clone 可按文档跑通；重复运行不依赖人工清 DB。 |

## 强制分层

| 层 | 范围 |
|---|---|
| `standalone` | 本地 apiserver + PostgreSQL + standalone RM；只能叫 smoke/local integration。 |
| `distributed` | 真实 PostgreSQL + MQTT + apiserver + JM + TM + sandbox/notifier processes。 |
| `k8s` | Helm/kind/k3s 覆盖真实 runtime cluster、Controller、K8sRM。 |
| `auth` | 本地 LDAP/OIDC provider containers。 |
| `use-cases` | `use-cases/` 下真实外部系统验证，不属于 portable IT。 |

## 执行顺序

1. 修正文档、命名和 harness 定位。
2. 补齐 MQTT JM/TM 往返和 state-only result 契约。
3. 补齐真实 Controller/runtime cluster/K8s E2E。
4. 补齐 Sandbox、Notifier、Trigger、失败路径。
5. 加强 DAG 断言，合并 fixtures，完善清理与 fresh-clone 可靠性。
