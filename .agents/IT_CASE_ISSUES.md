# IT E2E 用例改进问题清单

## 目标

`tests/it` 必须成为面向 `docs/01-L1-Engine-Architecture.md` 的真实 E2E 集成测试。

它与 `usecase/` 真实案例测试的唯一区别是：依赖的外部系统 API 可以本地 mock，例如 LLM provider API、GitHub API、SonarQube API、Telegram API、LDAP/OIDC provider API 等。除此之外，任何中间件、数据库、消息队列、Flowgent 运行时组件和部署形态都必须使用本地部署的真实实例，包括 PostgreSQL、MQTT broker、apiserver、controller、jobmanager、taskmanager、sandbox、notifier，以及架构要求的 Kubernetes/Application mode 行为。严禁用 in-memory、fake、stub、mock 替代这些核心组件；除非该测试明确标识为 standalone/local smoke，且不得声称覆盖真实 E2E。

目标体验：任意具备基础环境的机器 fresh clone 后，必须能按文档启动依赖并稳定运行 E2E。

## 强制工程要求

- 必须保持修改简洁且不冗余；严禁复制大段 flow 定义或 helper 逻辑。
- 必须保持高内聚、低耦合。测试 harness、mock、断言、场景定义必须按职责清晰拆分。
- 命名必须准确且优雅。测试名必须描述真实覆盖的行为；严禁用未实际启动的架构组件命名测试。
- 验证架构约束时，必须使用可复用的强断言；严禁只用 `run completed` 作为核心行为的唯一断言。
- 测试层次必须明确：standalone smoke、distributed/MQTT E2E、K8s Application mode E2E、auth E2E。
- 严禁用宽松断言掩盖缺失覆盖。文档行为被破坏时，对应用例必须失败。
- 当前 `tests/it` 目录与 test 文件结构已基本满意。除非有明确、必要且可证明的收益，否则严禁重命名、移动或大规模重排目录/文件；必须先在现有结构内补齐 harness、fixture、assertion 和场景覆盖。
- 任何代码修改必须遵循 `docs/01-L1-Engine-Architecture.md`，不得引入与架构相悖的测试捷径。
- 未满足以上约束的实现一律视为不合格，不得作为完成状态交付。

## 当前主要差距

当前 `tests/it` runner 启动了真实 apiserver、PostgreSQL-backed store、本地 JobManager poller，以及 `ProviderStandalone`。它没有启动 `docs/01-L1-Engine-Architecture.md` 描述的真实分布式链路：Controller、KubernetesResourceManager、MQTT broker、独立 TaskManager pod/process、独立 Sandbox pod/process、Notifier event consumer。

因此，现有套件只能作为本地集成冒烟测试，不得被标记为真实架构 E2E 测试套件。

## 待修复问题

### 1. 校准当前 Harness 定位

问题：`tests/it` 当前宣称覆盖真实 E2E，但 runner 使用 `ProviderStandalone` 和进程内执行。

期望：

- 保留当前目录和测试文件组织；除非存在明确、必要且可证明的收益，否则严禁移动或重命名。
- 在现有结构内明确标识当前快速套件属于 standalone/local integration，不得声称其覆盖 distributed true E2E。
- 新增或扩展 distributed E2E harness，启动本地部署的真实中间件和真实 Flowgent 组件。
- 在 `tests/it/README.md` 和 `Makefile` 中写清命令与前置依赖。

验收：

- 读者能清楚区分哪些是 standalone 冒烟测试，哪些是 distributed E2E 测试。
- 现有 `tests/it` 目录和 test 文件结构没有被无必要重排。
- `Makefile`、CI workflow、README 对依赖描述一致。需要 PostgreSQL 时，不得声称“无数据库依赖”。

### 2. 增加真实 MQTT 执行往返测试

问题：TaskManager 测试目前没有通过 MQTT/shared subscription 覆盖 `exec/plans` 和 `exec/results`。

期望：

- 必须启动本地部署的真实 MQTT broker，可由 Docker Compose 或测试托管服务提供。
- 使用 `KubernetesResourceManager` 或配置了 MQTT 的分布式 RM 启动 JM。
- 启动真实 TaskManager worker，消费 `$share/tm-pool/.../exec/plans`。
- 断言完整链路：JM publish plan -> TM consume -> TM 通过 REST 持久化 task -> TM publish result -> JM 解锁节点。

