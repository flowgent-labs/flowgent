package engine

import (
	"context"
	"sync"
	"time"

	"github.com/flowgent-labs/flowgent/src/queue"
)

const (
	defaultHeartbeatInterval = 5 * time.Second
	defaultLeaseTimeout      = 15 * time.Second
)

// ─── Heartbeat Pump (TM side) ──────────────────────────

// HeartbeatPump periodically publishes TM heartbeat messages to the queue.
type HeartbeatPump struct {
	tmID     string
	q        queue.Queue
	interval time.Duration
}

func NewHeartbeatPump(tmID string, q queue.Queue, interval time.Duration) *HeartbeatPump {
	if interval <= 0 {
		interval = defaultHeartbeatInterval
	}
	return &HeartbeatPump{tmID: tmID, q: q, interval: interval}
}

// Start begins the heartbeat loop. Runs in its own goroutine.
func (p *HeartbeatPump) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				hb := &queue.Heartbeat{
					TMID:      p.tmID,
					Timestamp: time.Now(),
				}
				_ = p.q.PublishHeartbeat(ctx, hb)
			}
		}
	}()
}

// ─── Heartbeat Monitor (JM side) ───────────────────────

// TMState tracks the health of a single TaskManager.
type TMState struct {
	TMID     string
	LastBeat time.Time
	Capacity int
	Load     int
	IsAlive  bool
}

// HeartbeatMonitor consumes heartbeats from the queue and detects
// failed TMs by lease expiration.
type HeartbeatMonitor struct {
	q             queue.Queue
	activeTMs     map[string]*TMState
	mu            sync.Mutex
	leaseTimeout  time.Duration
}

func NewHeartbeatMonitor(q queue.Queue, leaseTimeout time.Duration) *HeartbeatMonitor {
	if leaseTimeout <= 0 {
		leaseTimeout = defaultLeaseTimeout
	}
	return &HeartbeatMonitor{
		q:            q,
		activeTMs:    make(map[string]*TMState),
		leaseTimeout: leaseTimeout,
	}
}

// ActiveTMs returns a snapshot of currently alive TMs.
func (hm *HeartbeatMonitor) ActiveTMs() []TMState {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	var out []TMState
	for _, s := range hm.activeTMs {
		if s.IsAlive {
			out = append(out, *s)
		}
	}
	return out
}

// Start begins consuming heartbeats and detecting failures.
// Runs in its own goroutine.
func (hm *HeartbeatMonitor) Start(ctx context.Context) {
	go func() {
		// Re-check interval is half the lease timeout
		ticker := time.NewTicker(hm.leaseTimeout / 2)
		defer ticker.Stop()

		hbCh := make(chan *queue.Heartbeat, 32)
		go hm.subscribe(ctx, hbCh)

		for {
			select {
			case <-ctx.Done():
				return
			case hb := <-hbCh:
				hm.recordBeat(hb)
			case <-ticker.C:
				hm.detectExpired()
			}
		}
	}()
}

func (hm *HeartbeatMonitor) subscribe(ctx context.Context, ch chan<- *queue.Heartbeat) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		hb, err := hm.q.ConsumeHeartbeat(ctx, 5*time.Second)
		if err != nil || hb == nil {
			continue
		}
		ch <- hb
	}
}

func (hm *HeartbeatMonitor) recordBeat(hb *queue.Heartbeat) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	s, ok := hm.activeTMs[hb.TMID]
	if !ok {
		s = &TMState{TMID: hb.TMID, IsAlive: true}
		hm.activeTMs[hb.TMID] = s
	}
	s.LastBeat = hb.Timestamp
	s.IsAlive = true
}

func (hm *HeartbeatMonitor) detectExpired() {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	now := time.Now()
	for _, s := range hm.activeTMs {
		if s.IsAlive && now.Sub(s.LastBeat) > hm.leaseTimeout {
			s.IsAlive = false
		}
	}
}

// ExpiredTMs returns TM IDs whose lease has expired (non-thread-safe snapshot).
func (hm *HeartbeatMonitor) ExpiredTMs() []string {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	now := time.Now()
	var expired []string
	for tmID, s := range hm.activeTMs {
		if s.IsAlive && now.Sub(s.LastBeat) > hm.leaseTimeout {
			expired = append(expired, tmID)
		}
	}
	return expired
}
