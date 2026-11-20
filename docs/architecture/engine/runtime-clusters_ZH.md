# Runtime Clusters

[系统总览](../overview_ZH.md) · [Controller](controller_ZH.md) · [JobManager](jobmanager_ZH.md)

Flowgent 使用 Flink 风格运行时拓扑。调度隔离边界是
`runtime_cluster_id`，不是 namespace 范围的 worker pool。

## 运行模式

| 模式 | 生命周期归属 | 范围 | 适用场景 |
|---|---|---|---|
| `application` | Controller 为每个活跃 FlowRun 创建一个 JobManager；该 JM/RM 创建自己的 TM 和 Sandbox Deployment | namespace + Flow + Run + runtime_cluster_id | 强隔离、可预期清理、长链路或敏感工作流 |
| `session` | Helm 部署共享 session JobManager；该 JM/RM 只管理带相同 cluster id 的 TM/Sandbox Deployment | namespace + session runtime_cluster_id | 短任务和低隔离共享负载 |

两种模式使用同一条 JobManager → ResourceManager → TaskManager/Sandbox
控制线。差异只在生命周期，不在所有权语义。TM 或 Sandbox Pod **MUST**
只接受自己 `runtime_cluster_id` 对应的工作。

## 配置归属

- Helm 负责 session JobManager Pod 资源和 session TM/Sandbox 默认值。
- Helm 负责 application 模式 JM/TM/Sandbox 默认 Pod 资源。
- Helm 负责 application 模式 TM/Sandbox 副本数和 slot 默认值。
- Flow 定义只负责：
  - `runtime_mode`
  - application 模式可选的顶层 `resources.jobmanager/taskmanager/sandbox`
    Pod size 覆盖。

Application 模式 Flow resources **MUST NOT** 定义副本数或 slot。这些仍由平台
默认值统一控制，避免放置策略分散。

## 消息边界

Cluster 路由工作主题：

```text
flowgent/v1/{namespace}/clusters/{clusterId}/flows/{flowId}/runs/{runId}/exec/plans
flowgent/v1/{namespace}/clusters/{clusterId}/flows/{flowId}/runs/{runId}/sandbox/trigger
flowgent/v1/{namespace}/clusters/{clusterId}/runtime/{role}/{workerId}/ready
```

结果回调仍按 run 点对点：

```text
flowgent/v1/{namespace}/flows/{flowId}/runs/{runId}/exec/results
flowgent/v1/{namespace}/flows/{flowId}/runs/{runId}/sandbox/result
```

这样既能在一个 runtime cluster 内负载均衡分发，又能在 topic 路径中保留
flow/run 可观测性。
