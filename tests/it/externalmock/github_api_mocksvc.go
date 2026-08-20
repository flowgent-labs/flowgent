package externalmock

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

// GitHub API response structures match the real GitHub REST API v3 (GHES 3.21).
// Ref: /home/agent/tmpworks/ghes-3.21.min.json
//
// Schemas verified against the GHES 3.21 OpenAPI spec for these operations:
//
//	pulls/list, pulls/get, pulls/create, pulls/update, pulls/list-files
//	repos/list-commits, repos/get-commit
//	git/create-ref
//	repos/create-or-update-file-contents
//	issues/create, issues/update
//	issues/list-comments, issues/create-comment
//	actions/list-repo-secrets, actions/create-or-update-repo-secret
//
// Wiki is NOT available in GHES 3.21 REST API.

// GHMockPort is the fixed port for Docker Compose MCP containers.
const GHMockPort = ":19002"

// ─── Shared / Simple User ──────────────────────────────────────────────

// GHSimpleUser matches the GHES $simple-user schema.
type GHSimpleUser struct {
	Login             string `json:"login"`
	ID                int64  `json:"id"`
	NodeID            string `json:"node_id"`
	AvatarURL         string `json:"avatar_url"`
	GravatarID        string `json:"gravatar_id"`
	URL               string `json:"url"`
	HTMLURL           string `json:"html_url"`
	FollowersURL      string `json:"followers_url"`
	FollowingURL      string `json:"following_url"`
	GistsURL          string `json:"gists_url"`
	StarredURL        string `json:"starred_url"`
	SubscriptionsURL  string `json:"subscriptions_url"`
	OrganizationsURL  string `json:"organizations_url"`
	ReposURL          string `json:"repos_url"`
	EventsURL         string `json:"events_url"`
	ReceivedEventsURL string `json:"received_events_url"`
	Type              string `json:"type"`
	SiteAdmin         bool   `json:"site_admin"`
	Name              string `json:"name,omitempty"`
	Email             string `json:"email,omitempty"`
}

var ghBotUser = GHSimpleUser{
	Login:      "flowgent-bot",
	ID:         123456,
	NodeID:     "MDQ6VXNlcjEyMzQ1Ng==",
	AvatarURL:  "https://avatars.githubusercontent.com/u/123456?v=4",
	GravatarID: "",
	URL:        "https://api.github.com/users/flowgent-bot",
	HTMLURL:    "https://github.com/flowgent-bot",
	Type:       "User",
	SiteAdmin:  false,
}

// ─── Pull Requests ─────────────────────────────────────────────────────

