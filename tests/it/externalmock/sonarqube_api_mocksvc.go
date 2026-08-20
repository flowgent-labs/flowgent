package externalmock

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
)

// SonarQube API response structures match the real SonarQube REST API 100%.
// Ref: SonarQube 2026.4.0 OAS + MCPfather virtualTools jq expressions.
//
// The virtual tools get_newcode_issues / get_overall_issues chain:
//
//	GetIssuesSearch → foreach issue.key → GetSourcesIssueSnippets → emit minimal fields + codeSnippets
//
// Every field referenced by a jq expression MUST be present in the mock response.

// SQMockPort is the fixed port for Docker Compose MCP containers.
const SQMockPort = ":19001"

// ─── GetIssuesSearch / GetIssuesList ────────────────────────────────────

// SQTextRange matches the real SonarQube textRange object.
type SQTextRange struct {
	StartLine int `json:"startLine"`
	EndLine   int `json:"endLine"`
}

// SQIssue matches a single item in GET /api/issues/search → .issues[].
// Fields are exactly those referenced by the virtual-tool jq expressions:
//
//	key, rule, severity, type, message, component,
//	startLine: (.textRange.startLine // .line),
//	endLine:   (.textRange.endLine   // .line),
//	debt
//
// Additional standard SonarQube fields (status, resolution, creationDate)
// are included for completeness.
type SQIssue struct {
	Key          string       `json:"key"`
	Rule         string       `json:"rule"`
	Severity     string       `json:"severity"`
	Type         string       `json:"type"`
	Message      string       `json:"message"`
	Component    string       `json:"component"`
	TextRange    *SQTextRange `json:"textRange,omitempty"`
	Line         int          `json:"line,omitempty"`
	Debt         string       `json:"debt,omitempty"`
	Status       string       `json:"status,omitempty"`
	Resolution   string       `json:"resolution,omitempty"`
	CreationDate string       `json:"creationDate,omitempty"`
	UpdateDate   string       `json:"updateDate,omitempty"`
	CloseDate    string       `json:"closeDate,omitempty"`
	Assignee     string       `json:"assignee,omitempty"`
	Author       string       `json:"author,omitempty"`
	Tags         []string     `json:"tags,omitempty"`
	Project      string       `json:"project,omitempty"`
}

// SQIssuesResponse matches GET /api/issues/search and GET /api/issues/list.
// Pagination fields p/ps/total are referenced by the SonarQube API spec.
type SQIssuesResponse struct {
	Total       int       `json:"total"`
	P           int       `json:"p"`
	PS          int       `json:"ps"`
	Issues      []SQIssue `json:"issues"`
	EffortTotal int       `json:"effortTotal,omitempty"`
	DebtTotal   int       `json:"debtTotal,omitempty"`
}

// ─── GetSourcesIssueSnippets ───────────────────────────────────────────

// SQSnippetCodeLine is a single line of source code in a snippet block.
// The jq expression ($snippet | .[] | .sources[]?) pipes into
//
//	{ lineNumber: .line, code: (.code | gsub(...)) }
//
// so each element needs .line and .code.
type SQSnippetCodeLine struct {
	Line int    `json:"line"`
	Code string `json:"code"`
}

// SQSnippetBlock holds one contiguous block of source lines around an issue.
// In the real SonarQube response, .sources is an array of blocks.
type SQSnippetBlock struct {
	Sources [][]SQSnippetCodeLine `json:"sources"`
}

// SQSnippetsResponse matches GET /api/sources/issue_snippets.
// Top-level keys are component keys (e.g. "rengine:src/main/java/Login.java").
// The jq uses .[] to iterate top-level values, then .sources[]? for blocks.
type SQSnippetsResponse map[string]SQSnippetBlock

// ─── GetComponentsShow ─────────────────────────────────────────────────

// SQComponent matches GET /api/components/show → .component.
type SQComponent struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Qualifier string `json:"qualifier"`
	Path      string `json:"path,omitempty"`
	Language  string `json:"language,omitempty"`
	Project   string `json:"project,omitempty"`
}

// SQAncestor is an ancestor component in the hierarchy.
type SQAncestor struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Qualifier string `json:"qualifier"`
}

