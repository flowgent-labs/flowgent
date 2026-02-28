package taskmanager

import (
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/messaging/src"
	"github.com/flowgent-labs/flowgent/tests/testutil"
)

func TestHeartbeatMonitor_RecordAndExpire(t *testing.T) {
	q := testutil.NewTestQueue()
	hm := NewHeartbeatMonitor(q, 100*time.Millisecond)

	hm.recordBeat(&queue.Heartbeat{TMID: "tm-1", Timestamp: time.Now()})
	hm.recordBeat(&queue.Heartbeat{TMID: "tm-2", Timestamp: time.Now()})

	active := hm.ActiveTMs()
	if len(active) != 2 {
		t.Fatalf("expected 2 active TMs, got %d", len(active))
	}

	// Wait for lease to expire (lease=100ms, wait 300ms for safety)
	time.Sleep(300 * time.Millisecond)
	hm.detectExpired()

	active = hm.ActiveTMs()
	if len(active) != 0 {
		t.Fatalf("expected 0 active TMs after expiry, got %d", len(active))
	}

	expired := hm.ExpiredTMs()
	if len(expired) != 2 {
		t.Fatalf("expected 2 expired TMs, got %d", len(expired))
	}
}

func TestHeartbeatMonitor_KeepAlive(t *testing.T) {
	q := testutil.NewTestQueue()
	hm := NewHeartbeatMonitor(q, 100*time.Millisecond)

	hm.recordBeat(&queue.Heartbeat{TMID: "tm-1", Timestamp: time.Now()})

	// Refresh before expiry
	time.Sleep(50 * time.Millisecond)
	hm.recordBeat(&queue.Heartbeat{TMID: "tm-1", Timestamp: time.Now()})

	// Check — should still be alive
	hm.detectExpired()
	active := hm.ActiveTMs()
	if len(active) != 1 {
		t.Fatalf("expected 1 active TM after refresh, got %d", len(active))
	}
}

func TestHeartbeatMonitor_DefaultTimeout(t *testing.T) {
	q := testutil.NewTestQueue()
	hm := NewHeartbeatMonitor(q, 0) // zero → use defaultLeaseTimeout
	if hm.leaseTimeout != defaultLeaseTimeout {
		t.Fatalf("expected default lease timeout %v, got %v", defaultLeaseTimeout, hm.leaseTimeout)
	}
}

func TestHeartbeatMonitor_EmptyState(t *testing.T) {
	q := testutil.NewTestQueue()
	hm := NewHeartbeatMonitor(q, time.Second)
	if len(hm.ActiveTMs()) != 0 {
		t.Fatal("expected 0 active TMs initially")
	}
	if len(hm.ExpiredTMs()) != 0 {
		t.Fatal("expected 0 expired TMs initially")
	}
}