// GHLabel matches a label object in a PR.
type GHLabel struct {
	ID          int64  `json:"id"`
	NodeID      string `json:"node_id"`
	URL         string `json:"url"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Color       string `json:"color"`
	Default     bool   `json:"default"`
}

// GHMilestone matches the $milestone schema.
type GHMilestone struct {
	URL          string        `json:"url"`
	HTMLURL      string        `json:"html_url"`
	LabelsURL    string        `json:"labels_url"`
	ID           int           `json:"id"`
	NodeID       string        `json:"node_id"`
	Number       int           `json:"number"`
	State        string        `json:"state"`
	Title        string        `json:"title"`
	Description  string        `json:"description"`
	Creator      *GHSimpleUser `json:"creator"`
	OpenIssues   int           `json:"open_issues"`
	ClosedIssues int           `json:"closed_issues"`
	CreatedAt    string        `json:"created_at"`
	UpdatedAt    string        `json:"updated_at"`
	ClosedAt     string        `json:"closed_at"`
	DueOn        string        `json:"due_on"`
}

// GHPullRequest matches the GHES $pull-request schema (full).
// Includes both pull-request-simple fields and the extra fields from pulls/get.
type GHPullRequest struct {
	URL                string         `json:"url"`
	ID                 int64          `json:"id"`
	NodeID             string         `json:"node_id"`
	HTMLURL            string         `json:"html_url"`
	DiffURL            string         `json:"diff_url"`
	PatchURL           string         `json:"patch_url"`
	IssueURL           string         `json:"issue_url"`
	CommitsURL         string         `json:"commits_url"`
	ReviewCommentsURL  string         `json:"review_comments_url"`
	ReviewCommentURL   string         `json:"review_comment_url"`
	CommentsURL        string         `json:"comments_url"`
	StatusesURL        string         `json:"statuses_url"`
	Number             int            `json:"number"`
	State              string         `json:"state"`
	Locked             bool           `json:"locked"`
	Title              string         `json:"title"`
	User               *GHSimpleUser  `json:"user"`
	Body               string         `json:"body"`
	Labels             []GHLabel      `json:"labels"`
	Milestone          *GHMilestone   `json:"milestone"`
	ActiveLockReason   string         `json:"active_lock_reason,omitempty"`
	CreatedAt          string         `json:"created_at"`
	UpdatedAt          string         `json:"updated_at"`
	ClosedAt           any            `json:"closed_at"`
	MergedAt           any            `json:"merged_at"`
	MergeCommitSHA     any            `json:"merge_commit_sha"`
	Assignee           *GHSimpleUser  `json:"assignee"`
	Assignees          []GHSimpleUser `json:"assignees,omitempty"`
	RequestedReviewers []GHSimpleUser `json:"requested_reviewers,omitempty"`
	RequestedTeams     []any          `json:"requested_teams,omitempty"`
	Head               GHRef          `json:"head"`
	Base               GHRef          `json:"base"`
	Links              GHLinks        `json:"_links"`
	AuthorAssociation  string         `json:"author_association"`
	AutoMerge          any            `json:"auto_merge"`
	Draft              bool           `json:"draft"`
	// Extra fields from pulls/get (pull-request extends pull-request-simple)
	Merged              bool          `json:"merged"`
	Mergeable           bool          `json:"mergeable"`
	Rebaseable          bool          `json:"rebaseable,omitempty"`
	MergeableState      string        `json:"mergeable_state"`
	MergedBy            *GHSimpleUser `json:"merged_by"`
	Comments            int           `json:"comments"`
	ReviewComments      int           `json:"review_comments"`
	MaintainerCanModify bool          `json:"maintainer_can_modify"`
	Commits             int           `json:"commits"`
	Additions           int           `json:"additions"`
	Deletions           int           `json:"deletions"`
	ChangedFiles        int           `json:"changed_files"`
}

// GHRef matches the GHES head/base ref object in a PR plus git/refs responses.
type GHRef struct {
	Label string        `json:"label"`
	Ref   string        `json:"ref"`
	SHA   string        `json:"sha"`
	User  *GHSimpleUser `json:"user"`
	Repo  *GHRepoRef    `json:"repo"`
}

// GHRepoRef is a minimal repo reference used in PR head/base.
type GHRepoRef struct {
	ID            int64  `json:"id"`
	NodeID        string `json:"node_id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	HTMLURL       string `json:"html_url"`
	Description   string `json:"description"`
	Fork          bool   `json:"fork"`
	URL           string `json:"url"`
	DefaultBranch string `json:"default_branch"`
}

// GHLinks matches the GHES _links object on a PR.
type GHLinks struct {
	Self           GHLink `json:"self"`
	HTML           GHLink `json:"html"`
	Issue          GHLink `json:"issue"`
	Comments       GHLink `json:"comments"`
	ReviewComments GHLink `json:"review_comments"`
	ReviewComment  GHLink `json:"review_comment"`
	Commits        GHLink `json:"commits"`
	Statuses       GHLink `json:"statuses"`
}

// GHLink is a single href link.
type GHLink struct {
	HREF string `json:"href"`
}

// ─── Pull Request Files (diff-entry) ───────────────────────────────────

// GHPullFile matches the GHES $diff-entry schema.
type GHPullFile struct {
	SHA              string `json:"sha"`
	Filename         string `json:"filename"`
	Status           string `json:"status"`
	Additions        int    `json:"additions"`
	Deletions        int    `json:"deletions"`
	Changes          int    `json:"changes"`
	BlobURL          string `json:"blob_url"`
	RawURL           string `json:"raw_url"`
	ContentsURL      string `json:"contents_url"`
	Patch            string `json:"patch,omitempty"`
	PreviousFilename string `json:"previous_filename,omitempty"`
}

// ─── Commits ───────────────────────────────────────────────────────────

// GHGitUser matches the GHES $git-user schema.
type GHGitUser struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Date  string `json:"date"`
}

// GHVerification matches the GHES $verification schema.
type GHVerification struct {
	Verified   bool   `json:"verified"`
	Reason     string `json:"reason"`
	Payload    string `json:"payload"`
	Signature  string `json:"signature"`
	VerifiedAt string `json:"verified_at"`
}

// GHTree matches the inline tree object in a commit.
type GHTree struct {
	SHA string `json:"sha"`
	URL string `json:"url"`
}

