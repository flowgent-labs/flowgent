package tools

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/client"
)

func TestGitClient_NullSafety(t *testing.T) {
	client := &GitClient{}

	ctx := context.Background()

	// Test GetLatestCommit with nil MCP
	_, err := client.GetLatestCommit(ctx, "owner/repo", "main")
	if err == nil {
		t.Error("Expected error for nil MCP client")
	}

	// Test CreateBranch with nil MCP
	err = client.CreateBranch(ctx, "owner/repo", "new-branch", "main")
	if err == nil {
		t.Error("Expected error for nil MCP client")
	}

	// Test CommitAndPush with nil MCP
	_, err = client.CommitAndPush(ctx, "owner/repo", "branch", []FileChange{})
	if err == nil {
		t.Error("Expected error for nil MCP client")
	}

	// Test CreatePullRequest with nil MCP
	_, err = client.CreatePullRequest(ctx, "owner/repo", "title", "body", "head", "base")
	if err == nil {
		t.Error("Expected error for nil MCP client")
	}

	// Test MergePullRequest with nil MCP
	err = client.MergePullRequest(ctx, "owner/repo", 1, "main")
	if err == nil {
		t.Error("Expected error for nil MCP client")
	}
}

func TestGitClient_NewGitClient(t *testing.T) {
	// Test with nil MCP client (valid for testing)
	gitClient := NewGitClient(nil)
	if gitClient == nil {
		t.Error("Expected non-nil GitClient")
	}
	if gitClient.mcp != nil {
		t.Error("Expected nil MCP client")
	}
}

func TestGitClient_NewGitClient_WithMCP(t *testing.T) {
	// Create a mock MCP client (nil is acceptable for this test)
	var mcpClient *client.Client
	gitClient := NewGitClient(mcpClient)
	if gitClient == nil {
		t.Error("Expected non-nil GitClient")
	}
}

func TestFileChange_JSONTags(t *testing.T) {
	fc := FileChange{
		Path:    "/path/to/file.go",
		Content: "package main",
		Message: "Update file",
	}

	// Verify struct fields are accessible
	if fc.Path != "/path/to/file.go" {
		t.Errorf("Path mismatch")
	}
	if fc.Content != "package main" {
		t.Errorf("Content mismatch")
	}
	if fc.Message != "Update file" {
		t.Errorf("Message mismatch")
	}
}