验收：

- subscribe-before-publish 顺序被破坏时测试失败。
- per-node result routing 被破坏时测试失败。
- 测试验证 `exec/results` payload 符合架构契约。

### 3. 修复并测试 State-Only Exec Results

问题：架构要求 `exec/results` 只携带状态标识和执行状态，输出通过 REST 持久化。当前测试只检查 run completion，无法可靠发现 `exec/results` 泄漏完整输出。

期望：

- 如实现不符，修正 `exec/results`，不要携带完整 task output。
- 增加订阅 `exec/results` 的测试，断言不存在重输出 payload。
- 验证 task output 仍可通过 apiserver REST 或 DB-backed state 查询。

验收：

- `exec/results` 只包含 JM 所需的路由和状态字段。
- output 仍通过 task state 持久化可见。

### 4. 用真实 Controller E2E 替代 Controller 命名的冒烟测试

问题：当前 `controller` IT 测试通过 apiserver 和本地 JM poller 触发 run，没有启动或验证真实 Controller。

期望：

- 启动真实 Controller 进程/组件。
- 验证 Controller 通过 `FlowgentClient.ListFlows` 轮询 flow。
- 验证多 Controller 实例下的 hash-mod sharding。
- 验证 Application mode 会创建或确保 dedicated JM deployment/process。
- 验证 run creation 使用预期 namespace/Application mode 路由。
- 验证 flow 完成后的清理行为符合架构。

验收：

- Controller 未拥有目标 shard 时测试失败。
- dedicated JM 创建/清理缺失时测试失败。
- 只有真实启动 Controller 的测试才使用 Controller 命名。

### 5. 增加 Application Mode K8s E2E

问题：当前架构说明 Session mode 已禁用，Application mode 是有效分布式路径，但 IT 未覆盖该路径。

期望：

- 必须提供 K8s-capable E2E profile；本地具备 k3s/kind 时必须能运行。
- 安装或启动 apiserver、controller、notifier、MQTT、PostgreSQL 和必要 Flowgent 组件。
- 触发 high-priority flow，验证 dedicated JM 以及 TM/Sandbox 生命周期。

验收：

- 验证 namespace 命名、pod/deployment labels、per-flow JM 命名。
- 验证 JM 按设计自动扩缩 TM 和 Sandbox。
- 验证 run 完成后的 Controller cleanup 行为。

### 6. 加强 DAG 断言

问题：DAG 测试大多只断言终态 run status。

期望：

- 断言每个预期节点都有 task row。
- 在顺序重要时断言拓扑执行顺序。
- 断言 condition false branch 的节点被 skip。
- 断言 condition、committee、map、join、supervisor、subflow 的输出。
- 增加负向用例：节点失败、超时、死锁、非法依赖、retry 耗尽。

验收：

- branch skip 错误、committee decision 错误、task 持久化缺失时，对应用例失败。
- `run completed` 不能作为 DAG 行为测试的唯一断言。

### 7. 增加真实 Sandbox E2E

问题：Sandbox 测试允许 `COMPLETED` 或 `FAILED` 都通过，也没有验证文档中的独立 sandbox 执行模型。

期望：

- 在 distributed tests 中启动真实 sandbox worker/component。
- 验证 TM 将脚本写入共享 workspace。
- 验证 SandboxRunner 消费 trigger、执行脚本、写 result、发布 result。
- 验证 stdout/stderr/exit code 和 workspace 文件。
- 增加 allowed/denied egress 的网络策略测试。
- 支持 seccomp/security 能力的环境必须执行 raw socket、`bpf`、blocked host access 等测试；不支持时必须显式 skip 并说明原因。

验收：

- 成功 sandbox 脚本必须完成，并断言精确输出。
- 被拒绝的网络访问必须因预期原因失败。
- 同一场景不得同时接受成功和失败。

### 8. 增加真实 Notifier E2E

问题：Notifier 测试主要覆盖 channel CRUD 和 flow completion，没有验证 Notifier 服务的实际消息投递。

期望：

- 启动真实 Notifier 服务并消费 MQTT events。
- 使用本地 mock Telegram/webhook/email endpoint。
- 发布或触发 flow/human approval events。
- 断言投递请求和 payload 内容。
- 适用时断言 `notify/result` 确认消息。
- WebSocket 或 human-approval push 路径已实现时，必须在本地 E2E 中验证；未实现时必须明确记录为未实现，不得静默遗漏。

