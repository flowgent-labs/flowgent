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
When `payments.enabled: false` (or nil), Flowgent operates as a pure orchestration runtime.

---

## 2. Module Layout

```
src/payments/
├── model.go              # Core types
├── config.go             # PaymentsConfig (YAML-mapped)
├── secretstore.go        # SecretStoreProvider interface
├── x402/                 # HTTP 402 response parser
├── policy/               # Spending policy engine
├── wallet/               # Wallet abstraction + Manager
├── pwf/                  # PayableWebFetch runtime (core primitive)
├── facilitator/          # Coinbase x402 facilitator HTTP client
├── approvals/            # Human approval (reuses existing HumanApproval store)
├── receipts/             # Payment receipt persistence
└── providers/
    ├── default.go        # AES-256-GCM encrypted DB-backed store
    └── vault.go          # Hashicorp Vault provider (contract, SDK wiring deferred)

src/cmd/core/wallet.go     # flowgent wallet daemon (subcommand)
deploy/docker/facilitator/  # Facilitator Dockerfile + k8s manifest + x402-rs source
```

---

## 3. What We Built (Phase 1)

### 3.1 x402 Response Parsing (`src/payments/x402/`)

- Reads `X402-Payment` header from HTTP 402 responses
- Unmarshals JSON into `X402PaymentRequest`
- `Validate()` checks: asset, amount (>0), chain, recipient, settlement protocol, facilitator URL
- Constants: `HeaderX402Payment`, `HeaderX402Auth`
- `SetAuthorizationHeader()` attaches token for retry
- `IsX402Response()` predicate for 402 detection

### 3.2 Core Types (`src/payments/model.go`)

```
X402PaymentRequest  — parsed 402 response (asset, amount, chain, recipient, settlement, facilitator)
PaymentIntent       — created BEFORE authorization (PENDING→APPROVED→DENIED→PAID→FAILED)
PaymentReceipt      — returned by facilitator after settlement
PaymentAuthorization — signed auth sent to facilitator (intent_id, wallet, signature, payload)
PaymentError        — structured error (PAYMENT_DENIED, PAYMENT_REQUIRES_APPROVAL, etc.)
```

### 3.3 Spending Policy Engine (`src/payments/policy/`)

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

### 3.4 Wallet Abstraction (`src/payments/wallet/`)

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

### 3.5 Secret Store (`src/payments/secretstore.go` + `providers/`)

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

### 3.6 PayableWebFetch Runtime (`src/payments/pwf/`)

**PWF is a core economic-aware fetch primitive, NOT just a tool wrapper.**

Full flow:
```
1. HTTP request
2. Receive 402 Payment Required
3. Parse x402 metadata (X402-Payment header)
4. Create PaymentIntent (status=PENDING)
5. Evaluate spending policy → deny or proceed
6. If above threshold → request human approval (reuses existing HumanApproval store)
7. Wallet signs PaymentAuthorization
8. Send authorization to facilitator → receive PaymentReceipt
9. Record spend for daily budget tracking
10. Retry original request with X402-Authorization token
```

### 3.7 Facilitator Client (`src/payments/facilitator/`)

- POST `/authorize` with signed `PaymentAuthorization`
- Returns `PaymentReceipt` on success
- Health check: GET `/health`
- Flowgent sends signed auth. Facilitator handles onchain settlement.

### 3.8 Payment Approvals (`src/payments/approvals/`)

- Implements `pwf.ApprovalHandler`
- Reuses existing `model.HumanApproval` store (NO duplicate subsystem)
- Polls for approval resolution (production should use event-driven API callbacks)
- Timeout → `APPROVAL_TIMEOUT` error
- Rejection → `APPROVAL_REJECTED` error

### 3.9 Payment Receipts (`src/payments/receipts/`)

- DB table: `payment_receipts` (id, intent_id, tx_hash, asset, amount, chain, facilitator, authorization, paid_at, expires_at)
- Query by intent ID or date range
- Index on `intent_id`

