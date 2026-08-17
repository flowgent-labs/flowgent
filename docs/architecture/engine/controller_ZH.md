# Controller

[系统总览](../overview_ZH.md) · [资源池](resource-pools_ZH.md) · [JobManager](jobmanager_ZH.md)

Controller 是分布式运行时协调者。它通过 API Server 获取 Flow、活跃 Run 与资源池，
通过 hash-mod 在多个 Controller 副本之间分片，并创建或删除每 Flow 专用
JobManager；资源池删除后负责回收共享 Worker。Controller 不直接连接数据库。

```text
API Server REST + MQTT 生命周期事件
                  │
                  ▼
          Controller 分片
                  │
                  ├── 创建 / 协调 ──► 每 Flow 一个活跃 JobManager
                  └── 垃圾回收 ─────► Flow JM + Resource Pool Worker
```

## 协调与分发

每轮协调只获取一次 Peer 快照并复用于 Flow 所有权与 GC 判断：

```text
每 10 秒：
  1. IDiscoveryClient.DiscoverPeers(labelSelector)
  2. ListFlows + ListResourcePools(namespace)
  3. 对 shard(flow_id) == my_index 的 Flow 查询 PENDING/RUNNING/PAUSED Run
  4. 仅为有活跃 Run 的 Flow 创建/协调专用 JobManager
  5. 重建所属 cron trigger
  6. 回收无活跃 Run/已删除 Flow 的 JM 和已删除资源池的 Worker
```

Flow 导入与更新只注册元数据，不分配 Pod。REST、Webhook、A2A 或 cron 都先通过
API 创建 PENDING Run；专用 JM 只轮询自己的 Flow。`Flow.resource_pool_id` 与 Run
中固化的资源池快照决定 ExecutionPlan 的 MQTT topic 和可消费 Worker。

协调活跃 Flow 前，Controller 通过仅 Workload 可访问的 API 解析
namespace→Flow 有效配置，并把公开值/密钥分别写入每 Flow ConfigMap/Secret。
配置 checksum 会滚动 JM。资源池也有独立 checksum；资源池副本、slot、资源、
PriorityClass 或 NodeSelector 变化时滚动活跃 JM，由 K8sRM 完整协调共享
TM/Sandbox Pod 模板。

## 分片

```text
shard(flow_id) = fnv64a(flow_id) % total_controller_pods
```

Controller 通过 `IDiscoveryClient` 获取副本列表。资源池 GC 使用同样算法对
`resource-pool/{pool}` 分片，避免多个 Controller 同时拥有同一回收动作。

## 预期行为

1. 同一轮协调中，每个 Flow 只能归一个 Controller 所有。
2. 无活跃 Run 的已导入 Flow 不得创建 JM/TM/Sandbox。
3. 活跃 Run 必须存在命名、namespace、资源池 Label 正确的专用 JM。
4. API/Kubernetes 瞬时故障下协调必须幂等；活跃 Run 快照失败时不得误删 JM。
5. 资源池列表失败时不得清理 Worker；只有权威快照确认资源池删除后才可清理。
6. 资源池删除必须先由 API 确认不存在绑定 Flow 与活跃 Run。
