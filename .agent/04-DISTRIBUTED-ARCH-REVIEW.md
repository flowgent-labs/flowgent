# Distributed Architecture Review — Flink Alignment Analysis

**Date:** 2026-05-12
**Status:** Draft for review, not yet implemented

---

## 1. KubernetesScheduler: batch Job vs普通 Pod

当前实现：每个 task 创建一个 `batch/v1 Job`。

### 对比分析

| 维度 | batch Job (当前) | 普通 Pod (Flink 方式) |
|------|-----------------|---------------------|
| **生命周期** | Job 自动管理重试和完成状态 | 需要自己写 controller 监控 Pod 完成 |
| **资源清理** | `TTLSecondsAfterFinished` 自动清理 | 需要手动 delete pod + watch |
| **重试策略** | `backoffLimit` 内置 | 需要自己实现 restart logic |
| **并行度** | 每个 Job 一个 task，和 TM 一致 | 每个 Pod 一个 task，无区别 |
| **监控** | Job status -> Conditions (Complete/Failed) | Pod status -> 需要自己聚合 |
| **日志聚合** | 无差异 | 无差异 |

### 结论

**当前 Job 方式对 AI agent 场景更合适。** 理由：

1. **TaskManager 是 ephemeral 的**：AI agent 的 node 执行是短暂的（秒~分钟级），不是 Flink 流处理那种长期运行的 operator。Job 的生命周期模型天然匹配：create → run → complete → cleanup。

2. **Go 不是 JVM**：Flink 用普通 Pod 是因为 JVM 启动慢，需要复用 TM 进程。Go 二进制启动是毫秒级，每次新 pod 的开销可忽略。

3. **无需 slot 复用**：Flink TM 有 slot 概念（一个 TM 进程内跑多个 sub-task），因为流处理需要 state 共享。AI agent 的 node 执行是无状态的，每次独立 pod 更干净。

4. **Job API 自带 controller**：`batch/v1 Job` 内置了重试、完成检测、TTL 清理。如果用普通 Pod，这些都要自己写。

但如果未来需要 **hot standby TM 池**（类似 Flink session 模式），可以用 Deployment + 普通 Pod。两种不互斥——可以在调度器里支持两种策略。

---

## 2. 命名和结构规范问题

### `cmd/run.go` → 改 `launch`？

你说的对。`run.go` 作为入口文件名不够清晰。Flink 的入口是 `CliFrontend`，Spark 是 `SparkSubmit`。对于 Flowgent：

| 当前 | 建议 | 职责 |
|------|------|------|
| `cmd/core/run.go` | `cmd/core/launch.go` | 主入口，CLI 解析 + daemon 生命周期 |
| `cmd/core/otel.go` | 移到 `src/common/tracing/` | OTEL provider 初始化，与入口解耦 |

`otel.go` 确实不应该放在 `cmd/` 里。它属于基础设施初始化，应该：
- 放到 `src/common/tracing/` 包
- 或者直接合并到 `launch.go` 中作为 `initTelemetry()` 函数
- 推荐前者：`src/common/tracing/provider.go`，这样其他二进制（tasklet, MCP servers）也能复用

### TaskManagerRunner 命名

同意。Flink 的 `TaskManagerRunner` 是 TM 进程入口，命名清晰：
- 当前：`TaskManager` + `ExecuteNode()` → TM 是逻辑组件
- 建议：`TaskManagerRunner` 作为 pod 进程入口，调用 `TaskManager.ExecuteNode()`

但当前 `cmd/tasklet/` 已经是这个角色，命名可以改得更明确：

| 当前 | 建议 |
|------|------|
| `cmd/tasklet/main.go` | `cmd/taskmanager-runner/main.go` |

`tasklet` 不够 self-documenting。Flink 用户看到 `TaskManagerRunner` 秒懂。

---

## 3. JobManager / TaskManager / TaskManagerRunner 调用关系

### Flink 参考架构

