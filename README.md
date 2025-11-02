# Security Auto-Fix Bot

> **100% Golang** | **MCP Tools** | **GitHub EE / GitLab Agnostic** | **Test Framework Agnostic**

通用的安全扫描自动修复机器人，通过 MCP Tools 集成不同扫描引擎、Git 平台和测试框架。

---

## 快速开始

### 1. 构建

```bash
# 构建 Bot 和 MCP Servers
make build

# 运行测试
make test
```

### 2. 配置 MCP Servers

在 `~/.config/mcp.json` 配置 MCP 服务器:

```json
{
  "mcpServers": {
    "scanner": {
      "command": "scanner-mcp",
      "config": "/etc/security-bot/scanner.yaml"
    },
    "parser": {
      "command": "parser-mcp",
      "config": "/etc/security-bot/parser.yaml"
    },
    "fixer": {
      "command": "fixer-mcp",
      "config": "/etc/security-bot/fixer.yaml"
    },
    "git": {
      "command": "github-mcp-server",
      "env": {
        "GITHUB_TOKEN": "${GITHUB_TOKEN}",
        "GITHUB_BASE_URL": "https://github.example.com"
      }
    },
    "test": {
      "command": "test-mcp-server",
      "env": {
        "TEST_WORKSPACE": "/workspace"
      }
    }
  }
}
```

### 3. 设置环境变量

```bash
# GCP Secret Manager (推荐)
export GOOGLE_APPLICATION_CREDENTIALS="/path/to/service-account.json"

# GitHub Token
export GITHUB_TOKEN="..."
export GITHUB_BASE_URL="https://github.example.com"
```

### 4. 运行

```bash
# 指定配置运行
./bin/security-bot --config /etc/security-bot/config.yaml
```

---

## 架构

```
┌─────────────────────────────────────────────────────────────────┐
│                   Security Auto-Fix Bot                          │
│                        (Go Binary)                               │
└─────────────────────────────────────────────────────────────────┘
         │
         │  所有操作通过 MCP Tools
         │
         ├──────────────────────────────────────────────────────┐
         │                                                      │
         ▼                                                      ▼
┌─────────────────────────┐                       ┌─────────────────────────┐
│   Built-in Tools        │                       │   MCP Tools Server      │
│   (Go 内置实现)          │                       │   (可插拔 Provider)      │
│                         │                       │                         │
│ - deploy_verifier       │                       │ [scanner]               │
│   (K8s/GKE 标准化)       │                       │ - scan/get_jobs_by_commit│
│                         │                       │ - report/download       │
│                         │                       │                         │
│                         │                       │ [parser]                │
│                         │                       │ - parse/sast_to_html    │
│                         │                       │ - parse/dast_to_html    │
│                         │                       │ - ...                   │
│                         │                       │                         │
│                         │                       │ [fixer]                 │
│                         │                       │ - fix/get_foss_solution │
│                         │                       │ - fix/search_web        │
│                         │                       │                         │
│                         │                       │ [git]                   │
│                         │                       │ - git/get_latest_commit │
│                         │                       │ - git/create_branch     │
│                         │                       │ - git/commit_and_push   │
│                         │                       │ - git/create_pull_request│
│                         │                       │ - git/merge_pull_request│
│                         │                       │                         │
│                         │                       │ [test] ← 新增           │
│                         │                       │ - test/run_integration  │
│                         │                       │ - test/get_report       │
│                         │                       │                         │
│                         │                       │ Provider 可切换：        │
│                         │                       │ - github/gitlab         │
│                         │                       │ - maven/gradle/npm/pytest│
└─────────────────────────┘                       └─────────────────────────┘
```

### 完整 Tools 清单

| 类别 | MCP Tool | 职责 | 实现 |
|------|----------|------|------|
| **扫描发现** | `scan/get_jobs_by_commit` | 根据 commit 获取扫描作业列表 | MCP |
| **扫描发现** | `scan/get_status` | 获取扫描作业状态 | MCP |
| **报告下载** | `report/download` | 下载报告/PDF 到本地 | MCP |
| **报告解析** | `parse/sast_to_html` | SAST 报告转 HTML 并提取漏洞 | MCP |
| **报告解析** | `parse/dast_to_html` | DAST 报告转 HTML 并提取漏洞 | MCP |
| **报告解析** | `parse/cont_to_html` | 容器扫描报告转 HTML | MCP |
| **报告解析** | `parse/sonar_to_html` | SonarQube 报告转 HTML | MCP |
| **报告解析** | `parse/foss_to_html` | FOSS 报告转 HTML | MCP |
| **修复方案** | `fix/get_foss_solution` | 获取 FOSS 漏洞修复方案 | MCP |
| **修复方案** | `fix/search_web` | 搜索漏洞修复方案 | MCP |
| **Git 操作** | `git/get_latest_commit` | 获取分支最新 commit | MCP |
| **Git 操作** | `git/create_branch` | 创建新分支 | MCP |
| **Git 操作** | `git/commit_and_push` | 提交代码并推送 | MCP |
| **Git 操作** | `git/create_pull_request` | 创建 PR | MCP |
| **Git 操作** | `git/merge_pull_request` | 合并 PR | MCP |
| **测试执行** | `test/run_integration` | 运行集成测试 | MCP |
| **测试执行** | `test/get_report` | 获取测试报告详情 | MCP |
| **部署验证** | `deploy/wait_and_verify` | 等待部署并验证 | 内置 |

---

## 完整工作流程