// GHCommit matches the GHES $commit schema.
type GHCommit struct {
	URL         string         `json:"url"`
	SHA         string         `json:"sha"`
	NodeID      string         `json:"node_id"`
	HTMLURL     string         `json:"html_url"`
	CommentsURL string         `json:"comments_url"`
	Commit      GHCommitDetail `json:"commit"`
	Author      *GHSimpleUser  `json:"author"`
	Committer   *GHSimpleUser  `json:"committer"`
	Parents     []GHParent     `json:"parents"`
	Stats       *GHStats       `json:"stats,omitempty"`
	Files       []GHPullFile   `json:"files,omitempty"`
}

// GHCommitDetail matches the nested commit.commit object.
type GHCommitDetail struct {
	URL          string          `json:"url"`
	Author       GHGitUser       `json:"author"`
	Committer    GHGitUser       `json:"committer"`
	Message      string          `json:"message"`
	CommentCount int             `json:"comment_count"`
	Tree         GHTree          `json:"tree"`
	Verification *GHVerification `json:"verification,omitempty"`
}

// GHParent is a parent commit reference.
type GHParent struct {
	SHA     string `json:"sha"`
	URL     string `json:"url"`
	HTMLURL string `json:"html_url,omitempty"`
}

// GHStats matches the commit stats object.
type GHStats struct {
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
	Total     int `json:"total"`
}

// ─── Git Refs ──────────────────────────────────────────────────────────

// GHCreateRefRequest matches POST /repos/{owner}/{repo}/git/refs body.
type GHCreateRefRequest struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

// GHGritRefResponse matches the GHES $git-ref response.
type GHGritRefResponse struct {
	Ref    string       `json:"ref"`
	NodeID string       `json:"node_id"`
	URL    string       `json:"url"`
	Object GHGritObject `json:"object"`
}

// GHGritObject is the object field in a git ref response.
type GHGritObject struct {
	Type string `json:"type"`
	SHA  string `json:"sha"`
	URL  string `json:"url"`
}

// ─── Contents ──────────────────────────────────────────────────────────

// GHCreateContentRequest matches PUT /repos/{owner}/{repo}/contents/{path} body.
type GHCreateContentRequest struct {
	Message   string     `json:"message"`
	Content   string     `json:"content"` // Base64-encoded
	Branch    string     `json:"branch,omitempty"`
	SHA       string     `json:"sha,omitempty"`
	Committer *GHGitUser `json:"committer,omitempty"`
	Author    *GHGitUser `json:"author,omitempty"`
}

// GHContentFile matches the GHES $content-file schema.
type GHContentFile struct {
	Type            string         `json:"type"`
	Encoding        string         `json:"encoding"`
	Size            int            `json:"size"`
	Name            string         `json:"name"`
	Path            string         `json:"path"`
	Content         string         `json:"content"`
	SHA             string         `json:"sha"`
	URL             string         `json:"url"`
	GITURL          string         `json:"git_url"`
	HTMLURL         string         `json:"html_url"`
	DownloadURL     string         `json:"download_url"`
	Links           GHContentLinks `json:"_links"`
	Target          string         `json:"target,omitempty"`
	SubmoduleGITURL string         `json:"submodule_git_url,omitempty"`
}

// GHContentLinks matches the _links in content-file.
type GHContentLinks struct {
	GIT  string `json:"git"`
	HTML string `json:"html"`
	Self string `json:"self"`
}

// GHFileCommit matches the GHES $file-commit schema.
type GHFileCommit struct {
	Content *GHContentFile     `json:"content"`
	Commit  GHFileCommitDetail `json:"commit"`
}

// GHFileCommitDetail is the nested commit in a file-commit response.
type GHFileCommitDetail struct {
	SHA          string          `json:"sha"`
	NodeID       string          `json:"node_id"`
	URL          string          `json:"url"`
	HTMLURL      string          `json:"html_url"`
	Author       GHGitUser       `json:"author"`
	Committer    GHGitUser       `json:"committer"`
	Message      string          `json:"message"`
	Tree         GHTree          `json:"tree"`
	Parents      []GHParent      `json:"parents"`
	Verification *GHVerification `json:"verification,omitempty"`
}

// ─── Issues ────────────────────────────────────────────────────────────

// GHReactionRollup matches the GHES $reaction-rollup schema.
type GHReactionRollup struct {
	URL        string `json:"url"`
	TotalCount int    `json:"total_count"`
	PlusOne    int    `json:"+1"`
	MinusOne   int    `json:"-1"`
	Laugh      int    `json:"laugh"`
	Confused   int    `json:"confused"`
	Heart      int    `json:"heart"`
	Hooray     int    `json:"hooray"`
	Eyes       int    `json:"eyes"`
	Rocket     int    `json:"rocket"`
}

