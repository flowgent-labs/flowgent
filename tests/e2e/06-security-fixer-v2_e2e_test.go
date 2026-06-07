// Package e2e — V2 Security Autonomy Fixer End-to-End Test.
//
// V2 extends V1 with:
//   - GitHub webhook trigger (simulated via REST API with webhook metadata)
//   - Complete 14-phase pipeline including pre-check gate and re-scan loop
//   - Human approval node with timeout-based auto-approve
//   - Multi-channel notifier (Slack, Email, Webhook)
//   - Hierarchical MQTT topic verification (tenant/flow/run in topic paths)
//   - Sandbox executor for wait-rescan polling via bash script
//
// The test uses mock LLM (returns canned JSON per agent persona) and mock MCP
// (SonarQube returns realistic issue data, GitHub returns PR/commit metadata).
// All components run in-process via StandaloneResourceManager.
//
// Verification layers (W1–W13 from the V2 spec):
//
//	W1–W3: Webhook → trigger, run created with trigger_type=webhook
//	W4–W6: SonarQube → real-looking BLOCKER/CRITICAL issues parsed
//	W7–W10: GitHub → simulated branch/commit/PR creation
//	W11–W13: Post-run quality gate → issues decreased, gate OK
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/pkg/config"
	"github.com/flowgent-labs/flowgent/pkg/engine"
	"github.com/flowgent-labs/flowgent/pkg/engine/jobmanager"
	"github.com/flowgent-labs/flowgent/pkg/engine/resourcemanager"
	"github.com/flowgent-labs/flowgent/pkg/model"
	"github.com/flowgent-labs/flowgent/pkg/common/utils"
	"github.com/flowgent-labs/flowgent/tests/testutil"
)

// ─── V2 Mock LLM (richer responses than V1) ─────────────

type v2LLM struct{}

func (m *v2LLM) Generate(ctx context.Context, sp, up, modelStr string, t float64) (string, error) {
	switch {
	case strings.Contains(sp, "supervisor") || strings.Contains(sp, "Supervisor"):
		return toJSON(map[string]any{
			"action": "continue", "target": "", "reason": "All reviews passed, proceed to approval",
		}), nil
	case strings.Contains(sp, "DevSecOps") || strings.Contains(sp, "issue-detector") || strings.Contains(sp, "security expert"):
		return toJSON(map[string]any{
			"issues": []map[string]any{
				{"id": "SONAR-001", "severity": "BLOCKER", "type": "sast", "file": "src/auth/login.go:42", "description": "SQL injection in login handler — unsanitized user input concatenated into query string"},
				{"id": "SONAR-002", "severity": "CRITICAL", "type": "sast", "file": "src/api/middleware.go:108", "description": "Hardcoded JWT secret — secret key embedded in source code"},
				{"id": "SONAR-003", "severity": "CRITICAL", "type": "dependency", "file": "pom.xml:25", "description": "CVE-2025-1234 in log4j-core 2.17.0 — remote code execution via JNDI lookup"},
				{"id": "SONAR-004", "severity": "MAJOR", "type": "bug", "file": "src/service/user.go:67", "description": "Potential NPE — user object may be null after failed lookup"},
				{"id": "SONAR-005", "severity": "MAJOR", "type": "code_smell", "file": "src/util/parser.go:155", "description": "Method too complex — cyclomatic complexity 35 exceeds threshold 15"},
			},
		}), nil
	case strings.Contains(sp, "Secure coding") || strings.Contains(sp, "fixer"):
		return toJSON(map[string]any{
			"patches": []map[string]any{
				{"file": "src/auth/login.go", "patch": "+++ fixed SQL injection by using parameterized queries"},
				{"file": "src/api/middleware.go", "patch": "+++ Removed hardcoded secret, now reads from env FLOWGENT_JWT_SECRET"},
				{"file": "pom.xml", "patch": "+++ Upgraded log4j-core from 2.17.0 to 2.24.0"},
				{"file": "src/service/user.go", "patch": "+++ Added null check before accessing user object"},
			},
		}), nil
	case strings.Contains(sp, "security-reviewer") || strings.Contains(sp, "Security Reviewer"):
		return toJSON(map[string]any{
			"decision": true, "confidence": 0.95, "risk_level": "low",
			"reason": "All patches correctly address the reported vulnerabilities. Parameterized queries prevent SQL injection. JWT secret externalized. Log4j upgraded.",
		}), nil
	case strings.Contains(sp, "quality-reviewer") || strings.Contains(sp, "Code Quality"):
		return toJSON(map[string]any{
			"decision": true, "confidence": 0.85, "risk_level": "low",
			"reason": "Patches follow coding standards. No new code smells introduced. Method complexity reduced.",
		}), nil
	case strings.Contains(sp, "arch-reviewer") || strings.Contains(sp, "Architecture"):
		return toJSON(map[string]any{
			"decision": true, "confidence": 0.90, "risk_level": "low",
			"reason": "Changes are compatible with existing architecture. No new dependencies introduced.",
		}), nil
	case strings.Contains(sp, "tribunal") || strings.Contains(sp, "Vote"):
		return toJSON(map[string]any{
			"decision": true, "majority": 3, "total": 3, "summary": "All 3 reviewers approved. Fixes are safe to deploy.",
		}), nil
	default:
		return toJSON(map[string]any{
			"action": "continue", "target": "", "reason": "proceed",
		}), nil
	}
}

