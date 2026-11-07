package jobmanager

import (
	"context"
	"testing"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/core/pkg/lock"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

func TestPreparePolledRunUsesRuntimeBoundaryNamespace(t *testing.T) {
	run := &entities.FlowRunInfo{
		BaseEntity:  entities.BaseEntity{ID: "run-1"},
		Status:      entities.RunPending,
		RuntimeMode: entities.RuntimeModeApplication,
	}
	cfg := RunPollerConfig{
		RuntimeNamespace: "flowgent-tenant-a",
		AgentFlowRunID:   "run-1",
		RuntimeMode:      entities.RuntimeModeApplication,
	}
	if !preparePolledRun(run, cfg) {
		t.Fatal("matching pending run should be accepted")
	}
	if run.K8sNamespace != cfg.RuntimeNamespace {
		t.Fatalf("runtime namespace = %q, want %q", run.K8sNamespace, cfg.RuntimeNamespace)
	}
}

func TestPreparePolledRunRejectsDifferentRuntimeBoundary(t *testing.T) {
	tests := []struct {
		name string
		run  *entities.FlowRunInfo
	}{
		{name: "nil", run: nil},
		{name: "completed", run: &entities.FlowRunInfo{BaseEntity: entities.BaseEntity{ID: "run-1"}, Status: entities.RunCompleted, RuntimeMode: entities.RuntimeModeApplication}},
		{name: "other run", run: &entities.FlowRunInfo{BaseEntity: entities.BaseEntity{ID: "run-2"}, Status: entities.RunPending, RuntimeMode: entities.RuntimeModeApplication}},
		{name: "other mode", run: &entities.FlowRunInfo{BaseEntity: entities.BaseEntity{ID: "run-1"}, Status: entities.RunPending, RuntimeMode: entities.RuntimeModeSession}},
	}
	cfg := RunPollerConfig{AgentFlowRunID: "run-1", RuntimeMode: entities.RuntimeModeApplication}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if preparePolledRun(test.run, cfg) {
				t.Fatal("non-matching run should be rejected")
			}
		})
	}
}

func TestJobManagerClaimRunUsesDistributedLock(t *testing.T) {
	jm := &JobManager{logger: utils.NewLogger("JSON", "ERROR")}
	jm.SetRunLock(lock.NewMemoryLock())

	ctx, release, claimed, err := jm.claimRun(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("claimRun first err: %v", err)
	}
	if !claimed {
		t.Fatal("first claim should succeed")
	}
	if ctx == nil {
		t.Fatal("claimRun returned nil context")
	}

	_, _, claimedAgain, err := jm.claimRun(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("claimRun second err: %v", err)
	}
	if claimedAgain {
		t.Fatal("second claim for same run should be rejected while lock is held")
	}

	release()

	_, releaseAfter, claimedAfterRelease, err := jm.claimRun(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("claimRun after release err: %v", err)
	}
	if !claimedAfterRelease {
		t.Fatal("claim after release should succeed")
	}
	releaseAfter()
}
