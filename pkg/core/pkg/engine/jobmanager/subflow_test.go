package jobmanager

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

type durableTestState struct {
	mu      sync.Mutex
	tasks   map[string]*entities.TaskRunInfo
	graph   *entities.SubflowState
	updates int
}

func (s *durableTestState) SaveTask(_ context.Context, t *entities.TaskRunInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *t
	s.tasks[t.ID] = &copy
	return nil
}
func (s *durableTestState) UpdateRun(context.Context, *entities.FlowRunInfo) error {
	s.mu.Lock()
	s.updates++
	s.mu.Unlock()
	return nil
}
func (s *durableTestState) ListTasks(context.Context, string) ([]*entities.TaskRunInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var tasks []*entities.TaskRunInfo
	for _, v := range s.tasks {
		copy := *v
		tasks = append(tasks, &copy)
	}
	return tasks, nil
}
func (s *durableTestState) InitializeSubflow(context.Context, string, string) (*entities.SubflowState, error) {
	return s.graph, nil
}
func (s *durableTestState) ApplyGraphPatch(_ context.Context, p entities.GraphPatch) (*entities.SubflowState, error) {
	statuses := map[string]entities.TaskStatus{}
	for _, v := range s.tasks {
		for _, n := range s.graph.Graph.Nodes {
			if v.NodeKey == "repair/"+n.ID {
				statuses[n.ID] = v.Status
			}
		}
	}
	graph, err := s.graph.Apply(p, statuses)
	if err == nil {
		s.graph = graph
	}
	return graph, err
}

type parallelTestRM struct {
	mu                               sync.Mutex
	active, peak, calls, supervisors int
}

func (r *parallelTestRM) Provider() engine.Provider      { return engine.ProviderStandalone }
func (r *parallelTestRM) Validate(context.Context) error { return nil }
func (r *parallelTestRM) Shutdown(context.Context) error { return nil }
func (r *parallelTestRM) Schedule(ctx context.Context, p *entities.ExecutionPlan) (*entities.TaskResult, error) {
	r.mu.Lock()
	r.calls++
	r.active++
	if r.active > r.peak {
		r.peak = r.active
	}
	r.mu.Unlock()
	defer func() { r.mu.Lock(); r.active--; r.mu.Unlock() }()
	if p.TaskType == entities.TaskSupervisor {
		r.mu.Lock()
		r.supervisors++
		step := r.supervisors
		r.mu.Unlock()
		if step == 1 {
			return &entities.TaskResult{Output: map[string]any{"operations": []entities.PatchOperation{{Kind: "append", Node: &entities.Node{ID: "review", Kind: entities.NoopNode}}, {Kind: "update_pending_edges", From: "seed", To: "review"}}}}, nil
		}
		return &entities.TaskResult{Output: map[string]any{"operations": []entities.PatchOperation{{Kind: "seal"}}}}, nil
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(10 * time.Millisecond):
	}
	return &entities.TaskResult{Output: map[string]any{"done": p.NodeID}}, nil
}
func TestBoundedParallelExecutionAndDurableRestore(t *testing.T) {
	state := &durableTestState{tasks: map[string]*entities.TaskRunInfo{}}
	rm := &parallelTestRM{}
	flow := &entities.FlowInfo{BaseEntity: entities.BaseEntity{ID: "parallel", Namespace: "default"}}
	for i := 0; i < 10; i++ {
		flow.Nodes = append(flow.Nodes, entities.Node{ID: fmt.Sprintf("agent%d", i), Kind: entities.NoopNode})
	}
	flow.Nodes = append(flow.Nodes, entities.Node{ID: "join", Kind: entities.JoinNode})
	for i := 0; i < 10; i++ {
		flow.Edges = append(flow.Edges, entities.Edge{From: fmt.Sprintf("agent%d", i), To: "join"})
	}
	run := &entities.FlowRunInfo{BaseEntity: entities.BaseEntity{ID: "run"}}
	master := NewJobMaster(state, rm, utils.NewLogger("TEXT", "ERROR"), &JobManagerConfig{MaxConcurrentNodes: 3})
	if err := master.Execute(context.Background(), run, flow); err != nil {
		t.Fatal(err)
	}
	if run.Status != entities.RunCompleted || rm.peak < 2 || rm.peak > 3 || rm.calls != 11 {
		t.Fatalf("status %s peak %d calls %d", run.Status, rm.peak, rm.calls)
	}
	// New JM process state, same committed attempts: none of the 10 branches rerun.
	restored := NewJobMaster(state, rm, utils.NewLogger("TEXT", "ERROR"), &JobManagerConfig{MaxConcurrentNodes: 3})
	if err := restored.Execute(context.Background(), run, flow); err != nil {
		t.Fatal(err)
	}
	if rm.calls != 11 {
		t.Fatal("completed nodes reran after recovery")
	}
}
func TestAdaptiveSubflowUsesSameSchedulerAndSeals(t *testing.T) {
	state := &durableTestState{tasks: map[string]*entities.TaskRunInfo{}, graph: &entities.SubflowState{InstanceID: "repair", Revision: 1, Config: entities.SubflowConfig{Ref: "child", Mode: "adaptive", Supervisor: "planner", Mutation: entities.MutationPolicy{Allow: []string{"append", "update_pending_edges", "seal"}}, Limits: entities.FlowLimits{MaxPatches: 3}}, Graph: entities.FlowInfo{BaseEntity: entities.BaseEntity{ID: "child"}, Nodes: []entities.Node{{ID: "seed", Kind: entities.NoopNode}}}}}
	rm := &parallelTestRM{}
	master := NewJobMaster(state, rm, utils.NewLogger("TEXT", "ERROR"), nil)
	flow := &entities.FlowInfo{BaseEntity: entities.BaseEntity{ID: "parent", Namespace: "default"}, Nodes: []entities.Node{{ID: "repair", Kind: entities.AgentFlowNode, AgentFlowID: "child"}, {ID: "delivery", Kind: entities.NoopNode}}, Edges: []entities.Edge{{From: "repair", To: "delivery"}}}
	run := &entities.FlowRunInfo{BaseEntity: entities.BaseEntity{ID: "run"}}
	if err := master.Execute(context.Background(), run, flow); err != nil {
		t.Fatal(err)
	}
	if !state.graph.Sealed || state.graph.Revision != 3 || run.Status != entities.RunCompleted {
		t.Fatalf("subflow not sealed: %#v run %s", state.graph, run.Status)
	}
	if state.updates != 2 {
		t.Fatalf("child wrote parent lifecycle %d times", state.updates)
	}
	if rm.calls != 5 {
		t.Fatalf("unexpected reexecution %d", rm.calls)
	}
}
