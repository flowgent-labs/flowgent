package jobmanager

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

type candidateRecorder struct {
	items chan *entities.KnowledgeCandidate
}

func (r *candidateRecorder) CreateKnowledgeCandidate(_ context.Context, _ string,
	candidate *entities.KnowledgeCandidate,
) (*entities.ApprovalInfo, error) {
	copy := *candidate
	r.items <- &copy
	return &entities.ApprovalInfo{}, nil
}

func TestPostHandleRequiresSuccessfulEffectiveSummarization(t *testing.T) {
	recorder := &candidateRecorder{items: make(chan *entities.KnowledgeCandidate, 2)}
	jm := NewJobMaster(nil, nil, nil, nil)
	jm.SetKnowledgeWriter(recorder)
	jm.nodeOutputs = map[string]map[string]any{"node": {"result": "safe"}}
	spec := &entities.AgentGraph{BaseEntity: entities.BaseEntity{ID: "stable-graph", Namespace: "team"}, Name: "fixer"}

	jm.postHandle(&entities.GraphRun{BaseEntity: entities.BaseEntity{ID: "disabled"},
		Status: entities.GraphRunCompleted, SummarizeEnabled: false}, spec)
	jm.postHandle(&entities.GraphRun{BaseEntity: entities.BaseEntity{ID: "failed"},
		Status: entities.GraphRunFailed, SummarizeEnabled: true}, spec)
	select {
	case candidate := <-recorder.items:
		t.Fatalf("disabled or failed run created candidate: %+v", candidate)
	default:
	}
}

func TestPostHandleCreatesSanitizedApprovalCandidate(t *testing.T) {
	recorder := &candidateRecorder{items: make(chan *entities.KnowledgeCandidate, 1)}
	jm := NewJobMaster(nil, nil, nil, nil)
	jm.SetKnowledgeWriter(recorder)
	jm.nodeOutputs = map[string]map[string]any{
		"scan": {
			"finding": "verified alpha",
			"api_key": "must-never-leak",
			"nested":  map[string]any{"authorization": "Bearer abcdefghijklmnop", "count": 2},
		},
	}
	spec := &entities.AgentGraph{BaseEntity: entities.BaseEntity{ID: "stable-graph", Namespace: "team"}, Name: "fixer"}
	run := &entities.GraphRun{BaseEntity: entities.BaseEntity{ID: "run-1", Namespace: "team"},
		AgentGraphID: "stable-graph", Status: entities.GraphRunCompleted, SummarizeEnabled: true}
	jm.postHandle(run, spec)
	select {
	case candidate := <-recorder.items:
		if candidate.TargetScope != "graph" || candidate.TargetAgentGraphID != "stable-graph" ||
			candidate.TargetAgentGraphName != "fixer" || candidate.Type != "knowledge" {
			t.Fatalf("candidate target = %+v", candidate)
		}
		if strings.Contains(candidate.Content, "must-never-leak") ||
			strings.Contains(strings.ToLower(candidate.Content), "bearer") ||
			!strings.Contains(candidate.Content, "verified alpha") {
			t.Fatalf("unsafe summary content: %s", candidate.Content)
		}
		var body map[string]any
		if err := json.Unmarshal([]byte(candidate.Content), &body); err != nil {
			t.Fatalf("candidate is not JSON: %v", err)
		}
		if candidate.Provenance["origin"] != "built_in_summarizer" || candidate.ApprovalID != "" {
			t.Fatalf("candidate provenance/approval = %+v", candidate)
		}
	case <-time.After(time.Second):
		t.Fatal("completed summarized run did not create a candidate")
	}
}

func TestSanitizeSummaryRejectsCredentialLikeStrings(t *testing.T) {
	for _, value := range []string{
		"Authorization: Bearer abcdefghijklmnop",
		"api_key=sk-secretvalue123456",
		"-----BEGIN PRIVATE KEY-----",
	} {
		if sanitized, ok := sanitizeSummaryValue(value); ok {
			t.Fatalf("credential %q survived as %#v", value, sanitized)
		}
	}
}