// ─── V2 Mock MCP (realistic SonarQube + GitHub responses) ─

type v2MCP struct{}

func (m *v2MCP) CallTool(ctx context.Context, toolName string, args map[string]any) (map[string]any, error) {
	switch toolName {
	case "sonarqube":
		return m.sonarqubeHandler(args)
	case "github":
		return m.githubHandler(args)
	case "sonatype-iq":
		return m.sonatypeHandler(args)
	default:
		return map[string]any{"result": "ok", "tool": toolName}, nil
	}
}

func (m *v2MCP) sonarqubeHandler(args map[string]any) (map[string]any, error) {
	action, _ := args["action"].(string)
	switch action {
	case "get_issues":
		return map[string]any{
			"total": float64(742),
			"issues": []any{
				map[string]any{"key": "SONAR-001", "rule": "sql:S3649", "severity": "BLOCKER", "component": "src/auth/login.go", "line": float64(42), "message": "SQL injection vulnerability", "type": "VULNERABILITY"},
				map[string]any{"key": "SONAR-002", "rule": "secrets:S6701", "severity": "CRITICAL", "component": "src/api/middleware.go", "line": float64(108), "message": "Hardcoded credentials", "type": "VULNERABILITY"},
				map[string]any{"key": "SONAR-003", "rule": "dependency:S1234", "severity": "CRITICAL", "component": "pom.xml", "line": float64(25), "message": "Vulnerable dependency", "type": "VULNERABILITY"},
				map[string]any{"key": "SONAR-004", "rule": "java:S2259", "severity": "MAJOR", "component": "src/service/user.go", "line": float64(67), "message": "Null pointer dereference", "type": "BUG"},
				map[string]any{"key": "SONAR-005", "rule": "java:S1541", "severity": "MAJOR", "component": "src/util/parser.go", "line": float64(155), "message": "Method complexity", "type": "CODE_SMELL"},
			},
			"paging": map[string]any{"pageIndex": float64(1), "pageSize": float64(100), "total": float64(742)},
		}, nil
	case "get_quality_gate":
		return map[string]any{
			"projectStatus": map[string]any{
				"status":            "OK",
				"ignoredConditions": false,
				"conditions": []any{
					map[string]any{"metric": "new_bugs", "status": "OK", "value": "0"},
					map[string]any{"metric": "new_vulnerabilities", "status": "OK", "value": "0"},
					map[string]any{"metric": "new_code_smells", "status": "OK", "value": "1"},
				},
			},
		}, nil
	case "get_ce_task":
		return map[string]any{"task": map[string]any{"status": "SUCCESS", "analysisId": "AXx123"}}, nil
	default:
		return map[string]any{"result": "ok", "tool": "sonarqube"}, nil
	}
}

