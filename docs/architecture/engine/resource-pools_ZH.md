# 资源池

[系统总览](../overview_ZH.md) · [Controller](controller_ZH.md) · [JobManager](jobmanager.md) · [TaskManager](taskmanager.md)

资源池是 Namespace 范围内明确的调度、容量与放置边界。
每个 Flow 必须绑定一个 `resource_pool_id`，创建 FlowRun 时会把该值固化为不可变
快照。

资源池统一配置 TM/Sandbox 副本数、每 Pod slot、CPU/内存、Kubernetes
PriorityClass 与 NodeSelector。名称遵循
`^[A-Za-z][A-Za-z0-9_-]{0,31}$`，在 namespace 内大小写不敏感唯一。
存在绑定 Flow 或活跃 Run 时不能删除资源池；存在 PENDING、RUNNING 或 PAUSED
Run 时不能修改 Flow 的资源池绑定。

```text
namespace
├── Flow A ─ 专用 JobManager ─┐
├── Flow B ─ 专用 JobManager ─┼─► critical 资源池 TM/Sandbox
└── Flow C ─ 专用 JobManager ────► economy 资源池 TM/Sandbox
```

Controller 仅在 Flow 有活跃 Run 时创建专用 JobManager。同一
`namespace/resource-pool` 下的 Flow 共享 TM/Sandbox Deployment。ExecutionPlan
和 SandboxTrigger 的 MQTT topic 都包含 namespace 与资源池；Worker 的共享订阅
也只订阅该范围，并再次校验消息体范围，因而不会跨池抢占任务。

资源池变更会通过 checksum 滚动活跃 JobManager，由新 JobManager 完整协调 Worker
Pod 模板，包括镜像、slot、资源、PriorityClass、NodeSelector、环境、卷和标签。
资源池删除后，Controller 清理其共享 Worker。

资源池只管理容量与放置，不承载 Flow 配置。共享 TM 会在每次 Sandbox attempt
执行前解析 namespace→Flow 的有效 env/secret，并仅传给该 attempt；Flow 密钥不会
进入长生命周期共享 Worker 的进程环境。明文密钥仅允许内部 Controller/TM
身份通过 `flow.runtime.use` 获取，且禁止写入日志。

核心不变量：Run 的资源池快照不可变；消息与订阅均按 namespace/pool 隔离；
Worker 拒绝范围不匹配消息；删除检查 Flow 与活跃 Run 引用；Flow 密钥不得成为
共享池进程状态。
