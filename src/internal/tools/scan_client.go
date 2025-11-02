package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// ScanClient MCP 扫描客户端 - 统一封装所有扫描相关 MCP Tools
type ScanClient struct {
	scanner *client.Client
	parser  *client.Client
	fixer   *client.Client
}

// NewScanClient 创建扫描客户端
func NewScanClient(scanner, parser, fixer *client.Client) *ScanClient {
	return &ScanClient{
		scanner: scanner,
		parser:  parser,
		fixer:   fixer,
	}
}

// ScanJob 扫描任务元数据
type ScanJob struct {
	JobID     string   `json:"job_id"`
	Type      ScanType `json:"type"`
	Status    string   `json:"status"`
	ReportID  string   `json:"report_id"`
	CommitSHA string   `json:"commit_sha"`
	Repo      string   `json:"repo"`
}

// ScanType 扫描类型
type ScanType string

const (
	ScanTypeDAST  ScanType = "dast"
	ScanTypeSAST  ScanType = "sast"
	ScanTypeCONT  ScanType = "cont"
	ScanTypeFOSS  ScanType = "foss"
	ScanTypeSonar ScanType = "sonarqube"
)

// Vulnerability 漏洞元数据
type Vulnerability struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Severity string `json:"severity"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	CVE      string `json:"cve"`
	CWE      string `json:"cwe"`
	Summary  string `json:"summary"`
}

// GetJobsByCommit 获取扫描作业列表
func (c *ScanClient) GetJobsByCommit(ctx context.Context, repo, commitSHA string) ([]ScanJob, error) {
	if c.scanner == nil {
		return nil, fmt.Errorf("scanner MCP client not connected")
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = "scan/get_jobs_by_commit"
	req.Params.Arguments = map[string]any{
		"repo":       repo,
		"commit_sha": commitSHA,
	}

	result, err := c.scanner.CallTool(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("MCP call failed: %w", err)
	}

	// Extract text content from result
	var text string
	for _, content := range result.Content {
		if tc, ok := content.(mcp.TextContent); ok {
			text = tc.Text
			break
		}
	}
	if text == "" {
		return nil, fmt.Errorf("empty response from MCP server")
	}

	var scans []ScanJob
	if err := json.Unmarshal([]byte(text), &scans); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return scans, nil
}

// GetJobStatus 获取扫描作业状态
func (c *ScanClient) GetJobStatus(ctx context.Context, jobID string) (*ScanJob, error) {
	if c.scanner == nil {
		return nil, fmt.Errorf("scanner MCP client not connected")
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = "scan/get_status"
	req.Params.Arguments = map[string]any{
		"job_id": jobID,
	}

	result, err := c.scanner.CallTool(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("MCP call failed: %w", err)
	}

	var text string
	for _, content := range result.Content {
		if tc, ok := content.(mcp.TextContent); ok {
			text = tc.Text
			break
		}
	}
	if text == "" {
		return nil, fmt.Errorf("empty response from MCP server")
	}

	var status ScanJob
	if err := json.Unmarshal([]byte(text), &status); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return &status, nil
}

// DownloadReport 下载报告
func (c *ScanClient) DownloadReport(ctx context.Context, reportID, outputPath string) (string, error) {
	if c.scanner == nil {
		return "", fmt.Errorf("scanner MCP client not connected")
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = "report/download"
	req.Params.Arguments = map[string]any{
		"report_id":   reportID,
		"output_path": outputPath,
	}

	result, err := c.scanner.CallTool(ctx, req)
	if err != nil {
		return "", fmt.Errorf("MCP call failed: %w", err)
	}

	var text string
	for _, content := range result.Content {
		if tc, ok := content.(mcp.TextContent); ok {
			text = tc.Text
			break
		}
	}
	if text == "" {
		return "", fmt.Errorf("empty response from MCP server")
	}

	var res struct {
		Path   string `json:"path"`
		Format string `json:"format"`
	}
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	return res.Path, nil
}

// ParseReport 解析报告
func (c *ScanClient) ParseReport(ctx context.Context, scanType, reportPath string) ([]Vulnerability, error) {
	if c.parser == nil {
		return nil, fmt.Errorf("parser MCP client not connected")
	}

	toolName := fmt.Sprintf("parse/%s_to_html", scanType)
	req := mcp.CallToolRequest{}
	req.Params.Name = toolName
	req.Params.Arguments = map[string]any{
		"report_path": reportPath,
	}

	result, err := c.parser.CallTool(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("MCP call failed: %w", err)
	}

	var text string
	for _, content := range result.Content {
		if tc, ok := content.(mcp.TextContent); ok {
			text = tc.Text
			break
		}
	}
	if text == "" {
		return nil, fmt.Errorf("empty response from MCP server")
	}

	var res struct {
		HTMLPath        string          `json:"html_path"`
		Vulnerabilities json.RawMessage `json:"vulnerabilities"`
	}
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	var vulns []Vulnerability
	if err := json.Unmarshal(res.Vulnerabilities, &vulns); err != nil {
		return nil, fmt.Errorf("parse vulnerabilities: %w", err)
	}

	return vulns, nil
}

// GetFOSSolution 获取 FOSS 漏洞修复方案
func (c *ScanClient) GetFOSSolution(ctx context.Context, componentID string) (*UpgradeRecommendation, error) {
	if c.fixer == nil {
		return nil, fmt.Errorf("fixer MCP client not connected")
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = "fix/get_foss_solution"
	req.Params.Arguments = map[string]any{
		"component_id": componentID,
	}

	result, err := c.fixer.CallTool(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("MCP call failed: %w", err)
	}

	var text string
	for _, content := range result.Content {
		if tc, ok := content.(mcp.TextContent); ok {
			text = tc.Text
			break
		}
	}
	if text == "" {
		return nil, fmt.Errorf("empty response from MCP server")
	}

	var fix UpgradeRecommendation
	if err := json.Unmarshal([]byte(text), &fix); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return &fix, nil
}

// SearchWebSolution 搜索 Web 修复方案
func (c *ScanClient) SearchWebSolution(ctx context.Context, cveID string) (*WebSolution, error) {
	if c.fixer == nil {
		return nil, fmt.Errorf("fixer MCP client not connected")
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = "fix/search_web"
	req.Params.Arguments = map[string]any{
		"cve_id": cveID,
	}

	result, err := c.fixer.CallTool(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("MCP call failed: %w", err)
	}

	var text string
	for _, content := range result.Content {
		if tc, ok := content.(mcp.TextContent); ok {
			text = tc.Text
			break
		}
	}
	if text == "" {
		return nil, fmt.Errorf("empty response from MCP server")
	}

	var solution WebSolution
	if err := json.Unmarshal([]byte(text), &solution); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return &solution, nil
}

// GetFailed 获取失败的扫描作业
func (c *ScanClient) GetFailed(jobs []ScanJob) []ScanJob {
	var failed []ScanJob
	for _, job := range jobs {
		if job.Status == "FAILED" || job.Status == "failed" {
			failed = append(failed, job)
		}
	}
	return failed
}

// UpgradeRecommendation 升级建议
type UpgradeRecommendation struct {
	ComponentID    string   `json:"component_id"`
	GroupID        string   `json:"group_id"`
	ArtifactID     string   `json:"artifact_id"`
	CurrentVersion string   `json:"current_version"`
	FixedVersion   string   `json:"fixed_version"`
	HasUpgrade     bool     `json:"has_upgrade"`
	CVEs           []string `json:"cves"`
	UpgradePath    string   `json:"upgrade_path"`
}

// WebSolution Web 搜索的修复方案
type WebSolution struct {
	Solutions  []string `json:"solutions"`
	References []string `json:"references"`
	Confidence string   `json:"confidence"`
}