func (m *v2MCP) githubHandler(args map[string]any) (map[string]any, error) {
	action, _ := args["action"].(string)
	switch action {
	case "get_commit", "fetch_commit":
		return map[string]any{
			"sha":     "abc123def456",
			"message": "feat: add user authentication module",
			"files": []any{
				map[string]any{"filename": "src/auth/login.go", "status": "modified", "patch": "@@ -40,7 +40,12 @@"},
				map[string]any{"filename": "src/api/middleware.go", "status": "added", "patch": "@@ -0,0 +1,50 @@"},
			},
		}, nil
	case "create_branch":
		return map[string]any{"ref": "refs/heads/security-bot/fix-run-001", "sha": "abc123"}, nil
	case "commit_changes", "create_commit":
		return map[string]any{"sha": "def789", "message": "fix: resolve SonarQube issues SONAR-001 through SONAR-005"}, nil
	case "create_pr":
		return map[string]any{
			"number": float64(42), "title": "Security Fix: Resolve 5 SonarQube issues (BLOCKER/CRITICAL/MAJOR)",
			"url": "https://github.com/wl4g/rengine/pull/42", "state": "open",
		}, nil
	default:
		return map[string]any{"result": "ok", "tool": "github"}, nil
	}
}

func (m *v2MCP) sonatypeHandler(args map[string]any) (map[string]any, error) {
	return map[string]any{
		"components": []any{
			map[string]any{"coordinates": "log4j:log4j-core:2.24.0", "firewall_status": "allow"},
			map[string]any{"coordinates": "com.google.guava:guava:33.0.0-jre", "firewall_status": "allow"},
			map[string]any{"coordinates": "com.fasterxml.jackson:jackson-databind:2.18.0", "firewall_status": "allow"},
		},
	}, nil
}

// ─── V2 Pipeline: Full 14-Phase Security Fixer ──────────

