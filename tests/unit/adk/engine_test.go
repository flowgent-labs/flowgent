package adk_test

import (
	"context"
	"iter"
	"testing"

	"google.golang.org/genai"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/workflowagents/sequentialagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
)

func mkIC(t *testing.T, ag agent.Agent) agent.InvocationContext {
	t.Helper()
	svc := session.InMemoryService()
	r, err := svc.Create(context.Background(), &session.CreateRequest{AppName: "t", UserID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	return &uc{Context: context.Background(), ag: ag, sess: r.Session}
}

type uc struct {
	context.Context
	ag   agent.Agent
	sess session.Session
}

func (c *uc) Agent() agent.Agent                  { return c.ag }
func (c *uc) Artifacts() agent.Artifacts          { return nil }
func (c *uc) Memory() agent.Memory                { return nil }
func (c *uc) Session() session.Session            { return c.sess }
func (c *uc) InvocationID() string                { return "" }
func (c *uc) Branch() string                      { return "" }
func (c *uc) UserContent() *genai.Content         { return nil }
func (c *uc) RunConfig() *agent.RunConfig         { return nil }
func (c *uc) EndInvocation()                      {}
func (c *uc) Ended() bool                         { return false }
func (c *uc) WithContext(x context.Context) agent.InvocationContext {
	return &uc{Context: x, ag: c.ag, sess: c.sess}
}
func (c *uc) AgentName() string                    { return c.ag.Name() }
func (c *uc) ReadonlyState() session.ReadonlyState { return c.sess.State() }
func (c *uc) State() session.State                 { return c.sess.State() }
func (c *uc) AppName() string                      { return "t" }
func (c *uc) SessionID() string                    { return c.sess.ID() }
func (c *uc) UserID() string                       { return c.sess.UserID() }
func (c *uc) FunctionCallID() string               { return "" }
func (c *uc) Actions() *session.EventActions       { return nil }

func run(text string) func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
		return func(yield func(*session.Event, error) bool) {
			yield(&session.Event{LLMResponse: model.LLMResponse{Content: &genai.Content{Parts: []*genai.Part{{Text: text}}}}}, nil)
		}
	}
}

func TestAgentNewCreatesValidAgent(t *testing.T) {
	ag, err := agent.New(agent.Config{Name: "test_agent", Description: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if ag.Name() != "test_agent" {
		t.Fatalf("expected test_agent, got %s", ag.Name())
	}
}

func TestSequentialAgentPreservesOrder(t *testing.T) {
	captured := []string{}
	names := []string{"1st", "2nd", "3rd"}
	var ags []agent.Agent
	for _, n := range names {
		name := n
		a, _ := agent.New(agent.Config{
			Name: name, Description: name,
			Run: func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
				return func(yield func(*session.Event, error) bool) {
					captured = append(captured, name)
					yield(&session.Event{LLMResponse: model.LLMResponse{Content: &genai.Content{Parts: []*genai.Part{{Text: name}}}}}, nil)
				}
			},
		})
		ags = append(ags, a)
	}

	seq, err := sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{Name: "seq", Description: "", SubAgents: ags},
	})
	if err != nil {
		t.Fatal(err)
	}

	var events []*session.Event
	for ev, err := range seq.Run(mkIC(t, seq)) {
		if err != nil {
			t.Fatal(err)
		}
		if ev != nil {
			events = append(events, ev)
		}
	}

	if len(captured) != 3 {
		t.Fatalf("expected 3, got %d", len(captured))
	}
	for i, n := range names {
		if captured[i] != n {
			t.Fatalf("order mismatch: %v vs %v", names, captured)
		}
	}
}

func TestAgentTreeFindSubAgent(t *testing.T) {
	leaf, _ := agent.New(agent.Config{Name: "leaf", Description: "leaf"})
	parent, _ := agent.New(agent.Config{Name: "parent", Description: "", SubAgents: []agent.Agent{leaf}})

	found := parent.FindSubAgent("leaf")
	if found == nil {
		t.Fatal("expected leaf")
	}
	if found.Name() != "leaf" {
		t.Fatalf("expected leaf, got %s", found.Name())
	}

	found = parent.FindSubAgent("missing")
	if found != nil {
		t.Fatal("expected nil")
	}
}

func TestAgentWithCustomRunFunction(t *testing.T) {
	called := false
	ag, err := agent.New(agent.Config{
		Name: "custom", Description: "",
		Run: func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				called = true
				yield(&session.Event{LLMResponse: model.LLMResponse{Content: &genai.Content{Parts: []*genai.Part{{Text: "out"}}}}}, nil)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	for ev, err := range ag.Run(mkIC(t, ag)) {
		if err != nil {
			t.Fatal(err)
		}
		_ = ev
	}
	if !called {
		t.Fatal("run not called")
	}
}

func TestMultipleSequentialLayers(t *testing.T) {
	var ags []agent.Agent
	for i := 0; i < 3; i++ {
		n := string(rune('A' + i))
		a, _ := agent.New(agent.Config{Name: n, Description: n, Run: run(n)})
		ags = append(ags, a)
	}

	seq, _ := sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{Name: "seq", Description: "", SubAgents: ags},
	})

	count := 0
	for ev, err := range seq.Run(mkIC(t, seq)) {
		if err != nil {
			t.Fatal(err)
		}
		if ev != nil {
			count++
		}
	}
	if count != 3 {
		t.Fatalf("expected 3 events, got %d", count)
	}
}
