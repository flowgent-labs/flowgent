package testutil

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/flowgent-labs/flowgent/src/engine"
	"github.com/flowgent-labs/flowgent/src/model"
	"github.com/flowgent-labs/flowgent/src/queue"
)

// ─── MockStore ─────────────────────────────────────────

type MockStore struct {
	Mu     sync.Mutex
	Runs   map[string]*model.AgentFlowRun
	Tasks  map[string]*model.TaskRun
	Humans map[string]*model.HumanApproval
	Plans  map[string]*model.ExecutionPlan
	Checks map[string]*model.TaskCheckpoint
	Leases map[string]string
}

func NewMockStore() *MockStore {
	return &MockStore{
		Runs:   make(map[string]*model.AgentFlowRun),
		Tasks:  make(map[string]*model.TaskRun),
		Humans: make(map[string]*model.HumanApproval),
		Plans:  make(map[string]*model.ExecutionPlan),
		Checks: make(map[string]*model.TaskCheckpoint),
		Leases: make(map[string]string),
	}
}

// Standard CRUD
func (s *MockStore) CreateAgentFlowRun(ctx context.Context, run *model.AgentFlowRun) error {
	s.Mu.Lock(); defer s.Mu.Unlock(); s.Runs[run.ID] = run; return nil
}
func (s *MockStore) UpdateAgentFlowRun(ctx context.Context, run *model.AgentFlowRun) error {
	s.Mu.Lock(); defer s.Mu.Unlock(); s.Runs[run.ID] = run; return nil
}
func (s *MockStore) GetAgentFlowRun(ctx context.Context, id string) (*model.AgentFlowRun, error) {
	s.Mu.Lock(); defer s.Mu.Unlock(); return s.Runs[id], nil
}
func (s *MockStore) ListAgentFlowRuns(ctx context.Context, fid string, limit int) ([]model.AgentFlowRun, error) {
	s.Mu.Lock(); defer s.Mu.Unlock()
	var out []model.AgentFlowRun
	for _, r := range s.Runs {
		if fid == "" || r.AgentFlowID == fid {
			out = append(out, *r)
		}
	}
	return out, nil
}
func (s *MockStore) ListActiveRuns(ctx context.Context) ([]model.AgentFlowRun, error) { return nil, nil }
func (s *MockStore) CreateTaskRun(ctx context.Context, task *model.TaskRun) error {
	s.Mu.Lock(); defer s.Mu.Unlock(); task.ID = task.ExecID; s.Tasks[task.ID] = task; return nil
}
func (s *MockStore) UpdateTaskRun(ctx context.Context, task *model.TaskRun) error {
	s.Mu.Lock(); defer s.Mu.Unlock(); s.Tasks[task.ID] = task; return nil
}
func (s *MockStore) GetTaskRun(ctx context.Context, id string) (*model.TaskRun, error) {
	s.Mu.Lock(); defer s.Mu.Unlock(); return s.Tasks[id], nil
}
func (s *MockStore) GetTaskRunsByAgentFlowRun(ctx context.Context, rid string) ([]model.TaskRun, error) {
	s.Mu.Lock(); defer s.Mu.Unlock()
	var out []model.TaskRun
	for _, t := range s.Tasks {
		if t.AgentFlowRunID == rid {
			out = append(out, *t)
		}
	}
	return out, nil
}

// Human approval
func (s *MockStore) CreateHumanApproval(ctx context.Context, a *model.HumanApproval) error {
	s.Mu.Lock(); defer s.Mu.Unlock()
	a.Token = fmt.Sprintf("mock-token-%d", len(s.Humans)+1)
	s.Humans[a.TaskRunID] = a
	return nil
}
func (s *MockStore) GetHumanApproval(ctx context.Context, token string) (*model.HumanApproval, error) {
	s.Mu.Lock(); defer s.Mu.Unlock()
	for _, a := range s.Humans {
		if a.Token == token { return a, nil }
	}
	return nil, nil
}
func (s *MockStore) UpdateHumanApproval(ctx context.Context, a *model.HumanApproval) error {
	s.Mu.Lock(); defer s.Mu.Unlock(); s.Humans[a.TaskRunID] = a; return nil
}