// SQComponentsShowResponse matches GET /api/components/show.
type SQComponentsShowResponse struct {
	Component SQComponent  `json:"component"`
	Ancestors []SQAncestor `json:"ancestors,omitempty"`
}

// ─── GetProjectsExportFindings ─────────────────────────────────────────

// SQExportFindingsResponse matches GET /api/projects/export_findings.
type SQExportFindingsResponse struct {
	ExportedAt string      `json:"exported_at"`
	Project    SQComponent `json:"project"`
	Branch     string      `json:"branch,omitempty"`
	Issues     []SQIssue   `json:"issues"`
	Hotspots   []any       `json:"hotspots,omitempty"`
}

// ─── GetQualityProfilesSearch / Show ────────────────────────────────────

// SQQualityProfile matches a profile item in GET /api/qualityprofiles/search.
type SQQualityProfile struct {
	Key                       string `json:"key"`
	Name                      string `json:"name"`
	Language                  string `json:"language"`
	LanguageName              string `json:"languageName"`
	IsDefault                 bool   `json:"isDefault,omitempty"`
	IsInherited               bool   `json:"isInherited,omitempty"`
	ActiveRuleCount           int    `json:"activeRuleCount,omitempty"`
	ActiveDeprecatedRuleCount int    `json:"activeDeprecatedRuleCount,omitempty"`
	RulesUpdatedAt            string `json:"rulesUpdatedAt,omitempty"`
	UserUpdatedAt             string `json:"userUpdatedAt,omitempty"`
	LastUsed                  string `json:"lastUsed,omitempty"`
	ParentKey                 string `json:"parentKey,omitempty"`
	ParentName                string `json:"parentName,omitempty"`
	ProjectCount              int    `json:"projectCount,omitempty"`
}

// SQQualityProfilesSearchResponse matches GET /api/qualityprofiles/search.
type SQQualityProfilesSearchResponse struct {
	Profiles []SQQualityProfile `json:"profiles"`
}

// SQQualityProfileShowResponse matches GET /api/qualityprofiles/show.
type SQQualityProfileShowResponse struct {
	Profile           SQQualityProfile `json:"profile"`
	CompareToSonarWay *struct {
		Profile          string `json:"profile"`
		ProfileName      string `json:"profileName"`
		MissingRuleCount int    `json:"missingRuleCount"`
	} `json:"compareToSonarWay,omitempty"`
}

// ─── Handler ───────────────────────────────────────────────────────────

func newSonarQubeHandler(log *RequestLog) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var body string
		if r.Body != nil && r.ContentLength > 0 {
			b := make([]byte, r.ContentLength)
			r.Body.Read(b)
			body = string(b)
		}
		log.Record("sonarqube", r, body)

		path := strings.TrimRight(r.URL.Path, "/")

		switch path {
		case "/api/issues/search":
			handleSQIssuesSearch(w, r)
		case "/api/issues/list":
			handleSQIssuesList(w, r)
		case "/api/sources/issue_snippets":
			handleSQSourceIssueSnippets(w, r)
		case "/api/components/show":
			handleSQComponentsShow(w, r)
		case "/api/projects/export_findings":
			handleSQProjectsExportFindings(w, r)
		case "/api/qualityprofiles/search":
			handleSQQualityProfilesSearch(w, r)
		case "/api/qualityprofiles/show":
			handleSQQualityProfilesShow(w, r)

		// Legacy / diagnostic endpoints
		case "/api/project_analyses/search":
			json.NewEncoder(w).Encode(map[string]any{
				"analyses": []map[string]any{{"key": "AXYz...", "date": "2026-07-15T10:00:00+0000"}},
			})
		case "/api/system/health":
			json.NewEncoder(w).Encode(map[string]string{"status": "UP"})

		default:
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]any{
				"errors": []map[string]string{{"msg": fmt.Sprintf("Not found: %s", r.URL.Path)}},
			})
		}
	}
}

