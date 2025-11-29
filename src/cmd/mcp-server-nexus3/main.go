package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/go-resty/resty/v2"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

var (
	nexusURL  string
	nexusUser string
	nexusPass string
	nexusCli  *resty.Client
)

func main() {
	nexusURL = os.Getenv("NEXUS3_URL")
	if nexusURL == "" {
		nexusURL = "http://localhost:8081"
	}
	nexusUser = os.Getenv("NEXUS3_USER")
	if nexusUser == "" {
		nexusUser = "admin"
	}
	nexusPass = os.Getenv("NEXUS3_PASSWORD")

	nexusCli = resty.New()
	nexusCli.SetBaseURL(nexusURL)
	if nexusPass != "" {
		nexusCli.SetBasicAuth(nexusUser, nexusPass)
	}

	mcpServer := server.NewMCPServer("sonatype-nexus3-mcp-server", "1.0.0")

	mcpServer.AddTool(mcp.NewTool("get_foss_solution",
		mcp.WithDescription("Get FOSS artifact compliance and component info from Nexus Repository for a given repository/project"),
		mcp.WithString("repo", mcp.Description("Repository or project name (e.g. org/repo)"), mcp.Required()),
	), getFOSSSolutionHandler)

	mcpServer.AddTool(mcp.NewTool("search_components",
		mcp.WithDescription("Search components in Nexus Repository by name, group, or repository"),
		mcp.WithString("name", mcp.Description("Component name to search for")),
		mcp.WithString("group", mcp.Description("Component group ID")),
		mcp.WithString("repository", mcp.Description("Nexus repository name (e.g. maven-central)")),
	), searchComponentsHandler)

	mcpServer.AddTool(mcp.NewTool("get_component_details",
		mcp.WithDescription("Get detailed info about a specific component including versions and vulnerabilities"),
		mcp.WithString("component_id", mcp.Description("Component ID"), mcp.Required()),
	), getComponentDetailsHandler)

	log.Println("Sonatype Nexus3 MCP Server started")

	if err := server.ServeStdio(mcpServer); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func getFOSSSolutionHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	repo := mcp.ParseString(request, "repo", "")
	if repo == "" {
		return nil, fmt.Errorf("repo is required")
	}

	// Extract artifact/project name from repo path
	projectName := repo
	if idx := strings.Index(repo, "/"); idx >= 0 {
		projectName = repo[idx+1:]
	}

	components, err := searchNexusComponents(projectName)
	if err != nil {
		return nil, fmt.Errorf("search components: %w", err)
	}

	var result CompResult
	result.Repo = repo
	result.Components = components

	// Summarize FOSS compliance status
	vulnCount := 0
	licenseIssues := 0
	for _, c := range components {
		if c.HasVulnerabilities {
			vulnCount++
		}
		if c.LicenseRisk != "" && c.LicenseRisk != "none" {
			licenseIssues++
		}
	}
	result.Summary = CompSummary{
		TotalComponents:     len(components),
		VulnerableCount:     vulnCount,
		LicenseIssueCount:   licenseIssues,
		CompliantCount:      len(components) - vulnCount - licenseIssues,
	}

	data, _ := json.Marshal(result)
	return mcp.NewToolResultText(string(data)), nil
}

func searchComponentsHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := mcp.ParseString(request, "name", "")
	group := mcp.ParseString(request, "group", "")
	repository := mcp.ParseString(request, "repository", "")

	components, err := searchNexusComponentsByCriteria(name, group, repository)
	if err != nil {
		return nil, fmt.Errorf("search components: %w", err)
	}

	data, _ := json.Marshal(components)
	return mcp.NewToolResultText(string(data)), nil
}

func getComponentDetailsHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	componentID := mcp.ParseString(request, "component_id", "")
	if componentID == "" {
		return nil, fmt.Errorf("component_id is required")
	}

	detail, err := fetchNexusComponentDetail(componentID)
	if err != nil {
		return nil, fmt.Errorf("fetch component detail: %w", err)
	}

	data, _ := json.Marshal(detail)
	return mcp.NewToolResultText(string(data)), nil
}

// ─── Nexus3 API helpers ──────────────────────────────────────

func searchNexusComponents(name string) ([]ComponentInfo, error) {
	return searchNexusComponentsByCriteria(name, "", "")
}

func searchNexusComponentsByCriteria(name, group, repository string) ([]ComponentInfo, error) {
	params := map[string]string{}
	if name != "" {
		params["name"] = name
	}
	if group != "" {
		params["group"] = group
	}
	if repository != "" {
		params["repository"] = repository
	}

	var resp struct {
		Items []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Group   string `json:"group"`
			Version string `json:"version"`
			Format  string `json:"format"`
		} `json:"items"`
	}

	res, err := nexusCli.R().
		SetQueryParams(params).
		SetResult(&resp).
		Get("/service/rest/v1/search")

	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	if res.StatusCode() != 200 {
		return nil, fmt.Errorf("API returned status %d", res.StatusCode())
	}

	var components []ComponentInfo
	for _, item := range resp.Items {
		ci := ComponentInfo{
			ID:      item.ID,
			Name:    item.Name,
			Group:   item.Group,
			Version: item.Version,
			Format:  item.Format,
		}
		// Enrich with vulnerability/license info from component detail
		detail, err := fetchNexusComponentDetail(item.ID)
		if err == nil {
			ci.HasVulnerabilities = detail.VulnerabilityCount > 0
			ci.LicenseRisk = detail.LicenseRisk
			ci.Licenses = detail.Licenses
			ci.LatestVersion = detail.LatestVersion
		}
		components = append(components, ci)
	}
	return components, nil
}

