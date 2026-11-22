# Use-case 结构与场景评审（草稿）

**状态：** 阶段性评审笔记及历史验证观察；不是架构规范或当前版本的验收报告。

[English draft](use-cases-review.md)

每个用例拥有 `runner.py`、`deploy/flowgent/config/`、`deploy/`、`util/`、
`verifier/` 与 `reports/`。部署资产按服务归属组织；验证按产生证据的操作组织。
PG/向量、Workspace、Trace 与日志应归入对应操作，而不另设存储场景。

架构依据：[总览](../architecture/overview_ZH.md)、
[持久数据生命周期](../architecture/data-lifecycle_ZH.md)、
[AgentGraph](../architecture/agent-graph_ZH.md) 及各用例 README。

## 已确认的场景决定

| 项目 | 决定与契约 |
| --- | --- |
| 1. Security 可观测性 | 已批准。s13 移至 `otel/`；业务执行后补充 s51 指标与 s52 日志。使用 100% Trace 采样和精确的 Run/NodeRun Attempt/Plan 身份；Prometheus 必须核对真实业务 Counter/Histogram 的变化。 |
| 2. Security 断言 | 已批准。s32–35 在活动分支缺少证据时失败。要求来源 Finding ID、生成文件、交付字节、持久审批和真实重扫结果。 |
| 3. Revision/恢复/Fencing/拒绝 | 延后。Checkpoint/Barrier/Safepoint 语义仍在讨论；记录实现缺口，不新增可通过的占位场景，也不修改 Go 引擎。 |
| 4. AutoTest 基线 | 已批准。补充资源/数据作用域预检、Infra 与 OTEL 分组。依赖缺失或空矩阵不得通过。 |
| 5. AutoTest 业务场景 | 延后，直至缺失 Agent/MCP 以及 Map/Join/Feedback 契约达成一致。不得编造 Mock 或验收结果。 |
| 6. 日志生命周期 | 已批准。使用关联到来源 Plan/Run 的真实 INFO Sandbox Trigger/Result 与 TaskManager Success 事件，代替不可用的 DEBUG Start 事件；同时解析 JSON 与 Go Text 日志。 |
| 7. 真实扫描与重扫 | 已批准。使用固定版本的官方 SonarSource Scanner 初扫。重扫必须对应已交付 Commit、不同且成功的 CE Task，以及匹配的 Analysis Revision。已有 Findings 导出或缺失任务均不得通过。 |

Security 保留原有 23 个 ID，并加入遥测 ID 51/52，共 25 组：

| ID | 操作覆盖 |
| --- | --- |
| 01、11、12、14 | 目录结构、基础设施、Console 导入、认证 |
| 21–25 | API 生命周期、通知投递、运行时生命周期、MQTT 分发、A2A |
| 31–37 | Seed/Scan/Trigger、发现、修复、交付/重扫、Commit、Knowledge、Workspace |
| 41–46 | 浏览器 AgentGraph 执行、Agent、Skill、集成、Knowledge/通知、Hosted Login |
| 13、51、52 | 持久业务执行关联到 Trace、指标与生命周期日志 |

UI AgentGraph 写入自己的 Workspace 文件；Verifier 读取字节/Hash，并关联同一持久 Attempt
的 Jaeger 与运行时日志。主流程检查保留每个活动 Node 的身份。Verifier 写入的数据库
探针不得被当作业务产生的持久化或 Embedding 证据。

## 结构与运行时策略

- 两个原 `e2e/` 树已迁入各用例根目录。每个场景有具名类与
  `deploy/init/verifier` 生命周期。通用实现集中于 `tests/e2e_util/`，由允许的
  用例 `util/` 模块导出；应用客户端属于 `verifier/context.py`。
- `deploy/flowgent/` 仅包含配置、Compose Override 及正式 Dockerfile/Ignore
  文件链接。Helm 直接加载 `deploy/helm/flowgent`，不得在用例中保留副本或链接。
  部署初始化与验收逻辑属于 `verifier/infra/`；其他用例依赖拥有自己的 Source、
  Dockerfile、Compose 与 Helm 资产，Source 下不另建测试目录。
- AutoTest 的旧 Go MCP 保持真实 Stdio 契约，提供可选 `manual-stdio` Profile
  或可 Attach 的 Pod。二者已编译并响应 MCP Initialize/Tools-list；HTTP 集成仍按
  第 5 项延后。
- 统一镜像矩阵提供来源地址、别名、构建参数及 Compose/Helm 输入。区域优先级为
  `IN_GFW`、`NON_GFW`、旧 `IN_CN_GFW`；覆盖键为 `FLOWGENT_E2E_IMAGE_<KEY>`。
  替换仅匹配完整引用，不替换其他 Tag 的前缀。
- Workload 镜像使用 linux/amd64，包括翻译执行的 macOS/Windows Engine。macOS
  Solana 使用指定 `3.1.14-arm64`。用户批准同版本官方原生 arm64 SonarQube，
  因为 Elasticsearch Seccomp 无法在 Rosetta 下启动；Scanner/MCP/PostgreSQL
  仍使用 amd64。
- Colima、OrbStack 与原生 Docker/k3s/containerd 共用运行时发现与架构检查。
  Windows 进程清理由 Taskkill 完成。实际 OrbStack/Windows 主机当时不可用，
  对应路径只有契约测试。
- 镜像通过 `~/mirror-pull.sh` 与现有阿里云仓库获取。官方 Scanner 不构建中间
  Fixture 镜像，其 Python Adapter 挂载入固定版本厂商镜像。源码下载按精确
  Commit 缓存；扫描任务不得回退到 Latest Task。

## 历史验证与剩余限制

构建、真实矩阵及精确 Artifact Override 记录于用例报告。框架检查覆盖双区域渲染、
组件资产、镜像/运行时/平台选择、生命周期/类约束、证据缺失失败、Collector 作用域
RBAC 与进程清理。

当时获批准的 14-GiB Colima 重启已完成，原先运行的独立容器已恢复。官方 DeepSeek
Chat 请求在主机与 Pod 中成功。原生 Sonar API/Admin 检查及 385-tool MCP Bridge
通过，真实初扫产生 2,075 个 Finding；这些结果不能单独证明端到端修复或交付。

AutoTest 完整默认入口也已执行：预检会在修改前因缺失 Agent/MCP、Map 之外的 Item
引用和不支持的 Map/Feedback 契约而失败。不声称完整 AutoTest 应用通过。共享 Go
运行时限制见[精简缺口记录](e2e-runtime-gaps.md)。

该轮最终本地矩阵在两个后端都执行全部 25 组：Kubernetes **17/25**、Docker
**17/25**，使用官方 DeepSeek 与真实 Sonar 分析。两个主运行均停在翻译执行的
Seccomp Allowlist Sandbox；指标与 Docker 本地分发日志缺口也未通过严格断言。
Scanner 的真实已有分支重扫及两种标准网络能力探针独立通过。该轮根目录构建和
42 项共享 E2E 检查通过。精确证据及覆盖值见
[本地验证报告](../../use-cases/security-autonomy-fixer/reports/VALIDATION.md)。
