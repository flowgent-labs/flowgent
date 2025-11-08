package agent_e2e

import (
	"context"
	"iter"
	"testing"

	"google.golang.org/genai"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/workflowagents/parallelagent"
	"google.golang.org/adk/agent/workflowagents/sequentialagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
)

func mkIC(t *testing.T, ag agent.Agent) agent.InvocationContext {
	t.Helper()
	svc := session.InMemoryService()
	r, err := svc.Create(context.Background(), &session.CreateRequest{AppName: "t", UserID: "u"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return &ic{Context: context.Background(), ag: ag, sess: r.Session}
}

type ic struct {
	context.Context
	ag   agent.Agent
	sess session.Session
}

func (c *ic) Agent() agent.Agent                  { return c.ag }
func (c *ic) Artifacts() agent.Artifacts          { return nil }
func (c *ic) Memory() agent.Memory                { return nil }
func (c *ic) Session() session.Session            { return c.sess }
func (c *ic) InvocationID() string                { return "" }
func (c *ic) Branch() string                      { return "" }
func (c *ic) UserContent() *genai.Content         { return nil }
func (c *ic) RunConfig() *agent.RunConfig         { return nil }
func (c *ic) EndInvocation()                      {}
func (c *ic) Ended() bool                         { return false }
func (c *ic) WithContext(x context.Context) agent.InvocationContext {
	return &ic{Context: x, ag: c.ag, sess: c.sess}
}
func (c *ic) AgentName() string                    { return c.ag.Name() }
func (c *ic) ReadonlyState() session.ReadonlyState { return c.sess.State() }
func (c *ic) State() session.State                 { return c.sess.State() }
func (c *ic) AppName() string                      { return "t" }
func (c *ic) SessionID() string                    { return c.sess.ID() }
func (c *ic) UserID() string                       { return c.sess.UserID() }
func (c *ic) FunctionCallID() string               { return "" }
func (c *ic) Actions() *session.EventActions       { return nil }

func runAgent(text string) func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
		return func(yield func(*session.Event, error) bool) {
			yield(&session.Event{LLMResponse: model.LLMResponse{Content: &genai.Content{Parts: []*genai.Part{{Text: text}}}}}, nil)
		}
	}
}

func TestSequentialAgentExecutesSubAgentsInOrder(t *testing.T) {
	names := []string{"A", "B", "C"}
	var ags []agent.Agent
	for _, n := range names {
		a, _ := agent.New(agent.Config{Name: n, Description: n, Run: runAgent(n)})
		ags = append(ags, a)
	}

	seq, err := sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{Name: "seq", Description: "", SubAgents: ags},
	})
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for ev, err := range seq.Run(mkIC(t, seq)) {
		if err != nil {
			t.Fatal(err)
		}
		if ev != nil && ev.LLMResponse.Content != nil {
			for _, p := range ev.LLMResponse.Content.Parts {
				got = append(got, p.Text)
			}
		}
	}

	expect := "ABC"
	if s := stringJoin(got); s != expect {
		t.Fatalf("expected %q, got %q", expect, s)
	}
}

func TestParallelAgentRunsSubAgentsConcurrently(t *testing.T) {
	var ags []agent.Agent
	for i := 0; i < 4; i++ {
		n := string(rune('A' + i))
		a, _ := agent.New(agent.Config{Name: n, Description: n, Run: runAgent(n)})
		ags = append(ags, a)
	}

	par, err := parallelagent.New(parallelagent.Config{
		AgentConfig: agent.Config{Name: "par", Description: "", SubAgents: ags},
	})
	if err != nil {
		t.Fatal(err)
	}

	count := 0
	var got []string
	for ev, err := range par.Run(mkIC(t, par)) {
		if err != nil {
			t.Fatal(err)
		}
		if ev != nil && ev.LLMResponse.Content != nil {
			count++
			for _, p := range ev.LLMResponse.Content.Parts {
				got = append(got, p.Text)
			}
		}
	}
	if count != 4 {
		t.Fatalf("expected 4 events, got %d", count)
	}
}

func TestNestedWorkflowAgents(t *testing.T) {
	inner1, _ := agent.New(agent.Config{Name: "inner1", Description: "", Run: runAgent("1")})
	inner2, _ := agent.New(agent.Config{Name: "inner2", Description: "", Run: runAgent("2")})

	par, _ := parallelagent.New(parallelagent.Config{
		AgentConfig: agent.Config{Name: "inner_par", Description: "", SubAgents: []agent.Agent{inner1, inner2}},
	})

	wrapper, _ := agent.New(agent.Config{
		Name: "wrapper", Description: "",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return par.Run(ctx)
		},
	})

	seq, _ := sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{Name: "outer_seq", Description: "", SubAgents: []agent.Agent{wrapper}},
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
	if count != 2 {
		t.Fatalf("expected 2 events, got %d", count)
	}
}

func stringJoin(parts []string) string {
	s := ""
	for _, p := range parts {
		s += p
	}
	return s
}
