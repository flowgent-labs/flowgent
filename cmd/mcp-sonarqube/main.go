package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/go-resty/resty/v2"
)

var (
	sonarqubeURL  string
	sonarqubeUser string
	sonarqubePass string
	sonarqubeCli  *resty.Client

	analysisCache sync.Map
)

func main() {
	sonarqubeURL = os.Getenv("SONARQUBE_URL")
	if sonarqubeURL == "" {
		sonarqubeURL = "http://localhost:9000"
	}
	sonarqubeUser = os.Getenv("SONARQUBE_TOKEN")
	if sonarqubeUser == "" {
		sonarqubeUser = os.Getenv("SONARQUBE_USER")
	}
	sonarqubePass = os.Getenv("SONARQUBE_PASSWORD")

	sonarqubeCli = resty.New()
	sonarqubeCli.SetBaseURL(sonarqubeURL)
	sonarqubeCli.SetBasicAuth(sonarqubeUser, sonarqubePass)

	mcpServer := server.NewMCPServer("sonarqube-mcp-server", "1.0.0")

	mcpServer.AddTool(mcp.NewTool("scan/get_jobs_by_commit",
		mcp.WithDescription("Get SonarQube scan jobs by commit"),
		mcp.WithString("repo", mcp.Description("Repository name"), mcp.Required()),
		mcp.WithString("commit_sha", mcp.Description("Commit SHA"), mcp.Required()),
	), getJobsByCommitHandler)

	mcpServer.AddTool(mcp.NewTool("scan/get_status",
		mcp.WithDescription("Get SonarQube job status"),
		mcp.WithString("job_id", mcp.Description("Job ID (SonarQube branch/project key)"), mcp.Required()),
	), getJobStatusHandler)

	mcpServer.AddTool(mcp.NewTool("report/download",
		mcp.WithDescription("Download SonarQube report"),
		mcp.WithString("report_id", mcp.Description("Report ID (SonarQube project key)"), mcp.Required()),
		mcp.WithString("output_path", mcp.Description("Output file path"), mcp.Required()),
	), downloadReportHandler)

	mcpServer.AddTool(mcp.NewTool("parse/sonarqube_to_html",
		mcp.WithDescription("Parse SonarQube report to HTML and extract vulnerabilities"),
		mcp.WithString("report_path", mcp.Description("Report JSON file path"), mcp.Required()),
	), parseSonarqubeHandler)

	mcpServer.AddTool(mcp.NewTool("scan/get_issues",
		mcp.WithDescription("Get SonarQube issues for a project/branch"),
		mcp.WithString("project_key", mcp.Description("SonarQube project key"), mcp.Required()),
		mcp.WithString("branch", mcp.Description("Branch name")),
		mcp.WithString("severities", mcp.Description("Filter by severities (BLOCKER,CRITICAL,MAJOR,MINOR,CINFO)")),
	), getIssuesHandler)

	log.Println("SonarQube MCP Server started")

	if err := server.ServeStdio(mcpServer); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func getJobsByCommitHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	repo := mcp.ParseString(request, "repo", "")
	commitSHA := mcp.ParseString(request, "commit_sha", "")
	if repo == "" {
		return nil, fmt.Errorf("repo is required")
	}
	if commitSHA == "" {
		return nil, fmt.Errorf("commit_sha is required")
	}

	projectKey := strings.ReplaceAll(repo, "/", ":")
	issues, err := fetchSonarQubeIssues(projectKey, "", commitSHA)
	if err != nil {
		return nil, fmt.Errorf("fetch sonarqube issues: %w", err)
	}

	jobs := []ScanJob{
		{
			JobID:     projectKey,
			Type:      "sonarqube",
			Status:    "success",
			ReportID:  projectKey,
			CommitSHA: commitSHA,
			Repo:      repo,
		},
	}

	if len(issues) > 0 {
		hasBlocker := false
		for _, issue := range issues {
			if issue.Severity == "BLOCKER" {
				hasBlocker = true
				break
			}
		}
		if hasBlocker {
			jobs[0].Status = "failed"
		}
	}

	result, _ := json.Marshal(jobs)
	return mcp.NewToolResultText(string(result)), nil
}

func getJobStatusHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	jobID := mcp.ParseString(request, "job_id", "")
	if jobID == "" {
		return nil, fmt.Errorf("job_id is required")
	}

	status, err := fetchQualityGateStatus(jobID)
	if err != nil {
		return nil, fmt.Errorf("fetch status: %w", err)
	}

	result, _ := json.Marshal(status)
	return mcp.NewToolResultText(string(result)), nil
}

func downloadReportHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	reportID := mcp.ParseString(request, "report_id", "")
	outputPath := mcp.ParseString(request, "output_path", "")
	if reportID == "" {
		return nil, fmt.Errorf("report_id is required")
	}
	if outputPath == "" {
		return nil, fmt.Errorf("output_path is required")
	}

	report, err := fetchProjectAnalysis(reportID)
	if err != nil {
		return nil, fmt.Errorf("fetch analysis: %w", err)
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal report: %w", err)
	}

	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return nil, fmt.Errorf("write report: %w", err)
	}

	analysisCache.Store(reportID, report)

	result, _ := json.Marshal(map[string]string{
		"path":   outputPath,
		"format": "json",
	})
	return mcp.NewToolResultText(string(result)), nil
}

func parseSonarqubeHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	reportPath := mcp.ParseString(request, "report_path", "")
	if reportPath == "" {
		return nil, fmt.Errorf("report_path is required")
	}

	data, err := os.ReadFile(reportPath)
	if err != nil {
		return nil, fmt.Errorf("read report: %w", err)
	}

	var issues []SonarQubeIssue
	if err := json.Unmarshal(data, &issues); err != nil {
		return nil, fmt.Errorf("parse report: %w", err)
	}

	htmlPath := strings.TrimSuffix(reportPath, ".json") + ".html"
	if err := generateHTML(issues, htmlPath); err != nil {
		log.Printf("Warning: failed to generate HTML: %v", err)
	}

	vulns := convertIssuesToVulnerabilities(issues)

	result := map[string]interface{}{
		"html_path":        htmlPath,
		"vulnerabilities":  vulns,
	}

	resultJSON, _ := json.Marshal(result)
	return mcp.NewToolResultText(string(resultJSON)), nil
}

func getIssuesHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projectKey := mcp.ParseString(request, "project_key", "")
	if projectKey == "" {
		return nil, fmt.Errorf("project_key is required")
	}

	branch := mcp.ParseString(request, "branch", "")
	severities := mcp.ParseString(request, "severities", "")

	issues, err := fetchSonarQubeIssues(projectKey, branch, "")
	if err != nil {
		return nil, fmt.Errorf("fetch issues: %w", err)
	}

	if severities != "" {
		filtered := make([]SonarQubeIssue, 0)
		severitySet := strings.Split(severities, ",")
		for _, issue := range issues {
			for _, s := range severitySet {
				if issue.Severity == strings.TrimSpace(s) {
					filtered = append(filtered, issue)
					break
				}
			}
		}
		issues = filtered
	}

	result, _ := json.Marshal(issues)
	return mcp.NewToolResultText(string(result)), nil
}

// fetchQualityGateStatus 获取质量门禁状态
func fetchQualityGateStatus(projectKey string) (ScanJob, error) {
	var resp struct {
		ProjectStatus struct {
			Status  string `json:"status"`
			Conditions []struct {
				Status      string  `json:"status"`
				MetricKey   string  `json:"metricKey"`
				ActualValue float64 `json:"actualValue,string"`
			} `json:"conditions"`
		} `json:"projectStatus"`
	}

	res, err := sonarqubeCli.R().
		SetQueryParams(map[string]string{
			"projectKey": projectKey,
		}).
		SetResult(&resp).
		Get("/api/qualitygates/project_status")

	if err != nil {
		return ScanJob{}, fmt.Errorf("request: %w", err)
	}

	if res.StatusCode() != 200 {
		return ScanJob{}, fmt.Errorf("API returned status %d", res.StatusCode())
	}

	status := resp.ProjectStatus.Status
	if status == "" {
		status = "success"
	} else {
		status = strings.ToLower(status)
	}

	return ScanJob{
		JobID:  projectKey,
		Type:   "sonarqube",
		Status: status,
	}, nil
}

// fetchProjectAnalysis 获取项目分析数据
func fetchProjectAnalysis(projectKey string) ([]SonarQubeIssue, error) {
	return fetchSonarQubeIssues(projectKey, "", "")
}

