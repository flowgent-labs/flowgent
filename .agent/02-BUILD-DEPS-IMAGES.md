# Build Dependency Images — Facilitator, EVM (Anvil), Solana

How to build and push Docker images for the three middleware dependencies
required for local x402 payment development and testing.

**Date:** 2026-05-10
**Convention:** `registry.cn-shenzhen.aliyuncs.com/wl4g/<name>_<name>:<version>` (2-level, underscores, fixed version)

---

## 1. x402 Facilitator

**Source:** <https://github.com/qntx/facilitator>
**Image:** `registry.cn-shenzhen.aliyuncs.com/wl4g/qntx_facilitator:0.13.0`

Handles x402 payment verification and onchain settlement. Supports EVM and Solana chains.

### Build

```bash
# Option A — Pull from GHCR (recommended)
docker pull ghcr.io/qntx/facilitator:latest
docker tag ghcr.io/qntx/facilitator:latest registry.cn-shenzhen.aliyuncs.com/wl4g/qntx_facilitator:0.13.0
docker push registry.cn-shenzhen.aliyuncs.com/wl4g/qntx_facilitator:0.13.0

# Option B — Build from source (requires Rust, ~10 min)
git clone https://github.com/qntx/facilitator.git && cd facilitator
docker build -t qntx_facilitator:0.13.0 .
docker tag qntx_facilitator:0.13.0 registry.cn-shenzhen.aliyuncs.com/wl4g/qntx_facilitator:0.13.0
docker push registry.cn-shenzhen.aliyuncs.com/wl4g/qntx_facilitator:0.13.0
```

### Verify

```bash
docker run -d --name facilitator-dev -p 8085:8080 \
    -v $(pwd)/deploy/facilitator/config.toml:/app/config.toml:ro \
    registry.cn-shenzhen.aliyuncs.com/wl4g/qntx_facilitator:0.13.0

curl --noproxy '*' http://localhost:8085/health          # {"status":"ok"}
curl --noproxy '*' http://localhost:8085/supported       # lists chains + signers
```

### Endpoints

| Endpoint | Method | Purpose |
|---|---|---|
| `/health` | GET | Liveness check |
| `/supported` | GET | List payment networks and signers |
| `/verify` | POST | Verify payment (`{scheme, network, recipient, amount, asset}`) |
| `/settle` | POST | Settle verified payment on-chain (`{network, signature}`) |

### Configuration

`deploy/facilitator/config.toml` — chains and signers. Key sections:

```toml
[signers]
evm = ["0xPRIVATE_KEY"]          # hex, 0x-prefixed
solana = "$SOLANA_SIGNER_KEY"     # base58

# EVM testnet
[chains."eip155:84532"]
rpc = [{ http = "https://sepolia.base.org" }]

# EVM local (anvil)
[chains."eip155:31337"]
rpc = [{ http = "http://anvil-dev:8545" }]

# Solana devnet
[chains."solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1"]
rpc = "https://api.devnet.solana.com"
```

---

## 2. Anvil (Foundry) — Local EVM Node

**Source:** <https://github.com/foundry-rs/foundry>
**Image:** `registry.cn-shenzhen.aliyuncs.com/wl4g/foundry_anvil:1.7.1`

Anvil is the standard local Ethereum development node. Chosen over geth+lighthouse
and Avalanche L1 because:
- Single binary (~50MB) vs geth (multi-GB chain data)
- Instant startup vs minutes for geth/Avalanche sync
- Full `eth_*` JSON-RPC API, 10 pre-funded accounts (10000 ETH each)
- No consensus layer needed (single-node PoA)
- De facto standard for Ethereum dev testing

### Build

```bash
# Download Foundry release binary
curl -sSLo foundry.tar.gz \
  https://github.com/foundry-rs/foundry/releases/download/v1.7.1/foundry_v1.7.1_alpine_amd64.tar.gz

# Build image
cp foundry.tar.gz deploy/anvil/
docker build -t foundry_anvil:1.7.1 deploy/anvil/
docker tag foundry_anvil:1.7.1 registry.cn-shenzhen.aliyuncs.com/wl4g/foundry_anvil:1.7.1
docker push registry.cn-shenzhen.aliyuncs.com/wl4g/foundry_anvil:1.7.1
```

### Verify

```bash
docker run -d --name anvil-dev -p 8545:8545 \
    registry.cn-shenzhen.aliyuncs.com/wl4g/foundry_anvil:1.7.1

# Check block number
curl -X POST -H 'Content-Type: application/json' \
    -d '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}' \
    http://localhost:8545
# {"jsonrpc":"2.0","id":1,"result":"0x0"}

# Check test accounts
curl -X POST -H 'Content-Type: application/json' \
    -d '{"jsonrpc":"2.0","id":1,"method":"eth_accounts"}' \
    http://localhost:8545
```

### Default Accounts

Anvil creates 10 accounts with 10000 ETH each. Private keys are deterministic
(derived from mnemonic `test test test test test test test test test test test junk`).

---

## 3. Solana Test Validator

