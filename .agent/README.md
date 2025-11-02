# Security Auto-Fix Bot

> **100% Golang** | **MCP Tools** | **GitHub EE Integration**

通用的 GitHub EE 安全扫描自动修复机器人，通过 MCP Tools 集成不同扫描引擎。

---

## 背景

**问题**: 开发者提交 PR 后，各种安全扫描工具 (DAST/SAST/CONT/FOSS 等) 不断发现漏洞，需要自动修复。

**通用 CI/CD 流程**:
```
GitHub EE → CI 触发扫描 → 扫描结果回写 → Bot 检测 → 自动修复 → 验证 → 合并
```

**Bot 职责**:
1. 定期扫描配置的 repos → 获取 PR commits 触发的扫描作业
2. **MCP Tools 调用** → 下载报告 → 解析漏洞 → 获取修复方案
3. 创建独立修复 PR → 等待部署 → 运行测试 → 合并

---

## 架构设计

### MCP Tools 架构

```
┌─────────────────────────────────────────────────────────────────┐
│                      Security Auto-Fix Bot                       │
│                         (Go Binary)                              │
└─────────────────────────────────────────────────────────────────┘
         │
         │  MCP Tools 调用
         ├──────────────────────────────────────────────────────┐
         │                                                      │
         ▼                                                      ▼
┌─────────────────────────┐                       ┌─────────────────────────┐
│   Built-in Tools        │                       │   MCP Tools Server      │
│   (Go 内置实现)          │                       │   (扫描系统集成)         │
│                         │                       │                         │
│ - git_client          │                       │ - get_scan_jobs         │
│ - report_downloader   │                       │ - download_report       │
│ - pdf_parser          │                       │ - parse_sast_report     │
│ - test_runner         │                       │ - parse_dast_report     │
│ - gke_verifier        │                       │ - get_foss_fix          │
└─────────────────────────┘                       └─────────────────────────┘
```

### Tools 清单

| 类别 | Tool 名称 | 职责 | 实现方式 |
|------|----------|------|----------|
| **扫描发现** | `scan/get_jobs_by_commit` | 根据 commit 获取扫描作业列表 | MCP |
| **报告下载** | `report/download` | 下载报告/PDF 到本地 | MCP |
| **报告解析** | `parse/sast_to_html` | SAST 报告转 HTML | MCP |
| **报告解析** | `parse/dast_to_html` | DAST 报告转 HTML | MCP |
| **报告解析** | `parse/cont_to_html` | 容器扫描报告转 HTML | MCP |
| **报告解析** | `parse/sonar_to_html` | SonarQube 报告转 HTML | MCP |
| **修复方案** | `fix/get_foss_solution` | 获取 FOSS 漏洞修复方案 (SonatypeIQ) | MCP |
| **修复方案** | `fix/search_web` | 搜索漏洞修复方案 | MCP |
| **代码操作** | `git/commit_push` | 提交代码到 GitHub | 内置 |
| **测试执行** | `test/run_cucumber` | 运行 Cucumber 集成测试 | 内置 |
| **部署验证** | `deploy/wait_and_verify` | 等待部署并验证 | 内置 |

---

## 核心工作流程

```
┌─────────────────────────────────────────────────────────────────┐
│ 1. 扫描发现阶段                                                   │
│    遍历 repos → 获取 dev 分支最新 commit → MCP: get_scan_jobs     │
└─────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────┐
│ 2. 报告获取阶段                                                   │
│    过滤 FAILED 作业 → MCP: download_report → 保存 PDF             │
└─────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────┐
│ 3. 漏洞解析阶段                                                   │
│    MCP: parse/{sast|dast|cont|sonar}_to_html → 提取漏洞元数据     │
└─────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────┐
│ 4. 修复方案生成阶段                                               │
│    FOSS 漏洞：MCP: get_foss_solution                             │
│    其他漏洞：MCP: search_web                                     │
└─────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────┐
│ 5. 代码修复阶段                                                   │
│    生成修复代码 → git/commit_push → 创建独立 PR                   │
└─────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────┐
│ 6. 验证阶段                                                       │
│    等待 CI 部署 → deploy/wait_and_verify → test/run_cucumber     │
└─────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
                    ┌─────────────────────────┐
                    │ 测试通过 → 合并到 master  │
                    └─────────────────────────┘
```

---

## MCP Tools 详细设计

### 扫描发现 Tools

```go
// MCP Tool: scan/get_jobs_by_commit
// 输入：repo, commit_sha
// 输出：[]ScanJob
type ScanJob struct {
    JobID     string   `json:"job_id"`
    Type      string   `json:"type"`      // sast/dast/cont/foss/sonar
    Status    string   `json:"status"`    // success/failed/running
    ReportID  string   `json:"report_id"` // 报告 ID
    ScanType  string   `json:"scan_type"` // 扫描工具名称
}
```

### 报告下载 Tools

```go
// MCP Tool: report/download
// 输入：report_id, output_path
// 输出：{ path: string, format: string }
```

### 报告解析 Tools

```go
// MCP Tool: parse/sast_to_html
// 输入：report_path
// 输出：{ html_path, vulnerabilities: []Vulnerability }

type Vulnerability struct {
    ID       string `json:"id"`
    Type     string `json:"type"`      // SQLInjection/XSS/Dependency 等
    Severity string `json:"severity"`  // CRITICAL/HIGH/MEDIUM/LOW
    File     string `json:"file"`
    Line     int    `json:"line"`
    CVE      string `json:"cve"`
    CWE      string `json:"cwe"`
    Summary  string `json:"summary"`
}
```

### 修复方案 Tools

