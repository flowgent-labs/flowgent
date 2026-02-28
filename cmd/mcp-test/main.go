package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

var (
	defaultWorkspace string
)

func main() {
	defaultWorkspace = os.Getenv("TEST_WORKSPACE")
	if defaultWorkspace == "" {
		defaultWorkspace = "/workspace"
	}

	mcpServer := server.NewMCPServer("test-mcp-server", "1.0.0")

	mcpServer.AddTool(mcp.NewTool("test/run_integration",
		mcp.WithDescription("Run integration tests"),
		mcp.WithString("workspace", mcp.Description("Workspace directory"), mcp.Required()),
		mcp.WithString("profile", mcp.Description("Test profile"), mcp.Required()),
	), runIntegrationTestHandler)

	mcpServer.AddTool(mcp.NewTool("test/get_report",
		mcp.WithDescription("Get test report"),
		mcp.WithString("test_run_id", mcp.Description("Test run ID"), mcp.Required()),
	), getReportHandler)

	log.Println("Test MCP Server started (Maven/Cucumber)")

	if err := server.ServeStdio(mcpServer); err != nil {
		log.Fatalf("Server error: %v", err)
	}
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

// TestReport 测试报告
type TestReport struct {
	TestRunID string     `json:"test_run_id"`
	Workspace string     `json:"workspace"`
	Profile   string     `json:"profile"`
	StartTime string     `json:"start_time"`
	EndTime   string     `json:"end_time"`
	TestCases []TestCase `json:"test_cases"`
	Summary   TestSummary `json:"summary"`
}

// TestCase 测试用例
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

var testReports = make(map[string]*TestReport)

func runIntegrationTestHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	workspace := mcp.ParseString(request, "workspace", defaultWorkspace)
	profile := mcp.ParseString(request, "profile", "cucumber-integration")

	testRunID := uuid.New().String()
	startTime := time.Now().Format(time.RFC3339)

	log.Printf("Running integration tests: workspace=%s, profile=%s", workspace, profile)

	cmd := exec.CommandContext(ctx, "mvn", "test", "-P"+profile)
	cmd.Dir = workspace

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startExecTime := time.Now()
	err := cmd.Run()
	execDuration := time.Since(startExecTime)

	endTime := time.Now().Format(time.RFC3339)

	output := stdout.String()
	passed, failed, skipped, total := parseTestOutput(output)

	var failureTypes []string
	if failed > 0 {
		failureTypes = detectFailureTypes(output)
	}

	testResult := &TestResult{
		Success:      err == nil,
		Passed:       passed,
		Failed:       failed,
		Skipped:      skipped,
		Total:        total,
		Duration:     execDuration.String(),
		TestRunID:    testRunID,
		FailureTypes: failureTypes,
	}

	testReport := &TestReport{
		TestRunID: testRunID,
		Workspace: workspace,
		Profile:   profile,
		StartTime: startTime,
		EndTime:   endTime,
		Summary: TestSummary{
			Total:   total,
			Passed:  passed,
			Failed:  failed,
			Skipped: skipped,
		},
		TestCases: parseTestCases(output),
	}
	testReports[testRunID] = testReport

	log.Printf("Test completed: %d passed, %d failed, %d skipped", passed, failed, skipped)

	resultJSON, _ := json.Marshal(testResult)
	return mcp.NewToolResultText(string(resultJSON)), nil
}

func getReportHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	testRunID := mcp.ParseString(request, "test_run_id", "")
	if testRunID == "" {
		return mcp.NewToolResultError("test_run_id is required"), nil
	}

	report, exists := testReports[testRunID]
	if !exists {
		return mcp.NewToolResultError("test report not found: " + testRunID), nil
	}

	resultJSON, _ := json.Marshal(report)
	return mcp.NewToolResultText(string(resultJSON)), nil
}

func parseTestOutput(output string) (passed, failed, skipped, total int) {
	re := regexp.MustCompile(`Tests run: (\d+), Failures: (\d+), Errors: (\d+), Skipped: (\d+)`)
	matches := re.FindStringSubmatch(output)

	if len(matches) >= 5 {
		total, _ = strconv.Atoi(matches[1])
		failures, _ := strconv.Atoi(matches[2])
		errors, _ := strconv.Atoi(matches[3])
		skipped, _ = strconv.Atoi(matches[4])
		failed = failures + errors
		passed = total - failed - skipped
		return
	}

	reSimple := regexp.MustCompile(`Tests run: (\d+)`)
	match := reSimple.FindStringSubmatch(output)
	if len(match) >= 2 {
		total, _ = strconv.Atoi(match[1])
		passed = total
	}

	return
}

func detectFailureTypes(output string) []string {
	var types []string

	if regexp.MustCompile(`(?i)cucumber`).MatchString(output) {
		types = append(types, "cucumber")
	}

	if regexp.MustCompile(`(?i)assertion`).MatchString(output) {
		types = append(types, "assertion")
	}

	if regexp.MustCompile(`(?i)timeout`).MatchString(output) {
		types = append(types, "timeout")
	}

	if regexp.MustCompile(`(?i)connection\s*(refused|error)`).MatchString(output) {
		types = append(types, "connection")
	}

	if regexp.MustCompile(`(?i)http\s*(4\d{2}|5\d{2})`).MatchString(output) {
		types = append(types, "http_error")
	}

	if len(types) == 0 {
		types = append(types, "unknown")
	}

	return types
}

func parseTestCases(output string) []TestCase {
	var testCases []TestCase

	re := regexp.MustCompile(`(?m)^(?:Failed|ERROR):\s*(.+?)\s+(?:--|\n)`)
	matches := re.FindAllStringSubmatch(output, -1)

	for _, m := range matches {
		testCases = append(testCases, TestCase{
			Name:      m[1],
			Status:    "failed",
			ClassName: "",
			Duration:  "",
		})
	}

	if len(testCases) > 100 {
		testCases = testCases[:100]
	}

	return testCases
}
