# Coinbase x402 Go SDK (reference)

**Fork:** https://github.com/wl4g-blockchain/coinbase-x402-sdk
**Upstream:** https://github.com/coinbase/coinbase-x402-sdk

This directory contains the reference x402 Go SDK. It is NOT compiled as part
of the Flowgent binary (heavy dependencies: go-ethereum, solana-go, gin).
Instead, it serves as documentation and type reference.

## Usage in Flowgent

Our `src/payments/facilitator/` package implements the same wire protocol
types (VerifyRequest, SettleRequest, VerifyResponse, SettleResponse)
aligned with both:
- The x402-rs facilitator server (Rust, wire-level API)
- This Coinbase x402 SDK (Go, type definitions)

When a future upgrade requires the full SDK (e.g., for EVM/Solana chain
clients), enable it by:
1. Adding the `replace` directive in go.mod (already configured)
2. Importing types from `github.com/x402-foundation/x402/go/types`

## Build Integration

The `replace` directive in root `go.mod` points here:
```
replace github.com/x402-foundation/x402/go => ./src/payments/x402sdk
```

This allows optional imports without pulling in transitive dependencies.
