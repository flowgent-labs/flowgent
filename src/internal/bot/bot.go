package bot

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/cyberbot/cve-auto-fix/src/internal/config"
	"github.com/cyberbot/cve-auto-fix/src/internal/tools"
	"github.com/mark3labs/mcp-go/client"
)

// Bot 安全自动修复 Bot
type Bot struct {
	config *config.Config

	// MCP Clients
	scanClient *tools.ScanClient // 扫描相关 MCP Tools
	gitClient  *tools.GitClient  // Git 操作 MCP Tools
	testClient *tools.TestClient // 测试执行 MCP Tools

	// Built-in Tools
	deploy *tools.GKETool
}

// New 创建 Bot 实例
func New(ctx context.Context, cfg *config.Config) (*Bot, error) {
	b := &Bot{config: cfg}

	// 1. 初始化扫描相关 MCP Clients (使用 Stdio 传输)
	var scannerMCP, parserMCP, fixerMCP *client.Client
	var err error

	// 扫描集成服务器 (期望格式：command:arg1:arg2 或纯 command)
	if cfg.MCP.ScannerServer != "" {
		scannerMCP, err = createStdioClient(cfg.MCP.ScannerServer)
		if err != nil {
			log.Printf("Warning: Failed to create scanner MCP client: %v", err)
		}
	}

	// 报告解析服务器
	if cfg.MCP.ParserServer != "" {
		parserMCP, err = createStdioClient(cfg.MCP.ParserServer)
		if err != nil {
			log.Printf("Warning: Failed to create parser MCP client: %v", err)
		}
	}

	// 修复方案服务器
	if cfg.MCP.FixerServer != "" {
		fixerMCP, err = createStdioClient(cfg.MCP.FixerServer)
		if err != nil {
			log.Printf("Warning: Failed to create fixer MCP client: %v", err)
		}
	}

	// 2. 创建扫描客户端
	b.scanClient = tools.NewScanClient(scannerMCP, parserMCP, fixerMCP)

	// 3. 初始化 Git MCP Client
	var gitMCP *client.Client
	if cfg.MCP.GitServer != "" {
		gitMCP, err = createStdioClient(cfg.MCP.GitServer)
		if err != nil {
			return nil, fmt.Errorf("create git MCP client: %w", err)
		}
	}
	b.gitClient = tools.NewGitClient(gitMCP)

	// 4. 初始化测试 MCP Client
	var testMCP *client.Client
	if cfg.MCP.TestServer != "" {
		testMCP, err = createStdioClient(cfg.MCP.TestServer)
		if err != nil {
			log.Printf("Warning: Failed to create test MCP client: %v", err)
		}
	}
	b.testClient = tools.NewTestClient(testMCP)

	// 5. 初始化内置 Tools (转换 config 类型为 tools 类型)
	b.deploy = tools.NewGKETool(tools.GKEConfig{
		Cluster:   cfg.GKE.Cluster,
		Zone:      cfg.GKE.Zone,
		ProjectID: cfg.GKE.ProjectID,
		Namespace: cfg.GKE.Namespace,
	})

	return b, nil
}

// createStdioClient 从配置字符串创建 Stdio MCP 客户端
// 配置格式：command 或 command:arg1:arg2
func createStdioClient(serverConfig string) (*client.Client, error) {
	parts := strings.Split(serverConfig, ":")
	command := parts[0]
	args := []string{}
	if len(parts) > 1 {
		args = parts[1:]
	}

	env := os.Environ()
	c, err := client.NewStdioMCPClient(command, env, args...)
	return c, err
}

// Run 执行一次完整扫描修复流程
func (b *Bot) Run(ctx context.Context) error {
	log.Println("Starting security scan and fix cycle")

	var totalFound, totalFixed int

	// 遍历所有配置的 repos
	for _, repoCfg := range b.config.Repos {
		found, fixed, err := b.scanRepo(ctx, repoCfg)
		if err != nil {
			log.Printf("Scan repo %s failed: %v", repoCfg.FullName, err)
			continue
		}
		totalFound += found
		totalFixed += fixed
	}

	log.Printf("Scan complete: found %d vulnerabilities, fixed %d", totalFound, totalFixed)
	return nil
}

