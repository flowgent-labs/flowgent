# 构建依赖镜像：Facilitator、EVM（Anvil）与 Solana

本文说明如何构建并推送本地 x402 支付开发和测试所需的三个中间件镜像。

**日期：** 2026-05-10  
**命名约定：** `registry.cn-shenzhen.aliyuncs.com/wl4g/<name>_<name>:<version>`，两级路径、下划线、固定版本

[English](build-dependency-images.md)

## 1. 官方 x402 Facilitator

**Fork：** <https://github.com/wl4g-blockchain/x402-rs>  
**Upstream：** <https://github.com/x402-rs/x402-rs>（v1.4.9）  
**Image：** `registry.cn-shenzhen.aliyuncs.com/wl4g/x402_facilitator:1.4.9`  
**协议角色：** resource server 调用 facilitator 完成验证与结算。Flowgent 将
`PAYMENT-SIGNATURE` 发送给 resource server，不直接调用 facilitator。

官方 facilitator 负责支付验证和链上结算，支持 EVM、Solana 与 Aptos 多链。

### 构建

```bash
git clone https://github.com/x402-rs/x402-rs.git deploy/docker/facilitator/x402-rs
docker build -t x402_facilitator:1.4.9 deploy/docker/facilitator/
docker tag x402_facilitator:1.4.9 registry.cn-shenzhen.aliyuncs.com/wl4g/x402_facilitator:1.4.9
docker push registry.cn-shenzhen.aliyuncs.com/wl4g/x402_facilitator:1.4.9
```

### 验证

```bash
docker run -d --name facilitator-dev -p 8085:8080 \
    -v $(pwd)/deploy/docker/facilitator/config.json:/app/config.json:ro \
    registry.cn-shenzhen.aliyuncs.com/wl4g/x402_facilitator:1.4.9

curl --noproxy '*' http://localhost:8085/health
curl --noproxy '*' http://localhost:8085/supported
```

| Endpoint | Method | 目的 |
|---|---|---|
| `/health` | GET | 存活检查 |
| `/supported` | GET | 列出支持的 scheme 与 network |
| `/verify` | POST | 验证 x402 支付请求 |
| `/settle` | POST | 将已验证支付上链结算 |

### 配置

`deploy/docker/facilitator/config.json` 使用 JSON 定义 chain 与 scheme：

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

相对旧社区 fork `qntx/facilitator`，当前配置是 JSON 而不是 TOML；scheme ID 带版本；signer 按 chain 配置；二进制名为 `x402-facilitator`。

## 2. Anvil：本地 EVM 节点

**Source：** <https://github.com/foundry-rs/foundry>  
**Image：** `registry.cn-shenzhen.aliyuncs.com/wl4g/foundry_anvil:1.7.1`

Anvil 是标准的本地 Ethereum 开发节点。选择它是因为单二进制约 50MB、即时启动、提供完整 `eth_*` JSON-RPC、默认 10 个各有 10000 ETH 的账户，且单节点不需要另设 consensus layer。

### 构建

```bash
curl -sSLo foundry.tar.gz \
  https://github.com/foundry-rs/foundry/releases/download/v1.7.1/foundry_v1.7.1_alpine_amd64.tar.gz

cp foundry.tar.gz deploy/docker/anvil/
docker build -t foundry_anvil:1.7.1 deploy/docker/anvil/
docker tag foundry_anvil:1.7.1 registry.cn-shenzhen.aliyuncs.com/wl4g/foundry_anvil:1.7.1
docker push registry.cn-shenzhen.aliyuncs.com/wl4g/foundry_anvil:1.7.1
```

### 验证

```bash
docker run -d --name anvil-dev -p 8545:8545 \
    registry.cn-shenzhen.aliyuncs.com/wl4g/foundry_anvil:1.7.1

curl -X POST -H 'Content-Type: application/json' \
    -d '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}' \
    http://localhost:8545

curl -X POST -H 'Content-Type: application/json' \
    -d '{"jsonrpc":"2.0","id":1,"method":"eth_accounts"}' \
    http://localhost:8545
```