func issueQueryParams(r *http.Request) (components, branch, pullRequest, types, resolved, inNewCodePeriod string, ps int) {
	q := r.URL.Query()
	components = q.Get("components")
	branch = q.Get("branch")
	pullRequest = q.Get("pullRequest")
	types = q.Get("types")
	resolved = q.Get("resolved")
	inNewCodePeriod = q.Get("inNewCodePeriod")
	if v := q.Get("ps"); v != "" {
		fmt.Sscanf(v, "%d", &ps)
	}
	if ps <= 0 {
		ps = 100
	}
	return
}

func handleSQIssuesSearch(w http.ResponseWriter, r *http.Request) {
	components, branch, pullRequest, types, resolved, inNewCodePeriod, ps := issueQueryParams(r)
	_ = components
	_ = types
	_ = resolved
	_ = inNewCodePeriod

	var issues []SQIssue
	// Return an issue only when branch / pullRequest matches a known scenario.
	if branch != "" || pullRequest != "" {
		issues = []SQIssue{{
			Key:       "AYsqG5NQpS_lL_6_XS_7",
			Rule:      "java:S3649",
			Severity:  "BLOCKER",
			Type:      "VULNERABILITY",
			Message:   "Use PreparedStatement instead of string concatenation to prevent SQL injection.",
			Component: "rengine:src/main/java/com/wl4g/rengine/Login.java",
			TextRange: &SQTextRange{StartLine: 40, EndLine: 44},
			Line:      42,
			Debt:      "30min",
			Status:    "OPEN",
			Project:   "rengine",
		}}
	}

	json.NewEncoder(w).Encode(SQIssuesResponse{
		Total:  len(issues),
		P:      1,
		PS:     ps,
		Issues: issues,
	})
}

func handleSQIssuesList(w http.ResponseWriter, r *http.Request) {
	// GetIssuesList uses project/component instead of components
	project := r.URL.Query().Get("project")
	component := r.URL.Query().Get("component")
	_ = project
	_ = component

	var issues []SQIssue
	if project != "" || component != "" {
		issues = []SQIssue{{
			Key:       "AYsqG5NQpS_lL_6_XS_7",
			Rule:      "java:S3649",
			Severity:  "BLOCKER",
			Type:      "VULNERABILITY",
			Message:   "Use PreparedStatement instead of string concatenation to prevent SQL injection.",
			Component: "rengine:src/main/java/com/wl4g/rengine/Login.java",
			TextRange: &SQTextRange{StartLine: 40, EndLine: 44},
			Line:      42,
			Debt:      "30min",
			Status:    "OPEN",
			Project:   "rengine",
		}}
	}

	json.NewEncoder(w).Encode(SQIssuesResponse{
		Total:  len(issues),
		P:      1,
		PS:     100,
		Issues: issues,
	})
}

func handleSQSourceIssueSnippets(w http.ResponseWriter, r *http.Request) {
	issueKey := r.URL.Query().Get("issueKey")
	resp := make(SQSnippetsResponse)

	if issueKey != "" {
		// The jq expression ($snippet | .[] | .sources[]?) iterates top-level
		// values, then their .sources blocks. Each block is an array of {line, code}.
		resp["rengine:src/main/java/com/wl4g/rengine/Login.java"] = SQSnippetBlock{
			Sources: [][]SQSnippetCodeLine{
				{
					{Line: 38, Code: `<span class="k">public</span> <span class="k">class</span> LoginController {`},
					{Line: 39, Code: `  <span class="k">public</span> User <span class="nf">authenticate</span>(String username, String password) {`},
					{Line: 40, Code: `    String query = <span class="s">"SELECT * FROM users WHERE username = '"</span> + username + <span class="s">"' AND password = '"</span> + password + <span class="s">"'"</span>;`},
					{Line: 41, Code: `    <span class="k">return</span> jdbcTemplate.<span class="nf">queryForObject</span>(query, User.class);`},
					{Line: 42, Code: `  }`},
					{Line: 43, Code: `}`},
				},
			},
		}
	}

	json.NewEncoder(w).Encode(resp)
}