**Source:** <https://github.com/anza-xyz/agave> (release binaries)
**Image:** `registry.cn-shenzhen.aliyuncs.com/wl4g/anza_solana:3.1.14`

Local single-node Solana cluster for testing.

### Build

```bash
# Download agave release binary
curl -sSLo solana-release.tar.bz2 \
  https://github.com/anza-xyz/agave/releases/download/v3.1.14/solana-release-x86_64-unknown-linux-gnu.tar.bz2

# Build image
cp solana-release.tar.bz2 deploy/solana/
docker build -t anza_solana:3.1.14 deploy/solana/
docker tag anza_solana:3.1.14 registry.cn-shenzhen.aliyuncs.com/wl4g/anza_solana:3.1.14
docker push registry.cn-shenzhen.aliyuncs.com/wl4g/anza_solana:3.1.14
```

### Verify

```bash
docker run -d --name solana-dev -p 8899:8899 \
    registry.cn-shenzhen.aliyuncs.com/wl4g/anza_solana:3.1.14

curl -X POST -H 'Content-Type: application/json' \
    -d '{"jsonrpc":"2.0","id":1,"method":"getHealth"}' \
    http://localhost:8899
# {"jsonrpc":"2.0","result":"ok","id":1}
```

### Note

For projects that don't need a local Solana node, use the public devnet:
`https://api.devnet.solana.com`

---

## 4. Full Dev Stack

Start all three middleware services for local x402 development:

```bash
# Start EVM node (anvil)
docker compose -f deploy/anvil/docker-compose.yml up -d

# Start Solana validator
docker compose -f deploy/solana/docker-compose.yml up -d

# Start facilitator (depends on anvil + solana RPC endpoints)
docker compose -f deploy/facilitator/docker-compose.yml up -d
```

Verify all services:

```bash
# EVM
curl -X POST -H 'Content-Type: application/json' \
    -d '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}' \
    http://localhost:8545

# Solana
curl -X POST -H 'Content-Type: application/json' \
    -d '{"jsonrpc":"2.0","id":1,"method":"getHealth"}' \
    http://localhost:8899

# Facilitator
curl --noproxy '*' http://localhost:8085/health
```

### Facilitator Config for Local Dev

Create `deploy/facilitator/config.toml` pointing to local nodes:

```toml
host = "0.0.0.0"
port = 8080
log_level = "debug"

[signers]
# Use anvil's first test account private key
evm = ["0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"]
solana = ""

# Local anvil (chain-id 31337)
[chains."eip155:31337"]
rpc = [{ http = "http://anvil-dev:8545" }]

# Local solana
# [chains."solana:<local_genesis>"]
# rpc = "http://solana-dev:8899"
```

---

## 5. Registry Reference

All images follow: `registry.cn-shenzhen.aliyuncs.com/<namespace>/<underscore_name>:<version>`

| Service | Image | Ports |
|---|---|---|
| Facilitator | `wl4g/qntx_facilitator:0.13.0` | 8085→8080 |
| Anvil (EVM) | `wl4g/foundry_anvil:1.7.1` | 8545 |
| Solana | `wl4g/anza_solana:3.1.14` | 8899, 8900 |
| EMQX MQTT | `wl4g/emqx_emqx:5.5.0-elixir-amd64` | 1883, 18083 |
| PostgreSQL | `wl4g/bitnami_postgresql:18.3` | 5432 |
| Redis Cluster | `wl4g-k8s/bitnami_redis-cluster:7.0.14` | 6379 |

---

## 6. Troubleshooting

### Docker Hub blocked
This environment cannot reach `docker.io`. Use Alibaba Cloud (`registry.cn-shenzhen.aliyuncs.com`) or GHCR (`ghcr.io`). Build images locally with release binaries when needed.

### GitHub release download SSL errors
GitHub downloads may fail inside Docker builds. Workaround: download the binary on the host first, then `COPY` into the Docker build context.

### Facilitator "no handler registered"
Ensure `[[schemes]]` section is present (auto-generated) and the `scheme` field in verify requests matches `/supported` output (e.g. `"exact"` for EVM, or use scheme ID from config).

### Push denied (unauthorized)
Login first: `docker login registry.cn-shenzhen.aliyuncs.com`. Credentials are per-user; the repo contains only build scripts and configs.

### Why anvil over geth/lighthouse?
- **Geth+Lighthouse**: Requires syncing testnet chain data (hours, multi-GB), two containers, complex setup
- **Avalanche L1**: Full Avalanche node, heavier, more ops overhead
- **Anvil**: Single binary, instant start, zero chain data, full EVM API — purpose-built for local dev

### Facilitator "no handler registered" with custom chain
The facilitator's scheme registry may reject custom chain IDs (e.g. anvil's `eip155:31337`).
The chain provider is correctly connected, but scheme dispatch requires the chain to be
registered in the `r402::scheme::SchemeRegistry`. For a working fallback, use Base Sepolia
testnet (`eip155:84532`) which is pre-tested with the facilitator. See the commented
chain in `deploy/facilitator/config.toml`.

### Solana image size
The `rust:1.93-slim` base is ~800MB. Acceptable for dev; for production, use multi-stage build with a minimal runtime.