func fetchNexusComponentDetail(componentID string) (*ComponentDetail, error) {
	var resp struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		Group          string `json:"group"`
		Version        string `json:"version"`
		Format         string `json:"format"`
		Repository     string `json:"repository"`
		Assets         []struct {
			ID          string `json:"id"`
			DownloadURL string `json:"downloadUrl"`
			Path        string `json:"path"`
			Format      string `json:"format"`
			Checksum    map[string]string `json:"checksum"`
		} `json:"assets"`
	}

	res, err := nexusCli.R().
		SetPathParams(map[string]string{"id": componentID}).
		SetResult(&resp).
		Get("/service/rest/v1/components/{id}")

	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	if res.StatusCode() != 200 {
		return nil, fmt.Errorf("API returned status %d", res.StatusCode())
	}

	detail := &ComponentDetail{
		ID:         resp.ID,
		Name:       resp.Name,
		Group:      resp.Group,
		Version:    resp.Version,
		Format:     resp.Format,
		Repository: resp.Repository,
		Assets:     make([]AssetInfo, len(resp.Assets)),
	}
	for i, a := range resp.Assets {
		detail.Assets[i] = AssetInfo{
			ID:          a.ID,
			DownloadURL: a.DownloadURL,
			Path:        a.Path,
			Format:      a.Format,
		}
	}

	// Attempt to get vulnerability/license info (available with Nexus Lifecycle/IQ integration)
	var vulnResp struct {
		Vulnerabilities []struct {
			CVE        string `json:"cve"`
			Severity   string `json:"severity"`
			CVEScore   float64 `json:"cveScore"`
			Description string `json:"description"`
		} `json:"vulnerabilities"`
		LicenseData struct {
			Licenses []struct {
				Name     string `json:"name"`
				Risk     string `json:"risk"`
				Status   string `json:"status"`
			} `json:"licenses"`
		} `json:"licenseData"`
	}

	vulnRes, vulnErr := nexusCli.R().
		SetPathParams(map[string]string{"id": componentID}).
		SetResult(&vulnResp).
		Get("/service/rest/v1/components/{id}/vulnerabilities")

	if vulnErr == nil && vulnRes.StatusCode() == 200 {
		for _, v := range vulnResp.Vulnerabilities {
			detail.Vulnerabilities = append(detail.Vulnerabilities, VulnInfo{
				CVE:         v.CVE,
				Severity:    v.Severity,
				CVEScore:    v.CVEScore,
				Description: v.Description,
			})
		}
		detail.VulnerabilityCount = len(vulnResp.Vulnerabilities)

		for _, l := range vulnResp.LicenseData.Licenses {
			detail.Licenses = append(detail.Licenses, l.Name)
			if l.Risk != "" && l.Risk != "none" {
				if detail.LicenseRisk == "" || l.Risk == "high" {
					detail.LicenseRisk = l.Risk
				}
			}
		}
		if detail.LicenseRisk == "" {
			detail.LicenseRisk = "none"
		}

		// Check for newer version availability
		if len(resp.Assets) > 0 {
			var versionResp struct {
				Items []struct {
					Version string `json:"version"`
				} `json:"items"`
			}
			verRes, verErr := nexusCli.R().
				SetQueryParams(map[string]string{
					"name":  resp.Name,
					"group": resp.Group,
					"sort":  "version",
				}).
				SetResult(&versionResp).
				Get("/service/rest/v1/search")

			if verErr == nil && verRes.StatusCode() == 200 && len(versionResp.Items) > 0 {
				detail.LatestVersion = versionResp.Items[len(versionResp.Items)-1].Version
			}
		}
	}

	return detail, nil
}

// ─── Data types ──────────────────────────────────────────────

type CompResult struct {
	Repo       string          `json:"repo"`
	Summary    CompSummary     `json:"summary"`
	Components []ComponentInfo `json:"components"`
}

type CompSummary struct {
	TotalComponents   int `json:"total_components"`
	VulnerableCount   int `json:"vulnerable_count"`
	LicenseIssueCount int `json:"license_issue_count"`
	CompliantCount    int `json:"compliant_count"`
}

type ComponentInfo struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Group              string   `json:"group"`
	Version            string   `json:"version"`
	Format             string   `json:"format"`
	HasVulnerabilities bool     `json:"has_vulnerabilities"`
	LicenseRisk        string   `json:"license_risk"`
	Licenses           []string `json:"licenses,omitempty"`
	LatestVersion      string   `json:"latest_version,omitempty"`
}

type ComponentDetail struct {
	ID                 string      `json:"id"`
	Name               string      `json:"name"`
	Group              string      `json:"group"`
	Version            string      `json:"version"`
	Format             string      `json:"format"`
	Repository         string      `json:"repository"`
	Assets             []AssetInfo `json:"assets"`
	Vulnerabilities    []VulnInfo  `json:"vulnerabilities,omitempty"`
	VulnerabilityCount int         `json:"vulnerability_count"`
	Licenses           []string    `json:"licenses,omitempty"`
	LicenseRisk        string      `json:"license_risk"`
	LatestVersion      string      `json:"latest_version,omitempty"`
}

type AssetInfo struct {
	ID          string `json:"id"`
	DownloadURL string `json:"download_url"`
	Path        string `json:"path"`
	Format      string `json:"format"`
}

type VulnInfo struct {
	CVE         string  `json:"cve"`
	Severity    string  `json:"severity"`
	CVEScore    float64 `json:"cve_score"`
	Description string  `json:"description"`
}
