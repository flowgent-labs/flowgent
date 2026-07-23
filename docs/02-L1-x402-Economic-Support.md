# X402 Economic Layer — Design & Implementation

**Date:** 2026-05-09
**Status:** Phase 1 complete
**Scope:** Consumer-side x402 runtime. Settlement delegated to Coinbase facilitator.

---

## 1. Architecture Principle

```
Flowgent = orchestration + policy + economic runtime
Flowgent ≠ settlement infrastructure
```

Economic layer is **optional** and **isolated** from the orchestration engine.
When `wallet.enabled: false` (or nil), Flowgent operates as a pure orchestration runtime.

---

## 2. Module Layout

```
pkg/core/pkg/client/
├── x402.go                         # HTTP 402 parser (body v2 + V1 header fallback)
├── x402_httpclient.go              # x402-aware HTTP client used by TM/tool calls
├── httpclient_factory_x402.go      # enables x402 client when wallet.enabled=true
├── policy/                         # Spending policy engine
├── facilitator/                    # x402 facilitator HTTP client
└── signclient/                     # MQTT SignClient (TM → Wallet)

pkg/model/pkg/
├── payment.go                      # PaymentIntent, PaymentReceipt, PaymentAuthorization
└── signclient.go                   # SignClient interface

pkg/wallet/pkg/
├── walletmanager.go                # Wallet daemon: HTTP key API + MQTT signer
├── secretstore.go                  # SecretStoreProvider interface
├── providers/
│   ├── default.go                  # AES-256-GCM encrypted SQLite-backed store
│   ├── csi.go                      # CSI-mounted secret provider
│   └── vault.go                    # Hashicorp Vault provider
├── approvals/                      # Approval helper package
├── receipts/                       # Payment receipt persistence
└── wallet/wallet.go                # Wallet abstraction

pkg/wallet/pkg/cmd/main.go          # flowgent-wallet CLI
deploy/docker/facilitator/  # Facilitator Dockerfile + k8s manifest + x402-rs source
```

---

## 3. What We Built (Phase 1)

### 3.1 x402 Response Parsing (`pkg/core/pkg/client/x402.go`)

- Parses x402 v2 `PaymentRequired` JSON body first
- Falls back to V1 `X402-Payment` header parsing
- Validates non-empty `Accepts`
- Constants: `HeaderX402Auth`
- `SetAuthorizationHeader()` attaches token for retry
- `IsX402Response()` predicate for 402 detection

### 3.2 Core Types (`pkg/model/pkg/payment.go`)

```
PaymentIntent       — created BEFORE authorization (PENDING→APPROVED→DENIED→PAID→FAILED)
PaymentReceipt      — returned by facilitator after settlement
PaymentAuthorization — signed auth sent to facilitator (intent_id, wallet, signature, payload)
PaymentError        — structured error (PAYMENT_DENIED, PAYMENT_REQUIRES_APPROVAL, etc.)
```

### 3.3 Spending Policy Engine (`pkg/core/pkg/client/policy/`)

Evaluates **before** authorization:

| Rule | Config Key |
|------|-----------|
| Asset allowlist | `allowed_assets` |
| Chain allowlist | `allowed_chains` |
| Max single payment | `max_single_payment_usd` |
| Daily budget cap | `max_daily_budget_usd` |
| Domain allowlist | `allowed_domains` (supports `*.example.com` wildcards) |
| Domain blocklist | `blocked_domains` (takes precedence over allowlist) |
| Approval threshold | `require_human_approval_above_usd` |

`SpendingStore` interface for daily spend tracking (in-memory default, DB-backed for production).

### 3.4 Wallet Abstraction (`pkg/wallet/pkg/wallet/`)

```
Wallet interface:
  Address() string
  SignAuthorization(ctx, data []byte) ([]byte, error)
  Balance(ctx) (decimal.Decimal, error)

Manager:
  - Multi-wallet support
  - Default wallet selection
  - SignPaymentAuthorization() → PaymentAuthorization
```

### 3.5 Secret Store (`pkg/wallet/pkg/secretstore.go` + `pkg/wallet/pkg/providers/`)

```
SecretStoreProvider interface:
  GetSecret / PutSecret / DeleteSecret / ListSecrets
```

**DefaultSecretStoreProvider:**
- AES-256-GCM encryption
- DB-backed (SQLite or Postgres via `database/sql`)
- Master key resolution: config value → config file → `FLOWGENT_MASTER_KEY` env → `FLOWGENT_MASTER_KEY_FILE` env
- Kubernetes: mount via Vault CSI Driver or GCP Secret Provider Class
- Table: `payment_secrets (key TEXT PK, value BYTEA, created_at, updated_at)`

**VaultSecretStoreProvider:**
- Hashicorp Vault contract defined
- SDK wiring deferred to production deployment time
- Wallet keys SHOULD NOT persist locally when using Vault