// GHIssue matches the GHES $issue schema.
type GHIssue struct {
	ID                int               `json:"id"`
	NodeID            string            `json:"node_id"`
	URL               string            `json:"url"`
	RepositoryURL     string            `json:"repository_url"`
	LabelsURL         string            `json:"labels_url"`
	CommentsURL       string            `json:"comments_url"`
	EventsURL         string            `json:"events_url"`
	HTMLURL           string            `json:"html_url"`
	Number            int               `json:"number"`
	State             string            `json:"state"`
	StateReason       string            `json:"state_reason,omitempty"`
	Title             string            `json:"title"`
	Body              string            `json:"body,omitempty"`
	User              *GHSimpleUser     `json:"user"`
	Labels            []any             `json:"labels"`
	Assignee          *GHSimpleUser     `json:"assignee"`
	Assignees         []GHSimpleUser    `json:"assignees,omitempty"`
	Milestone         *GHMilestone      `json:"milestone"`
	Locked            bool              `json:"locked"`
	ActiveLockReason  string            `json:"active_lock_reason,omitempty"`
	Comments          int               `json:"comments"`
	PRRef             *GHIssuePRRef     `json:"pull_request,omitempty"`
	ClosedAt          string            `json:"closed_at"`
	CreatedAt         string            `json:"created_at"`
	UpdatedAt         string            `json:"updated_at"`
	Draft             bool              `json:"draft,omitempty"`
	ClosedBy          *GHSimpleUser     `json:"closed_by,omitempty"`
	BodyHTML          string            `json:"body_html,omitempty"`
	BodyText          string            `json:"body_text,omitempty"`
	TimelineURL       string            `json:"timeline_url,omitempty"`
	AuthorAssociation string            `json:"author_association,omitempty"`
	Reactions         *GHReactionRollup `json:"reactions,omitempty"`
}

// GHIssuePRRef is the pull_request field on issues that are PRs.
type GHIssuePRRef struct {
	MergedAt string `json:"merged_at,omitempty"`
	DiffURL  string `json:"diff_url"`
	HTMLURL  string `json:"html_url"`
	PatchURL string `json:"patch_url"`
	URL      string `json:"url"`
}

// GHCreateIssueRequest matches POST /repos/{owner}/{repo}/issues body.
type GHCreateIssueRequest struct {
	Title    string `json:"title"`
	Body     string `json:"body,omitempty"`
	Assignee string `json:"assignee,omitempty"`
	Labels   []any  `json:"labels,omitempty"`
}

// ─── Issue Comments ────────────────────────────────────────────────────

// GHIssueComment matches the GHES $issue-comment schema.
type GHIssueComment struct {
	ID                int               `json:"id"`
	NodeID            string            `json:"node_id"`
	URL               string            `json:"url"`
	Body              string            `json:"body,omitempty"`
	BodyText          string            `json:"body_text,omitempty"`
	BodyHTML          string            `json:"body_html,omitempty"`
	HTMLURL           string            `json:"html_url"`
	User              *GHSimpleUser     `json:"user"`
	CreatedAt         string            `json:"created_at"`
	UpdatedAt         string            `json:"updated_at"`
	IssueURL          string            `json:"issue_url"`
	AuthorAssociation string            `json:"author_association,omitempty"`
	Reactions         *GHReactionRollup `json:"reactions,omitempty"`
}

// ─── Actions Secrets ───────────────────────────────────────────────────