```go
// MCP Tool: fix/get_foss_solution
// 输入：component_id (SonatypeIQ ID)
// 输出：{ current_version, fixed_version, upgrade_path }

// MCP Tool: fix/search_web
// 输入：cve_id 或 vulnerability_description
// 输出：{ solutions: []string, references: []string }
```

---

## 内置 Tools 设计

### Git 客户端

```go
// 内置：git_client
type GitClient struct {
    github *github.Client
}

func (g *GitClient) GetLatestCommit(ctx, repo, branch) (string, error)
func (g *GitClient) CreateBranch(ctx, repo, branch, base) error
func (g *GitClient) CommitAndPush(ctx, repo, branch, changes) error
func (g *GitClient) CreatePullRequest(ctx, repo, title, body, head, base) (int, error)
func (g *GitClient) MergePR(ctx, repo, prNumber, target) error
```

### 测试执行器

```go
// 内置：test_runner
type TestRunner struct {
    workspace string
}

func (t *TestRunner) RunCucumber(ctx, profile) (*TestResult, error)
```

### 部署验证器

```go
// 内置：deploy_verifier
type DeployVerifier struct {
    kubectl *Kubectl
}

func (d *DeployVerifier) WaitAndVerify(ctx, service, namespace) (*DeployStatus, error)
```

---

## 配置示例

```yaml
# /etc/security-bot/config.yaml

repos:
  - full_name: "org/service-a"
    dev_branch: "dev"
    service_name: "service-a"
    scan_types:
      - sast
      - dast
      - foss

  - full_name: "org/service-b"
    dev_branch: "develop"
    service_name: "service-b"
    scan_types:
      - cont
      - sonar

# MCP Tools 配置
mcp:
  servers:
    # 扫描集成服务器
    scanner:
      command: "security-scanner-mcp"
      config: "/etc/security-bot/scanner.yaml"
    
    # 报告解析服务器
    parser:
      command: "report-parser-mcp"
      config: "/etc/security-bot/parser.yaml"
    
    # 修复方案服务器
    fixer:
      command: "fix-solution-mcp"
      config: "/etc/security-bot/fixer.yaml"

# 部署验证配置
deploy:
  cluster: "dev-cluster"
  zone: "us-central1-c"
  project_id: "my-project"
  namespace: "dev"

# 测试配置
test:
  workspace: "/workspace"
  cucumber_profile: "integration"

# 调度配置
scheduling:
  scan_interval_hours: 1
```

---

## 初始化示例

```go
func main() {
    ctx := context.Background()
    
    // 1. 连接 MCP Tools
    scannerMCP, _ := mcp.Connect("scanner")
    parserMCP, _ := mcp.Connect("parser")
    fixerMCP, _ := mcp.Connect("fixer")
    
    // 2. 初始化内置 Tools
    git := NewGitClient(token, baseURL)
    test := NewTestRunner("/workspace")
    deploy := NewDeployVerifier(config)
    
    // 3. 执行流程
    for _, repo := range config.Repos {
        // 获取扫描作业
        jobs, _ := scannerMCP.Call(ctx, "scan/get_jobs_by_commit", map[string]any{
            "repo": repo.FullName,
            "commit_sha": git.GetLatestCommit(ctx, repo.FullName, repo.DevBranch),
        })
        
        // 下载报告
        for _, job := range jobs {
            if job.Status == "FAILED" {
                reportPath, _ := scannerMCP.Call(ctx, "report/download", map[string]any{
                    "report_id": job.ReportID,
                    "output_path": fmt.Sprintf("/tmp/%s.pdf", job.JobID),
                })
                
                // 解析报告
                var vulns []Vulnerability
                switch job.Type {
                case "sast":
                    result, _ := parserMCP.Call(ctx, "parse/sast_to_html", map[string]any{
                        "report_path": reportPath,
                    })
                    vulns = result.Vulnerabilities
                case "dast":
                    result, _ := parserMCP.Call(ctx, "parse/dast_to_html", map[string]any{
                        "report_path": reportPath,
                    })
                    vulns = result.Vulnerabilities
                }
                
                // 获取修复方案
                for _, v := range vulns {
                    if v.Type == "Dependency" {
                        fix, _ := fixerMCP.Call(ctx, "fix/get_foss_solution", map[string]any{
                            "component_id": v.ID,
                        })
                        // 生成 pom.xml 修改
                    } else {
                        solutions, _ := fixerMCP.Call(ctx, "fix/search_web", map[string]any{
                            "cve_id": v.CVE,
                        })
                        // 生成代码修复
                    }
                }
            }
        }
    }
}
```

---

## 扩展 MCP Tools

### 添加新的扫描集成

```go
// 示例：添加 Fortify SAST 集成
// mcp-server-fortify/main.go

func main() {
    mcp := server.NewServer("fortify-mcp")
    
    mcp.RegisterTool("scan/get_jobs_by_commit", fortifyListJobs)
    mcp.RegisterTool("report/download", fortifyDownload)
    mcp.RegisterTool("parse/sast_to_html", fortifyParse)
    
    mcp.ServeStdio()
}
```

### 添加新的报告解析器

```go
// 示例：添加 Checkmarx 解析器
// mcp-server-checkmarx/main.go

func parseCheckmarxReport(ctx, params) (interface{}, error) {
    // 解析 Checkmarx XML 报告
    // 提取漏洞信息
    // 返回标准 Vulnerability 格式
}
```

---

## 安全合规

- ✅ 所有凭证从 Secret Manager 获取
- ✅ 使用 kubectl 直接调用 (非 bash)
- ✅ 完整审计日志
- ✅ 独立修复 PR (不与业务 PR 混合)
- ✅ 测试通过后才合并

---

## License

Internal Use Only