// Supervisor
func (s *MockStore) LogSupervisorDecision(ctx context.Context, arID, trID string, input, decision map[string]any) error {
	return nil
}
func (s *MockStore) GetTaskRunByExecID(ctx context.Context, execID string) (*model.TaskRun, error) {
	s.Mu.Lock(); defer s.Mu.Unlock(); return s.Tasks[execID], nil
}

// AgentFlow definitions
func (s *MockStore) SaveAgentFlowDefinition(ctx context.Context, d *model.AgentFlowVersion) error { return nil }
func (s *MockStore) GetLatestAgentFlowDefinition(ctx context.Context, id string) (*model.AgentFlowVersion, error) {
	return nil, nil
}
func (s *MockStore) GetAgentFlowDefinition(ctx context.Context, id string, v int64) (*model.AgentFlowVersion, error) {
	return nil, nil
}
func (s *MockStore) ListAgentFlowDefinitions(ctx context.Context) ([]model.AgentFlowVersion, error) { return nil, nil }
func (s *MockStore) GetPendingApprovals(ctx context.Context) ([]model.HumanApproval, error) { return nil, nil }
func (s *MockStore) DB() any { return nil }

// ExecutionPlan
func (s *MockStore) SaveExecutionPlan(ctx context.Context, plan *model.ExecutionPlan) error {
	s.Mu.Lock(); defer s.Mu.Unlock(); s.Plans[plan.PlanID] = plan; return nil
}
func (s *MockStore) LoadExecutionPlan(ctx context.Context, planID string) (*model.ExecutionPlan, error) {
	s.Mu.Lock(); defer s.Mu.Unlock(); return s.Plans[planID], nil
}
func (s *MockStore) ListExecutionPlans(ctx context.Context, agentFlowRunID string) ([]*model.ExecutionPlan, error) {
	s.Mu.Lock(); defer s.Mu.Unlock()
	var out []*model.ExecutionPlan
	for _, p := range s.Plans {
		if p.AgentFlowRunID == agentFlowRunID {
			out = append(out, p)
		}
	}
	return out, nil
}

// Checkpoint
func (s *MockStore) SaveCheckpoint(ctx context.Context, planID string, cp *model.TaskCheckpoint) error {
	s.Mu.Lock(); defer s.Mu.Unlock(); s.Checks[planID] = cp; return nil
}
func (s *MockStore) LoadCheckpoint(ctx context.Context, planID string) (*model.TaskCheckpoint, error) {
	s.Mu.Lock(); defer s.Mu.Unlock(); return s.Checks[planID], nil
}

// Lease
func (s *MockStore) ClaimLease(ctx context.Context, planID, tmID string, dur time.Duration) error {
	s.Mu.Lock(); defer s.Mu.Unlock(); s.Leases[planID] = tmID; return nil
}
func (s *MockStore) ReleaseLease(ctx context.Context, planID string) error {
	s.Mu.Lock(); defer s.Mu.Unlock(); delete(s.Leases, planID); return nil
}