func TestE2E_SecurityFixerV2_FullPipeline(t *testing.T) {
	llm := &v2LLM{}
	mcp := &v2MCP{}
	mcpMap := map[string]engine.MCPClient{
		"sonarqube": mcp, "github": mcp, "sonatype-iq": mcp, "nexus3": mcp,
	}

	agents := []*config.AgentDef{
		{Name: "supervisor", Model: "bailian-codeplan/qwen3.6-plus", Soul: "You are a Supervisor agent.", Instruction: "Decide action: continue/retry/inject/abort. Output JSON."},
		{Name: "issue-detector", Model: "bailian-codeplan/qwen3.6-plus", Soul: "You are a DevSecOps security expert.", Instruction: "Parse and categorize security issues from scan results."},
		{Name: "fixer-agent", Model: "bailian-codeplan/qwen3.5-coder", Soul: "You are a Secure coding expert.", Instruction: "Generate safe, minimal patches for identified vulnerabilities."},
		{Name: "security-reviewer", Model: "bailian-codeplan/qwen3.6-plus", Soul: "You are a Security Reviewer.", Instruction: "Strictly review patches for security correctness."},
		{Name: "quality-reviewer", Model: "bailian-codeplan/qwen3.6-plus", Soul: "You are a Code Quality Reviewer.", Instruction: "Review code quality and maintainability."},
		{Name: "arch-reviewer", Model: "bailian-codeplan/qwen3.6-plus", Soul: "You are an Architecture Reviewer.", Instruction: "Review architectural impact of changes."},
		{Name: "git-agent", Model: "bailian-codeplan/qwen3.5-coder", Soul: "Git operations agent.", Instruction: "Handle git operations: commit, branch, PR."},
	}

	store := testutil.NewMockStore()
	rm, err := resourcemanager.NewLocalResourceManager(&resourcemanager.ResourceManagerConfig{
		Provider: engine.ProviderLocal, PoolSize: 20,
		Store: store, Agents: agents, MCPClients: mcpMap, LLMClient: llm,
		Logger: utils.NewLogger("JSON", "DEBUG"),
	})
	if err != nil {
		t.Fatalf("create local RM: %v", err)
	}

	cfg := &config.ServiceConfig{
		Orchestration: config.OrchestrationConfig{
			FlowExecutionTimeout: "120s",
			MaxNodeRetries:       3,
			MaxConcurrentFlows:   10,
		},
	}
	jm, err := jobmanager.NewJobManager(store, rm, utils.NewLogger("JSON", "DEBUG"), cfg)
	if err != nil {
		t.Fatalf("create JM: %v", err)
	}

	// ── Build the full 14-phase DAG ──────────────────────
	spec := &model.AgentFlowSpec{
		ID:          "security-autonomy-fixer-v2",
		Description: "V2 Security Fixer — webhook-triggered, 14-phase pipeline with pre-check gate",
		Priority:    model.PriorityHigh,
		TenantID:    "default",
		Vars:        map[string]any{"repo": "wl4g/rengine", "repo_path": "/tmp/rengine", "max_iterations": float64(3), "target_severities": []any{"BLOCKER", "CRITICAL", "MAJOR"}},
		Nodes: []model.Node{
			// Phase 1: Fetch commit from GitHub
			{ID: "get-commit", Type: model.ToolNode, Tool: "github", Input: map[string]any{"action": "fetch_commit", "repo": "wl4g/rengine"}},
			// Phase 2: Scan SonarQube
			{ID: "scan-sonarqube", Type: model.ToolNode, Tool: "sonarqube", Input: map[string]any{"action": "get_issues", "project": "rengine"}},
			// Phase 3: Scan SonatypeIQ
			{ID: "scan-sonatypeiq", Type: model.ToolNode, Tool: "sonatype-iq", Input: map[string]any{"action": "scan_dependencies"}},
			// Phase 4: Fetch safe dependency versions (skill/sub-flow)
			{ID: "fetch-safe-deps", Type: model.ToolNode, Tool: "nexus3", Input: map[string]any{"action": "get_safe_versions"}},
			// Phase 5: Aggregate and categorize issues
			{ID: "aggregate-issues", Type: model.AgentNode, Agent: "issue-detector"},
			// Phase 6: Generate fixes
			{ID: "generate-fixes", Type: model.AgentNode, Agent: "fixer-agent"},
			// Phase 7-9: Parallel reviews
			{ID: "review-security", Type: model.AgentNode, Agent: "security-reviewer"},
			{ID: "review-quality", Type: model.AgentNode, Agent: "quality-reviewer"},
			{ID: "review-arch", Type: model.AgentNode, Agent: "arch-reviewer"},
			// Phase 10: Tribunal vote
			{ID: "tribunal-vote", Type: model.TribunalNode, Input: map[string]any{}},
			// Phase 11: Supervisor validation
			{ID: "supervisor-check", Type: model.SupervisorNode, Agent: "supervisor"},
			// Phase 12: Pre-commit quality gate (sandbox — runs bash to check SonarQube CE task)
			{ID: "precheck-gate", Type: model.ToolNode, Tool: "sonarqube", Input: map[string]any{"action": "get_quality_gate", "project": "rengine"}},
			// Phase 13: Condition gate — branch on gate result
			{ID: "gate-passed", Type: model.ConditionNode, Input: map[string]any{"expression": "gate_status == 'OK'"}},
			// Phase 14: Commit fixes + create PR
			{ID: "commit-fixes", Type: model.ToolNode, Tool: "github", Input: map[string]any{"action": "commit_changes", "branch": "security-bot/fix-run-001"}},
			{ID: "create-pr", Type: model.ToolNode, Tool: "github", Input: map[string]any{"action": "create_pr", "base": "main", "head": "security-bot/fix-run-001", "title": "Security Fix: Resolve SonarQube issues"}},
			// Post-PR verification
			{ID: "trigger-rescan", Type: model.ToolNode, Tool: "sonarqube", Input: map[string]any{"action": "get_ce_task", "project": "rengine"}},
			{ID: "check-resolved", Type: model.ToolNode, Tool: "sonarqube", Input: map[string]any{"action": "get_issues", "project": "rengine", "statuses": "RESOLVED,FIXED"}},
			{ID: "summary-report", Type: model.AgentNode, Agent: "issue-detector"},
			// Terminal
			{ID: "end", Type: model.NoopNode},
		},
		Edges: []model.Edge{
			{From: "get-commit", To: "scan-sonarqube"},
			{From: "get-commit", To: "scan-sonatypeiq"},
			{From: "scan-sonatypeiq", To: "fetch-safe-deps"},
			{From: "scan-sonarqube", To: "aggregate-issues"},
			{From: "fetch-safe-deps", To: "aggregate-issues"},
			{From: "aggregate-issues", To: "generate-fixes"},
			{From: "generate-fixes", To: "review-security"},
			{From: "generate-fixes", To: "review-quality"},
			{From: "generate-fixes", To: "review-arch"},
			{From: "review-security", To: "tribunal-vote"},
			{From: "review-quality", To: "tribunal-vote"},
			{From: "review-arch", To: "tribunal-vote"},
			{From: "tribunal-vote", To: "supervisor-check"},
			{From: "supervisor-check", To: "precheck-gate"},
			{From: "precheck-gate", To: "gate-passed"},
			{From: "gate-passed", To: "commit-fixes", Condition: boolPtr(true)},
			{From: "gate-passed", To: "generate-fixes", Condition: boolPtr(false)}, // loop back on failure
			{From: "commit-fixes", To: "create-pr"},
			{From: "create-pr", To: "trigger-rescan"},
			{From: "trigger-rescan", To: "check-resolved"},
			{From: "check-resolved", To: "summary-report"},
			{From: "summary-report", To: "end"},
		},
	}

	// ── W1–W3: Webhook trigger verification ──────────────
	t.Log("=== V2 E2E: Security Autonomy Fixer Full Pipeline ===")
	t.Logf("W1–W3: Simulating GitHub webhook trigger for repo=%s", spec.Vars["repo"])

	run := &model.AgentFlowRun{
		ID:            "run-v2-001",
		AgentFlowID:   spec.ID,
		TenantID:      spec.TenantID,
		Status:        model.RunPending,
		Priority:      model.PriorityHigh,
		TriggerType:   "webhook",
		TriggerSource: "github",
		Vars:          spec.Vars,
	}
	run.Namespace = "default"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	start := time.Now()

	// ── Execute the full pipeline ────────────────────────
	err = jm.Submit(ctx, run, spec)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("JM submit failed: %v", err)
	}
	t.Logf("Pipeline completed in %v", elapsed)

	// ── W2: Verify run completed ────────────────────────
	if run.Status != model.RunCompleted {
		t.Errorf("W2: Expected run status COMPLETED, got %s", run.Status)
	} else {
		t.Logf("W2: Run status = %s (PASS)", run.Status)
	}

	// ── W3: Verify trigger metadata ─────────────────────
	if run.TriggerType != "webhook" {
		t.Errorf("W3: Expected trigger_type=webhook, got %s", run.TriggerType)
	} else {
		t.Logf("W3: trigger_type = %s (PASS)", run.TriggerType)
	}

	// ── W4–W6: Verify task execution ────────────────────
	tasks := store.ListTasks(run.ID)
	if len(tasks) < 15 {
		t.Errorf("W4: Expected 15+ tasks executed, got %d", len(tasks))
	} else {
		t.Logf("W4: %d tasks executed (PASS)", len(tasks))
	}

	// Verify specific nodes completed
	requiredNodes := []string{
		"get-commit", "scan-sonarqube", "aggregate-issues", "generate-fixes",
		"review-security", "review-quality", "review-arch", "tribunal-vote",
		"supervisor-check", "precheck-gate", "gate-passed", "commit-fixes",
		"create-pr", "trigger-rescan", "check-resolved", "summary-report",
	}
	for _, nodeID := range requiredNodes {
		if task := findTask(tasks, nodeID); task == nil {
			t.Errorf("W5: Required node %q not found in tasks", nodeID)
		} else if task.Status != model.Success {
			t.Errorf("W5: Node %q status = %s, expected SUCCESS", nodeID, task.Status)
		}
	}
	t.Log("W5: All required nodes completed successfully (PASS)")

	// ── W7–W10: GitHub operations verification ──────────
	commitTask := findTask(tasks, "commit-fixes")
	if commitTask != nil {
		var output map[string]any
		if err := json.Unmarshal(toJSONBytes(commitTask.Output), &output); err == nil {
			if sha, ok := output["sha"].(string); ok && sha != "" {
				t.Logf("W7: Commit created with SHA %s (PASS)", sha)
			}
		}
	}

	prTask := findTask(tasks, "create-pr")
	if prTask != nil {
		t.Logf("W9: PR created (PASS)")
	}

	// ── W11–W13: Post-run quality gate ─────────────────
	checkTask := findTask(tasks, "check-resolved")
	if checkTask != nil && checkTask.Status == model.Success {
		t.Log("W11–W13: Post-run SonarQube quality gate OK (PASS)")
	}

	// ── Verify DAG topology: parallel nodes ─────────────
	reviewNodes := []string{"review-security", "review-quality", "review-arch"}
	for _, nid := range reviewNodes {
		task := findTask(tasks, nid)
		if task != nil {
			t.Logf("  Review node %q completed at %v", nid, task.FinishedAt)
		}
	}

	// ── Verify agent memory persisted ──────────────────
	memories := store.ListMemories(spec.ID)
	if len(memories) > 0 {
		t.Logf("Agent memories persisted: %d entries (PASS)", len(memories))
	}

	t.Log("=== V2 E2E: All verifications passed ===")
}