// GHActionsSecret matches the GHES $actions-secret schema.
type GHActionsSecret struct {
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// GHActionsSecretsResponse matches GET /repos/{owner}/{repo}/actions/secrets.
type GHActionsSecretsResponse struct {
	TotalCount int               `json:"total_count"`
	Secrets    []GHActionsSecret `json:"secrets"`
}

// GHActionsCreateSecretRequest matches PUT .../actions/secrets/{name} body.
type GHActionsCreateSecretRequest struct {
	EncryptedValue string `json:"encrypted_value"`
	KeyID          string `json:"key_id"`
}

// ─── Handler ───────────────────────────────────────────────────────────

// ghState holds mutable state across requests for a single handler instance.
type ghState struct {
	mu            sync.Mutex
	prNumber      int
	prFiles       []GHPullFile
	branchCommits []GHCommit
	branchName    string
	repoURL       string // e.g. "https://api.github.com/repos/wl4g/rengine"
}

func ghNow() string { return time.Now().UTC().Format(time.RFC3339) }

func newGitHubHandler(log *RequestLog) http.HandlerFunc {
	st := &ghState{
		repoURL: "https://api.github.com/repos/wl4g/rengine",
	}

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-GitHub-Request-Id", fmt.Sprintf("mock-gh-%d", log.Count("github")))
		var bodyBytes []byte
		if r.Body != nil && r.ContentLength > 0 {
			bodyBytes = make([]byte, r.ContentLength)
			r.Body.Read(bodyBytes)
		}
		body := string(bodyBytes)
		log.Record("github", r, body)

		path := strings.Trim(r.URL.Path, "/")
		parts := strings.Split(path, "/")

		switch {
		// ── GET /repos/{owner}/{repo}/pulls (list) ──
		case r.Method == http.MethodGet && len(parts) == 4 && parts[3] == "pulls":
			json.NewEncoder(w).Encode([]GHPullRequest{newGHPullRequest(st)})

		// ── GET /repos/{owner}/{repo}/pulls/{number} ──
		case r.Method == http.MethodGet && len(parts) == 5 && parts[3] == "pulls" && !strings.HasSuffix(path, "/files") && !strings.HasSuffix(path, "/commits"):
			json.NewEncoder(w).Encode(newGHPullRequest(st))

		// ── GET /repos/{owner}/{repo}/pulls/{number}/files ──
		case r.Method == http.MethodGet && strings.Contains(path, "/pulls/") && strings.Contains(path, "/files"):
			st.mu.Lock()
			if len(st.prFiles) == 0 {
				st.prFiles = []GHPullFile{
					{
						SHA:         "abc123",
						Filename:    "src/main/java/com/wl4g/rengine/Login.java",
						Status:      "modified",
						Additions:   3,
						Deletions:   1,
						Changes:     4,
						BlobURL:     st.repoURL + "/git/blobs/abc123",
						RawURL:      "https://github.com/wl4g/rengine/raw/fix-branch/src/main/java/com/wl4g/rengine/Login.java",
						ContentsURL: st.repoURL + "/contents/src/main/java/com/wl4g/rengine/Login.java",
						Patch:       "@@ -40,5 +40,7 @@\n-    String query = \"SELECT * FROM users WHERE username = '\" + username;\n+    String query = \"SELECT * FROM users WHERE username = ?\";\n+    PreparedStatement ps = conn.prepareStatement(query);\n+    ps.setString(1, username);",
					},
					{
						SHA:         "def456",
						Filename:    "src/test/java/com/wl4g/rengine/LoginTest.java",
						Status:      "added",
						Additions:   15,
						Deletions:   0,
						Changes:     15,
						BlobURL:     st.repoURL + "/git/blobs/def456",
						RawURL:      "https://github.com/wl4g/rengine/raw/fix-branch/src/test/java/com/wl4g/rengine/LoginTest.java",
						ContentsURL: st.repoURL + "/contents/src/test/java/com/wl4g/rengine/LoginTest.java",
					},
				}
			}
			st.mu.Unlock()
			st.mu.Lock()
			files := st.prFiles
			st.mu.Unlock()
			json.NewEncoder(w).Encode(files)

		// ── GET /repos/{owner}/{repo}/commits ──
		case r.Method == http.MethodGet && strings.Contains(path, "/commits"):
			st.mu.Lock()
			if len(st.branchCommits) == 0 {
				st.branchCommits = []GHCommit{newGHCommit("1234567890abcdef", "fix: resolve SonarQube BLOCKER S3649 — parameterize SQL query")}
			}
			commits := st.branchCommits
			st.mu.Unlock()
			json.NewEncoder(w).Encode(commits)

		// ── POST /repos/{owner}/{repo}/git/refs ──
		case r.Method == http.MethodPost && strings.Contains(path, "/git/refs"):
			var req GHCreateRefRequest
			json.Unmarshal(bodyBytes, &req)
			st.mu.Lock()
			st.branchName = strings.TrimPrefix(req.Ref, "refs/heads/")
			st.mu.Unlock()
			json.NewEncoder(w).Encode(GHGritRefResponse{
				Ref:    req.Ref,
				NodeID: "REF_" + req.SHA[:8],
				URL:    st.repoURL + "/git/refs/" + req.Ref,
				Object: GHGritObject{Type: "commit", SHA: req.SHA, URL: st.repoURL + "/git/commits/" + req.SHA},
			})

		// ── PUT /repos/{owner}/{repo}/contents/{path} ──
		case r.Method == http.MethodPut && strings.Contains(path, "/contents/"):
			var req GHCreateContentRequest
			json.Unmarshal(bodyBytes, &req)
			st.mu.Lock()
			commitSHA := fmt.Sprintf("pushed-%x", len(st.branchCommits))
			st.branchCommits = append(st.branchCommits, newGHCommit(commitSHA, req.Message))
			st.mu.Unlock()

			filePath := extractPathFromContentsURL(path)
			fileName := filePath[strings.LastIndex(filePath, "/")+1:]

			json.NewEncoder(w).Encode(GHFileCommit{
				Content: &GHContentFile{
					Type:        "file",
					Encoding:    "base64",
					Size:        len(req.Content),
					Name:        fileName,
					Path:        filePath,
					Content:     req.Content,
					SHA:         commitSHA,
					URL:         st.repoURL + "/contents/" + filePath,
					GITURL:      st.repoURL + "/git/blobs/" + commitSHA,
					HTMLURL:     "https://github.com/wl4g/rengine/blob/" + req.Branch + "/" + filePath,
					DownloadURL: "https://raw.githubusercontent.com/wl4g/rengine/" + req.Branch + "/" + filePath,
					Links:       GHContentLinks{GIT: st.repoURL + "/git/blobs/" + commitSHA, HTML: "https://github.com/wl4g/rengine/blob/" + req.Branch + "/" + filePath, Self: st.repoURL + "/contents/" + filePath},
				},
				Commit: GHFileCommitDetail{
					SHA:       commitSHA,
					NodeID:    "C_" + commitSHA,
					URL:       st.repoURL + "/commits/" + commitSHA,
					HTMLURL:   "https://github.com/wl4g/rengine/commit/" + commitSHA,
					Author:    GHGitUser{Name: "Flowgent Labs", Email: "bot@flowgent.ai", Date: ghNow()},
					Committer: GHGitUser{Name: "Flowgent Labs", Email: "bot@flowgent.ai", Date: ghNow()},
					Message:   req.Message,
					Tree:      GHTree{SHA: "tree-" + commitSHA, URL: st.repoURL + "/git/trees/tree-" + commitSHA},
					Parents:   []GHParent{{SHA: "abcdef1234567890", URL: st.repoURL + "/commits/abcdef1234567890", HTMLURL: "https://github.com/wl4g/rengine/commit/abcdef1234567890"}},
				},
			})

		// ── POST /repos/{owner}/{repo}/pulls (create PR) ──
		case r.Method == http.MethodPost && strings.HasSuffix(strings.TrimRight(path, "/"), "/pulls"):
			var req struct {
				Title string `json:"title"`
				Head  string `json:"head"`
				Base  string `json:"base"`
				Body  string `json:"body"`
			}
			json.Unmarshal(bodyBytes, &req)
			w.WriteHeader(http.StatusCreated)
			st.mu.Lock()
			st.branchName = req.Head
			st.mu.Unlock()
			json.NewEncoder(w).Encode(newGHPullRequest(st))

		// ── POST /repos/{owner}/{repo}/issues ──
		case r.Method == http.MethodPost && strings.Contains(path, "/issues") && !strings.Contains(path, "/comments"):
			var req GHCreateIssueRequest
			json.Unmarshal(bodyBytes, &req)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(GHIssue{
				ID:                2001,
				NodeID:            "I_issue_2001",
				URL:               st.repoURL + "/issues/1",
				RepositoryURL:     st.repoURL,
				LabelsURL:         st.repoURL + "/issues/1/labels{/name}",
				CommentsURL:       st.repoURL + "/issues/1/comments",
				EventsURL:         st.repoURL + "/issues/1/events",
				HTMLURL:           "https://github.com/wl4g/rengine/issues/1",
				Number:            1,
				State:             "open",
				Title:             req.Title,
				Body:              req.Body,
				User:              &ghBotUser,
				Labels:            req.Labels,
				Assignee:          &ghBotUser,
				Locked:            false,
				Comments:          0,
				CreatedAt:         ghNow(),
				UpdatedAt:         ghNow(),
				AuthorAssociation: "COLLABORATOR",
			})

		// ── POST /repos/{owner}/{repo}/issues/{number}/comments ──
		case r.Method == http.MethodPost && strings.Contains(path, "/issues/") && strings.Contains(path, "/comments"):
			var req struct {
				Body string `json:"body"`
			}
			json.Unmarshal(bodyBytes, &req)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(GHIssueComment{
				ID:                99,
				NodeID:            "IC_comment_99",
				URL:               st.repoURL + "/issues/comments/99",
				HTMLURL:           "https://github.com/wl4g/rengine/pull/4#issuecomment-99",
				Body:              req.Body,
				BodyText:          req.Body,
				BodyHTML:          "<p>" + req.Body + "</p>",
				User:              &ghBotUser,
				CreatedAt:         ghNow(),
				UpdatedAt:         ghNow(),
				IssueURL:          st.repoURL + "/issues/4",
				AuthorAssociation: "COLLABORATOR",
			})

		// ── GET /repos/{owner}/{repo}/issues/{number}/comments ──
		case r.Method == http.MethodGet && strings.Contains(path, "/issues/") && strings.Contains(path, "/comments"):
			json.NewEncoder(w).Encode([]GHIssueComment{{
				ID:                99,
				NodeID:            "IC_comment_99",
				URL:               st.repoURL + "/issues/comments/99",
				HTMLURL:           "https://github.com/wl4g/rengine/pull/4#issuecomment-99",
				Body:              "Flowgent security-autonomy-fixer report: 1 BLOCKER fixed.",
				User:              &ghBotUser,
				CreatedAt:         ghNow(),
				UpdatedAt:         ghNow(),
				IssueURL:          st.repoURL + "/issues/4",
				AuthorAssociation: "COLLABORATOR",
			}})

		// ── GET /repos/{owner}/{repo}/actions/secrets ──
		case r.Method == http.MethodGet && strings.Contains(path, "/actions/secrets"):
			json.NewEncoder(w).Encode(GHActionsSecretsResponse{
				TotalCount: 2,
				Secrets: []GHActionsSecret{
					{Name: "SONARQUBE_TOKEN", CreatedAt: ghNow(), UpdatedAt: ghNow()},
					{Name: "DEPLOY_KEY", CreatedAt: ghNow(), UpdatedAt: ghNow()},
				},
			})

		// ── PUT /repos/{owner}/{repo}/actions/secrets/{secret_name} ──
		case r.Method == http.MethodPut && strings.Contains(path, "/actions/secrets/"):
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{})

		default:
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]any{
				"message":           "Not Found",
				"documentation_url": "https://docs.github.com/rest",
			})
		}
	}
}