```
Flink Client
  → JobManager (dispatcher)
    → JobMaster (per job)
      → Scheduler
        → TaskManagerRunner (process/pod)
          → TaskManager (slot)
            → Task (execution)
```

### 当前 Flowgent 调用链

```
Config/Trigger
  → 创建 JobManager (per agentflow run)
    → JobManager.StartJob()
      → buildGraph(spec)
      → for each ready node:
        → scheduler.SubmitTask()
          ├─ LocalScheduler:
          │   → TaskManager.ExecuteNode()  [same process]
          │
          └─ KubernetesScheduler:
              → create batch/v1 Job
                → pod runs TaskManagerRunner (cmd/taskmanager-runner)
                  → reads TaskSubmit from env
                  → connects to shared store
                  → loads TaskRun
                  → TaskManager.ExecuteNode()  [pod process]
                  → exit (result in store)
      → collect TaskResult
      → Done(node) / Fail(node)
```

### 发现问题

1. **JobManager 每次 StartJob 创建一次** — 类似 Flink 的 JobMaster 而非 JobManager。
2. **TaskSubmit 传递 Node 对象** — 在 K8s 模式下需要完整序列化。Flink 的 Task 是 `JobVertex` 的子任务，有序列化机制。

### 建议调整

```
长期常驻进程:
  Flowgent Server (apiserver + dispatcher)
    → JobManager (per agentflow, session mode)
      → Scheduler
        → 创建 TaskManagerRunner pod (per node)
          → TaskManager.ExecuteNode()
```

---

## 4. 多 AgentFlow 管理：Session 模式 vs Job 模式

### 当前问题

每次 `StartJob()` 创建一个 `JobManager{}` 实例。谁来管理多个实例？

### Flink 的两种模式

| 模式 | Flink | Flowgent 参考 |
|------|-------|-------------|
| **Session** | 一个 JobManager 集群常驻，接收多个 job | 一个 Flowgent 主 pod 常驻，管理多个 agentflow |
| **Job** | 每个 job 启动独立集群，用完销毁 | 每个 agentflow 启动独立资源池，用完销毁 |

### 推荐：Session模式 + JobManager 池

```
[Flowgent Server 主 Pod]  ← 常驻 (类似 Flink Session Cluster)
  │
  ├── Dispatcher / TenantManager
  │     ├── 管理多个 AgentFlow 的生命周期
  │     ├── 每个 agentflow → 创建 JobManager 实例
  │     ├── 资源隔离: 每个 agentflow 有独立 ClusterID
  │     └── 支持: start / stop / restart / list agentflows
  │
  ├── CronScheduler
  │     └── 定时触发 agentflow
  │
  ├── Webhook Listener
  │     └── 事件触发 agentflow
  │
  └── API Gateway
        └── 外部请求触发 agentflow

当 agentflow 被触发时:
  Dispatcher 创建 JobManager{clusterID}
    → JobManager.StartJob(run, spec)
      → Scheduler 分配任务到 TaskManagerRunner pods
        → 完成后销毁 pods
      → JobManager 被 Dispatcher 回收
```

这类似 Flink Session 模式的好处：
- 主 pod 常驻，无需每次启动 JVM
- 资源可以被多个 agentflow 共享
- 适合 AI agent 场景（触发频率不固定，但需要低延迟响应）

### 当前代码已经接近

`cmd/core/run.go` 中的 `startRunPoller` 已经是这个模式的雏形：
```go
func startRunPoller(ctx context.Context, s engine.Store, tm *engine.TaskManager, ...) {
    for {
        runs := s.ListAgentFlowRuns(...)  // 轮询 pending runs
        for each pending run:
            jm := engine.NewJobManager(s, scheduler, logger)  // 每个 run 一个 JM
            jm.StartJob(ctx, &r, spec)
    }
}
```

但问题是：
1. run 需要先被创建（通过 API/trigger）
2. 没有真正的 JobManager 池管理（Pool of JMs）
3. 没有资源隔离/quota 控制