### 3.6 X402-Aware HTTP Client (`pkg/core/pkg/client/x402_httpclient.go`)

**The x402-aware HTTP client is the core economic-aware fetch primitive used by TaskManager/tool execution.**

Full flow:
```
1. HTTP request
2. Receive 402 Payment Required
3. Parse x402 metadata (X402-Payment header)
4. Create PaymentIntent (status=PENDING)
5. Evaluate spending policy → deny or proceed
6. If above threshold → request human approval (reuses existing HumanApproval store)
7. Publish unsigned payload via MQTT `sign/request`; wallet signs it
8. Send authorization to facilitator → receive PaymentReceipt
9. Record spend for daily budget tracking
10. Retry original request with X402-Authorization token
```

### 3.7 Facilitator Client (`pkg/core/pkg/client/facilitator/`)

- POST `/authorize` with signed `PaymentAuthorization`
- Returns `PaymentReceipt` on success
- Health check: GET `/health`
- Flowgent sends signed auth. Facilitator handles onchain settlement.

### 3.8 Payment Approvals (`pkg/wallet/pkg/approvals/`)

- Implements the approval bridge for x402 payment intents
- Reuses existing `model.HumanApproval` store (NO duplicate subsystem)
- Polls for approval resolution (production should use event-driven API callbacks)
- Timeout → `APPROVAL_TIMEOUT` error
- Rejection → `APPROVAL_REJECTED` error

### 3.9 Payment Receipts (`pkg/wallet/pkg/receipts/`)

- DB table: `payment_receipts` (id, intent_id, tx_hash, asset, amount, chain, facilitator, authorization, paid_at, expires_at)
- Query by intent ID or date range
- Index on `intent_id`

### 3.10 Wallet Daemon (`pkg/wallet/pkg/walletmanager.go`, `pkg/wallet/pkg/cmd/main.go`)

Standalone daemon:
```
flowgent-wallet start|stop|restart   # Daemon lifecycle
flowgent-wallet generate-key         # Ed25519 keypair generation
  --format text|json|base64          # Output format
```

REST API on `:9901`:
- `GET  /health`
- `POST /api/v1/wallet/sign`   (body: {wallet, payload})
- `GET  /api/v1/wallet/address` (query: ?wallet=...)
- `GET  /api/v1/wallet/balance`
- `GET  /api/v1/wallet/keys`
- `GET  /api/v1/wallet/keys/{name}`
- `POST /api/v1/wallet/keys`
- `DELETE /api/v1/wallet/keys/{name}`

MQTT signing:
- Subscribe: `$share/wallet-pool/flowgent/v1/+/flows/+/runs/+/sign/request`
- Publish: `flowgent/v1/{tenant}/flows/{flow}/runs/{run}/sign/response`
- Request/response payloads: `messager.SignRequest` / `messager.SignResponse`

**Important boundary:** Wallet service is a signer and key manager only. It does **not** parse HTTP 402 responses, evaluate policy, create `PaymentIntent`, or call the facilitator. Those steps live in the TM-side x402 client (`pkg/core/pkg/client/`).

### 3.11 Config Integration

```yaml
wallet:
  enabled: true
  policies:
    max_single_payment_usd: 1
    max_daily_budget_usd: 50
    allowed_domains: ["*.googleapis.com", "*.openai.com"]
    blocked_domains: ["*.unknown.xyz"]
    require_human_approval_above_usd: 5
    allowed_assets: [USDC]
    allowed_chains: [base, solana]
  secret_store:
    provider: default          # default | csi | vault
    master_key_file: /var/secrets/master.key
    vault:
      address: ""
      token: ""
      token_file: ""
      mount_path: "secret"
      secret_path: "wallet"
      role: "flowgent"
  x402:
    default_facilitator: "http://localhost:8085"
    timeout: 30s
    max_retries: 3
```

---

## 4. What We Explicitly DO NOT Implement

These are intentional exclusions. Flowgent is consumer-side only.

| NOT implemented | Why |
|-----------------|-----|
| Onchain settlement | Facilitator responsibility (Coinbase) |
| Bridging | Facilitator responsibility |
| Liquidity routing | Facilitator responsibility |
| Escrow | Facilitator responsibility |
| Facilitator server | Flowgent uses Coinbase's facilitator container |
| AP2 protocol | Not in scope |
| MCP Server Gateway (monetization) | Future design only — documented below |
| Billing / metering | Future design only — documented below |

---

## 5. Key Design Decisions

### 5.1 Flowgent NEVER stores raw private keys
Keys are encrypted at rest (AES-256-GCM) or provided by CSI/Vault. Wallet daemon is the ONLY process with key access. Orchestration runtime asks wallet to sign opaque unsigned payloads via MQTT `sign/request`; the wallet HTTP API is for key management and local/admin signing operations.

