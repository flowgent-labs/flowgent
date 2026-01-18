# Developer Quickstart — Middleware Images

How to build and push the x402 Facilitator and Solana Test Validator Docker images for local development.

**Date:** 2026-05-10
**Convention:** `registry.cn-shenzhen.aliyuncs.com/wl4g/<name>_<name>:<version>` (2-level, underscores, fixed version)

---

## 1. x402 Facilitator

**Source:** <https://github.com/qntx/facilitator>
**Image:** `registry.cn-shenzhen.aliyuncs.com/wl4g/qntx_facilitator:0.13.0`

### Build

```bash
# Option A — Pull pre-built image from GHCR (recommended)
docker pull ghcr.io/qntx/facilitator:latest
docker tag ghcr.io/qntx/facilitator:latest registry.cn-shenzhen.aliyuncs.com/wl4g/qntx_facilitator:0.13.0
docker push registry.cn-shenzhen.aliyuncs.com/wl4g/qntx_facilitator:0.13.0

# Option B — Build from source (requires Rust toolchain, ~10 min)
git clone https://github.com/qntx/facilitator.git
cd facilitator
docker build -t qntx_facilitator:0.13.0 .
docker tag qntx_facilitator:0.13.0 registry.cn-shenzhen.aliyuncs.com/wl4g/qntx_facilitator:0.13.0
docker push registry.cn-shenzhen.aliyuncs.com/wl4g/qntx_facilitator:0.13.0
```

### Verify

```bash
docker run -d --name facilitator-dev -p 8085:8080 \
    -v $(pwd)/deploy/facilitator/config.toml:/app/config.toml:ro \
    registry.cn-shenzhen.aliyuncs.com/wl4g/qntx_facilitator:0.13.0

curl --noproxy '*' http://localhost:8085/health
# {"status":"ok"}
```

### Configuration

See `deploy/facilitator/config.toml`:
- EVM chains configured via `[chains."eip155:<id>"]`
- Solana chains configured via `[chains."solana:<hash>"]`
- Signer keys via `[signers]` (use env var references for production)
- Schemes auto-generated from chains

### Key Endpoints

| Endpoint | Method | Purpose |
|---|---|---|
| `/health` | GET | Liveness check |
| `/supported` | GET | List payment networks + signers |
| `/verify` | POST | Verify payment (body: `{scheme, network, recipient, amount, asset}`) |
| `/settle` | POST | Settle verified payment (body: `{network, signature}`) |

---

## 2. Solana Test Validator

**Source:** <https://github.com/anza-xyz/agave> (release binaries)
**Image:** `registry.cn-shenzhen.aliyuncs.com/wl4g/anza_solana:3.1.14`

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

# Health check (JSON-RPC)
curl -X POST -H 'Content-Type: application/json' \
    -d '{"jsonrpc":"2.0","id":1,"method":"getHealth"}' \
    http://localhost:8899
# {"jsonrpc":"2.0","result":"ok","id":1}
```

### Architecture Note

The Solana test validator runs a local single-node Solana cluster for testing:
- RPC on port 8899, Faucet on port 8900
- No persistent state (ledger in `/tmp`)
- Alternative: use public devnet `https://api.devnet.solana.com`

---

## 3. Full Dev Stack

```bash
# Start Solana (optional — use public devnet if not needed)
docker compose -f deploy/solana/docker-compose.yml up -d

# Start facilitator
docker compose -f deploy/facilitator/docker-compose.yml up -d

# Verify both
curl --noproxy '*' http://localhost:8085/health
curl -X POST -H 'Content-Type: application/json' \
    -d '{"jsonrpc":"2.0","id":1,"method":"getHealth"}' http://localhost:8899
```

---

## 4. Facilitator Config for Solana + EVM

Example `deploy/facilitator/config.toml` with both chains:

```toml
host = "0.0.0.0"
port = 8080
log_level = "debug"

[signers]
evm = ["$EVM_SIGNER_PRIVATE_KEY"]
solana = "$SOLANA_SIGNER_PRIVATE_KEY"

# EVM — Base Sepolia testnet
[chains."eip155:84532"]
rpc = [{ http = "https://sepolia.base.org" }]

# Solana — Devnet
[chains."solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1"]
rpc = "https://api.devnet.solana.com"

# Solana — Local test validator (if running)
# [chains."solana:<local_genesis_hash>"]
# rpc = "http://localhost:8899"
```

---

## 5. Registry Reference

All images follow: `registry.cn-shenzhen.aliyuncs.com/<namespace>/<underscore_name>:<version>`

| Service | Image |
|---|---|
| x402 Facilitator | `registry.cn-shenzhen.aliyuncs.com/wl4g/qntx_facilitator:0.13.0` |
| Solana Validator | `registry.cn-shenzhen.aliyuncs.com/wl4g/anza_solana:3.1.14` |
| EMQX MQTT | `registry.cn-shenzhen.aliyuncs.com/wl4g/emqx_emqx:5.5.0-elixir-amd64` |
| PostgreSQL | `registry.cn-shenzhen.aliyuncs.com/wl4g/bitnami_postgresql:18.3` |
| Redis Cluster | `registry.cn-shenzhen.aliyuncs.com/wl4g-k8s/bitnami_redis-cluster:7.0.14` |

---

## 6. Troubleshooting

### Docker Hub blocked
This environment cannot reach `docker.io`. Use Alibaba Cloud (`registry.cn-shenzhen.aliyuncs.com`) or GHCR (`ghcr.io`).

### Facilitator "no handler registered" error
Ensure `[[schemes]]` section is present or chains are auto-generated. Verify the network/scheme in verify requests matches `/supported` output.

### Solana image too large
The `rust:1.93-slim` base image is ~800MB. For production, use a multi-stage build with a minimal runtime. For local dev, size is acceptable.

### Push denied (unauthorized)
Registry credentials required. Login with `docker login registry.cn-shenzhen.aliyuncs.com` before push.