// ─── Helpers ───────────────────────────────────────────────────────────

func newGHPullRequest(st *ghState) GHPullRequest {
	st.mu.Lock()
	prNum := st.prNumber
	branch := st.branchName
	st.mu.Unlock()
	if prNum == 0 {
		prNum = 4
	}
	if branch == "" {
		branch = "fix/flowgent_sec_auto_fix"
	}
	now := ghNow()

	return GHPullRequest{
		URL:               st.repoURL + "/pulls/" + fmt.Sprintf("%d", prNum),
		ID:                1000 + int64(prNum),
		NodeID:            fmt.Sprintf("PR_%d", prNum),
		HTMLURL:           fmt.Sprintf("https://github.com/wl4g/rengine/pull/%d", prNum),
		DiffURL:           fmt.Sprintf("https://github.com/wl4g/rengine/pull/%d.diff", prNum),
		PatchURL:          fmt.Sprintf("https://github.com/wl4g/rengine/pull/%d.patch", prNum),
		IssueURL:          st.repoURL + "/issues/" + fmt.Sprintf("%d", prNum),
		CommitsURL:        st.repoURL + "/pulls/" + fmt.Sprintf("%d", prNum) + "/commits",
		ReviewCommentsURL: st.repoURL + "/pulls/" + fmt.Sprintf("%d", prNum) + "/comments",
		ReviewCommentURL:  st.repoURL + "/pulls/comments{/number}",
		CommentsURL:       st.repoURL + "/issues/" + fmt.Sprintf("%d", prNum) + "/comments",
		StatusesURL:       st.repoURL + "/statuses/abcdef1234567890",
		Number:            prNum,
		State:             "open",
		Locked:            false,
		Title:             "fix: resolve SonarQube BLOCKER S3649",
		User:              &ghBotUser,
		Body:              "Automated security fix generated by Flowgent.",
		Labels:            []GHLabel{},
		Milestone:         nil,
		CreatedAt:         now,
		UpdatedAt:         now,
		ClosedAt:          nil,
		MergedAt:          nil,
		MergeCommitSHA:    nil,
		Assignee:          &ghBotUser,
		Head: GHRef{
			Label: "wl4g:" + branch,
			Ref:   branch,
			SHA:   "1234567890abcdef",
			User:  &ghBotUser,
			Repo:  newGHRepoRef(),
		},
		Base: GHRef{
			Label: "wl4g:main",
			Ref:   "main",
			SHA:   "abcdef1234567890",
			User:  &ghBotUser,
			Repo:  newGHRepoRef(),
		},
		Links: GHLinks{
			Self:           GHLink{HREF: st.repoURL + "/pulls/" + fmt.Sprintf("%d", prNum)},
			HTML:           GHLink{HREF: fmt.Sprintf("https://github.com/wl4g/rengine/pull/%d", prNum)},
			Issue:          GHLink{HREF: st.repoURL + "/issues/" + fmt.Sprintf("%d", prNum)},
			Comments:       GHLink{HREF: st.repoURL + "/issues/" + fmt.Sprintf("%d", prNum) + "/comments"},
			ReviewComments: GHLink{HREF: st.repoURL + "/pulls/" + fmt.Sprintf("%d", prNum) + "/comments"},
			ReviewComment:  GHLink{HREF: st.repoURL + "/pulls/comments{/number}"},
			Commits:        GHLink{HREF: st.repoURL + "/pulls/" + fmt.Sprintf("%d", prNum) + "/commits"},
			Statuses:       GHLink{HREF: st.repoURL + "/statuses/1234567890abcdef"},
		},
		AuthorAssociation:   "COLLABORATOR",
		AutoMerge:           nil,
		Draft:               false,
		Merged:              false,
		Mergeable:           true,
		MergeableState:      "clean",
		MergedBy:            nil,
		Comments:            0,
		ReviewComments:      0,
		MaintainerCanModify: true,
		Commits:             1,
		Additions:           3,
		Deletions:           1,
		ChangedFiles:        1,
	}
}

