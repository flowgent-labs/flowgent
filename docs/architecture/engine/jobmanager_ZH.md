# JobManager

[系统总览](../overview_ZH.md) · [TaskManager](taskmanager_ZH.md) · [Sandbox](sandbox_ZH.md)

JobManager 是每个 Run 的 DAG 编排器。它通过 API Server 读取 Flow/Run 状态，
将可运行 DAG Node 转换为 `ExecutionPlan` 消息，消费仅含状态的执行结果，并
通过可插拔 ResourceManager 管理 TaskManager 与 Sandbox 容量。它从不直接
连接数据库。

```text
API Server REST ──► JobManager / JobMaster
                         │
                         ├── ExecutionPlan ──MQTT──► TaskManager
                         ├── exec/result ◄──MQTT──── TaskManager
                         └── 扩缩 / 清理 ───────────► TM + Sandbox Deployment
```

## DAG 编排

JM 是一个活跃 Flow 的单一控制面。它通过 API Server REST
（`FlowgentClient.ListRuns`）按 Namespace 过滤并轮询 `PENDING` Run，解析
`AgentFlowSpec` JSON，为每个 Run 创建一个 **JobMaster**。每个 JobMaster
根据 `AgentFlowSpec.Nodes` 与 `Edges` 构建 DAG，通过 API Server REST
持久化执行状态，并经 ResourceManager 按拓扑顺序执行 Node。

### JobMaster——每 Run 一个 DAG 编排器

JobMaster 保存该 Run 的 DAG 状态：Node、Edge、依赖、完成/失败/跳过标记、
Node 输出与 Condition。每次 `Submit()` 创建一个实例，不同 Run 不共享状态。

```text
Execute(run, spec):
  // 第 1 步：从 AgentFlowSpec JSON 构建 DAG
  buildExecutionGraph(spec, runID)
    → 对每个 Node：在内存 planMap 中创建 ExecutionPlan
    → 每个 DAG Node 对应一个 ExecutionPlan

  // 第 2 步：拓扑循环，处理每个 Ready Node
  for each ready node (all dependencies satisfied):
    plan := resolveInputs(node, previousOutputs)
    result := scheduleNodeWithRetry(ctx, plan)
      → 每次 attempt 前：通过 REST 持久化独立的 PENDING TaskRun
      → rm.Schedule(ctx, plan)            // 分发至 TM（MQTT 或 Standalone）
      → 保存本次 attempt 的输出/错误，并在 Node policy 允许时重试
    state.UpdateRun(ctx, run)             // 通过 API Server REST 更新 Run 状态
    Done(nodeID)
    if condition → SetConditionResult → Skip(false-branch)
    if supervisor → validate action → Inject/Retry/Abort

  // 第 3 步：结束
  → run.Status = COMPLETED | FAILED
  → 通过 API Server REST 持久化最终状态
```

JobMaster 是执行时间戳的权威所有者：进入执行时写 `started_at`，任何终态转换时写
`finished_at`，并通过 API Server 在同一次生命周期更新中提交 status、error 与
两个时间戳。Controller 和浏览器可以创建或观察 Run，但不得推断或补造执行时间；
API 会拒绝 `finished_at` 早于 `started_at` 的更新。

### DAG 状态方法

| 方法 | 用途 |
|---|---|
| `BuildGraphNodes(nodes, edges)` | 根据 Spec 初始化 DAG |
| `Ready()` | 返回所有依赖已满足的 Node |
| `Done(id)` | 标记 Node 完成 |
| `Skip(id)` | 跳过 Node（Condition 的非匹配路径） |
| `Fail(id)` | 标记 Node 失败 |
| `Inject(id, deps)` | 注入 Supervisor 创建的 Node |
| `IsComplete()` | 所有 Node 已完成或跳过 |
| `HasFailed()` | 是否存在失败 Node |
| `SetConditionResult(id, bool)` | 保存 Condition 分支结果 |

### JobManager 范围