// fetchSonarQubeIssues 获取 SonarQube issues
func fetchSonarQubeIssues(projectKey, branch, commitSHA string) ([]SonarQubeIssue, error) {
	params := map[string]string{
		"componentKeys": projectKey,
		"ps":            "500",
		"statuses":      "OPEN",
		"additionalFields": "rules",
	}
	if branch != "" {
		params["branch"] = branch
	}

	var resp struct {
		Issues []SonarQubeIssue `json:"issues"`
		Total  int              `json:"total"`
		Paging struct {
			PageSize int `json:"pageSize"`
		} `json:"paging"`
	}

	res, err := sonarqubeCli.R().
		SetQueryParams(params).
		SetResult(&resp).
		Get("/api/issues/search")

	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}

	if res.StatusCode() != 200 {
		return nil, fmt.Errorf("API returned status %d", res.StatusCode())
	}

	for len(resp.Issues) < resp.Total {
		params["p"] = fmt.Sprintf("%d", len(resp.Issues)/500+2)
		var nextResp struct {
			Issues []SonarQubeIssue `json:"issues"`
		}
		res, err = sonarqubeCli.R().
			SetQueryParams(params).
			SetResult(&nextResp).
			Get("/api/issues/search")
		if err != nil {
			return nil, fmt.Errorf("request next page: %w", err)
		}
		if res.StatusCode() != 200 {
			return resp.Issues, nil
		}
		resp.Issues = append(resp.Issues, nextResp.Issues...)
	}

	return resp.Issues, nil
}

// convertIssuesToVulnerabilities 将 SonarQube issues 转换为标准 Vulnerability 格式
func convertIssuesToVulnerabilities(issues []SonarQubeIssue) []Vulnerability {
	var vulns []Vulnerability
	for _, issue := range issues {
		vuln := Vulnerability{
			ID:       issue.Key,
			Type:     issue.Type,
			Severity: issue.Severity,
			File:     issue.Component,
			Line:     issue.Line,
			CVE:      extractCVEFromRule(issue.Rule),
			CWE:      extractCWEFromRule(issue.Rule),
			Summary:  issue.Message,
		}
		vulns = append(vulns, vuln)
	}
	return vulns
}

func extractCVEFromRule(rule string) string {
	prefix := "cwe:"
	if idx := strings.Index(strings.ToLower(rule), prefix); idx >= 0 {
		return ""
	}
	return ""
}

func extractCWEFromRule(rule string) string {
	if strings.HasPrefix(strings.ToLower(rule), "cwe:") {
		return strings.ToUpper(rule)
	}
	return ""
}

func generateHTML(issues []SonarQubeIssue, htmlPath string) error {
	html := `<!DOCTYPE html><html><head><meta charset="UTF-8"><title>SonarQube Report</title></head><body>`
	html += fmt.Sprintf("<h1>SonarQube Analysis Report</h1><p>Total Issues: %d</p>", len(issues))
	html += `<table border="1"><tr><th>Severity</th><th>Type</th><th>File</th><th>Line</th><th>Message</th></tr>`
	for _, issue := range issues {
		html += fmt.Sprintf("<tr><td>%s</td><td>%s</td><td>%s</td><td>%d</td><td>%s</td></tr>",
			issue.Severity, issue.Type, issue.Component, issue.Line, issue.Message)
	}
	html += `</table></body></html>`
	return os.WriteFile(htmlPath, []byte(html), 0644)
}

// SonarQubeIssue SonarQube 问题
type SonarQubeIssue struct {
	Key              string `json:"key"`
	Rule             string `json:"rule"`
	Severity         string `json:"severity"`
	Type             string `json:"type"`
	Component        string `json:"component"`
	Project          string `json:"project"`
	Line             int    `json:"line"`
	Message          string `json:"message"`
	Status           string `json:"status"`
	Author           string `json:"author"`
	CreatedAt        string `json:"createdAt"`
	Effort           string `json:"effort"`
	Debt             string `json:"debt"`
	Comments         []IssueComment `json:"comments,omitempty"`
	Hash             string `json:"hash"`
	Gap              int    `json:"gap,omitempty"`
	QuickFixAvailable bool   `json:"quickFixAvailable,omitempty"`
	Flow             []IssueFlow `json:"flows,omitempty"`
}

// IssueComment SonarQube 评论
type IssueComment struct {
	Key       string `json:"key"`
	Login     string `json:"login,omitempty"`
	Markdown  bool   `json:"markdown"`
	Text      string `json:"text"`
	CreatedAt string `json:"createdAt"`
	Updatable bool   `json:"updatable"`
}

// IssueFlow SonarQube 数据流
type IssueFlow struct {
	Locations []IssueLocation `json:"locations"`
}

// IssueLocation SonarQube 位置
type IssueLocation struct {
	Message         string `json:"message"`
	Component       string `json:"component"`
	Line            int    `json:"line"`
	Gap             int    `json:"gap"`
	QuickFixAvailable bool `json:"quickFixAvailable,omitempty"`
}

// ScanJob 扫描作业
type ScanJob struct {
	JobID     string `json:"job_id"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	ReportID  string `json:"report_id"`
	CommitSHA string `json:"commit_sha"`
	Repo      string `json:"repo"`
}

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
