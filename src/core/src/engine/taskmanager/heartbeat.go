package taskmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	messager "github.com/flowgent-labs/flowgent/messager/src"
)

const (
	defaultHeartbeatInterval = 5 * time.Second
	defaultLeaseTimeout      = 15 * time.Second
)

// ─── TM-side heartbeat ─────────────────────────────────

func startHeartbeat(tmID string, q messager.IMessager, interval time.Duration) {
	if interval <= 0 {
		interval = defaultHeartbeatInterval
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			hb := &messager.Heartbeat{TMID: tmID, Timestamp: time.Now()}
			data, _ := json.Marshal(hb)
			_ = q.Publish(context.Background(), messager.HeartbeatTopic(tmID), &messager.InterMessage{
				ID:      fmt.Sprintf("hb-%s-%d", tmID, time.Now().UnixNano()),
				Payload: data,
			})
		}
	}()
}

// ─── JM-side heartbeat monitor ──────────────────────────

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
	q            messager.IMessager
	activeTMs    map[string]*TMState
	mu           sync.Mutex
	leaseTimeout time.Duration
}

func NewHeartbeatMonitor(q messager.IMessager, leaseTimeout time.Duration) *HeartbeatMonitor {
	if leaseTimeout <= 0 {
		leaseTimeout = defaultLeaseTimeout
	}
	return &HeartbeatMonitor{
		q:            q,
		activeTMs:    make(map[string]*TMState),
		leaseTimeout: leaseTimeout,
	}
}

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

func (hm *HeartbeatMonitor) Start(ctx context.Context) {
	hm.q.Subscribe(ctx, messager.HeartbeatWildcard(), func(topic string, payload []byte) {
		var hb messager.Heartbeat
		if err := json.Unmarshal(payload, &hb); err != nil {
			return
		}
		hm.recordBeat(&hb)
	})

	go func() {
		ticker := time.NewTicker(hm.leaseTimeout / 2)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				hm.detectExpired()
			}
		}
	}()
}

func (hm *HeartbeatMonitor) recordBeat(hb *messager.Heartbeat) {
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

func (hm *HeartbeatMonitor) ExpiredTMs() []string {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	now := time.Now()
	var expired []string
	for tmID, s := range hm.activeTMs {
		if now.Sub(s.LastBeat) > hm.leaseTimeout {
			expired = append(expired, tmID)
		}
	}
	return expired
}