Session 部署以 `jobmanager start` 启动并只轮询 session Run。Application 部署以
`jobmanager start --flow-id <id> --run-id <runId>` 启动并只轮询该 FlowRun。
调度时使用 FlowRun 中不可变的 runtime mode 与 `runtime_cluster_id`；之后修改
Flow 不会迁移活跃 Run。

### DAG 依赖协调——Iteration Loop + 阻塞 Schedule

JM 是 Flow Run **唯一的依赖控制器**。它不会盲目发送 ExecutionPlan，而是通过
两层循环主动保证 DAG 拓扑约束：外层 Iteration Loop 反复发现新 Ready Node；
内层逐 Node 调用阻塞 `Schedule()`，等待 TM 完成当前 Node 后才分发下一个。

这里的 **Iteration** 是一次调度就绪扫描，不会重置 Node 状态。Node 一旦完成
或跳过，回边无法让它在同一 Run 内再次执行。见
[AgentFlow 循环限制](../agent-flow_ZH.md)。

#### 执行循环（伪代码）

```text
Execute(run, spec):
  buildExecutionGraph(spec, runID)   // 初始化 DAG 状态

  for iteration := 1; ; iteration++:
    if HasFailed()  → run.Status = FAILED, persist, return
    if IsComplete() → run.Status = COMPLETED, persist, return

    ready := Ready()                 // 所有依赖已满足的 Node
    if len(ready) == 0:
      break                          // 死锁：某些 Node 存在未满足依赖

    for each nodeID in ready:
      plan := buildPlan(nodeID, resolvedInputs)
      result := scheduleNodeWithRetry(plan)
        state.SaveTask(attempt)      // 每次 attempt 一条持久记录
        rm.Schedule(plan)            // ← 阻塞等待 TM 结果
      if result.Error != "":
        Fail(nodeID)                 // 子 Node 保持阻塞，Flow 失败
        continue

      nodeOutputs[nodeID] = result.Output
      Done(nodeID)                   // 在下一 Iteration 解锁子 Node

      if node is condition:
        SetConditionResult(nodeID, bool)
        for each child with false-match edge:
          Skip(child)                // 跳过不匹配分支
```

**关键点：** `rm.Schedule()` 是**同步阻塞调用**。JM 必须等待 TM 执行 Plan 并
报告结果才继续。因此同一 Iteration 中依赖 A 的 B1/B2/B3 等 Sibling 当前是
**顺序分发**，不是并发；每个 Node 的结果返回后才分发下一个。未来可通过一次
分发全部 Sibling 并以 `sync.WaitGroup` 收集结果实现真正并发，但当前顺序方案
更简单，也避免部分失败时的回滚复杂度。

#### Node attempt 与重试归属

Node 级重试由 JobManager 负责，因为只有 DAG 编排器能决定失败 Node 是否可以
再次分发且不推进其 Child。未配置 Node retry 时只执行一次；配置后最多执行
`retry.max + 1` 次，并使用可由 Context 取消的指数退避。

每次 attempt 使用独立且长度有界的 Task ID、从 1 开始的 `sequence`、从 0
开始的 `retry_count`，并以 `parent_task_run_id` 指向上一失败 attempt。每次
解析后的输入、精确输出/错误及时间戳都通过 API Server REST 持久化。
ExecutionPlan 还携带上一 attempt ID，使 JobManager 在 Worker 写结果后故障时，
TaskManager 仍不会破坏重试链。

每次 attempt 同时产生 `jobmaster.node.attempt` Span。Attributes 只保存
Run/Node/Task/Attempt ID 与有界输入输出元数据，不保存 payload 正文。模型 Schema
修正等 Executor 内部纠错循环表现为嵌套 Span，不产生额外 TaskRun，因为它们
不是 DAG Node 重试。

#### 依赖解析——`depsDone()` 算法

