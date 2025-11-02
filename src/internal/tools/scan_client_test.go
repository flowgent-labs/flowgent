package tools

import (
	"context"
	"testing"
)

func TestScanClient_GetFailed(t *testing.T) {
	sc := &ScanClient{}

	jobs := []ScanJob{
		{JobID: "1", Status: "success"},
		{JobID: "2", Status: "failed"},
		{JobID: "3", Status: "FAILED"},
		{JobID: "4", Status: "running"},
	}

	failed := sc.GetFailed(jobs)

	if len(failed) != 2 {
		t.Errorf("Expected 2 failed jobs, got %d", len(failed))
	}

	for _, job := range failed {
		if job.Status != "failed" && job.Status != "FAILED" {
			t.Errorf("Expected failed status, got %s", job.Status)
		}
	}
}

func TestScanType_Constants(t *testing.T) {
	tests := []struct {
		name     ScanType
		expected string
	}{
		{ScanTypeDAST, "dast"},
		{ScanTypeSAST, "sast"},
		{ScanTypeCONT, "cont"},
		{ScanTypeFOSS, "foss"},
		{ScanTypeSonar, "sonarqube"},
	}

	for _, tt := range tests {
		t.Run(string(tt.name), func(t *testing.T) {
			if string(tt.name) != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, tt.name)
			}
		})
	}
}

func TestScanJob_JSONMarshaling(t *testing.T) {
	job := ScanJob{
		JobID:     "test-job-123",
		Type:      ScanTypeDAST,
		Status:    "success",
		ReportID:  "report-456",
		CommitSHA: "abc123",
		Repo:      "owner/repo",
	}

	if job.JobID != "test-job-123" {
		t.Errorf("JobID mismatch")
	}
	if job.Type != ScanTypeDAST {
		t.Errorf("Type mismatch")
	}
	if job.Status != "success" {
		t.Errorf("Status mismatch")
	}
}

func TestVulnerability_JSONMarshaling(t *testing.T) {
	vuln := Vulnerability{
		ID:       "CVE-2024-1234",
		Type:     "SQLInjection",
		Severity: "CRITICAL",
		File:     "/src/main/java/com/example/App.java",
		Line:     42,
		CVE:      "CVE-2024-1234",
		CWE:      "CWE-89",
		Summary:  "SQL injection vulnerability",
	}

	if vuln.ID != "CVE-2024-1234" {
		t.Errorf("ID mismatch")
	}
	if vuln.Line != 42 {
		t.Errorf("Line mismatch")
	}
	if vuln.Severity != "CRITICAL" {
		t.Errorf("Severity mismatch")
	}
}

func TestScanClient_NullSafety(t *testing.T) {
	sc := &ScanClient{}

	ctx := context.Background()

	_, err := sc.GetJobsByCommit(ctx, "repo", "sha")
	if err == nil {
		t.Error("Expected error for nil scanner")
	}

	_, err = sc.GetJobStatus(ctx, "job123")
	if err == nil {
		t.Error("Expected error for nil scanner")
	}

	_, err = sc.DownloadReport(ctx, "report123", "/tmp")
	if err == nil {
		t.Error("Expected error for nil scanner")
	}

	_, err = sc.ParseReport(ctx, "sast", "/tmp/report.pdf")
	if err == nil {
		t.Error("Expected error for nil parser")
	}

	_, err = sc.GetFOSSolution(ctx, "component123")
	if err == nil {
		t.Error("Expected error for nil fixer")
	}

	_, err = sc.SearchWebSolution(ctx, "CVE-2024-1234")
	if err == nil {
		t.Error("Expected error for nil fixer")
	}
}
