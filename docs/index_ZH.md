# Flowgent 文档

[English Index](index.md)

本目录将现行设计、应用、运维指南、开发计划和历史记录分开管理。
查阅时应进入拥有该主题的最小范围文档。

## 现行设计

| 层次 | 文档 | 范围 |
|---|---|---|
| L1 引擎 | [引擎总览](architecture/overview_ZH.md) | 跨组件调用、状态与消息契约、部署及共享模块 |
| L1 引擎 | [API Server](architecture/engine/apiserver_ZH.md) | 外部网关与唯一 durable-state client |
| L1 引擎 | [Controller](architecture/engine/controller_ZH.md) | 活跃 Run 的 JobManager 协调 |
| L1 引擎 | [Runtime Clusters](architecture/engine/runtime-clusters_ZH.md) | Flink 风格 application/session 运行时隔离 |
| L1 引擎 | [JobManager](architecture/engine/jobmanager_ZH.md) | DAG scheduling 与 runtime ownership |
| L1 引擎 | [TaskManager](architecture/engine/taskmanager_ZH.md) | Slot execution 与 executor routing |
| L1 引擎 | [Sandbox](architecture/engine/sandbox_ZH.md) | 隔离脚本执行 |
| L1 引擎 | [Notifier](architecture/engine/notifier_ZH.md) | Delivery 与 UI event 边界 |
| L1 引擎 | [Wallet](architecture/engine/wallet_ZH.md) | 外部 key custody 与 digest-signing 边界 |
| L2 AgentFlow | [AgentFlow 应用架构](architecture/agent-flow_ZH.md) | 通用 DAG 模型、能力、限制与编排实践 |

## 应用

每个应用在 `usecase/{use-case}/README.md` 中归口自己的 Nodes、Edges、
Integrations、Limits 与 Verification；可执行资源与其放在同一用例目录。
共享 DAG 语义归属 [AgentFlow 应用架构](architecture/agent-flow_ZH.md)。

| 应用 | 范围 | 入口 |
|---|---|---|
| Security Autonomy Fixer | 从 SonarQube 修复到 GitHub 交付及修改后验证；27 Nodes/31 Edges DAG | [设计](../usecase/security-autonomy-fixer/README.md) · [中文](../usecase/security-autonomy-fixer/README_ZH.md) · [Manifest](../usecase/security-autonomy-fixer/config/flows/security-autonomy-fixer.yaml) · [E2E](../usecase/security-autonomy-fixer/e2e/VERIFICATION.md) |
| AutoTest Generation | 由 Confluence 驱动，为 Spring Boot、Flask 与 React 生成测试；18 Nodes/22 Edges Draft | [设计](../usecase/autotest-generator/README.md) · [中文](../usecase/autotest-generator/README_ZH.md) · [Manifest](../usecase/autotest-generator/config/flows/autotest-generation-v1.yaml) |

## 运维

| 文档 | 范围 |
|---|---|
| [依赖镜像](runbooks/build-dependency-images_ZH.md) | 构建和运行本地 Facilitator、EVM 与 Solana 镜像 |

## 开发与历史

| 状态 | 位置 | 含义 |
|---|---|---|
| 活跃 | [`development/`](development/) | 仍承载当前验收条件的工作 |
| 历史 | [`development/archive/`](development/archive/) | 早期设计意图和带日期的实施记录，仅作背景参考 |

当前活动开发计划：

- [Security Fixer 纯 UI 完整端到端闭环](development/security-fixer-ui-e2e_ZH.md)

## 语言版本

- 现行设计、应用、Runbook 和活跃开发文档以不带 `_ZH` 的英文文件为规范源文档。
- 中文评审版本与英文源文件同目录，后缀为 `_ZH.md`。
- 历史归档是冻结记录，不参与现行双语评审配对。
- 两个版本 **MUST** 保留相同的设计语义、接口、约束及预期行为。
- 图和示例 **MAY** 本地化或精简，但其中表达的关系、边界和限制 **MUST**
  保持不变。
- 任何语义变更 **MUST** 在同一个变更中同步更新两个语言版本。

## 维护规则

- 当前行为 **MUST** 写入拥有该行为的现行设计文档。
- 通用 AgentFlow 语义 **MUST** 保留在 `architecture/`；应用专属 Nodes、Edges、
  Integrations 与 E2E 证据 **MUST** 保留在所属的
  `usecase/{use-case}/README.md`。
- 每个真实应用 **MUST** 提供 `README.md` 和 `README_ZH.md`；其 Flow、Agent 与
  MCP Definition **MUST** 在 `usecase/{use-case}/config/` 下保持自包含，运行凭据
  **MUST** 保留在 Manifest 之外。
- 每个设计概念 **MUST** 只有一个规范归属位置；其他文档 **MUST** 通过链接引用，禁止复制。
- 设计文档 **MUST** 明确区分已实现行为、保留设计和已知限制。
- 规范性要求 **MUST** 使用 `MUST`、`MUST NOT` 或 `DO NOT` 等明确关键字。
- 图和案例 **MUST** 用于澄清契约与数据流；**MUST NOT** 替代精确的行为约束。
- 历史文档 **MUST** 保留在 `development/archive/`，**MUST NOT** 作为当前实现依据。
- **MUST NOT** 将运行 Manifest、源码、密钥、生成报告或大体积资源复制到 `docs/`。
- **MUST NOT** 创建空分类目录或推测性的占位文档。
- 文档变更只有在链接和标题验证通过后，才能视为完成；现行及活跃文档还
  **MUST** 验证双语配对。
