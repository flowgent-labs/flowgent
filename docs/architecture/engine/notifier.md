# Notifier

[System overview](../overview.md) · [API Server](apiserver.md)

Notifier is the outbound delivery service. It consumes notification events from
MQTT shared subscriptions, resolves channel configuration through the API
Server, delivers messages to external channels, publishes completion results,
and owns the WebSocket path used for UI/human-approval updates.

```text
Engine publisher ──notify event/MQTT──► Notifier ──► Slack / email / webhook / ...
       ▲                                  │
       └────notify result/MQTT────────────┴──────► WebSocket UI clients
```

## Delivery and Discovery

Bridges internal agentflow events to external communication channels.

### Architecture

```
  ┌────────────────────────────────────────────┐
  │       Notifier Service (2+ pods)        │
  │  ┌──────────────────┐  ┌────────────────┐  │
  │  │ MQTT Queue       │  │ Human Approval │  │
  │  │ Consumer         │  │ Scanner (5s)   │  │
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

MQTT shared subscription per namespace+flow: `flowgent/v1/{namespace}/flows/{flow}/runs/{run}/notify/event`.
Messages load-balanced across notifier pods via `$share/notify-pool`. Each message
dispatched to configured channels for that namespace.

### Dynamic channel secrets

Channel endpoints, tokens, passwords, signing secrets, and secret header maps are
managed from the UI but are write-only. `ISecretCipher` isolates cryptography from
the channel model; the default implementation uses AES-256-GCM with a random nonce,
versioned key ID, and namespace/channel/provider associated data. The API persists
one versioned ciphertext envelope in the channel config and returns only the set of
configured secret field names. The Notifier receives the opaque envelope, decrypts
it immediately before configuring a fresh sender, and publishes `DELIVERED` or
`FAILED` only after the external operation completes.

The active key and decryption key ring come from
`notifier.secret_encryption`. Helm mounts that key only into API Server and
Notifier pods; production deployments may reference an externally managed Secret.
Rotation changes the active key for new writes while retaining old keys until rows
have been re-encrypted.

### IDiscoveryClient — Pluggable Service Discovery

```go
type IDiscoveryClient interface {
    DiscoverPeers(ctx, labelSelector) ([]Peer, error)
    Self() Peer
    IsLeader(ctx, labelSelector) (bool, error)
    WatchPeers(ctx, labelSelector) (<-chan []Peer, error)
}
```

Implementations: `K8sDiscoveryClient` (label-selector pod listing, like Flink's
KubernetesHA), `StaticDiscoveryClient` (env-var based, for dev/CI).

---

## Expected Behavior

1. **Given** a notification event, **when** one shared-subscription consumer
   accepts it, **then** configured namespace channels MUST each receive the
   intended payload at most once per delivery attempt.
2. **Given** channel success or failure, **when** delivery finishes, **then** a
   correlated notification result MUST be published for the originating run.
3. **Given** a human-approval UI event, **when** it is pushed, **then** WebSocket
   clients may receive it but no engine component may depend on WebSocket for
   scheduling or durable state.
4. **Given** multiple Notifier replicas, **when** discovery changes, **then**
   shared subscriptions and leadership-sensitive work MUST converge without
   duplicate ownership.
5. **Given** a channel secret, **when** it is written, read, updated, or used for
   delivery, **then** plaintext MUST appear neither in the database nor in any
   management response, and a blank update MUST NOT erase it implicitly.
