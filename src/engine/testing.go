package engine

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/flowgent-labs/flowgent/src/config"
	"github.com/flowgent-labs/flowgent/src/model"
	"github.com/flowgent-labs/flowgent/src/util"
)

// ─── VisibleForTesting — mock implementations exported for e2e test access ───

// MockStore is an in-memory Store for testing engine components.
type MockStore struct {
	Mu     sync.Mutex
	Runs   map[string]*model.AgentFlowRun
	Tasks  map[string]*model.TaskRun
	Humans map[string]*model.HumanApproval
}

func NewMockStore() *MockStore {
	return &MockStore{
		Runs:   make(map[string]*model.AgentFlowRun),
		Tasks:  make(map[string]*model.TaskRun),
		Humans: make(map[string]*model.HumanApproval),
	}
}

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
func (s *MockStore) CreateHumanApproval(ctx context.Context, a *model.HumanApproval) error {
	s.Mu.Lock(); defer s.Mu.Unlock(); s.Humans[a.TaskRunID] = a; return nil
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
func (s *MockStore) LogSupervisorDecision(ctx context.Context, arID, trID string, input, decision map[string]any) error {
	return nil
}
func (s *MockStore) GetTaskRunByExecID(ctx context.Context, execID string) (*model.TaskRun, error) {
	s.Mu.Lock(); defer s.Mu.Unlock(); return s.Tasks[execID], nil
}
func (s *MockStore) SaveAgentFlowDefinition(ctx context.Context, d *model.AgentFlowVersion) error { return nil }
func (s *MockStore) GetLatestAgentFlowDefinition(ctx context.Context, id string) (*model.AgentFlowVersion, error) {
	return nil, nil
}
func (s *MockStore) GetAgentFlowDefinition(ctx context.Context, id string, v int64) (*model.AgentFlowVersion, error) {
	return nil, nil
}
func (s *MockStore) ListAgentFlowDefinitions(ctx context.Context) ([]model.AgentFlowVersion, error) { return nil, nil }
func (s *MockStore) GetPendingApprovals(ctx context.Context) ([]model.HumanApproval, error) { return nil, nil }
func (s *MockStore) DB() interface{ Close() error } { return nil }

var _ Store = (*MockStore)(nil)

// ─── Test helpers ──────────────────────────────────────

// NewTestTaskManager creates a TaskManager with a test logger.
func NewTestTaskManager(store Store, mcp map[string]MCPClient, agents []*config.AgentDef, llm LLMClient) *TaskManager {
	return NewTaskManager(store, mcp, agents, llm, util.NewLogger("JSON", "DEBUG"))
}

// NewTestJobManager creates a JobManager backed by an in-memory MockStore
// and a LocalScheduler with pool size 10. Returns the shared store
// and the JobManager — both reference the same in-memory store.
func NewTestJobManager(mcp map[string]MCPClient, agents []*config.AgentDef, llm LLMClient) (*MockStore, *JobManager) {
	s := NewMockStore()
	tm := NewTaskManager(s, mcp, agents, llm, util.NewLogger("JSON", "DEBUG"))
	scheduler := NewLocalScheduler(tm, 10)
	jm := NewJobManager(s, scheduler, util.NewLogger("JSON", "DEBUG"))
	jm.SetTaskManager(tm)
	return s, jm
}

// BoolPtr returns a pointer to a bool.
func BoolPtr(b bool) *bool { return &b }

// MustJSON marshals v to JSON or panics.
func MustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