```text
depsDone(node):
  if node has zero dependencies → true (root node)

  对每条入边分类：
    unconditional + source completed → active
    unconditional + source pending   → unresolved（阻塞）
    conditional + source 未求值      → dormant
    conditional + result 匹配        → active
    conditional + result 不匹配      → inactive
    source skipped                   → inactive

  存在任一 active edge → ready（dormant feedback 不阻塞）
  无 active 且存在 dormant → unresolved（等待）
  全部 edge inactive → resolved-inactive（跳过 Node）
```

**条件 Edge 处理：** `Ready()` 根据 Edge 活性把 resolved-inactive Node 传播为
`SKIPPED`，直到固定点。Merge Node 只要存在一个 active 入边仍可运行；未选择分支及
其单路径后继都会被跳过。Dormant 条件反馈边不阻塞已经 active 的初始路径，但自身
不能令 Node Ready。这只是调度兼容：已完成 DAG Node 不会再次执行，所以条件回边
不代表循环语义；迭代工作流仍需要未来明确的 loop/subflow 契约。

#### K8s 模式：先订阅后发布 + 每 Node Channel 路由

K8s（MQTT）模式的 `K8sRM.Schedule()` 使用**先订阅后发布**模式，避免丢失
TM 快速返回的响应：

```text
K8sRM.Schedule(plan):
  1. Create resultCh := make(chan execResult, 1)

  2. SUBSCRIBE to exec/results (once per run):
     if s.runResults[runID] == nil:
       q.Subscribe("exec/results/{namespace}/{flow}/{run}", callback)
     s.runResults[runID][plan.NodeID] = resultCh
     // ^^ 发布前注册 Channel，不存在竞态

  3. PUBLISH plan to exec/plans/{namespace}/{cluster}/{flow}/{run}
     → TM Pod 通过 $share/tm-{namespace}-{cluster} 消费

  4. BLOCK on resultCh (with planTimeout):
     select {
       case <-ctx.Done():  → timeout, return error
       case er := <-resultCh:
         if er.State == "FAILED" → return error
         return TaskResult{Output: {plan_id, node_id, state}}
     }
```

Callback 用 `er.NodeID` 匹配已注册 Channel，将 `exec/results` 路由到正确 Node，
然后删除该 Channel。`runResults` Map 以 `(runID,nodeID)` 为 Key，每个 Node
有专属 Channel。因此即使当前顺序分发，如果 JM 循环改为并发分发/收集，底层
路由也能够支持。

#### 单 Node 往返时序

```text
JM.Execute()          K8sRM.Schedule()         MQTT                TM.SlotWorker
──────────            ────────────────          ────                ─────────────
Ready() → [A]
  │
  ├─Schedule(A) ──►   Subscribe exec/results
  │                   Register chan[A]
  │                   Publish exec/plans ────►  ──────────────►  Dequeue ($share)
  │                                                                Router 执行
  │                   ◄── Publish exec/results ──  ◄────────────  REST 保存 Task
  │                   Route er.NodeID → chan[A]                    回报状态
  │                   delete chan[A]
  ◄── result ────────
  Done(A)

Ready() → [B]          // B 先前被 A 阻塞，现在 Ready
  ...
```

#### 边界情况

| 场景 | 行为 |
|---|---|
| **Root Node（无依赖）** | `Ready()` 在第一次 Iteration 立即返回 |
| **并行意图 Sibling** | 共享 Parent `Done()` 后，同一 Iteration 全部 Ready；当前顺序分发，每个独立阻塞 |
| **Condition → 非匹配分支** | `Ready()` 以固定点把 inactive Edge 传播为 `SKIPPED`；若 Merge Node 还有 active 入边则仍会运行 |
| **Node 失败** | `Fail(node)`；Child 永不 Ready，`HasFailed()` 终止 Flow |
| **死锁**（Ready=∅、未完成、未失败） | 存在永久未满足依赖；循环退出并返回 nil，Node 留在 `PENDING` |
| **TM 超时** | `planTimeout` 默认 5 分钟；`Schedule()` 返回错误、Node 标记失败、Flow 终止 |
| **TM 执行中崩溃** | JM 心跳监控 30 秒后判定死亡并重新分发孤儿 Plan |

---