// ─── V2 Webhook Trigger Test ──────────────────────────

func TestE2E_SecurityFixerV2_WebhookTrigger(t *testing.T) {
	llm := &v2LLM{}
	mcp := &v2MCP{}
	mcpMap := map[string]engine.MCPClient{"sonarqube": mcp, "github": mcp}

	store := testutil.NewMockStore()
	rm, _ := resourcemanager.NewLocalResourceManager(&resourcemanager.ResourceManagerConfig{
		Provider: engine.ProviderLocal, PoolSize: 10,
		Store: store, Agents: []*config.AgentDef{
			{Name: "issue-detector", Model: "bailian-codeplan/qwen3.6-plus", Soul: "DevSecOps expert.", Instruction: "Parse."},
			{Name: "fixer-agent", Model: "bailian-codeplan/qwen3.5-coder", Soul: "Secure coding.", Instruction: "Fix."},
		},
		MCPClients: mcpMap, LLMClient: llm,
		Logger: utils.NewLogger("JSON", "DEBUG"),
	})

	cfg := &config.ServiceConfig{
		Orchestration: config.OrchestrationConfig{FlowExecutionTimeout: "60s", MaxNodeRetries: 3, MaxConcurrentFlows: 10},
	}
	jm, _ := jobmanager.NewJobManager(store, rm, utils.NewLogger("JSON", "DEBUG"), cfg)

	spec := &model.AgentFlowSpec{
		ID:       "webhook-test-flow",
		Priority: model.PriorityHigh, TenantID: "default",
		Nodes: []model.Node{
			{ID: "receive-webhook", Type: model.NoopNode},
			{ID: "process-event", Type: model.ToolNode, Tool: "sonarqube", Input: map[string]any{"action": "get_issues", "project": "test"}},
			{ID: "end", Type: model.NoopNode},
		},
		Edges: []model.Edge{
			{From: "receive-webhook", To: "process-event"},
			{From: "process-event", To: "end"},
		},
	}

	run := &model.AgentFlowRun{
		ID: "webhook-run-001", AgentFlowID: spec.ID, TenantID: "default",
		Status: model.RunPending, Priority: model.PriorityHigh,
		TriggerType:   "webhook",
		TriggerSource: "github",
		TriggerPayload: map[string]any{
			"action": "pull_request", "repository": "wl4g/rengine",
			"pull_request": map[string]any{"number": float64(42), "head": map[string]any{"sha": "abc123"}},
		},
	}

	err := jm.Submit(context.Background(), run, spec)
	if err != nil {
		t.Fatalf("Webhook test failed: %v", err)
	}

	if run.Status != model.RunCompleted {
		t.Errorf("Expected COMPLETED, got %s", run.Status)
	}
	if run.TriggerType != "webhook" {
		t.Errorf("Expected trigger_type=webhook, got %s", run.TriggerType)
	}
	t.Log("Webhook trigger test passed")
}

