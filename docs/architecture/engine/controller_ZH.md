# Controller

[系统总览](../overview_ZH.md) · [Runtime Clusters](runtime-clusters_ZH.md) · [JobManager](jobmanager_ZH.md)

Controller 是分布式运行时协调者。它通过 API Server 获取 Flow 与活跃
application Run，通过 hash-mod 在多个 Controller 副本之间分片，并创建或删除每
FlowRun 专用 application JobManager；同时回收 application runtime Deployment。
Controller 不直接连接数据库。

```text
API Server REST + MQTT 生命周期事件
                  │
                  ▼
          Controller 分片
                  │
                  ├── 创建 / 协调 ──► 每 Run 一个 application JobManager
                  └── 垃圾回收 ─────► application JM + TM/Sandbox
```

## 协调与分发

每轮协调只获取一次 Peer 快照并复用于 Flow 所有权与 GC 判断：

```text
每 10 秒：
  1. IDiscoveryClient.DiscoverPeers(labelSelector)
  2. ListFlows(namespace)
  3. 对 shard(flow_id) == my_index 的 Flow 查询 PENDING/RUNNING/PAUSED application Run
  4. 仅为有活跃 application Run 的 Flow 创建/协调专用 JobManager
  5. 重建所属 cron trigger
  6. 回收无活跃 Run/已删除 Flow 的 application JM 和 TM/Sandbox
```

Flow 导入与更新只注册元数据，不分配 Pod。REST、Webhook、A2A 或 cron 都先通过
API 创建 PENDING Run；application 专用 JM 只轮询自己的 FlowRun。`Flow.runtime_mode`
与 runtime cluster id 决定 ExecutionPlan 的 MQTT topic 和可消费 Worker。Session
Run 由 Helm 部署的 session JM 消费。

协调活跃 Flow 前，Controller 通过仅 Workload 可访问的 API 解析
namespace→Flow 有效配置，并把公开值/密钥分别写入每 Flow ConfigMap/Secret。
配置 checksum 会滚动 application JM。该 JM 的 K8sRM 只协调匹配
`runtime_cluster_id` 的 TM/Sandbox Pod 模板。

## 分片

```text
shard(flow_id) = fnv64a(flow_id) % total_controller_pods
```

Controller 通过 `IDiscoveryClient` 获取副本列表。同一轮协调必须复用同一 peer
快照，避免多个 Controller 同时拥有同一 Flow 的创建或回收动作。

## 预期行为

1. 同一轮协调中，每个 Flow 只能归一个 Controller 所有。
2. 无活跃 Run 的已导入 Flow 不得创建 JM/TM/Sandbox。
3. 活跃 application Run 必须存在命名、namespace、runtime cluster Label 正确的专用 JM。
4. API/Kubernetes 瞬时故障下协调必须幂等；活跃 Run 快照失败时不得误删 JM。
5. 活跃 Run 快照失败时不得把缺失活动误判为可清理。
