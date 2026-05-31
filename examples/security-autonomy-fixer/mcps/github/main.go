package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/google/go-github/v59/github"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"golang.org/x/oauth2"
)

var (
	githubToken   string
	githubBaseURL string
	githubClient  *github.Client
)

func ptr[T any](v T) *T {
	return &v
}

func main() {
	githubToken = os.Getenv("GITHUB_TOKEN")
	githubBaseURL = os.Getenv("GITHUB_BASE_URL")

	if githubToken == "" {
		log.Fatal("GITHUB_TOKEN is required")
	}

	ctx := context.Background()
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: githubToken})
	tc := oauth2.NewClient(ctx, ts)

	var err error
	githubClient = github.NewClient(tc)
	if githubBaseURL != "" {
		githubClient, err = githubClient.WithEnterpriseURLs(githubBaseURL, githubBaseURL)
		if err != nil {
			log.Fatalf("Failed to configure enterprise URL: %v", err)
		}
	}

	mcpServer := server.NewMCPServer("github-mcp-server", "1.0.0")

	// Register Git Tools
	mcpServer.AddTool(mcp.NewTool("git/get_latest_commit",
		mcp.WithDescription("Get latest commit SHA from a branch"),
		mcp.WithString("repo", mcp.Description("Repository name in owner/repo format"), mcp.Required()),
		mcp.WithString("branch", mcp.Description("Branch name"), mcp.Required()),
	), getLatestCommitHandler)

	mcpServer.AddTool(mcp.NewTool("git/create_branch",
		mcp.WithDescription("Create a new branch"),
		mcp.WithString("repo", mcp.Description("Repository name"), mcp.Required()),
		mcp.WithString("branch", mcp.Description("New branch name"), mcp.Required()),
		mcp.WithString("base_branch", mcp.Description("Base branch name"), mcp.Required()),
	), createBranchHandler)

	mcpServer.AddTool(mcp.NewTool("git/commit_and_push",
		mcp.WithDescription("Commit changes and push"),
		mcp.WithString("repo", mcp.Description("Repository name"), mcp.Required()),
		mcp.WithString("branch", mcp.Description("Branch name"), mcp.Required()),
		mcp.WithArray("changes", mcp.Description("File changes"), mcp.Required()),
	), commitAndPushHandler)

	mcpServer.AddTool(mcp.NewTool("git/create_pull_request",
		mcp.WithDescription("Create a pull request"),
		mcp.WithString("repo", mcp.Description("Repository name"), mcp.Required()),
		mcp.WithString("title", mcp.Description("PR title"), mcp.Required()),
		mcp.WithString("body", mcp.Description("PR description")),
		mcp.WithString("head", mcp.Description("Head branch"), mcp.Required()),
		mcp.WithString("base", mcp.Description("Base branch"), mcp.Required()),
	), createPullRequestHandler)

	mcpServer.AddTool(mcp.NewTool("git/merge_pull_request",
		mcp.WithDescription("Merge a pull request"),
		mcp.WithString("repo", mcp.Description("Repository name"), mcp.Required()),
		mcp.WithNumber("pr_number", mcp.Description("PR number"), mcp.Required()),
		mcp.WithString("target_branch", mcp.Description("Target branch"), mcp.Required()),
	), mergePullRequestHandler)

	log.Println("GitHub MCP Server started")

	if err := server.ServeStdio(mcpServer); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func getLatestCommitHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	repo := mcp.ParseString(request, "repo", "")
	if repo == "" {
		return nil, fmt.Errorf("repo is required")
	}

	branch := mcp.ParseString(request, "branch", "")
	if branch == "" {
		return nil, fmt.Errorf("branch is required")
	}

	owner, name, err := parseRepo(repo)
	if err != nil {
		return nil, err
	}

	ref, _, err := githubClient.Git.GetRef(ctx, owner, name, "refs/heads/"+branch)
	if err != nil {
		return nil, fmt.Errorf("get ref: %w", err)
	}

	result := fmt.Sprintf(`{"commit_sha":"%s"}`, ref.GetObject().GetSHA())
	return mcp.NewToolResultText(result), nil
}

func createBranchHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	repo := mcp.ParseString(request, "repo", "")
	if repo == "" {
		return nil, fmt.Errorf("repo is required")
	}

	branch := mcp.ParseString(request, "branch", "")
	if branch == "" {
		return nil, fmt.Errorf("branch is required")
	}

	baseBranch := mcp.ParseString(request, "base_branch", "")
	if baseBranch == "" {
		return nil, fmt.Errorf("base_branch is required")
	}

	owner, name, err := parseRepo(repo)
	if err != nil {
		return nil, err
	}

	ref, _, err := githubClient.Git.GetRef(ctx, owner, name, "refs/heads/"+baseBranch)
	if err != nil {
		return nil, fmt.Errorf("get base ref: %w", err)
	}

	newRef := &github.Reference{
		Ref: ptr("refs/heads/" + branch),
		Object: &github.GitObject{
			SHA: ref.Object.SHA,
		},
	}
	_, _, err = githubClient.Git.CreateRef(ctx, owner, name, newRef)
	if err != nil {
		return nil, fmt.Errorf("create ref: %w", err)
	}

	result := fmt.Sprintf(`{"branch":"%s","sha":"%s"}`, branch, ref.GetObject().GetSHA())
	return mcp.NewToolResultText(result), nil
}

func commitAndPushHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	repo := mcp.ParseString(request, "repo", "")
	if repo == "" {
		return nil, fmt.Errorf("repo is required")
	}

	branch := mcp.ParseString(request, "branch", "")
	if branch == "" {
		return nil, fmt.Errorf("branch is required")
	}

	owner, name, err := parseRepo(repo)
	if err != nil {
		return nil, err
	}

	changesRaw := mcp.ParseArgument(request, "changes", nil)
	if changesRaw == nil {
		return nil, fmt.Errorf("changes is required")
	}

	var changes []FileChange
	for _, c := range changesRaw.([]any) {
		cMap, ok := c.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid change format")
		}
		changes = append(changes, FileChange{
			Path:    cMap["path"].(string),
			Content: cMap["content"].(string),
			Message: cMap["message"].(string),
		})
	}

	var lastCommitSHA string
	for _, change := range changes {
		var sha string
		fileContent, _, _, err := githubClient.Repositories.GetContents(ctx, owner, name, change.Path, &github.RepositoryContentGetOptions{Ref: branch})
		if err == nil && fileContent != nil {
			sha = fileContent.GetSHA()
		}

		opts := &github.RepositoryContentFileOptions{
			Message: ptr(change.Message),
			Content: []byte(change.Content),
			Branch:  ptr(branch),
		}
		if sha != "" {
			opts.SHA = ptr(sha)
		}

		resp, _, err := githubClient.Repositories.UpdateFile(ctx, owner, name, change.Path, opts)
		if err != nil {
			return nil, fmt.Errorf("update %s: %w", change.Path, err)
		}

		if resp != nil {
			lastCommitSHA = resp.Commit.GetSHA()
		}
	}

	result := fmt.Sprintf(`{"commit_sha":"%s"}`, lastCommitSHA)
	return mcp.NewToolResultText(result), nil
}

func createPullRequestHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	repo := mcp.ParseString(request, "repo", "")
	if repo == "" {
		return nil, fmt.Errorf("repo is required")
	}

	title := mcp.ParseString(request, "title", "")
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}

	body := mcp.ParseString(request, "body", "")
	head := mcp.ParseString(request, "head", "")
	base := mcp.ParseString(request, "base", "")

	if head == "" {
		return nil, fmt.Errorf("head is required")
	}
	if base == "" {
		return nil, fmt.Errorf("base is required")
	}

	owner, name, err := parseRepo(repo)
	if err != nil {
		return nil, err
	}

	pr, _, err := githubClient.PullRequests.Create(ctx, owner, name, &github.NewPullRequest{
		Title: ptr(title),
		Body:  ptr(body),
		Head:  ptr(head),
		Base:  ptr(base),
	})
	if err != nil {
		return nil, fmt.Errorf("create PR: %w", err)
	}

	result := fmt.Sprintf(`{"pr_number":%d}`, pr.GetNumber())
	return mcp.NewToolResultText(result), nil
}

func mergePullRequestHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	repo := mcp.ParseString(request, "repo", "")
	if repo == "" {
		return nil, fmt.Errorf("repo is required")
	}

	prNumberRaw := mcp.ParseFloat64(request, "pr_number", 0)
	if prNumberRaw == 0 {
		return nil, fmt.Errorf("pr_number is required")
	}
	prNumber := int(prNumberRaw)

	owner, name, err := parseRepo(repo)
	if err != nil {
		return nil, err
	}

	_, _, err = githubClient.PullRequests.Merge(ctx, owner, name, prNumber, "Auto-merged by Security Bot", &github.PullRequestOptions{
		MergeMethod: "squash",
	})
	if err != nil {
		return nil, fmt.Errorf("merge PR: %w", err)
	}

	return mcp.NewToolResultText(`{"success":true}`), nil
}

func parseRepo(repo string) (owner, name string, err error) {
	for i, r := range repo {
		if r == '/' && i > 0 {
			return repo[:i], repo[i+1:], nil
		}
	}
	return "", "", fmt.Errorf("invalid repo format: %s", repo)
}

// FileChange 文件变更
type FileChange struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Message string `json:"message"`
}
