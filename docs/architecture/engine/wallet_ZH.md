# AgentVault 与 x402 边界（L1）

[English](wallet.md)

AgentVault 是固定在仓库根目录 `agentvault/` 的独立 Rust 安全与支付服务。它拥有
Payer/Payee Identity、Key Custody、Policy、Approval、持久支付状态、Settlement 及
自身 AuthGuard 集成。Flowgent **MUST NOT** 加载、解密、记录或导出 AgentVault 私钥。

## 当前集成状态

该边界尚未完成正式集成：

- AgentVault 是独立 Repository/Submodule，拥有自身 API、SDK、数据库、Release 与
  Makefile。
- Flowgent 可选、带 Build Tag 的 x402 实现仍使用旧 `wallet.sign.v1` Digest-signing
  Protocol，经 MQTT 或 Unix Socket 调用；同时仍保留 Flowgent 本地支付 Policy 和
  进程内 Spending Ledger。
- 旧 Client 未使用当前 AgentVault SDK/API 或持久支付生命周期。因此
  `make test-x402` 只证明 Flowgent Client 单元与 Wire 行为，不代表生产级
  AgentVault 集成已通过。
- Flowgent 默认构建不启用 x402，只有 `x402` Build Tag 才会编译该能力。

文档和新代码 **MUST** 区分这些事实：不得把旧 Signer 称为 AgentVault，也不得把
Flowgent 旧 Policy Ledger 当成真实支付授权事实源。

## 目标职责归属

| 关注点 | AgentVault | Flowgent |
|---|---|---|
| 私钥、签名身份、Key Rotation | 拥有 | **MUST NOT** 访问 |
| Payer/Payee Identity 与支付凭据 | 拥有 | 只引用外部身份 |
| 支付授权 Policy 与人工审批 | 拥有 | 提供工作流上下文并等待决定 |
| 持久幂等、Execution Record、Settlement、Reconciliation | 拥有 | 只保存 Run 所需的编排/结果引用 |
| HTTP 402 Challenge 与支付执行 | 通过 API 校验/执行 | 识别 Challenge 并调用 AgentVault Adapter |
| Tool/Run 状态与业务结果 | 不拥有 | 拥有 |

批准不等于执行成功。Flowgent 支付节点 **MUST** 核对 AgentVault Execution/Settlement
结果，并在释放下游节点前持久化幂等副作用引用。

## 旧 Client（已实现但处于过渡期）

当前 `pkg/core/pkg/client/signclient` 发送有界 JSON `SignRequest` 并接收
`SignResponse`。Request Version 是 `wallet.sign.v1`，包含预计算的 32-byte
EIP-712 Digest、Request ID、Client/Key/Purpose Identity 与有界有效期。成功响应带
EOA Address 和可恢复 `r || s || v` Signature；Flowgent 会在本地恢复 Signer，
不匹配时 fail closed。

Transport 为二者之一：

```text
MQTT: wallet/v1/sign/requests/{client_id}
      wallet/v1/sign/responses/{client_id}
Unix: 通过 mode-0600 Socket 交换 newline-delimited JSON
```

只有 TaskManager 会构造当前具备支付能力的 HTTP Client。API Server、Controller、
JobManager、Notifier 和管理路径 **MUST NOT** 打开旧签名 Transport；
`wallet.enabled=false` 是安全默认值。

该路径可作为迁移输入，但新支付能力 **MUST** 通过 AgentVault Adapter 实现，不得继续
扩展旧 Protocol、本地 Approval 或进程内 Spending Store。

## 必须完成的迁移

1. 基于固定版本的 AgentVault SDK/API 定义 Flowgent Adapter；外部 Payer/Payee 使用
   Identity Reference，而不是 Key Alias。
2. 用 AgentVault Policy/Approval 生命周期替换 Flowgent 本地预算授权与审批。
   `orh_approval` 可作为 Flowgent 工作流 Gate，但 **MUST NOT** 冒充 AgentVault
   Settlement Authorization。
3. 支付 Request 必须绑定不可变 Tool、Amount、Asset、Network、Recipient、
   Run/NodeRun、Request Hash、Expiry 与 Idempotency Key。
4. 在 Flowgent External Effect Record 中保存 AgentVault Payment/Execution ID，
   对不确定网络结果执行 Reconciliation，而不是盲目重试。
5. 增加真实 Integration/E2E，覆盖 Approval、重复 Callback、Expiry、Settlement、
   Failure 与 Reconciliation；该场景全绿后才可移除旧 Signer。

当前 `orh_effect_execution` Schema 为持久外部副作用执行保留，但 Go Runtime 尚未接入；
完成它也是本次迁移的一部分。

## 独立生命周期

AgentVault 命令 **MUST** 经过自身 Makefile；Flowgent 根 Makefile **MUST NOT** 代理：

```bash
make -C agentvault help
make -C agentvault test
make -C agentvault lint
make -C agentvault build
```

AgentVault Rust Build 前按其仓库规则检查 `agentvault/target/` 是否超过 10 GiB；必要时
使用其 Makefile Clean Target。Flowgent 变更只通过仓库根 Makefile，包括：

```bash
make test-x402
```

## 预期行为

1. **Given** x402 被关闭，**when** Tool 返回 HTTP 402，**then** Flowgent
   **MUST NOT** 构造 Signer 或暴露 Key 配置。
2. **Given** 过渡 Signer 返回 Signature，**when** 恢复地址与配置地址不同，
   **then** Flowgent **MUST** 拒绝。
3. **Given** 新支付集成，**when** 需要 Custody、Policy、Approval、Execution 或
   Settlement，**then** 这些职责 **MUST** 委托 AgentVault，不能在 Flowgent 重做。
4. **Given** 模糊或重复支付 Callback，**when** Flowgent 处理它，**then** 同一不可变
   Request/Idempotency Key **MUST NOT** 导致重复支付或重复下游执行。
5. **Given** AgentVault Source 变更，**when** 被接受，**then** 其自身 Makefile Test
   **MUST** 独立于 Flowgent 根 Makefile 通过。
