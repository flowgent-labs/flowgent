package workflow

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
		t.Fatalf("session: %v", err)
	}
	return &wfIC{Context: context.Background(), ag: ag, sess: r.Session}
}

type wfIC struct {
	context.Context
	ag   agent.Agent
	sess session.Session
}

func (c *wfIC) Agent() agent.Agent                  { return c.ag }
func (c *wfIC) Artifacts() agent.Artifacts          { return nil }
func (c *wfIC) Memory() agent.Memory                { return nil }
func (c *wfIC) Session() session.Session            { return c.sess }
func (c *wfIC) InvocationID() string                { return "" }
func (c *wfIC) Branch() string                      { return "" }
func (c *wfIC) UserContent() *genai.Content         { return nil }
func (c *wfIC) RunConfig() *agent.RunConfig         { return nil }
func (c *wfIC) EndInvocation()                      {}
func (c *wfIC) Ended() bool                         { return false }
func (c *wfIC) WithContext(x context.Context) agent.InvocationContext {
	return &wfIC{Context: x, ag: c.ag, sess: c.sess}
}
func (c *wfIC) AgentName() string                    { return c.ag.Name() }
func (c *wfIC) ReadonlyState() session.ReadonlyState { return c.sess.State() }
func (c *wfIC) State() session.State                 { return c.sess.State() }
func (c *wfIC) AppName() string                      { return "t" }
func (c *wfIC) SessionID() string                    { return c.sess.ID() }
func (c *wfIC) UserID() string                       { return c.sess.UserID() }
func (c *wfIC) FunctionCallID() string               { return "" }
func (c *wfIC) Actions() *session.EventActions       { return nil }

func run(text string) func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
		return func(yield func(*session.Event, error) bool) {
			yield(&session.Event{LLMResponse: model.LLMResponse{Content: &genai.Content{Parts: []*genai.Part{{Text: text}}}}}, nil)
		}
	}
}

func TestAlphaAgentsParallelExecution(t *testing.T) {
	names := []string{"build_test_alpha", "foss_alpha", "code_quality_alpha", "cyberflow_alpha"}
	var ags []agent.Agent
	for _, n := range names {
		a, _ := agent.New(agent.Config{Name: n, Description: n, Run: run(n)})
		ags = append(ags, a)
	}

	alphaGroup, err := parallelagent.New(parallelagent.Config{
		AgentConfig: agent.Config{Name: "alpha_group", Description: "", SubAgents: ags},
	})
	if err != nil {
		t.Fatal(err)
	}

	count := 0
	for ev, err := range alphaGroup.Run(mkIC(t, alphaGroup)) {
		if err != nil {
			t.Fatal(err)
		}
		if ev != nil {
			count++
		}
	}
	if count != 4 {
		t.Fatalf("expected 4 events, got %d", count)
	}
}

func TestReviewBoardVoting(t *testing.T) {
	var ags []agent.Agent
	for i := 1; i <= 3; i++ {
		a, _ := agent.New(agent.Config{Name: "r" + string(rune('0'+i)), Description: "", Run: run("ACCEPT")})
		ags = append(ags, a)
	}

	rb, err := parallelagent.New(parallelagent.Config{
		AgentConfig: agent.Config{Name: "review_board", Description: "", SubAgents: ags},
	})
	if err != nil {
		t.Fatal(err)
	}

	accepted := 0
	for ev, err := range rb.Run(mkIC(t, rb)) {
		if err != nil {
			t.Fatal(err)
		}
		if ev != nil && ev.LLMResponse.Content != nil {
			for _, p := range ev.LLMResponse.Content.Parts {
				if p.Text == "ACCEPT" {
					accepted++
				}
			}
		}
	}
	if accepted != 3 {
		t.Fatalf("expected 3 ACCEPT, got %d", accepted)
	}
}

func TestFullSecurityFixWorkflow(t *testing.T) {
	alpha1, _ := agent.New(agent.Config{Name: "foss_alpha", Description: "", Run: run("FOSS fixed")})
	alpha2, _ := agent.New(agent.Config{Name: "build_alpha", Description: "", Run: run("Build fixed")})
	reviewer, _ := agent.New(agent.Config{Name: "reviewer", Description: "", Run: run("APPROVED")})

	alphaGroup, _ := parallelagent.New(parallelagent.Config{
		AgentConfig: agent.Config{Name: "alpha_group", Description: "", SubAgents: []agent.Agent{alpha1, alpha2}},
	})

	supervisor, err := sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{Name: "supervisor", Description: "", SubAgents: []agent.Agent{alphaGroup, reviewer}},
	})
	if err != nil {
		t.Fatal(err)
	}

	count := 0
	for ev, err := range supervisor.Run(mkIC(t, supervisor)) {
		if err != nil {
			t.Fatal(err)
		}
		if ev != nil {
			count++
		}
	}
	if count < 3 {
		t.Fatalf("expected >=3 events, got %d", count)
	}
}
