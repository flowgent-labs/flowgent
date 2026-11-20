# AgentVault and x402 Boundary (L1)

[中文版本](wallet_ZH.md)

AgentVault is the independent Rust security and payment service pinned at
repository-root `agentvault/`. It owns payer/payee identities, key custody,
policy, approval, durable payment state, settlement, and its own AuthGuard
integration. Flowgent MUST NOT load, decrypt, log, or export AgentVault private
keys.

## Current Integration Status

The boundary is not yet fully integrated.

- AgentVault is an independent repository/submodule with its own API, SDKs,
  database, release, and Makefile.
- Flowgent's optional build-tagged x402 implementation still uses the older
  `wallet.sign.v1` digest-signing protocol over MQTT or a Unix socket. It also
  retains Flowgent-local payment policy and a process-local spending ledger.
- The legacy client does not use the current AgentVault SDK/API or durable
  AgentVault payment lifecycle. Therefore `make test-x402` proves the Flowgent
  client units and wire behavior only; it is not evidence of a production
  AgentVault integration.
- The default Flowgent build does not enable x402. It is compiled only with the
  `x402` build tag.

Documentation and new code MUST distinguish these facts. Do not call the
legacy signer AgentVault, and do not treat the old Flowgent policy ledger as
the authority for a real payment.

## Target Ownership

| Concern | AgentVault | Flowgent |
|---|---|---|
| Private keys, signing identities, key rotation | owns | MUST NOT access |
| Payer/payee identity and payment credentials | owns | references an external identity |
| Payment authorization policy and human approval | owns | supplies workflow context and waits for decision |
| Durable idempotency, execution record, settlement, reconciliation | owns | stores only the orchestration/result reference needed by the Run |
| HTTP 402 challenge detection and workflow routing | validates/executes payment through its API | detects the challenge and invokes the AgentVault adapter |
| Tool/Run state and business outcome | does not own | owns |

Approval does not equal execution success. A Flowgent payment node MUST verify
the AgentVault execution/settlement result and persist an idempotent side-effect
reference before unblocking downstream work.

## Legacy Client (Implemented but Transitional)

The current `pkg/core/pkg/client/signclient` path sends one bounded JSON
`SignRequest` and receives one `SignResponse`. The request version is
`wallet.sign.v1`; it carries a 32-byte precomputed EIP-712 digest, request ID,
client/key/purpose identity, and bounded validity window. A successful response
contains the EOA address and recoverable `r || s || v` signature. Flowgent
recovers the signer locally and fails closed on mismatch.

The transport is either:

```text
MQTT: wallet/v1/sign/requests/{client_id}
      wallet/v1/sign/responses/{client_id}
Unix: newline-delimited JSON over a mode-0600 socket
```

Only TaskManager constructs the current payment-capable HTTP client. API Server,
Controller, JobManager, Notifier, and administrative paths MUST NOT open the
legacy signing transport. `wallet.enabled=false` is the safe default.

This path remains useful as migration input, but new payment capabilities MUST
be implemented through an AgentVault adapter rather than expanding the legacy
protocol, local approval, or in-memory spending store.

## Required Migration

1. Define a Flowgent AgentVault adapter against a pinned AgentVault SDK/API and
   model the external payer/payee identity as a reference, not a key alias.
2. Replace Flowgent-local budget authorization and approval with AgentVault's
   policy/approval lifecycle. The unified `orh_approval` may gate the Flowgent
   workflow but MUST NOT impersonate AgentVault settlement authorization.
3. Bind each payment request to immutable tool, amount, asset, network,
   recipient, Run/NodeRun, request hash, expiry, and idempotency key.
4. Persist the AgentVault payment/execution identifier in the Flowgent external
   effect record and reconcile ambiguous network outcomes instead of retrying
   blindly.
5. Add a real integration/E2E scenario covering approval, duplicate callback,
   expiry, settlement, failure, and reconciliation. Retire the legacy signer
   only after that scenario is green.

The current `orh_effect_execution` schema is reserved for durable external
side-effect execution but is not yet wired through the Go runtime; completing
that path is part of this migration.

## Independent Lifecycle

AgentVault commands MUST go through its own Makefile; the Flowgent root Makefile
MUST NOT proxy them:

```bash
make -C agentvault help
make -C agentvault test
make -C agentvault lint
make -C agentvault build
```

Before an AgentVault Rust build, follow its repository rules and check whether
`agentvault/target/` exceeds 10 GiB; use its Makefile clean target when needed.
Flowgent changes use only the repository-root Makefile, including:

```bash
make test-x402
```

## Expected Behavior

1. **Given** x402 is disabled, **when** a tool returns HTTP 402, **then**
   Flowgent MUST NOT construct a signer or expose key configuration.
2. **Given** the transitional signer returns a signature, **when** the recovered
   address differs from the configured address, **then** Flowgent MUST reject it.
3. **Given** a new payment integration, **when** it needs custody, policy,
   approval, execution, or settlement, **then** those responsibilities MUST be
   delegated to AgentVault rather than reimplemented in Flowgent.
4. **Given** an ambiguous or repeated payment callback, **when** Flowgent
   handles it, **then** the same immutable request/idempotency key MUST NOT
   cause duplicate payment or duplicate downstream execution.
5. **Given** an AgentVault source change, **when** it is accepted, **then** its
   own Makefile tests MUST pass independently of the Flowgent root Makefile.