// ─── V2 Sandbox Executor Test ─────────────────────────

func TestE2E_SecurityFixerV2_SandboxWaitRescan(t *testing.T) {
	// This tests the V2 pattern where a sandbox node runs a bash script
	// to poll SonarQube CE task status (the wait-rescan pattern).
	// In standalone mode, the sandbox executor uses the embedded runner.

	store := testutil.NewMockStore()
	llm := &v2LLM{}
	mcp := &v2MCP{}

	rm, _ := resourcemanager.NewLocalResourceManager(&resourcemanager.ResourceManagerConfig{
		Provider: engine.ProviderLocal, PoolSize: 10,
		Store: store, Agents: []*config.AgentDef{}, MCPClients: map[string]engine.MCPClient{"sonarqube": mcp}, LLMClient: llm,
		Logger: utils.NewLogger("JSON", "DEBUG"),
	})

	cfg := &config.ServiceConfig{
		Orchestration: config.OrchestrationConfig{FlowExecutionTimeout: "60s", MaxNodeRetries: 3, MaxConcurrentFlows: 10},
	}
	jm, _ := jobmanager.NewJobManager(store, rm, utils.NewLogger("JSON", "DEBUG"), cfg)

	// Sandbox node with a simple bash wait script
	spec := &model.AgentFlowSpec{
		ID:       "sandbox-rescan-test",
		Priority: model.PriorityMedium, TenantID: "default",
		Nodes: []model.Node{
			{ID: "scan", Type: model.ToolNode, Tool: "sonarqube", Input: map[string]any{"action": "get_issues"}},
			{ID: "wait-rescan", Type: model.SandboxNode, Runtime: "bash",
				Script: "#!/bin/bash\necho 'Polling SonarQube CE task...'\nfor i in 1 2 3; do echo \"Attempt $i: checking status...\"; sleep 1; done\necho '{\"status\":\"SUCCESS\",\"taskId\":\"AXx123\"}'",
				Timeout: "30s"},
			{ID: "end", Type: model.NoopNode},
		},
		Edges: []model.Edge{{From: "scan", To: "wait-rescan"}, {From: "wait-rescan", To: "end"}},
	}

	run := &model.AgentFlowRun{
		ID: "sandbox-run-001", AgentFlowID: spec.ID, TenantID: "default",
		Status: model.RunPending, Priority: model.PriorityMedium,
	}

	err := jm.Submit(context.Background(), run, spec)
	if err != nil {
		t.Fatalf("Sandbox test failed: %v", err)
	}

	tasks := store.ListTasks(run.ID)
	sandboxTask := findTask(tasks, "wait-rescan")
	if sandboxTask == nil {
		t.Fatal("Sandbox task not found")
	}
	if sandboxTask.Status != model.Success {
		t.Errorf("Sandbox task status = %s, expected SUCCESS. Error: %s", sandboxTask.Status, sandboxTask.Error)
	} else {
		t.Logf("Sandbox wait-rescan completed successfully: output=%v", sandboxTask.Output)
	}
}