### 3.10 Wallet Daemon (`src/cmd/core/wallet.go`)

Standalone daemon, integrated as `flowgent wallet` subcommand:
```
flowgent wallet start|stop|restart   # Daemon lifecycle
flowgent wallet generate-key         # Ed25519 keypair generation
  --format text|json|base64          # Output format
```

REST API on `:9901`:
- `GET  /health`
- `POST /api/v1/wallet/sign`   (body: {wallet, payload})
- `GET  /api/v1/wallet/address` (query: ?wallet=...)
- `GET  /api/v1/wallet/balance`

### 3.11 Config Integration

```yaml
payments:
  enabled: true
  policies:
    max_single_payment_usd: 1
    max_daily_budget_usd: 50
    allowed_domains: ["*.googleapis.com", "*.openai.com"]
    blocked_domains: ["*.unknown.xyz"]
    require_human_approval_above_usd: 5
    allowed_assets: [USDC]
    allowed_chains: [base, solana]
  wallet:
    endpoint: "http://localhost:9901"
    default_wallet: "default"
    secret_store:
      provider: default          # default | vault
      master_key: ""
      master_key_file: /var/secrets/master.key
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
Keys are encrypted at rest (AES-256-GCM). Wallet daemon is the ONLY process with key access. Orchestration runtime calls wallet via REST API.

### 5.2 PaymentIntent before payment
DO NOT pay immediately on 402. Create PaymentIntent → evaluate policy → optionally seek approval → then authorize. This enables audit trail and policy enforcement.

### 5.3 Human approval reuses existing subsystem
`src/payments/approvals/` uses `model.HumanApproval` and the engine's `Store.CreateHumanApproval` / `GetHumanApproval` / `UpdateHumanApproval`. No second approval system.

### 5.4 Spending policy evaluated client-side
Flowgent evaluates policies BEFORE sending authorization. Facilitator never sees policy rules. This keeps Flowgent as the policy decision point.

### 5.5 Economic layer is optional
`payments.enabled: false` → entire `src/payments/` tree is never invoked. No import side effects. No mandatory dependencies beyond `shopspring/decimal`.

---

## 6. Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/shopspring/decimal` | Fixed-point decimal for amounts |
| `github.com/spf13/cobra` | CLI (wallet subcommand) |
| `github.com/mattn/go-sqlite3` | Wallet DB (secret store) |
| `crypto/aes`, `crypto/cipher` | AES-256-GCM (stdlib) |
| `crypto/ed25519` | Key generation + signing (stdlib) |

No external SDKs required for Phase 1. Vault SDK wiring is deferred.

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

1. **Vault SDK wiring** — `VaultSecretStoreProvider` has the interface contract but is not connected to the Hashicorp Vault Go SDK. Wire `vault.NewClient()` at construction time.
2. **Unit tests for payments packages** — x402, policy, pwf, wallet, facilitator need dedicated tests.
3. **PWF integration into engine** — PWF is a standalone runtime. Integrating it as an optional `tool` node type variant (payment-aware fetch) would make it accessible from agentflow YAML.
4. **Approval event-driven mode** — Current approval polls; production needs webhook/callback from the external API.
5. **PG spending store** — `SpendingStore` uses in-memory default. Postgres-backed store needed for distributed mode.

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

- **Entry point:** `src/cmd/core/main.go` (`flowgent wallet` subcommand)
- **Config struct:** `src/payments/config.go` → `PaymentsConfig`
- **Core flow:** `src/payments/pwf/pwf.go` → `Runtime.Fetch()`
- **Policy check:** `src/payments/policy/policy.go` → `Engine.Allow()`
- **Wallet signing:** `src/payments/wallet/wallet.go` → `Manager.SignPaymentAuthorization()`
- **Facilitator call:** `src/payments/facilitator/facilitator.go` → `Client.Authorize()`
- **Approval gate:** `src/payments/approvals/approvals.go` → `PaymentApprover.RequestApproval()`
- **Secret encryption:** `src/payments/providers/default.go` → `encrypt()` / `decrypt()`