### 5.2 PaymentIntent before payment
DO NOT pay immediately on 402. Create PaymentIntent → evaluate policy → optionally seek approval → then authorize. This enables audit trail and policy enforcement.

### 5.3 Human approval reuses existing subsystem
Approval should reuse `model.HumanApproval` and the engine's `Store.CreateHumanApproval` / `GetHumanApproval` / `UpdateHumanApproval`. No second approval system.

### 5.4 Spending policy evaluated client-side
Flowgent evaluates policies BEFORE sending authorization. Facilitator never sees policy rules. This keeps Flowgent as the policy decision point.

### 5.5 Economic layer is optional
`wallet.enabled: false` → x402-aware HTTP client is not used; Flowgent falls back to `GenericHttpClient`. x402 code is compiled only with the `x402` build tag.

---

## 6. Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/shopspring/decimal` | Fixed-point decimal for amounts |
| `github.com/spf13/cobra` | CLI (wallet subcommand) |
| `modernc.org/sqlite` | Wallet DB (secret store) |
| `crypto/aes`, `crypto/cipher` | AES-256-GCM (stdlib) |
| `crypto/ed25519` | Key generation + signing (stdlib) |

Vault support is implemented behind `pkg/wallet/pkg/providers/vault.go`; CSI-mounted secret support is implemented in `pkg/wallet/pkg/providers/csi.go`.

---

## 7. Deploy

### Docker Compose (all-in-one)
```bash
docker compose -f deploy/docker-compose.all-in-one.yml up
# flowgent + wallet + facilitator (+ postgres/emqx with --profile distributed)
```

### Kubernetes (Helm)
```bash
helm install flowgent deploy/helm/flowgent \
  --set wallet.enabled=true
# Creates all Flowgent services (apiserver, controller, jobmanager,
# taskmanager, wallet, notification). See deploy/helm/flowgent/values.yaml
# for full configuration.
#
# Facilitator (local dev only):
kubectl apply -f deploy/docker/facilitator/k8s-deployment.yaml
```

---

## 8. Current Gaps (Non-blocking)

1. **Build tag coverage** — x402 support is behind the `x402` build tag. Release/build targets should decide whether x402 is included by default for wallet-enabled deployments.
2. **Unit tests for x402 path** — parser, policy, facilitator, MQTT sign client, wallet signer, and HTTP retry flow need focused tests.
3. **Approval event-driven mode** — Current approval integration still needs production-grade callback/event handling.
4. **PG spending store** — `SpendingStore` uses in-memory default. Postgres-backed store is needed for distributed budget tracking.
5. **Wallet E2E execution** — `use-cases/security-autonomy-fixer/e2e-verification/scenarios/11_wallet_verifier.py` is added but still depends on a running wallet service and EMQX to execute fully.

---

## 9. Future Architecture (Documentation Only — DO NOT Implement)

### 9.1 Flowgent as Paid Capability Provider

Future: external A2A/MCP clients accessing Flowgent could be monetized via x402.

```
External Agents (A2A/MCP)
        ↓
MCP Server Gateway (managed by Flowgent)
        ↓ x402 Payment
Coinbase Facilitator → Settlement
```

### 9.2 Potential Billing Units

- Per agentflow execution
- Per node execution
- Per token usage
- Per GPU second
- Per tool invocation
- Per streaming minute

### 9.3 Future Module: MCP Server Gateway

```
mcp-server-gateway/src/
  routing/     — Request routing + x402 gate
  pricing/     — Pricing models per capability
  billing/     — Usage aggregation
  metering/    — Real-time consumption tracking
```

**This MUST NOT be implemented now. Design documentation only.**

---

## 10. Quick Reference for Other Agents

- **Wallet entry point:** `pkg/wallet/pkg/cmd/main.go` (`flowgent-wallet`)
- **Config struct:** `pkg/config/pkg/config/config.go` → `WalletConfig`
- **HTTP client factory:** `pkg/core/pkg/client/httpclient_factory_x402.go` → `NewHttpClient()`
- **Core x402 flow:** `pkg/core/pkg/client/x402_httpclient.go` → `X402PaymentHttpClient.Do()`
- **402 parser:** `pkg/core/pkg/client/x402.go` → `Parse()`
- **Policy check:** `pkg/core/pkg/client/policy/policy.go` → `Engine.Allow()`
- **MQTT signing client:** `pkg/core/pkg/client/signclient/mqtt.go` → `MqttSignClient.Sign()`
- **Wallet signer:** `pkg/wallet/pkg/walletmanager.go` → `handleSignRequest()`
- **Facilitator call:** `pkg/core/pkg/client/facilitator/facilitator.go` → `Client.Authorize()`
- **Secret encryption:** `pkg/wallet/pkg/providers/default.go` → `encrypt()` / `decrypt()`