验收：

- Notifier consumer 未运行时测试失败。
- 外部 mock endpoint 未收到预期通知时测试失败。

### 9. 增加 Trigger Path 覆盖

问题：当前覆盖集中在 manual 和 GitHub webhook trigger。架构还描述了 A2A、webhook adapters、Controller 发现的 schedule trigger。

期望：

- 覆盖 REST trigger path。
- 覆盖 GitHub/GitLab/Gitea webhook adapter 行为；已实现的 provider 至少各有一个正向和一个 unmatched event。
- 组件启用时覆盖 A2A `/a2a/tasks`。
- 已实现的 cron/interval/controller-triggered flows 必须覆盖；未实现时必须明确记录为未实现，不得静默遗漏。

验收：

- 每条 trigger path 都验证 run 创建、vars 传递、JM 异步 pickup。

### 10. 增加状态写入与 Apiserver-Only DB 契约检查

问题：架构要求只有 apiserver 访问 DB；非 apiserver 组件必须通过 REST 和 MQTT。

期望：

- 测试中允许 DB 访问只用于验证，不要替代组件行为；DB 本身必须是真实本地部署实例。
- 在 E2E 路径中增加 JM/TM/Sandbox/Notifier 通过 apiserver client 持久化状态的断言。
- 避免 test-only shortcut 绕过生产使用的相同契约。

验收：

- 除明确测试 standalone/local mode 外，E2E setup 不向非 apiserver 组件注入直接 DB store。
- 状态写入可同时通过 apiserver REST 和 DB 验证观察到。

### 11. 增加失败、超时、取消与 Failover E2E

问题：架构中的重要运行时边界场景缺失。

期望：

- 节点执行失败 -> run failed 且包含有用错误。
- task timeout -> run failed。
- run cancellation -> 正在运行的 task 停止或不再调度新 task。
- TM heartbeat loss -> orphaned work 按当前设计被回收或失败。
- distributed mode 缺少 MQTT broker -> 启动 fail fast。

验收：

- 每个边界场景都有确定性 setup 和清晰断言。
- 有可观测信号时，不依赖任意长 sleep。

### 12. 清理重复 Flow Fixtures

问题：多个 package 中重复定义了相似的 `securityFixerFlow()`。

期望：

- 将共享 flow fixtures 抽取到 `tests/it` 下高内聚的 fixture 模块。
- 场景特定覆盖保持小而明确。
- 避免共享可变 fixture 导致测试间状态泄漏。

验收：

- flow 定义易读、易复用。
- 更新 Security Autonomy Fixer fixture 不需要修改多个副本。

### 13. 提升测试隔离性与 Fresh-Clone 可运行性

问题：测试使用共享 PostgreSQL 和固定端口，重复本地运行容易相互影响。

期望：

- 对 test namespace、flows、runs、tasks、providers、MCPs、knowledge、approvals、channels 提供确定性清理。
- 需要并行安全时使用唯一 ID。
- 只有固定外部端口确实需要时才保留 `-p 1`，并说明原因。
- 所有 mocks 和真实中间件的启动/检查方式必须一致。

验收：

- fresh clone 后按文档启动依赖即可成功运行。
- 重复运行套件不依赖人工清理 DB。

## 强制测试分层

严禁用一个含糊的 `it` bucket 覆盖所有目标；必须在不破坏现有目录/文件结构的前提下显式分层：

- `standalone`：快速本地 apiserver + PostgreSQL + standalone RM，无 broker/K8s。
- `distributed`：真实 PostgreSQL + MQTT + apiserver + JM + TM + sandbox/notifier processes。
- `k8s`：通过 Helm/kind/k3s 覆盖真实 Application mode，包括 Controller 和 K8sRM。
- `auth`：使用本地 provider containers 的 LDAP/OIDC 测试。
- `usecase`：位于 `usecase/` 下的完整真实外部系统验证，不属于 portable IT。

## 执行顺序

1. 修正文档和命名，避免夸大当前覆盖范围。
2. 增加 MQTT JM/TM 往返和 state-only result 断言。
3. 增加真实 Controller/Application-mode E2E。
4. 增加真实 Sandbox 和 Notifier E2E。
5. 加强 DAG 与失败路径断言。
6. 重构 fixtures 和 cleanup，提升可维护性。

