package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/go-resty/resty/v2"
)

var (
	iqURL      string
	iqUser     string
	iqPass     string
	iqAppID    string
	iqCli      *resty.Client
)

func main() {
	iqURL = os.Getenv("SONATYPEIQ_URL")
	if iqURL == "" {
		iqURL = "http://localhost:8070"
	}
	iqUser = os.Getenv("SONATYPEIQ_USER")
	iqPass = os.Getenv("SONATYPEIQ_PASSWORD")
	iqAppID = os.Getenv("SONATYPEIQ_APP_ID")

	iqCli = resty.New()
	iqCli.SetBaseURL(iqURL)
	iqCli.SetBasicAuth(iqUser, iqPass)

	mcpServer := server.NewMCPServer("sonatype-iq-mcp-server", "1.0.0")

	mcpServer.AddTool(mcp.NewTool("scan/get_jobs_by_commit",
		mcp.WithDescription("Get Sonatype IQ FOSS scan jobs by commit"),
		mcp.WithString("repo", mcp.Description("Repository name"), mcp.Required()),
		mcp.WithString("commit_sha", mcp.Description("Commit SHA"), mcp.Required()),
	), getJobsByCommitHandler)

	mcpServer.AddTool(mcp.NewTool("scan/get_status",
		mcp.WithDescription("Get Sonatype IQ scan job status"),
		mcp.WithString("job_id", mcp.Description("Job ID (application ID or report ID)"), mcp.Required()),
	), getJobStatusHandler)

	mcpServer.AddTool(mcp.NewTool("report/download",
		mcp.WithDescription("Download Sonatype IQ report"),
		mcp.WithString("report_id", mcp.Description("Report ID (application ID)"), mcp.Required()),
		mcp.WithString("output_path", mcp.Description("Output file path"), mcp.Required()),
	), downloadReportHandler)

	mcpServer.AddTool(mcp.NewTool("parse/foss_to_html",
		mcp.WithDescription("Parse FOSS report to HTML and extract vulnerabilities"),
		mcp.WithString("report_path", mcp.Description("Report JSON file path"), mcp.Required()),
	), parseFossHandler)

	mcpServer.AddTool(mcp.NewTool("fix/get_foss_solution",
		mcp.WithDescription("Get FOSS vulnerability upgrade recommendation"),
		mcp.WithString("component_id", mcp.Description("Component ID"), mcp.Required()),
	), getFOSSSolutionHandler)

	mcpServer.AddTool(mcp.NewTool("fix/search_web",
		mcp.WithDescription("Search web for vulnerability fix solution"),
		mcp.WithString("cve_id", mcp.Description("CVE ID"), mcp.Required()),
	), searchWebHandler)

	log.Println("Sonatype IQ MCP Server started")

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

	appID := iqAppID
	if appID == "" {
		appID, _ = resolveApplicationID(repo)
	}
	if appID == "" {
		return nil, fmt.Errorf("no application ID configured for repo: %s", repo)
	}

	violations, err := fetchApplicationViolations(appID)
	if err != nil {
		return nil, fmt.Errorf("fetch violations: %w", err)
	}

	status := "success"
	if len(violations) > 0 {
		status = "failed"
	}

	jobs := []ScanJob{
		{
			JobID:     appID,
			Type:      "foss",
			Status:    status,
			ReportID:  appID,
			CommitSHA: commitSHA,
			Repo:      repo,
		},
	}

	result, _ := json.Marshal(jobs)
	return mcp.NewToolResultText(string(result)), nil
}

func getJobStatusHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	jobID := mcp.ParseString(request, "job_id", "")
	if jobID == "" {
		return nil, fmt.Errorf("job_id is required")
	}

	violations, err := fetchApplicationViolations(jobID)
	if err != nil {
		return nil, fmt.Errorf("fetch violations: %w", err)
	}

	status := "success"
	if len(violations) > 0 {
		status = "failed"
	}

	result, _ := json.Marshal(ScanJob{
		JobID:  jobID,
		Type:   "foss",
		Status: status,
	})
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

	report, err := fetchApplicationReport(reportID)
	if err != nil {
		return nil, fmt.Errorf("fetch report: %w", err)
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal report: %w", err)
	}

	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return nil, fmt.Errorf("write report: %w", err)
	}

	result, _ := json.Marshal(map[string]string{
		"path":   outputPath,
		"format": "json",
	})
	return mcp.NewToolResultText(string(result)), nil
}

func parseFossHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	reportPath := mcp.ParseString(request, "report_path", "")
	if reportPath == "" {
		return nil, fmt.Errorf("report_path is required")
	}

	data, err := os.ReadFile(reportPath)
	if err != nil {
		return nil, fmt.Errorf("read report: %w", err)
	}

	var report FOSSReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("parse report: %w", err)
	}

	htmlPath := strings.TrimSuffix(reportPath, ".json") + ".html"
	if err := generateHTML(report.Components, htmlPath); err != nil {
		log.Printf("Warning: failed to generate HTML: %v", err)
	}

	var vulns []Vulnerability
	for _, comp := range report.Components {
		for _, v := range comp.Vulnerabilities {
			vulns = append(vulns, Vulnerability{
				ID:       comp.ComponentID,
				Type:     "Dependency",
				Severity: v.Severity,
				File:     "",
				Line:     0,
				CVE:      v.CVE,
				CWE:      v.CWE,
				Summary:  v.Description,
			})
		}
		for _, v := range comp.LicenseViolations {
			vulns = append(vulns, Vulnerability{
				ID:       comp.ComponentID,
				Type:     "License",
				Severity: v.PolicySeverity,
				File:     "",
				Line:     0,
				CVE:      "",
				CWE:      "",
				Summary:  fmt.Sprintf("License violation: %s (%s)", v.License, v.PolicyName),
			})
		}
	}

	result := map[string]interface{}{
		"html_path":       htmlPath,
		"vulnerabilities": vulns,
	}
	resultJSON, _ := json.Marshal(result)
	return mcp.NewToolResultText(string(resultJSON)), nil
}

func getFOSSSolutionHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	componentID := mcp.ParseString(request, "component_id", "")
	if componentID == "" {
		return nil, fmt.Errorf("component_id is required")
	}

	recommendation, err := fetchUpgradeRecommendation(componentID)
	if err != nil {
		return nil, fmt.Errorf("fetch recommendation: %w", err)
	}

	result, _ := json.Marshal(recommendation)
	return mcp.NewToolResultText(string(result)), nil
}

func searchWebHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cveID := mcp.ParseString(request, "cve_id", "")
	if cveID == "" {
		return nil, fmt.Errorf("cve_id is required")
	}

	solution := searchCVESolution(cveID)
	result, _ := json.Marshal(solution)
	return mcp.NewToolResultText(string(result)), nil
}

// fetchApplicationViolations 获取应用违规
func fetchApplicationViolations(appID string) ([]ApplicationViolation, error) {
	var resp struct {
		Violations []ApplicationViolation `json:"violations"`
	}

	res, err := iqCli.R().
		SetPathParams(map[string]string{"app_id": appID}).
		SetResult(&resp).
		Get("/api/v3/applications/{app_id}/violations")

	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}

	if res.StatusCode() != 200 {
		return nil, fmt.Errorf("API returned status %d", res.StatusCode())
	}

	return resp.Violations, nil
}