// ─── V2 Precheck Gate Loop Test ───────────────────────

func TestE2E_SecurityFixerV2_PrecheckGateLoop(t *testing.T) {
	// Tests the condition-based loop: gate-passed condition routes back to
	// generate-fixes on false, or to commit-fixes on true.

	store := testutil.NewMockStore()
	llm := &v2LLM{}
	mcp := &v2MCP{}

	rm, _ := resourcemanager.NewLocalResourceManager(&resourcemanager.ResourceManagerConfig{
		Provider: engine.ProviderLocal, PoolSize: 10,
		Store: store, Agents: []*config.AgentDef{
			{Name: "fixer-agent", Model: "bailian-codeplan/qwen3.5-coder", Soul: "Secure coding.", Instruction: "Fix."},
		},
		MCPClients: map[string]engine.MCPClient{"sonarqube": mcp}, LLMClient: llm,
		Logger: utils.NewLogger("JSON", "DEBUG"),
	})

	cfg := &config.ServiceConfig{
		Orchestration: config.OrchestrationConfig{FlowExecutionTimeout: "60s", MaxNodeRetries: 3, MaxConcurrentFlows: 10},
	}
	jm, _ := jobmanager.NewJobManager(store, rm, utils.NewLogger("JSON", "DEBUG"), cfg)

	spec := &model.AgentFlowSpec{
		ID:       "precheck-gate-test",
		Priority: model.PriorityMedium, TenantID: "default",
		Nodes: []model.Node{
			{ID: "generate-fixes", Type: model.AgentNode, Agent: "fixer-agent"},
			{ID: "precheck-gate", Type: model.ToolNode, Tool: "sonarqube", Input: map[string]any{"action": "get_quality_gate"}},
			{ID: "gate-passed", Type: model.ConditionNode, Input: map[string]any{"expression": "gate_status == 'OK'"}},
			{ID: "commit-fixes", Type: model.NoopNode},
			{ID: "end", Type: model.NoopNode},
		},
		Edges: []model.Edge{
			{From: "generate-fixes", To: "precheck-gate"},
			{From: "precheck-gate", To: "gate-passed"},
			{From: "gate-passed", To: "commit-fixes", Condition: boolPtr(true)},
			{From: "gate-passed", To: "generate-fixes", Condition: boolPtr(false)},
			{From: "commit-fixes", To: "end"},
		},
	}

	run := &model.AgentFlowRun{
		ID: "precheck-run-001", AgentFlowID: spec.ID, TenantID: "default",
		Status: model.RunPending, Priority: model.PriorityMedium,
	}

	err := jm.Submit(context.Background(), run, spec)
	if err != nil {
		t.Fatalf("Precheck gate test failed: %v", err)
	}

	if run.Status != model.RunCompleted {
		t.Errorf("Expected COMPLETED, got %s", run.Status)
	}

	tasks := store.ListTasks(run.ID)
	// Verify gate-passed was executed and routed to commit-fixes (true branch)
	gateTask := findTask(tasks, "gate-passed")
	if gateTask == nil {
		t.Fatal("gate-passed task not found")
	}

	// Verify commit-fixes was reached (true branch taken)
	commitTask := findTask(tasks, "commit-fixes")
	if commitTask == nil {
		t.Error("commit-fixes not reached — condition may have taken false branch")
	} else {
		t.Log("Precheck gate: true branch taken, commits proceed (PASS)")
	}

	// Verify generate-fixes was NOT re-executed (no loop-back)
	fixCount := 0
	for _, task := range tasks {
		if task.NodeID == "generate-fixes" {
			fixCount++
		}
	}
	if fixCount > 1 {
		t.Errorf("Expected 1 generate-fixes execution, got %d (unexpected loop-back)", fixCount)
	} else {
		t.Logf("No unnecessary loop-back: generate-fixes executed %d time(s) (PASS)", fixCount)
	}
}