func newGHRepoRef() *GHRepoRef {
	return &GHRepoRef{
		ID:            12345,
		NodeID:        "R_rengine",
		Name:          "rengine",
		FullName:      "wl4g/rengine",
		Private:       false,
		HTMLURL:       "https://github.com/wl4g/rengine",
		Description:   "Enterprise rule engine",
		Fork:          false,
		URL:           "https://api.github.com/repos/wl4g/rengine",
		DefaultBranch: "main",
	}
}

func newGHCommit(sha, message string) GHCommit {
	now := ghNow()
	return GHCommit{
		URL:         "https://api.github.com/repos/wl4g/rengine/commits/" + sha,
		SHA:         sha,
		NodeID:      "C_" + sha,
		HTMLURL:     "https://github.com/wl4g/rengine/commit/" + sha,
		CommentsURL: "https://api.github.com/repos/wl4g/rengine/commits/" + sha + "/comments",
		Commit: GHCommitDetail{
			URL:          "https://api.github.com/repos/wl4g/rengine/git/commits/" + sha,
			Author:       GHGitUser{Name: "Flowgent Labs", Email: "bot@flowgent.ai", Date: now},
			Committer:    GHGitUser{Name: "Flowgent Labs", Email: "bot@flowgent.ai", Date: now},
			Message:      message,
			CommentCount: 0,
			Tree:         GHTree{SHA: "tree-" + sha, URL: "https://api.github.com/repos/wl4g/rengine/git/trees/tree-" + sha},
			Verification: &GHVerification{Verified: true, Reason: "valid", Payload: "", Signature: "", VerifiedAt: now},
		},
		Author:    &ghBotUser,
		Committer: &ghBotUser,
		Parents:   []GHParent{{SHA: "abcdef1234567890", URL: "https://api.github.com/repos/wl4g/rengine/commits/abcdef1234567890", HTMLURL: "https://github.com/wl4g/rengine/commit/abcdef1234567890"}},
		Stats:     &GHStats{Additions: 3, Deletions: 1, Total: 4},
	}
}

// extractPathFromContentsURL extracts the file path from a URL like
// "repos/wl4g/rengine/contents/src/main/java/Foo.java".
func extractPathFromContentsURL(path string) string {
	idx := strings.Index(path, "/contents/")
	if idx < 0 {
		return path
	}
	return path[idx+len("/contents/"):]
}

// ─── Server constructors ───────────────────────────────────────────────

// NewMockGitHubAPI returns an httptest server speaking the real GitHub REST v3 API.
func NewMockGitHubAPI(log *RequestLog) *httptest.Server {
	return httptest.NewServer(newGitHubHandler(log))
}

// NewFixedPortGitHubAPI listens on GHMockPort so Docker MCP containers can
// reach the mock via host.docker.internal.
func NewFixedPortGitHubAPI(log *RequestLog) *http.Server {
	srv := &http.Server{Addr: GHMockPort, Handler: newGitHubHandler(log)}
	go func() { _ = srv.ListenAndServe() }()
	return srv
}
