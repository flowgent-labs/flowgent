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
	tasks   map[string]*entities.NodeRun
	graph   *entities.SubGraph
	updates int
}

func (s *durableTestState) SaveNodeRun(_ context.Context, t *entities.NodeRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *t
	s.tasks[t.ID] = &copy
	return nil
}
func (s *durableTestState) UpdateGraphRun(context.Context, *entities.GraphRun) error {
	s.mu.Lock()
	s.updates++
	s.mu.Unlock()
	return nil
}
func (s *durableTestState) ListNodeRuns(context.Context, string) ([]*entities.NodeRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var tasks []*entities.NodeRun
	for _, v := range s.tasks {
		copy := *v
		tasks = append(tasks, &copy)
	}
	return tasks, nil
}
func (s *durableTestState) InitializeSubGraph(context.Context, string, string) (*entities.SubGraph, error) {
	return s.graph, nil
}
func (s *durableTestState) ApplyGraphPatch(_ context.Context, p entities.GraphPatch) (*entities.SubGraph, error) {
	statuses := map[string]entities.NodeRunStatus{}
	for _, v := range s.tasks {
		for _, n := range s.graph.Graph.Nodes {
			if v.NodeInstanceID == "repair/"+n.ID {
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
func (r *parallelTestRM) Schedule(ctx context.Context, p *entities.ExecutionPlan) (*entities.NodeResult, error) {
	r.mu.Lock()
	r.calls++
	r.active++
	if r.active > r.peak {
		r.peak = r.active
	}
	r.mu.Unlock()
	defer func() { r.mu.Lock(); r.active--; r.mu.Unlock() }()
	if p.NodeKind == entities.SupervisorNode {
		r.mu.Lock()
		r.supervisors++
		step := r.supervisors
		r.mu.Unlock()
		if step == 1 {
			return &entities.NodeResult{Output: map[string]any{"operations": []entities.PatchOperation{{Kind: "append", Node: &entities.GraphNode{ID: "review", Kind: entities.NoopNode}}, {Kind: "update_pending_edges", From: "seed", To: "review"}}}}, nil
		}
		return &entities.NodeResult{Output: map[string]any{"operations": []entities.PatchOperation{{Kind: "seal"}}}}, nil
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(10 * time.Millisecond):
	}
	return &entities.NodeResult{Output: map[string]any{"done": p.NodeInstanceID}}, nil
}
func TestBoundedParallelExecutionAndDurableRestore(t *testing.T) {
	state := &durableTestState{tasks: map[string]*entities.NodeRun{}}
	rm := &parallelTestRM{}
	graph := &entities.AgentGraph{Name: "parallel", BaseEntity: entities.BaseEntity{ID: "parallel", Namespace: "default"}}
	for i := 0; i < 10; i++ {
		graph.Nodes = append(graph.Nodes, entities.GraphNode{ID: fmt.Sprintf("agent%d", i), Kind: entities.NoopNode})
	}
	graph.Nodes = append(graph.Nodes, entities.GraphNode{ID: "join", Kind: entities.JoinNode})
	for i := 0; i < 10; i++ {
		graph.Edges = append(graph.Edges, entities.GraphEdge{From: fmt.Sprintf("agent%d", i), To: "join"})
	}
	run := &entities.GraphRun{BaseEntity: entities.BaseEntity{ID: "run"}}
	master := NewJobMaster(state, rm, utils.NewLogger("TEXT", "ERROR"), &JobManagerConfig{MaxConcurrentNodes: 3})
	if err := master.Execute(context.Background(), run, graph); err != nil {
		t.Fatal(err)
	}
	if run.Status != entities.GraphRunCompleted || rm.peak < 2 || rm.peak > 3 || rm.calls != 11 {
		t.Fatalf("status %s peak %d calls %d", run.Status, rm.peak, rm.calls)
	}
	// New JM process state, same committed attempts: none of the 10 branches rerun.
	restored := NewJobMaster(state, rm, utils.NewLogger("TEXT", "ERROR"), &JobManagerConfig{MaxConcurrentNodes: 3})
	if err := restored.Execute(context.Background(), run, graph); err != nil {
		t.Fatal(err)
	}
	if rm.calls != 11 {
		t.Fatal("completed nodes reran after recovery")
	}
}
func TestAdaptiveSubGraphUsesSameSchedulerAndSeals(t *testing.T) {
	state := &durableTestState{tasks: map[string]*entities.NodeRun{}, graph: &entities.SubGraph{InstanceID: "repair", Revision: 1, Config: entities.SubGraphConfig{Ref: "child", Mode: "adaptive", Supervisor: "planner", Mutation: entities.MutationPolicy{Allow: []string{"append", "update_pending_edges", "seal"}}, Limits: entities.AgentGraphLimits{MaxPatches: 3}}, Graph: entities.AgentGraph{Name: "child", BaseEntity: entities.BaseEntity{ID: "child"}, Nodes: []entities.GraphNode{{ID: "seed", Kind: entities.NoopNode}}}}}
	rm := &parallelTestRM{}
	master := NewJobMaster(state, rm, utils.NewLogger("TEXT", "ERROR"), nil)
	graph := &entities.AgentGraph{Name: "parent", BaseEntity: entities.BaseEntity{ID: "parent", Namespace: "default"}, Nodes: []entities.GraphNode{{ID: "repair", Kind: entities.SubGraphNode, SubGraph: &entities.SubGraphConfig{Ref: "child"}}, {ID: "delivery", Kind: entities.NoopNode}}, Edges: []entities.GraphEdge{{From: "repair", To: "delivery"}}}
	run := &entities.GraphRun{BaseEntity: entities.BaseEntity{ID: "run"}}
	if err := master.Execute(context.Background(), run, graph); err != nil {
		t.Fatal(err)
	}
	if !state.graph.Sealed || state.graph.Revision != 3 || run.Status != entities.GraphRunCompleted {
		t.Fatalf("subgraph not sealed: %#v run %s", state.graph, run.Status)
	}
	if state.updates != 2 {
		t.Fatalf("child wrote parent lifecycle %d times", state.updates)
	}
	if rm.calls != 5 {
		t.Fatalf("unexpected reexecution %d", rm.calls)
	}
}
