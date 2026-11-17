# Runtime Clusters

[系统总览](../overview_ZH.md) · [Controller](controller_ZH.md) · [JobManager](jobmanager_ZH.md) · [Sandbox 固定池](sandbox_ZH.md)

**状态：** application/session JM/TM Cluster 已实现；2026-11-14 确认的独立
Sandbox 固定池边界尚未实现。下表描述目标归属，末节说明当前 Sandbox 差异。

## 运行模式与归属

| 资源 | `application` | `session` |
| --- | --- | --- |
| JobManager | Controller 按活跃 FlowRun 创建 | Helm 部署共享 Session JM |
| TaskManager | JM/RM 管理 `app-{runId}` 的 Worker | Session JM/RM 管理 Session Cluster Worker |
| Sandbox | Flow 显式引用 Controller 管理的固定池 | Flow 显式引用 Controller 管理的固定池 |

`runtime_cluster_id` 继续作为 JM/TM 执行边界；Sandbox 的执行边界是
`(namespace_id, sandbox_id)`，独立于 Runtime Mode。不同 Runtime Cluster 的
获授权 Flow 可在 Namespace/信任边界内共用池。Run 完成回收 Application JM/TM，
但 **MUST NOT** 删除被引用 Sandbox 池。

## 配置归属

- Helm 提供 Session JM 资源与 JM/TM 运行默认值。
- Application Flow 可覆盖 `resources.jobmanager` 和 `resources.taskmanager`
  的 Pod Size；TM 副本/Slot 继续使用平台默认值。
- Sandbox 资源拥有镜像、副本/Slot、CPU/内存、存储引用和入站/出站规则，
  由 Controller 独立协调。
- 每个 Flow 声明 `runtime_mode` 并引用 `sandbox_id`。Run 保留所选绑定，
  后续 Flow 编辑不能静默转移已有 Run 到其他池。
- 旧 `resources.sandbox`、`sandbox_policy` 和 Node 网络覆盖必须显式迁移，
  不能静默降级或忽略字段。

池持久化、接纳、策略变更、Workspace/凭据边界和 Trigger/Result 契约归
[Sandbox 设计](sandbox_ZH.md)统一定义。此归属决定不改变 Checkpoint/恢复语义。

## 消息边界

JM→TM Plan 保持 Runtime Cluster 路由：

```text
flowgent/v1/{namespace}/clusters/{clusterId}/flows/{flowId}/runs/{runId}/exec/plans
flowgent/v1/{namespace}/flows/{flowId}/runs/{runId}/exec/results
```

TM→Sandbox 使用所选 Sandbox-ID Topic 和按池建立的 MQTT 共享组。Sandbox
结果关联精确 NodeRun Attempt，不能只有 Run。`runtime_cluster_id` 可保留为
来源元数据，但不选择 Sandbox 消费者。精确[池 Topic](sandbox_ZH.md#建议-mqtt-契约)
只在归口 Sandbox 文档定义。

## 当前实现差异

当前代码仍由 JM ResourceManager 管理 Runtime Cluster Sandbox Deployment，
Trigger 按 `clusterId` 路由，Result 按 Run 返回。现有全局/Flow/Node Policy
字段并未实现 Sandbox 目录或固定池 API。相关代码及部署/E2E 资产须随
Controller、Model/Store/API/UI、TM/Worker 消息同步迁移；本文不声称新边界
已经交付。