func handleSQComponentsShow(w http.ResponseWriter, r *http.Request) {
	componentKey := r.URL.Query().Get("component")
	branch := r.URL.Query().Get("branch")
	pullRequest := r.URL.Query().Get("pullRequest")
	_ = branch
	_ = pullRequest

	json.NewEncoder(w).Encode(SQComponentsShowResponse{
		Component: SQComponent{
			Key:       componentKey,
			Name:      "Login.java",
			Qualifier: "FIL",
			Path:      "src/main/java/com/wl4g/rengine/Login.java",
			Language:  "java",
			Project:   "rengine",
		},
		Ancestors: []SQAncestor{
			{Key: "rengine:src/main/java/com/wl4g/rengine", Name: "com/wl4g/rengine", Qualifier: "DIR"},
			{Key: "rengine", Name: "rengine", Qualifier: "TRK"},
		},
	})
}

func handleSQProjectsExportFindings(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	branch := r.URL.Query().Get("branch")
	pullRequest := r.URL.Query().Get("pullRequest")
	_ = branch
	_ = pullRequest

	json.NewEncoder(w).Encode(SQExportFindingsResponse{
		ExportedAt: "2026-07-15T10:00:00+0000",
		Project:    SQComponent{Key: project, Name: project, Qualifier: "TRK"},
		Issues: []SQIssue{{
			Key:       "AYsqG5NQpS_lL_6_XS_7",
			Rule:      "java:S3649",
			Severity:  "BLOCKER",
			Type:      "VULNERABILITY",
			Message:   "Use PreparedStatement instead of string concatenation to prevent SQL injection.",
			Component: "rengine:src/main/java/com/wl4g/rengine/Login.java",
			TextRange: &SQTextRange{StartLine: 40, EndLine: 44},
			Line:      42,
			Debt:      "30min",
			Status:    "OPEN",
			Project:   project,
		}},
		Hotspots: []any{},
	})
}

func handleSQQualityProfilesSearch(w http.ResponseWriter, r *http.Request) {
	language := r.URL.Query().Get("language")
	defaults := r.URL.Query().Get("defaults")
	_ = defaults

	profiles := []SQQualityProfile{
		{
			Key:                       "java-sonar-way-12345",
			Name:                      "Sonar way",
			Language:                  "java",
			LanguageName:              "Java",
			IsDefault:                 true,
			ActiveRuleCount:           234,
			ActiveDeprecatedRuleCount: 0,
			RulesUpdatedAt:            "2026-07-01T00:00:00+0000",
			UserUpdatedAt:             "2026-06-15T00:00:00+0000",
			LastUsed:                  "2026-07-15T10:00:00+0000",
		},
		{
			Key:          "java-flowgent-way-67890",
			Name:         "Flowgent way",
			Language:     "java",
			LanguageName: "Java",
			IsDefault:    false,
			ParentKey:    "java-sonar-way-12345",
			ParentName:   "Sonar way",
		},
	}

	// Filter by language if requested
	if language != "" {
		var filtered []SQQualityProfile
		for _, p := range profiles {
			if p.Language == language {
				filtered = append(filtered, p)
			}
		}
		profiles = filtered
	}

	json.NewEncoder(w).Encode(SQQualityProfilesSearchResponse{Profiles: profiles})
}

func handleSQQualityProfilesShow(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")

	json.NewEncoder(w).Encode(SQQualityProfileShowResponse{
		Profile: SQQualityProfile{
			Key:                       key,
			Name:                      "Sonar way",
			Language:                  "java",
			LanguageName:              "Java",
			IsDefault:                 true,
			ActiveRuleCount:           234,
			ActiveDeprecatedRuleCount: 0,
			RulesUpdatedAt:            "2026-07-01T00:00:00+0000",
		},
	})
}

// ─── Server constructors ───────────────────────────────────────────────

// NewMockSonarQubeAPI returns an httptest server speaking the real SonarQube
// REST API. Use for in-process IT tests without Docker MCP containers.
func NewMockSonarQubeAPI(log *RequestLog) *httptest.Server {
	return httptest.NewServer(newSonarQubeHandler(log))
}

// NewFixedPortSonarQubeAPI listens on SQMockPort so Docker MCP containers can
// reach the mock via host.docker.internal.
func NewFixedPortSonarQubeAPI(log *RequestLog) *http.Server {
	srv := &http.Server{Addr: SQMockPort, Handler: newSonarQubeHandler(log)}
	go func() { _ = srv.ListenAndServe() }()
	return srv
}