10 个账户和私钥都由 mnemonic `test test test test test test test test test test test junk` 确定性生成，仅供开发。

## 3. Solana Test Validator

**Fork：** <https://github.com/wl4g-blockchain/solana>  
**Upstream：** <https://github.com/anza-xyz/agave>  
**Image：** `registry.cn-shenzhen.aliyuncs.com/wl4g/anza_solana:3.1.14`

该镜像提供本地单节点 Solana 测试集群。

### 构建

```bash
curl -sSLo solana-release.tar.bz2 \
  https://github.com/anza-xyz/agave/releases/download/v3.1.14/solana-release-x86_64-unknown-linux-gnu.tar.bz2

cp solana-release.tar.bz2 deploy/docker/solana/
docker build -t anza_solana:3.1.14 deploy/docker/solana/
docker tag anza_solana:3.1.14 registry.cn-shenzhen.aliyuncs.com/wl4g/anza_solana:3.1.14
docker push registry.cn-shenzhen.aliyuncs.com/wl4g/anza_solana:3.1.14
```

### 验证

```bash
docker run -d --name solana-dev -p 8899:8899 \
    registry.cn-shenzhen.aliyuncs.com/wl4g/anza_solana:3.1.14

curl -X POST -H 'Content-Type: application/json' \
    -d '{"jsonrpc":"2.0","id":1,"method":"getHealth"}' \
    http://localhost:8899
# {"jsonrpc":"2.0","result":"ok","id":1}
```

不需要本地节点的项目可直接使用 `https://api.devnet.solana.com`。

## 4. 完整本地开发栈

```bash
docker compose -f deploy/docker/anvil/docker-compose.yml up -d
docker compose -f deploy/docker/solana/docker-compose.yml up -d
docker compose -f deploy/docker/facilitator/docker-compose.yml up -d
```

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

本地 facilitator 的 `config.json`：

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

## 5. Registry 对照

镜像统一使用 `registry.cn-shenzhen.aliyuncs.com/<namespace>/<underscore_name>:<version>`：

| 服务 | 镜像 | 端口 |
|---|---|---|
| Facilitator | `wl4g/x402_facilitator:1.4.9` | 8085→8080 |
| Anvil | `wl4g/foundry_anvil:1.7.1` | 8545 |
| Solana | `wl4g/anza_solana:3.1.14` | 8899, 8900 |
| EMQX MQTT | `wl4g/emqx_emqx:5.5.0-elixir-amd64` | 1883, 18083 |
| PostgreSQL | `wl4g/bitnami_postgresql:18.3` | 5432 |
| Redis Cluster | `wl4g-k8s/bitnami_redis-cluster:7.0.14` | 6379 |

## 6. 排障

### Docker Hub 不可达

使用阿里云 `registry.cn-shenzhen.aliyuncs.com` 或 GHCR `ghcr.io`；必要时先在宿主机下载 release binary，再本地构建镜像。

### GitHub release 下载出现 SSL 错误

Docker build 内下载可能失败。先在宿主机下载 binary，再 `COPY` 到 build context。

### Push denied

先执行 `docker login registry.cn-shenzhen.aliyuncs.com`。凭据按用户管理，仓库只保存构建脚本和配置。

### 为什么选 Anvil

- Geth+Lighthouse 需要两个容器、同步数小时及多 GB 数据。
- Avalanche L1 更重、运维负担更高。
- Anvil 单二进制、即时启动、零链数据并提供完整 EVM API，适合本地开发。

### Facilitator 报 “no handler registered”

确认请求 scheme ID 与配置一致：EVM 使用 `v1-eip155-exact` 或 `v2-eip155-exact`；`/supported` 会列出全部已注册 scheme 与 network。

### Solana 镜像过大

`rust:1.93-slim` base 约 800MB，开发环境可接受；生产构建应使用 multi-stage 和最小 runtime。
