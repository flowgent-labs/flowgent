package engine

import "sync"

// DAGScheduler handles topological ordering of node execution.
type DAGScheduler struct {
	mu             sync.Mutex
	nodes          []string
	edges          [][2]string
	edgeConditions map[string]*bool        // key: "from->to" → condition value
	deps           map[string][]string
	children       map[string][]string
	completed      map[string]bool
	skipped        map[string]bool
	failed         map[string]bool
	pending        map[string]bool
	conditions     map[string]bool
	injected       map[string][]string
}

// EdgeCondition stores an edge-level condition for routing.
type EdgeCondition struct {
	From      string
	To        string
	Condition *bool
}

// SetEdgeConditions registers edge-level conditions.
func (s *DAGScheduler) SetEdgeConditions(ecs []EdgeCondition) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.edgeConditions == nil {
		s.edgeConditions = make(map[string]*bool)
	}
	for _, ec := range ecs {
		key := ec.From + "->" + ec.To
		s.edgeConditions[key] = ec.Condition
	}
}

// GetChildCondition returns the condition for the given edge, if any.
func (s *DAGScheduler) GetChildCondition(from, to string) *bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.edgeConditions == nil {
		return nil
	}
	key := from + "->" + to
	return s.edgeConditions[key]
}

func NewDAGScheduler(nodes []string, edges [][2]string) *DAGScheduler {
	s := &DAGScheduler{
		nodes:      nodes,
		edges:      edges,
		deps:       make(map[string][]string),
		children:   make(map[string][]string),
		completed:  make(map[string]bool),
		skipped:    make(map[string]bool),
		failed:     make(map[string]bool),
		pending:    make(map[string]bool),
		conditions: make(map[string]bool),
		injected:   make(map[string][]string),
	}
	for _, n := range nodes {
		s.deps[n] = []string{}
		s.children[n] = []string{}
		s.pending[n] = true
	}
	for _, e := range edges {
		s.deps[e[1]] = append(s.deps[e[1]], e[0])
		s.children[e[0]] = append(s.children[e[0]], e[1])
	}
	return s
}

func (s *DAGScheduler) Ready() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ready []string
	for _, n := range s.nodes {
		if s.completed[n] || s.skipped[n] || s.failed[n] {
			continue
		}
		if s.allDepsDone(n) {
			ready = append(ready, n)
		}
	}
	return ready
}

func (s *DAGScheduler) allDepsDone(node string) bool {
	deps := s.deps[node]
	if injDeps, ok := s.injected[node]; ok {
		deps = append(deps, injDeps...)
	}
	for _, d := range deps {
		if s.skipped[d] {
			continue
		}
		if !s.completed[d] {
			return false
		}
	}
	return true
}

func (s *DAGScheduler) Done(node string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completed[node] = true
	s.pending[node] = false
}

func (s *DAGScheduler) Skip(node string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.skipped[node] = true
	s.pending[node] = false
}

func (s *DAGScheduler) Fail(node string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failed[node] = true
	s.pending[node] = false
}

func (s *DAGScheduler) IsComplete() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range s.nodes {
		if !s.completed[n] && !s.skipped[n] && !s.failed[n] {
			return false
		}
	}
	return true
}

func (s *DAGScheduler) HasFailed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range s.nodes {
		if s.failed[n] {
			return true
		}
	}
	return false
}

func (s *DAGScheduler) SetConditionResult(node string, result bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conditions[node] = result
}

func (s *DAGScheduler) ConditionResult(node string) (bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.conditions[node]
	return r, ok
}

func (s *DAGScheduler) Inject(node string, dependsOn []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.injected[node] = dependsOn
	s.nodes = append(s.nodes, node)
	s.deps[node] = dependsOn
	s.children[node] = []string{}
	s.pending[node] = true
}

func (s *DAGScheduler) Children(node string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.children[node]
}

func (s *DAGScheduler) Deps(node string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deps[node]
}