---

## 5. Flowgent Operator vs 内置 TenantManager

### 两种方案

#### 方案 A: Flowgent Operator (类似 Flink Kubernetes Operator)

```
Kubernetes
  ├── Flowgent Operator (CRD: AgentFlow, AgentFlowRun)
  │     ├── watches AgentFlow CR
  │     ├── 创建 Flowgent 主 Pod (Session 模式)
  │     └── 管理 TaskManagerRunner pods 生命周期
  │
  ├── AgentFlow CR → Flowgent 主 Pod
  │     └── Dispatcher → JobManager → TaskManagerRunner pods
  │
  └── 优势: K8s 原生声明式管理
      劣势: CRD 复杂度，operator 开发维护成本
```

#### 方案 B: 内置 TenantManager (Platform Tenant Manager Service)

```
Flowgent 主 Pod
  ├── TenantManager (内置)
  │     ├── API: POST /api/v1/agentflow/{id}/start
  │     ├── 管理 JobManager 实例池
  │     ├── 资源 Quota (max concurrent flows per tenant)
  │     └── 生命周期管理
  │
  ├── Scheduler
  │     └── Local / Kubernetes 分发
  │
  └── 优势: 简单，无需 CRD，二进制单一部署
      劣势: 不是 K8s 原生
```

### 推荐：阶段实施

**Phase 1 (现在) — 内置 TenantManager：**

因为在孵化阶段，Operator 太重。内置 TenantManager 作为 `Dispatcher` 组件：
- 在 `src/engine/dispatcher.go` 中实现
- `Dispatcher` 持有 store + scheduler + taskManager
- `Dispatch(ctx, agentFlowID)` → 创建 run → 创建 JobManager → StartJob
- 管理多个并发 agentflow，限制 maxConcurrent
- 提供状态查询/停止/重启

**Phase 2 (未来) — Flowgent Operator：**

```
apiVersion: flowgent.ai/v1
kind: AgentFlow
metadata:
  name: security-autonomy-fixer
spec:
  schedule: "0 */6 * * *"
  nodes: [...]
  edges: [...]
```

Operator watch AgentFlow CR → 创建 Session cluster pod → 提交 agentflow 执行。

### 当前 Phase 1 TenantManager 设计草案

```go
// src/engine/dispatcher.go — 新增

type Dispatcher struct {
    store      Store
    scheduler  Scheduler
    taskManager *TaskManager
    logger     *util.Logger
    
    mu           sync.Mutex
    activeJobs   map[string]*JobManager  // agentflow_run_id → JM
    maxConcurrent int
    clusterIDGen int
}

func (d *Dispatcher) Dispatch(ctx context.Context, spec *model.AgentFlowSpec, trigger model.TriggerInfo) error {
    // 1. 创建 AgentFlowRun
    // 2. 创建 JobManager
    // 3. 注册 active job
    // 4. 启动 goroutine: jm.StartJob()
    // 5. 完成时清理
}

func (d *Dispatcher) Cancel(runID string) error {
    // 找到对应的 JobManager
    // 调用取消
}

func (d *Dispatcher) ListActive() []AgentFlowStatus {
    // 列出所有活跃 flow
}
```

---

## 待办清单（待你批准后实现）

- [ ] **1. 命名整理**: `cmd/run.go` → `cmd/launch.go`, `cmd/tasklet/` → `cmd/taskmanager-runner/`, `otel.go` → `src/common/tracing/`
- [ ] **2. Dispatcher**: 实现 `src/engine/dispatcher.go`，替代 `startRunPoller`
- [ ] **3. 调用链梳理**: 修正 JobManager/Scheduler/TaskManager/TaskManagerRunner 关系
- [ ] **4. 文档更新**: 更新 01-ENGINE-IMPL-DESIGN.md 和 PROGRESS.md
- [ ] **5. KubernetesScheduler**: 评估是否保留 batch Job 还是切普通 Pod