```
┌─────────────────────────────────────────────────────────────────┐
│ 1. 扫描发现                                                      │
│    git/get_latest_commit → scan/get_jobs_by_commit               │
└─────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────┐
│ 2. 报告获取                                                      │
│    report/download (for each failed job)                        │
└─────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────┐
│ 3. 漏洞解析                                                      │
│    parse/{type}_to_html → []Vulnerability                       │
└─────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────┐
│ 4. 修复方案生成                                                  │
│    FOSS: fix/get_foss_solution                                  │
│    Code: fix/search_web                                         │
└─────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────┐
│ 5. 代码修复                                                      │
│    git/create_branch → git/commit_and_push                      │
└─────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────┐
│ 6. 创建 PR                                                       │
│    git/create_pull_request                                      │
└─────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────┐
│ 7. 验证阶段                                                      │
│    等待 CI 部署 → deploy/wait_and_verify (内置)                  │
│    运行测试 → test/run_integration (MCP)                        │
└─────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
                    ┌─────────────────────────┐
                    │ git/merge_pull_request  │
                    └─────────────────────────┘
```

---

## MCP Servers

### 当前实现

| Server | 描述 | 状态 |
|--------|------|------|
| `github-mcp-server` | GitHub EE Provider | ✅ 已实现 |
| `test-mcp-server` | 测试执行 (Maven/Cucumber) | ✅ 已实现 |
| `scanner-mcp` | 扫描系统集成 (示例) | 📝 模板 |
| `parser-mcp` | 报告解析器 (示例) | 📝 模板 |
| `fixer-mcp` | 修复方案集成 (示例) | 📝 模板 |

### 未来扩展

| Server | 描述 |
|--------|------|
| `gitlab-mcp-server` | GitLab Provider |
| `bitbucket-mcp-server` | Bitbucket Provider |
| `gradle-test-mcp` | Gradle 测试执行 |
| `npm-test-mcp` | npm test 执行 |
| `pytest-mcp` | pytest 测试执行 |
| `sonarqube-mcp-server` | SonarQube 集成 |
| `fortify-mcp-server` | Fortify SAST 集成 |
| `zap-mcp-server` | OWASP ZAP 集成 |

---

## 配置说明

### MCP 配置

```yaml
mcp:
  # Git 操作服务器 (当前：GitHub EE, 未来可切换到 GitLab)
  git_server: git
  
  # 测试执行服务器 (当前：Maven/Cucumber, 未来可切换到 Gradle/npm/pytest)
  test_server: test
  
  # 扫描集成服务器
  scanner_server: scanner
  
  # 报告解析服务器
  parser_server: parser
  
  # 修复方案服务器
  fixer_server: fixer
```

### 切换 Git Provider

只需修改配置即可切换 Git 平台：

```yaml
# GitHub EE
mcp:
  git_server: git  # 对应 github-mcp-server

# 未来切换到 GitLab
mcp:
  git_server: gitlab  # 对应 gitlab-mcp-server
```

### 切换测试框架

只需修改配置即可切换测试框架：

```yaml
# Maven/Cucumber (Java)
mcp:
  test_server: test  # 对应 test-mcp-server (Maven)

# 未来切换到 Gradle
mcp:
  test_server: gradle-test  # 对应 gradle-test-mcp

# 未来切换到 npm (Node.js)
mcp:
  test_server: npm-test  # 对应 npm-test-mcp

# 未来切换到 pytest (Python)
mcp:
  test_server: pytest  # 对应 pytest-mcp
```

---

## 目录结构

```
security-bot/
├── cmd/
│   ├── security-bot/           # Bot 主程序
│   │   └── main.go
│   ├── mcp-server-github/      # GitHub MCP Server
│   │   └── main.go
│   └── mcp-server-test/        # Test MCP Server
│       └── main.go
├── internal/
│   ├── bot/
│   │   └── bot.go              # Bot 协调器 (全 MCP 调用)
│   ├── config/
│   │   └── config.go           # 配置加载
│   └── tools/
│       ├── scan_client.go      # 扫描 MCP 客户端
│       ├── git_client.go       # Git MCP 客户端
│       ├── test_client.go      # 测试 MCP 客户端 (新增)
│       └── gke.go              # 部署验证 (内置)
├── configs/
│   └── config.yaml.example     # 配置示例
├── go.mod
├── Makefile
└── README.md
```

---

## 设计决策

### 为什么测试执行也设计为 MCP？

| 考虑 | 内置实现 | MCP 实现 |
|------|----------|----------|
| 多测试框架支持 | ❌ 需修改代码 | ✅ 配置切换 |
| 公司差异 | ❌ 硬编码特定框架 | ✅ Provider 适配 |
| 未来扩展 | ❌ 修改 Bot 核心 | ✅ 新增 Provider |
| 当前复杂度 | ✅ 低 | 中 |

**决策**: 选择 MCP 实现，因为不同公司集成测试框架差异大 (Maven/Gradle/npm/pytest)。

### 什么保留为内置？

- **deploy_verifier**: K8s/GKE 部署验证是标准化的，各公司差异小

---

## Testing & Optional Services

为了验证 CyberBot 的端到端 (E2E) 漏洞修复能力，你可能需要部署本地的 SAST 和 FOSS 扫描环境。
我们提供了详细的系统设计文档以及部署指南：

- **核心架构文档**: [docs/01-core-engine-design.md](docs/01-core-engine-design.md)
- **应用层与扫描部署**: [docs/02-application-scanners.md](docs/02-application-scanners.md)

---

## License

Internal Use Only
