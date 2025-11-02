package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// TestClient 测试 MCP 客户端
type TestClient struct {
	mcp *client.Client
}

// NewTestClient 创建测试客户端
func NewTestClient(mcpClient *client.Client) *TestClient {
	return &TestClient{mcp: mcpClient}
}

// RunIntegrationTest 运行集成测试
func (t *TestClient) RunIntegrationTest(ctx context.Context, workspace, profile string) (*TestResult, error) {
	if t.mcp == nil {
		return nil, fmt.Errorf("test MCP client not connected")
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = "test/run_integration"
	req.Params.Arguments = map[string]any{
		"workspace": workspace,
		"profile":   profile,
	}

	result, err := t.mcp.CallTool(ctx, req)
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

	var testResult TestResult
	if err := json.Unmarshal([]byte(text), &testResult); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return &testResult, nil
}

// GetTestReport 获取测试报告详情
func (t *TestClient) GetTestReport(ctx context.Context, testRunID string) (*TestReport, error) {
	if t.mcp == nil {
		return nil, fmt.Errorf("test MCP client not connected")
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = "test/get_report"
	req.Params.Arguments = map[string]any{
		"test_run_id": testRunID,
	}

	result, err := t.mcp.CallTool(ctx, req)
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

	var report TestReport
	if err := json.Unmarshal([]byte(text), &report); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return &report, nil
}

// TestResult 测试结果
type TestResult struct {
	Success      bool     `json:"success"`
	Passed       int      `json:"passed"`
	Failed       int      `json:"failed"`
	Skipped      int      `json:"skipped"`
	Total        int      `json:"total"`
	Duration     string   `json:"duration"`
	TestRunID    string   `json:"test_run_id"`
	FailureTypes []string `json:"failure_types"`
}

// TestReport 测试报告详情
type TestReport struct {
	TestRunID string     `json:"test_run_id"`
	Workspace string     `json:"workspace"`
	Profile   string     `json:"profile"`
	StartTime string     `json:"start_time"`
	EndTime   string     `json:"end_time"`
	TestCases []TestCase `json:"test_cases"`
	Summary   TestSummary `json:"summary"`
}

// TestCase 单个测试用例
type TestCase struct {
	Name      string `json:"name"`
	ClassName string `json:"class_name"`
	Status    string `json:"status"`
	Duration  string `json:"duration"`
	Error     string `json:"error,omitempty"`
}

// TestSummary 测试摘要
type TestSummary struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}