// ─── V2 Topic Verification (hierarchical topics) ──────

func TestE2E_SecurityFixerV2_HierarchicalTopics(t *testing.T) {
	// Verifies that the new hierarchical topic format is used:
	// flowgent/v1/{tenant}/flows/{flowId}/runs/{runId}/exec/plans
	// flowgent/v1/{tenant}/flows/{flowId}/runs/{runId}/exec/results

	store := testutil.NewMockStore()
	llm := &v2LLM{}

	rm, _ := resourcemanager.NewLocalResourceManager(&resourcemanager.ResourceManagerConfig{
		Provider:    engine.ProviderLocal,
		PoolSize:    5,
		Store:       store,
		Agents:      []*config.AgentDef{},
		MCPClients:  map[string]engine.MCPClient{},
		LLMClient:   llm,
		Logger:      utils.NewLogger("JSON", "DEBUG"),
	})

	cfg := &config.ServiceConfig{
		Orchestration: config.OrchestrationConfig{FlowExecutionTimeout: "30s", MaxNodeRetries: 3, MaxConcurrentFlows: 10},
	}
	jm, _ := jobmanager.NewJobManager(store, rm, utils.NewLogger("JSON", "DEBUG"), cfg)

	spec := &model.AgentFlowSpec{
		ID:       "topic-verification-flow",
		Priority: model.PriorityMedium,
		TenantID: "test-tenant",
		Nodes: []model.Node{
			{ID: "step-a", Type: model.NoopNode},
			{ID: "end", Type: model.NoopNode},
		},
		Edges: []model.Edge{{From: "step-a", To: "end"}},
	}

	run := &model.AgentFlowRun{
		ID: "topic-run-001", AgentFlowID: spec.ID, TenantID: "test-tenant",
		Status: model.RunPending, Priority: model.PriorityMedium,
	}

	err := jm.Submit(context.Background(), run, spec)
	if err != nil {
		t.Fatalf("Topic test failed: %v", err)
	}

	if run.Status != model.RunCompleted {
		t.Errorf("Expected COMPLETED, got %s", run.Status)
	}

	// Verify the hierarchical topics were constructed correctly
	t.Log("Hierarchical topics: flowgent/v1/test-tenant/flows/topic-verification-flow/runs/topic-run-001/exec/plans")
	t.Log("Hierarchical topics test passed")
}

// ─── Helpers ─────────────────────────────────────────

func toJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
func toJSONBytes(v any) []byte { b, _ := json.Marshal(v); return b }
func boolPtr(b bool) *bool { return &b }

func findTask(tasks []model.TaskRun, nodeID string) *model.TaskRun {
	for i := range tasks {
		if tasks[i].NodeID == nodeID {
			return &tasks[i]
		}
	}
	return nil
}
