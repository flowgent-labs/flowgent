# 持久数据生命周期

[English](data-lifecycle.md)

本文档是当前持久数据契约的规范归属。空库初始化 Schema 位于
[`migration/postgres/20260616a/01_init.ddl.sql`](../../migration/postgres/20260616a/01_init.ddl.sql)
和
[`migration/sqlite/20260616a/01_init.ddl.sql`](../../migration/sqlite/20260616a/01_init.ddl.sql)，
旧模型到当前模型的映射见
[`migration/SCHEMA_MAPPING.md`](../../migration/SCHEMA_MAPPING.md)。

## 领域边界

| 前缀 | 归属 |
|---|---|
| `orh_*` | Namespace、Flow Revision、Run、NodeRun、Checkpoint、Dispatch、Event、Outbox、Approval、Effect、运行配置与 Release |
| `llm_*` | Provider、MCP、Agent 及 Revision、可复用 Skill Definition 与文件、Instruction |
| `knw_*` | Source、Document 及 Revision、层级 Content、Embedding Profile/Vector 与发布 Candidate |
| `nfy_*` | 加密的通知渠道配置 |

Flowgent 不保存 IAM User、Role、Policy、Credential 或 Session。审计 Actor 使用
AuthGuard canonical principal 外部引用。Namespace、Approval、Audit、Event 和迁移记录
是业务/控制面状态，不是 IAM 副本。

API Server 是持久业务状态的规范网关。只有两个明确的窄例外：独立 A2A Server 只拥有
`orh_a2a_task`；JobManager 可把 PostgreSQL 或 Redis 用作协调锁后端，但不会绕过 API
读写业务表。

## 稳定身份与不可变版本

Flow、Agent、可复用 Skill 与 Knowledge Document 均把稳定身份和不可变内容版本分开：

- `orh_flow` 指向 `orh_flow_revision`；
- `llm_agent` 指向 `llm_agent_revision`；
- `llm_skill` 指向 `llm_skill_revision`，`llm_skill_file` 属于具体 Revision，并保存
  内容寻址的文件元数据；
- `knw_document` 指向 `knw_document_revision`。

保存或发布会创建新 Revision，并通过 CAS 切换 Owner 当前指针。Run 总是引用确定的
Flow Revision；数据库会强制 current-revision pointer 属于其 Owner。

公开 API 与持久层对执行尝试的规范名称是 **NodeRun**。部分 Go 内部类型仍使用旧名
`TaskRunInfo`；该名字 **MUST NOT** 再成为新的 REST 或数据库契约。

## Instruction、Knowledge 与 Execution Memory

三者 **MUST** 保持分离：

- **Instruction** 控制“怎么做”。Namespace/Flow Instruction 以不可变 Revision 保存于
  `llm_instruction`。System Instruction 由部署提供，不是可编辑业务配置。Run 可归档正文
  或不可变引用。
- **Knowledge** 是可检索事实或经验，遵循 Source → Document → DocumentRevision →
  层级 Content → Embedding。
- **Execution Memory** 只属于某个 Run/NodeRun attempt，包括 Message、Tool State、
  Workspace Version、Checkpoint、Lease 与 Fencing Token。新 Run 不继承旧 Run 会话。

`knw_content` 用父级与 ordinal 统一表示 Section、Chunk 和 Summary。已发布 Document
Revision 及正文不可覆盖。跨 Run 学习 **MUST** 通过审批与发布进入 Knowledge 或
Instruction，**MUST NOT** 把失败会话状态复制到后续 Run。

## Context Snapshot 与恢复

创建 Run 时使用 serializable transaction 锁定：

- 稳定 Flow 身份及确定的 Flow Revision/checksum；
- 引用的 Agent Revision ID；
- Namespace/Flow Instruction pointer；
- 当时已授权且当前可见的 Knowledge Revision ID；
- 最终生效的 `summarize_enabled`。

结果写入 `orh_run.context_snapshot`。其中保存版本清单或索引引用，不复制整个知识库；
快照创建后撤销权限仍应立即生效。

`orh_node_run`、`orh_node_checkpoint`、`orh_node_message`、`orh_dispatch` 以及
Lease/Fencing 字段保存安全重试所需的物理状态。同一物理 attempt 的重投继续原 attempt；
业务重试创建新 attempt，并可从选定 Checkpoint 恢复。过期 fencing token 写入会被拒绝。

当前限制：快照采集已实现，但运行时尚未强制使用其中每一个版本。Agent 执行仍可能按
名称解析 latest Agent；Knowledge Search 尚未限制到快照的 `knowledge_revision_ids`；
默认 System Instruction 是 `deployment://system-instruction` 引用而非归档正文。Schema
具备持久 Checkpoint 恢复结构，但调度层尚未在所有重派路径把已存 Checkpoint 装载回新
ExecutionPlan。这些属于正确性缺口，不是已完成能力。

## Knowledge 检索与发布

PostgreSQL 以原生 `pgvector` 列保存向量；SQLite 使用 `sqlite-vec`，由普通元数据表映射
到按维度建立的 `vec0 float[D]` 虚拟表。向量 **MUST NOT** 用 JSON/TEXT 伪装。不可变
Profile Key 标识模型、模型版本、维度与距离度量，不同 Profile 不参与同一次相似度计算。

已实现的检索包括带 Scope 的全文检索、原生向量检索、ACL/AuthGuard 过滤以及父级/相邻
Content 扩展；正文进入 LLM 前即完成授权。当前尚无 GraphRAG、外部图/向量数据库，也
没有完整的生产级 Embedding 生成与摄取流水线。

已发布 Knowledge 在直接 API 中只读。新增内容通过 `knw_candidate` 和统一审批流：

1. 创建包含 Provenance、目标 Scope、基线 Revision、内容 Hash 与幂等键的不可变 Candidate；
2. 创建带冻结 Request 和 SHA-256 Request Hash 的 `orh_approval`；
3. 审批只能决定一次；过期、取消、拒绝或 Request 漂移均 fail closed；
4. 在一个事务中完成基线 CAS、不可变 Revision、Owner Pointer CAS、Candidate 状态和
   Outbox/Event 写入。

## 统一审批与总结

`orh_approval` 是 `human_gate`、`tool_call`、`payment`、`knowledge_publish` 与
`instruction_publish` 的唯一审批事实源。状态只能从 `pending` 进入 `approved`、
`rejected`、`expired` 或 `cancelled` 之一，终态不得改判。批准只证明允许执行；外部
副作用仍需幂等执行与结果核对。

Flow 总结默认关闭。创建 Run 时会把 Run 显式覆盖或 Flow Revision 配置合成为最终有效值
并持久化。只有生效值为 true 且 Run 已完成时，built-in summarizer 才会运行；它会清理
明显 Secret、生成 Flow-scope Candidate，并始终要求人工审批。它不会直接发布 Knowledge
或 Instruction，也不会重新拉起已完成 Run。

## Schema 演进

当前仓库有意提供干净的空库初始化 Schema，而不保留废弃兼容表；这适用于空库与 E2E。
下一次不兼容 Schema 变更进入持久生产数据前，项目除更新 bootstrap DDL 外，**MUST**
定义并验证非破坏性升级路径。