func (s *MockStore) DeleteAgentFlowDefinition(ctx context.Context, id string) error { return nil }
func (s *MockStore) UpdateAgentFlowSpec(ctx context.Context, spec *model.AgentFlowSpec, by, comment string) error { return nil }
func (s *MockStore) GetAgentFlowSpec(ctx context.Context, id string) (*model.AgentFlowSpec, error) { return nil, nil }
func (s *MockStore) SaveAgent(ctx context.Context, a *model.AgentDef) error { return nil }
func (s *MockStore) GetAgent(ctx context.Context, name string) (*model.AgentDef, error) { return nil, nil }
func (s *MockStore) ListAgents(ctx context.Context, tenantID string) ([]model.AgentDef, error) { return nil, nil }
func (s *MockStore) DeleteAgent(ctx context.Context, name string) error { return nil }
func (s *MockStore) DeleteAgentFlowRun(ctx context.Context, id string) error { return nil }
func (s *MockStore) CancelAgentFlowRun(ctx context.Context, id string) error { return nil }
func (s *MockStore) SaveNotificationChannel(ctx context.Context, ch *model.NotifierChannel) error { return nil }
func (s *MockStore) GetNotificationChannel(ctx context.Context, id string) (*model.NotifierChannel, error) { return nil, nil }
func (s *MockStore) ListNotificationChannels(ctx context.Context, tenantID string) ([]model.NotifierChannel, error) { return nil, nil }
func (s *MockStore) DeleteNotificationChannel(ctx context.Context, id string) error { return nil }
func (s *MockStore) SaveSubscriptionRoute(ctx context.Context, r *model.SubscriptionRoute) error { return nil }
func (s *MockStore) GetSubscriptionRoutesByAgentFlow(ctx context.Context, id string) ([]model.SubscriptionRoute, error) { return nil, nil }
func (s *MockStore) DeleteSubscriptionRoute(ctx context.Context, id string) error { return nil }
func (s *MockStore) DeleteSubscriptionRoutesByPod(ctx context.Context, podID string) error { return nil }
func (s *MockStore) CleanupOrphanedRoutes(ctx context.Context, podID string, maxAge time.Duration) (int64, error) { return 0, nil }


// ─── Test helpers ──────────────────────────────────────


// TestResourceManager is a no-op resource manager for unit tests.
type TestResourceManager struct{ slots int }
func (ts *TestResourceManager) Type() engine.Provider { return engine.ProviderLocal }
func (ts *TestResourceManager) Validate(ctx context.Context) error { return nil }
func (ts *TestResourceManager) Schedule(ctx context.Context, p *model.ExecutionPlan) (*model.TaskResult, error) {
	return &model.TaskResult{Output: map[string]any{"status": "ok"}}, nil
}
func (ts *TestResourceManager) Shutdown(ctx context.Context) error { return nil }

func BoolPtr(b bool) *bool { return &b }
func MustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

// ─── TestQueue (in-memory, implements queue.Queue) ─────

type TestQueue struct {
	ch chan *queue.Message
}

func NewTestQueue() *TestQueue {
	return &TestQueue{ch: make(chan *queue.Message, 100)}
}

func (q *TestQueue) Push(ctx context.Context, msg *queue.Message) error {
	select {
	case q.ch <- msg:
	default:
	}
	return nil
}
func (q *TestQueue) Pop(ctx context.Context, timeout time.Duration) (*queue.Message, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case msg := <-q.ch:
		return msg, nil
	case <-timer.C:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (q *TestQueue) Dequeue(ctx context.Context, group string) (*queue.Message, error) {
	return q.Pop(ctx, 10*time.Second)
}
func (q *TestQueue) PublishHeartbeat(ctx context.Context, hb *queue.Heartbeat) error { return nil }
func (q *TestQueue) ConsumeHeartbeat(ctx context.Context, timeout time.Duration) (*queue.Heartbeat, error) {
	return nil, nil
}
func (q *TestQueue) Ack(ctx context.Context, id string) error  { return nil }
func (q *TestQueue) Nack(ctx context.Context, id string) error { return nil }
func (q *TestQueue) Close() error                              { return nil }

func (s *MockStore) DeleteNotifierChannel(ctx context.Context, id string) error { return nil }
func (s *MockStore) GetNotifierChannel(ctx context.Context, id string) (*model.NotifierChannel, error) { return nil, nil }
func (s *MockStore) ListNotifierChannels(ctx context.Context, tenantID string) ([]model.NotifierChannel, error) { return nil, nil }
func (s *MockStore) SaveNotifierChannel(ctx context.Context, ch *model.NotifierChannel) error { return nil }
