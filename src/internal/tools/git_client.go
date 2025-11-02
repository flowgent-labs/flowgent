package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// GitClient Git MCP 客户端
type GitClient struct {
	mcp *client.Client
}

// NewGitClient 创建 Git 客户端
func NewGitClient(mcpClient *client.Client) *GitClient {
	return &GitClient{mcp: mcpClient}
}

// GetLatestCommit 获取分支最新 commit SHA
func (g *GitClient) GetLatestCommit(ctx context.Context, repo, branch string) (string, error) {
	if g.mcp == nil {
		return "", fmt.Errorf("git MCP client not connected")
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = "git/get_latest_commit"
	req.Params.Arguments = map[string]any{
		"repo":   repo,
		"branch": branch,
	}

	result, err := g.mcp.CallTool(ctx, req)
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
		CommitSHA string `json:"commit_sha"`
	}
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	return res.CommitSHA, nil
}

// CreateBranch 创建新分支
func (g *GitClient) CreateBranch(ctx context.Context, repo, branch, baseBranch string) error {
	if g.mcp == nil {
		return fmt.Errorf("git MCP client not connected")
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = "git/create_branch"
	req.Params.Arguments = map[string]any{
		"repo":        repo,
		"branch":      branch,
		"base_branch": baseBranch,
	}

	_, err := g.mcp.CallTool(ctx, req)
	if err != nil {
		return fmt.Errorf("MCP call failed: %w", err)
	}

	return nil
}

// CommitAndPush 提交代码并推送
func (g *GitClient) CommitAndPush(ctx context.Context, repo, branch string, changes []FileChange) (string, error) {
	if g.mcp == nil {
		return "", fmt.Errorf("git MCP client not connected")
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = "git/commit_and_push"
	req.Params.Arguments = map[string]any{
		"repo":    repo,
		"branch":  branch,
		"changes": changes,
	}

	result, err := g.mcp.CallTool(ctx, req)
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
		CommitSHA string `json:"commit_sha"`
	}
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	return res.CommitSHA, nil
}

// CreatePullRequest 创建 PR
func (g *GitClient) CreatePullRequest(ctx context.Context, repo, title, body, head, base string) (int, error) {
	if g.mcp == nil {
		return 0, fmt.Errorf("git MCP client not connected")
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = "git/create_pull_request"
	req.Params.Arguments = map[string]any{
		"repo":  repo,
		"title": title,
		"body":  body,
		"head":  head,
		"base":  base,
	}

	result, err := g.mcp.CallTool(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("MCP call failed: %w", err)
	}

	var text string
	for _, content := range result.Content {
		if tc, ok := content.(mcp.TextContent); ok {
			text = tc.Text
			break
		}
	}
	if text == "" {
		return 0, fmt.Errorf("empty response from MCP server")
	}

	var res struct {
		PRNumber int `json:"pr_number"`
	}
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		return 0, fmt.Errorf("parse response: %w", err)
	}

	return res.PRNumber, nil
}

// MergePullRequest 合并 PR
func (g *GitClient) MergePullRequest(ctx context.Context, repo string, prNumber int, targetBranch string) error {
	if g.mcp == nil {
		return fmt.Errorf("git MCP client not connected")
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = "git/merge_pull_request"
	req.Params.Arguments = map[string]any{
		"repo":          repo,
		"pr_number":     prNumber,
		"target_branch": targetBranch,
	}

	_, err := g.mcp.CallTool(ctx, req)
	if err != nil {
		return fmt.Errorf("MCP call failed: %w", err)
	}

	return nil
}

// FileChange 文件变更
type FileChange struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Message string `json:"message"`
}