## 资源管理与调度

JM 对每个 Ready Node 调用一次 `Schedule(ctx, plan)`。RM 根据 Pending Plan
和空闲 Slot 决定执行位置与方式。在生产 K8s 模式，它还管理 TM Pod 生命周期：
在 Deployment 不存在时创建，并自动扩缩副本以满足需求。

```go
type ResourceManager interface {
    Provider() engine.Provider
    Validate(ctx) error
    Schedule(ctx, plan) (*TaskResult, error)
    Shutdown(ctx) error
}
```

### StandaloneResourceManager（all-in-one）

使用 Channel Semaphore（`make(chan struct{}, poolSize)`）构成有界进程内
Goroutine Pool。`Schedule()` 以非阻塞 Select 获取 Slot；全部忙时立即返回
`INSUFFICIENT_RESOURCES`。获取 Slot 后，在同一 Goroutine 中同步调用
`tm.ExecutePlan()`。

### KubernetesResourceManager（生产）

包含三项职责：**Task 分发、TM Pod 管理、Sandbox Pod 管理**。

**分发：** `Schedule()` 是阻塞调用，不是 Fire-and-forget。它为 Run 订阅一次
`exec/results`，注册以 `nodeID` 为 Key 的 Go Channel，向 `exec/plans` 发布
Plan，再阻塞等待 TM 结果。先订阅后发布可以避免 TM 在 JM 开始监听前返回结果。

每 Run 的路由 Map（`runResults map[string]map[string]chan execResult`）根据
`er.NodeID` 把结果路由到对应 Channel，收到后删除条目。每次 `Schedule()`
创建并等待独立 Channel，因此当前 Sibling 是顺序分发，而非并发。完整规则见
[DAG 依赖协调](#dag-依赖协调iteration-loop-阻塞-schedule)。

**TM Pod 管理：** 协调 `flowgent-taskmanager-{namespaceId}-{clusterId}`，应用平台
默认值和 application 模式 Pod size 覆盖，等待对应范围 Worker Ready 后才发布
ExecutionPlan。

**Sandbox Pod 管理：** 协调 `flowgent-sandbox-{namespaceId}-{clusterId}`。每个
Pod 运行 configured slots，并只共享订阅对应 namespace/cluster topic。

`scalingLoop` 协调观测副本与 Ready 状态。`runtime_cluster_id` 是硬隔离边界，
FlowRun 不能静默借用其他 cluster 的 Worker。

---

## 预期行为

1. **给定**一个 Flow Run，**当** JobMaster 构建 Graph 时，**则**该 Run 的每个配置 Node **MUST** 映射到一个稳定的 ExecutionPlan 标识。
2. **给定**未满足依赖，**当**评估 Ready 状态时，**则**依赖 Node **MUST** 保持 Pending；只有 Root 和依赖完全满足的 Node 才能 Ready。
3. **给定**条件 Edge，**当** Source 解析完成时，**则**只有匹配分支可以执行，另一直接 Child **MUST** 被跳过。
4. **给定**已分发 Plan，**当**结果延迟或路由错误时，**则** JobManager **MUST** 等待匹配的 `(runID,nodeID)` 结果，并在配置超时后失败，**MUST NOT** 提前推进 DAG。
5. **给定** Task 或 Sandbox 需求，**当**协调容量时，**则**副本、Slot 与资源 **MUST** 匹配所选 runtime mode、cluster id、平台默认值及 application Pod size 覆盖。
6. **给定**失败 Node，**当**执行循环观察到失败时，**则** Run **MUST** 变为失败，Child **MUST NOT** 按成功路径执行。
7. **给定**多个 Ready Sibling，**当**当前 Scheduler 执行它们时，**则**可观察分发 **MUST** 保持顺序，直到并发调度被明确实现并验证。
8. **给定** Node retry policy，**当**一次 attempt 失败时，**则** JobManager **MUST** 在退避前持久化该失败 attempt，并为下一次 attempt 使用独立且由 Parent 关联的 TaskRun。