// fetchApplicationReport 获取应用完整报告
func fetchApplicationReport(appID string) (FOSSReport, error) {
	var report FOSSReport
	report.ApplicationID = appID

	var resp struct {
		Dependencies []struct {
			ComponentID        string `json:"componentId"`
			GroupID            string `json:"groupId"`
			ArtifactID         string `json:"artifactId"`
			Version            string `json:"version"`
			CPE                string `json:"cpe"`
			Hash               string `json:"hash"`
			Violations         []struct {
				CVE           string `json:"cveId"`
				Severity      string `json:"severity"`
				Description   string `json:"description"`
				CWE           string `json:"cweId"`
			} `json:"violations"`
			LicenseViolations []struct {
				License       string `json:"licenseName"`
				PolicyName    string `json:"policyName"`
				PolicySeverity string `json:"policySeverity"`
			} `json:"licenseViolations"`
			RecommendedVersion string `json:"recommendedVersion"`
			LatestVersion      string `json:"latestVersion"`
		} `json:"dependencies"`
	}

	res, err := iqCli.R().
		SetPathParams(map[string]string{"app_id": appID}).
		SetResult(&resp).
		Get("/api/v3/applications/{app_id}/report")

	if err != nil {
		return report, fmt.Errorf("request: %w", err)
	}

	if res.StatusCode() != 200 {
		return report, fmt.Errorf("API returned status %d", res.StatusCode())
	}

	for _, dep := range resp.Dependencies {
		var vulns []ComponentVulnerability
		var licViolations []LicenseViolation

		for _, v := range dep.Violations {
			vulns = append(vulns, ComponentVulnerability{
				CVE:        v.CVE,
				Severity:   v.Severity,
				CWE:        v.CWE,
				Description: v.Description,
			})
		}

		for _, lv := range dep.LicenseViolations {
			licViolations = append(licViolations, LicenseViolation{
				License:      lv.License,
				PolicyName:   lv.PolicyName,
				PolicySeverity: lv.PolicySeverity,
			})
		}

		report.Components = append(report.Components, FOSSComponent{
			ComponentID:       dep.ComponentID,
			GroupID:           dep.GroupID,
			ArtifactID:        dep.ArtifactID,
			Version:           dep.Version,
			CPE:               dep.CPE,
			Hash:              dep.Hash,
			Vulnerabilities:   vulns,
			LicenseViolations: licViolations,
			RecommendedVersion: dep.RecommendedVersion,
			LatestVersion:     dep.LatestVersion,
		})
	}

	return report, nil
}

// fetchUpgradeRecommendation 获取升级建议
func fetchUpgradeRecommendation(componentID string) (*UpgradeRecommendation, error) {
	var dep struct {
		ComponentID         string `json:"componentId"`
		GroupID             string `json:"groupId"`
		ArtifactID          string `json:"artifactId"`
		Version             string `json:"version"`
		RecommendedVersion  string `json:"recommendedVersion"`
		LatestVersion       string `json:"latestVersion"`
	}

	res, err := iqCli.R().
		SetPathParams(map[string]string{"component_id": componentID}).
		SetResult(&dep).
		Get("/api/v3/components/{component_id}")

	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}

	if res.StatusCode() != 200 {
		return nil, fmt.Errorf("API returned status %d", res.StatusCode())
	}

	hasUpgrade := dep.RecommendedVersion != "" || dep.LatestVersion != ""
	fixedVersion := dep.RecommendedVersion
	if fixedVersion == "" {
		fixedVersion = dep.LatestVersion
	}

	return &UpgradeRecommendation{
		ComponentID:    dep.ComponentID,
		GroupID:        dep.GroupID,
		ArtifactID:     dep.ArtifactID,
		CurrentVersion: dep.Version,
		FixedVersion:   fixedVersion,
		HasUpgrade:     hasUpgrade,
		CVEs:           []string{},
	}, nil
}

// resolveApplicationID 通过仓库名解析应用 ID
func resolveApplicationID(repo string) (string, error) {
	var resp struct {
		Applications []struct {
			ApplicationID   string `json:"applicationId"`
			ApplicationName string `json:"applicationName"`
		} `json:"applications"`
	}

	res, err := iqCli.R().
		SetResult(&resp).
		Get("/api/v3/applications")

	if err != nil {
		return "", fmt.Errorf("request: %w", err)
	}

	if res.StatusCode() != 200 {
		return "", fmt.Errorf("API returned status %d", res.StatusCode())
	}

	for _, app := range resp.Applications {
		if strings.Contains(app.ApplicationName, repo) || strings.HasSuffix(app.ApplicationName, repo) {
			return app.ApplicationID, nil
		}
	}

	return "", nil
}