// scanRepo 扫描单个 repo
func (b *Bot) scanRepo(ctx context.Context, repoCfg config.RepoConfig) (int, int, error) {
	log.Printf("Scanning repo: %s, branch: %s", repoCfg.FullName, repoCfg.DevBranch)

	// 1. 通过 MCP 获取 dev 分支最新 commit SHA
	commitSHA, err := b.gitClient.GetLatestCommit(ctx, repoCfg.FullName, repoCfg.DevBranch)
	if err != nil {
		return 0, 0, fmt.Errorf("get latest commit: %w", err)
	}
	log.Printf("Latest commit: %s", commitSHA)

	// 2. 通过 MCP 获取扫描作业列表
	jobs, err := b.scanClient.GetJobsByCommit(ctx, repoCfg.FullName, commitSHA)
	if err != nil {
		return 0, 0, fmt.Errorf("list scans: %w", err)
	}
	log.Printf("Found %d scan jobs", len(jobs))

	// 3. 获取失败的扫描
	failedJobs := b.scanClient.GetFailed(jobs)
	if len(failedJobs) == 0 {
		log.Printf("No failed scans for %s", repoCfg.FullName)
		return 0, 0, nil
	}
	log.Printf("Found %d failed scans", len(failedJobs))

	// 4. 下载报告并解析漏洞
	var allVulns []tools.Vulnerability
	for _, job := range failedJobs {
		// 下载报告
		reportPath, err := b.scanClient.DownloadReport(ctx, job.ReportID, fmt.Sprintf("/tmp/%s_%s.pdf", job.Type, job.JobID))
		if err != nil {
			log.Printf("Download %s report failed: %v", job.Type, err)
			continue
		}

		// 解析报告 (根据扫描类型调用对应的解析器)
		vulns, err := b.scanClient.ParseReport(ctx, string(job.Type), reportPath)
		if err != nil {
			log.Printf("Parse %s report failed: %v", job.Type, err)
			continue
		}
		log.Printf("Extracted %d vulnerabilities from %s", len(vulns), job.Type)
		allVulns = append(allVulns, vulns...)
	}

	if len(allVulns) == 0 {
		log.Println("No vulnerabilities found in failed scans")
		return 0, 0, nil
	}

	// 5. 获取修复方案
	var fileChanges []tools.FileChange

	for _, vuln := range allVulns {
		// 根据漏洞类型获取修复方案
		if vuln.Type == "Dependency" || vuln.Type == "FOSS" {
			// FOSS 漏洞：获取升级建议
			solution, err := b.scanClient.GetFOSSolution(ctx, vuln.ID)
			if err != nil {
				log.Printf("Get FOSS solution for %s failed: %v", vuln.ID, err)
				continue
			}
			if solution.HasUpgrade {
				// 生成 pom.xml 修改
				change := tools.FileChange{
					Path:    "pom.xml",
					Content: fmt.Sprintf("<!-- Upgrade %s from %s to %s -->", solution.ArtifactID, solution.CurrentVersion, solution.FixedVersion),
					Message: fmt.Sprintf("chore: upgrade %s to %s to fix CVEs", solution.ArtifactID, solution.FixedVersion),
				}
				fileChanges = append(fileChanges, change)
			}
		} else {
			// 代码漏洞：搜索 Web 修复方案
			solution, err := b.scanClient.SearchWebSolution(ctx, vuln.CVE)
			if err != nil {
				log.Printf("Search web solution for %s failed: %v", vuln.CVE, err)
				continue
			}
			// 根据搜索结果生成代码修复
			if len(solution.Solutions) > 0 {
				log.Printf("Found solution for %s: %s", vuln.CVE, solution.Solutions[0])
				// TODO: 根据漏洞位置和类型生成具体代码修复
			}
		}
	}

	if len(fileChanges) == 0 {
		log.Println("No automated fixes available")
		return len(allVulns), 0, nil
	}

	// 6. 通过 MCP 创建独立修复分支
	branch := generateFixBranchName()
	err = b.gitClient.CreateBranch(ctx, repoCfg.FullName, branch, repoCfg.DevBranch)
	if err != nil {
		return len(allVulns), 0, fmt.Errorf("create branch: %w", err)
	}

	// 7. 通过 MCP 提交修复
	_, err = b.gitClient.CommitAndPush(ctx, repoCfg.FullName, branch, fileChanges)
	if err != nil {
		return len(allVulns), 0, fmt.Errorf("commit changes: %w", err)
	}

	log.Println("Committed fixes, creating independent PR...")

	// 8. 通过 MCP 创建独立修复 PR
	prTitle := fmt.Sprintf("[Security Fix] Auto-generated fixes - %s", time.Now().Format("2006-01-02"))
	prNumber, err := b.gitClient.CreatePullRequest(ctx, repoCfg.FullName, prTitle,
		"Automatically created by Security Bot to fix vulnerabilities detected by CI scans.\n\nThis PR contains security fixes generated based on scan results.",
		branch, repoCfg.DevBranch)
	if err != nil {
		return len(allVulns), 0, fmt.Errorf("create PR: %w", err)
	}
	log.Printf("Created PR #%d", prNumber)

	// 9. 等待 CI 部署
	log.Println("Waiting for CI/CD deployment...")
	status, err := b.deploy.WaitAndVerify(ctx, repoCfg.ServiceName)
	if err != nil {
		return len(allVulns), 0, fmt.Errorf("wait deployment: %w", err)
	}

	if !status.Success {
		return len(allVulns), 0, fmt.Errorf("deployment failed: %s", status.Message)
	}
	log.Printf("Deployment successful: %s", status.ServiceURL)

	// 10. 通过 MCP 运行集成测试
	log.Println("Running integration tests...")
	testResult, err := b.testClient.RunIntegrationTest(ctx, b.config.Test.Workspace, b.config.Test.CucumberProfile)
	if err != nil {
		return len(allVulns), 0, fmt.Errorf("run tests: %w", err)
	}

	if !testResult.Success {
		log.Printf("Tests failed: %d passed, %d failed, %d skipped", testResult.Passed, testResult.Failed, testResult.Skipped)
		return len(allVulns), 0, fmt.Errorf("tests failed: %d passed, %d failed", testResult.Passed, testResult.Failed)
	}
	log.Printf("All tests passed: %d passed, %d failed, %d skipped", testResult.Passed, testResult.Failed, testResult.Skipped)

	// 11. 通过 MCP 合并到 master
	log.Printf("Merging PR #%d to %s...", prNumber, b.config.GitHub.DefaultBranch)
	err = b.gitClient.MergePullRequest(ctx, repoCfg.FullName, prNumber, b.config.GitHub.DefaultBranch)
	if err != nil {
		return len(allVulns), prNumber, fmt.Errorf("merge PR: %w", err)
	}
	log.Printf("Successfully merged PR #%d to %s!", prNumber, b.config.GitHub.DefaultBranch)

	return len(allVulns), prNumber, nil
}

// getSecret 从 GCP Secret Manager 获取凭证
func getSecret(ctx context.Context, secretName string) (string, error) {
	// TODO: 实现 GCP Secret Manager 调用
	// 简化实现：从环境变量读取
	return os.Getenv("SECRET_" + secretName), nil
}

// generateFixBranchName 生成独立修复分支名
func generateFixBranchName() string {
	return "security-bot/fix-" + time.Now().Format("20060102-150405")
}
