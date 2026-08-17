# Notifier

[系统总览](../overview_ZH.md) · [API Server](apiserver_ZH.md)

Notifier 是出站投递服务。它从 MQTT 共享订阅消费通知事件，通过 API Server
解析 Channel 配置，将消息投递到外部 Channel，发布完成结果，并拥有用于 UI/
人工审批更新的 WebSocket 路径。

```text
引擎发布者 ──通知事件/MQTT──► Notifier ──► Slack / Email / Webhook / ...
     ▲                           │
     └────通知结果/MQTT──────────┴──────► WebSocket UI 客户端
```

## 投递与发现

Notifier 将内部 AgentFlow 事件桥接到外部通信 Channel。

### 架构

```text
  ┌────────────────────────────────────────────┐
  │          Notifier Service（2+ Pod）         │
  │  ┌──────────────────┐  ┌────────────────┐  │
  │  │ MQTT Queue       │  │ Human Approval │  │
  │  │ Consumer         │  │ Scanner（5s）   │  │
  │  │ Topic: /flowgent/│  │ SELECT PENDING │  │
  │  │ notify/queue/    │  │ human_approvals│  │
  │  │ {namespace}/{flow}  │  └───────┬────────┘  │
  │  └────────┬─────────┘          │           │
  │           └──────────┬─────────┘           │
  │                      ▼                     │
  │           ┌──────────────────┐            │
  │           │ Channel Dispatcher│            │
  │           │ Slack/DingTalk/   │            │
  │           │ Telegram/Email/   │            │
  │           │ Webhook           │            │
  │           └──────────────────┘            │
  └────────────────────────────────────────────┘
```

### Queue Consumer

每个 Namespace+Flow 使用 MQTT 共享订阅：
`flowgent/v1/{namespace}/flows/{flow}/runs/{run}/notify/event`。消息通过
`$share/notify-pool` 在 Notifier Pod 间负载均衡，并分发到该 Namespace 配置的 Channel。

### 动态 Channel Secret

Channel endpoint、token、password、签名 Secret 与敏感 Header Map 可在 UI 中维护，
但全部只写不读。`ISecretCipher` 将密码学实现与 Channel 模型隔离；默认实现使用
AES-256-GCM、随机 Nonce、带版本 Key ID，并把 Namespace/Channel/Provider 作为
Associated Data。API 只在 Channel Config 中持久化一个带版本的密文信封，读取时仅
返回已配置的 Secret 字段名。Notifier 获取不透明信封，在创建全新 Sender 前即时
解密，并且只有外部投递真正结束后才发布 `DELIVERED` 或 `FAILED`。

活动密钥和解密 Key Ring 来自 `notifier.secret_encryption`。Helm 只把密钥挂载到
API Server 与 Notifier Pod；生产环境可引用外部管理的 Secret。轮换时新写入使用新
活动密钥，旧密钥保留到已有记录完成重加密。

### IDiscoveryClient——可插拔服务发现

```go
type IDiscoveryClient interface {
    DiscoverPeers(ctx, labelSelector) ([]Peer, error)
    Self() Peer
    IsLeader(ctx, labelSelector) (bool, error)
    WatchPeers(ctx, labelSelector) (<-chan []Peer, error)
}
```

实现包括：`K8sDiscoveryClient`（通过 Label Selector 列出 Pod，类似 Flink
KubernetesHA）和 `StaticDiscoveryClient`（基于环境变量，用于开发/CI）。

---

## 预期行为

1. **给定**通知事件，**当**一个共享订阅消费者接收它时，**则**每个已配置 Namespace Channel 在单次投递尝试中 **MUST** 至多收到一次目标 Payload。
2. **给定** Channel 成功或失败，**当**投递结束时，**则 MUST** 为来源 Run 发布可关联的通知结果。
3. **给定**人工审批 UI 事件，**当**推送该事件时，**则** WebSocket 客户端可以接收，但任何引擎组件 **MUST NOT** 依赖 WebSocket 进行调度或持久状态管理。
4. **给定**多个 Notifier 副本，**当**发现结果变化时，**则**共享订阅和需要 Leader 的工作 **MUST** 收敛，且 **MUST NOT** 产生重复所有权。
5. **给定**任意 Channel Secret，**当**写入、读取、更新或投递使用时，**则**明文
   **MUST NOT** 出现在数据库或管理响应中，且空值更新 **MUST NOT** 隐式删除它。
