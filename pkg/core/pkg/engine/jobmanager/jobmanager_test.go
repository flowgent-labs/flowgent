package jobmanager

import (
	"context"
	"testing"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/core/pkg/lock"
)

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
