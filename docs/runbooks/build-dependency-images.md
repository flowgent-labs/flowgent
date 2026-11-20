# Build Dependency Images — Facilitator, EVM (Anvil), Solana

How to build and push Docker images for the three middleware dependencies
required for local x402 payment development and testing.

**Date:** 2026-05-10
**Convention:** `registry.cn-shenzhen.aliyuncs.com/wl4g/<name>_<name>:<version>` (2-level, underscores, fixed version)

---

## 1. x402 Facilitator (Official)

**Fork:** <https://github.com/wl4g-blockchain/x402-rs>
**Upstream:** <https://github.com/x402-rs/x402-rs> (v1.4.9)
**Image:** `registry.cn-shenzhen.aliyuncs.com/wl4g/x402_facilitator:1.4.9`
**Protocol role:** resource servers call the facilitator for verification and
settlement. Flowgent sends `PAYMENT-SIGNATURE` to the resource server and does
not call the facilitator directly.

Official x402 protocol facilitator. Handles payment verification and onchain settlement
with multi-chain support (EVM, Solana, Aptos).

### Build

```bash
# Clone official repo and build via Dockerfile
git clone https://github.com/x402-rs/x402-rs.git deploy/docker/facilitator/x402-rs
docker build -t x402_facilitator:1.4.9 deploy/docker/facilitator/
docker tag x402_facilitator:1.4.9 registry.cn-shenzhen.aliyuncs.com/wl4g/x402_facilitator:1.4.9
docker push registry.cn-shenzhen.aliyuncs.com/wl4g/x402_facilitator:1.4.9
```

### Verify

```bash
docker run -d --name facilitator-dev -p 8085:8080 \
    -v $(pwd)/deploy/docker/facilitator/config.json:/app/config.json:ro \
    registry.cn-shenzhen.aliyuncs.com/wl4g/x402_facilitator:1.4.9

curl --noproxy '*' http://localhost:8085/health          # {"status":"ok"}
curl --noproxy '*' http://localhost:8085/supported       # lists chains + signers
```

### Endpoints

| Endpoint | Method | Purpose |
|---|---|---|
| `/health` | GET | Liveness check |
| `/supported` | GET | List supported schemes + networks |
| `/verify` | POST | Verify x402 payment request |
| `/settle` | POST | Settle verified payment on-chain |

### Configuration

`deploy/docker/facilitator/config.json` — JSON format with chains and schemes:

```json
{
  "port": 8080,
  "host": "0.0.0.0",
  "chains": {
    "eip155:31337": {
      "eip1559": false,
      "signers": ["0xPRIVATE_KEY"],
      "rpc": [{ "http": "http://anvil-dev:8545" }]
    }
  },
  "schemes": [
    { "id": "v1-eip155-exact", "chains": "eip155:31337" },
    { "id": "v2-eip155-exact", "chains": "eip155:31337" }
  ]
}
```

Key differences from the previous community fork (`qntx/facilitator`):
- Config is **JSON** (not TOML)
- Scheme IDs: `v1-eip155-exact`, `v2-eip155-exact` (versioned)
- Signers per-chain (not global)
- Binary: `x402-facilitator`

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
cp foundry.tar.gz deploy/docker/anvil/
docker build -t foundry_anvil:1.7.1 deploy/docker/anvil/
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

**Fork:** <https://github.com/wl4g-blockchain/solana>
**Upstream:** <https://github.com/anza-xyz/agave> (release binaries)
**Image:** `registry.cn-shenzhen.aliyuncs.com/wl4g/anza_solana:3.1.14`

Local single-node Solana cluster for testing.

### Build

```bash
# Download agave release binary
curl -sSLo solana-release.tar.bz2 \
  https://github.com/anza-xyz/agave/releases/download/v3.1.14/solana-release-x86_64-unknown-linux-gnu.tar.bz2

# Build image
cp solana-release.tar.bz2 deploy/docker/solana/
docker build -t anza_solana:3.1.14 deploy/docker/solana/
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
docker compose -f deploy/docker/anvil/docker-compose.yml up -d

# Start Solana validator
docker compose -f deploy/docker/solana/docker-compose.yml up -d

# Start facilitator (depends on anvil + solana RPC endpoints)
docker compose -f deploy/docker/facilitator/docker-compose.yml up -d
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

Create `deploy/docker/facilitator/config.json` pointing to local nodes:

```json
{
  "port": 8080,
  "host": "0.0.0.0",
  "chains": {
    "eip155:31337": {
      "eip1559": false,
      "signers": ["0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"],
      "rpc": [{ "http": "http://anvil-dev:8545" }]
    }
  },
  "schemes": [
    { "id": "v1-eip155-exact", "chains": "eip155:31337" },
    { "id": "v2-eip155-exact", "chains": "eip155:31337" }
  ]
}
```

---

## 5. Registry Reference

All images follow: `registry.cn-shenzhen.aliyuncs.com/<namespace>/<underscore_name>:<version>`

| Service | Image | Ports |
|---|---|---|
| Facilitator (official) | `wl4g/x402_facilitator:1.4.9` | 8085→8080 |
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

### Push denied (unauthorized)
Login first: `docker login registry.cn-shenzhen.aliyuncs.com`. Credentials are per-user; the repo contains only build scripts and configs.

### Why anvil over geth/lighthouse?
- **Geth+Lighthouse**: Requires syncing testnet chain data (hours, multi-GB), two containers, complex setup
- **Avalanche L1**: Full Avalanche node, heavier, more ops overhead
- **Anvil**: Single binary, instant start, zero chain data, full EVM API — purpose-built for local dev

### Facilitator "no handler registered"
Ensure the scheme IDs in verify requests match the configured schemes:
`v1-eip155-exact` or `v2-eip155-exact` for EVM chains.
The `/supported` endpoint lists all registered schemes and networks.

### Solana image size
The `rust:1.93-slim` base is ~800MB. Acceptable for dev; for production, use multi-stage build with a minimal runtime.