// searchCVESolution 搜索 CVE 修复方案
func searchCVESolution(cveID string) *WebSolution {
	var cveInfo struct {
		Description string `json:"description"`
		References  []string `json:"references"`
		Fix         string `json:"fix"`
	}

	res, err := iqCli.R().
		SetPathParams(map[string]string{"cve_id": cveID}).
		SetResult(&cveInfo).
		Get("/api/v3/cve/{cve_id}")

	if err != nil || res.StatusCode() != 200 {
		return &WebSolution{
			Solutions:  []string{fmt.Sprintf("Search NVD for %s: https://nvd.nist.gov/vuln/detail/%s", cveID, cveID)},
			References: []string{fmt.Sprintf("https://nvd.nist.gov/vuln/detail/%s", cveID)},
			Confidence: "low",
		}
	}

	var solutions []string
	if cveInfo.Fix != "" {
		solutions = append(solutions, cveInfo.Fix)
	}
	if cveInfo.Description != "" {
		solutions = append(solutions, cveInfo.Description)
	}

	return &WebSolution{
		Solutions:  solutions,
		References: cveInfo.References,
		Confidence: "medium",
	}
}

func generateHTML(components []FOSSComponent, htmlPath string) error {
	html := `<!DOCTYPE html><html><head><meta charset="UTF-8"><title>Sonatype IQ Report</title></head><body>`
	html += fmt.Sprintf("<h1>Sonatype IQ Report</h1><p>Components: %d</p>", len(components))

	html += `<table border="1"><tr><th>Component</th><th>Version</th><th>Vulnerabilities</th><th>Licenses</th></tr>`
	for _, comp := range components {
		html += fmt.Sprintf("<tr><td>%s:%s</td><td>%s</td><td>%d</td><td>%d</td></tr>",
			comp.GroupID, comp.ArtifactID, comp.Version,
			len(comp.Vulnerabilities), len(comp.LicenseViolations))
	}
	html += `</table>`

	html += `<h2>Vulnerabilities</h2><table border="1"><tr><th>CVE</th><th>Severity</th><th>CWE</th><th>Component</th><th>Description</th></tr>`
	for _, comp := range components {
		for _, vuln := range comp.Vulnerabilities {
			html += fmt.Sprintf("<tr><td>%s</td><td>%s</td><td>%s</td><td>%s:%s</td><td>%s</td></tr>",
				vuln.CVE, vuln.Severity, vuln.CWE, comp.GroupID, comp.ArtifactID, vuln.Description)
		}
	}
	html += `</table></body></html>`

	return os.WriteFile(htmlPath, []byte(html), 0644)
}

// Data structures

type ScanJob struct {
	JobID     string `json:"job_id"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	ReportID  string `json:"report_id"`
	CommitSHA string `json:"commit_sha"`
	Repo      string `json:"repo"`
}

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

type ApplicationViolation struct {
	PolicyName      string `json:"policyName"`
	ComponentID     string `json:"componentId"`
	Severity        string `json:"severity"`
	ViolationType   string `json:"violationType"`
	Component       string `json:"component"`
}

type FOSSReport struct {
	ApplicationID string          `json:"application_id"`
	Components    []FOSSComponent `json:"components"`
}

type FOSSComponent struct {
	ComponentID        string              `json:"component_id"`
	GroupID            string              `json:"group_id"`
	ArtifactID         string              `json:"artifact_id"`
	Version            string              `json:"version"`
	CPE                string              `json:"cpe"`
	Hash               string              `json:"hash"`
	Vulnerabilities    []ComponentVulnerability `json:"vulnerabilities"`
	LicenseViolations  []LicenseViolation  `json:"license_violations"`
	RecommendedVersion string              `json:"recommended_version"`
	LatestVersion      string              `json:"latest_version"`
}

type ComponentVulnerability struct {
	CVE         string `json:"cve"`
	Severity    string `json:"severity"`
	CWE         string `json:"cwe"`
	Description string `json:"description"`
}

type LicenseViolation struct {
	License       string `json:"license"`
	PolicyName    string `json:"policy_name"`
	PolicySeverity string `json:"policy_severity"`
}

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

type WebSolution struct {
	Solutions  []string `json:"solutions"`
	References []string `json:"references"`
	Confidence string   `json:"confidence"`
}
