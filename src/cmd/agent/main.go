package main

import (
	"context"
	"fmt"
	"iter"
	"log"

	"google.golang.org/genai"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/workflowagents/parallelagent"
	"google.golang.org/adk/agent/workflowagents/sequentialagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
)

func main() {
	ctx := context.Background()

	supervisor := buildSupervisor()
	sessSvc := session.InMemoryService()
	resp, err := sessSvc.Create(ctx, &session.CreateRequest{
		AppName: "cyberbot-app",
		UserID:  "user-1",
	})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	ic := &invContext{
		Context: ctx,
		ag:      supervisor,
		sess:    resp.Session,
	}

	log.Println("Starting security fix bot")
	for event, err := range supervisor.Run(ic) {
		if err != nil {
			log.Printf("Event error: %v", err)
			continue
		}
		if event != nil && event.LLMResponse.Content != nil {
			for _, part := range event.LLMResponse.Content.Parts {
				if part.Text != "" {
					fmt.Println(part.Text)
				}
			}
		}
	}
}

type invContext struct {
	context.Context
	ag   agent.Agent
	sess session.Session
}

func (c *invContext) Agent() agent.Agent          { return c.ag }
func (c *invContext) Artifacts() agent.Artifacts  { return nil }
func (c *invContext) Memory() agent.Memory        { return nil }
func (c *invContext) Session() session.Session    { return c.sess }
func (c *invContext) InvocationID() string        { return "" }
func (c *invContext) Branch() string              { return "" }
func (c *invContext) UserContent() *genai.Content { return nil }
func (c *invContext) RunConfig() *agent.RunConfig { return nil }
func (c *invContext) EndInvocation()              {}
func (c *invContext) Ended() bool                 { return false }
func (c *invContext) WithContext(ctx context.Context) agent.InvocationContext {
	return &invContext{Context: ctx, ag: c.ag, sess: c.sess}
}
func (c *invContext) AgentName() string               { return c.ag.Name() }
func (c *invContext) ReadonlyState() session.ReadonlyState { return c.sess.State() }
func (c *invContext) State() session.State            { return c.sess.State() }
func (c *invContext) AppName() string                 { return "cyberbot-app" }
func (c *invContext) SessionID() string               { return c.sess.ID() }
func (c *invContext) UserID() string                  { return c.sess.UserID() }
func (c *invContext) FunctionCallID() string          { return "" }
func (c *invContext) Actions() *session.EventActions  { return nil }

var _ agent.CallbackContext = (*invContext)(nil)

func buildSupervisor() agent.Agent {
	alphaGroup := buildAlphaGroup()
	reviewBoard := buildReviewBoard()

	supervisor, err := sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{
			Name:        "supervisor",
			Description: "Orchestrates end-to-end security fix lifecycle",
			SubAgents:   []agent.Agent{alphaGroup, reviewBoard},
		},
	})
	if err != nil {
		panic(err)
	}
	return supervisor
}

func buildAlphaGroup() agent.Agent {
	buildAlpha, _ := agent.New(agent.Config{
		Name:        "build_test_alpha",
		Description: "Fixes broken builds by updating blocked dependencies",
		Run:         runBuildTestAlpha,
	})

	fossAlpha, _ := agent.New(agent.Config{
		Name:        "foss_alpha",
		Description: "Fixes FOSS dependency vulnerabilities via upgrade or waiver",
		Run:         runFOSSAlpha,
	})

	cqAlpha, _ := agent.New(agent.Config{
		Name:        "code_quality_alpha",
		Description: "Fixes SonarQube code quality issues",
		Run:         runCodeQualityAlpha,
	})

	cyberflowAlpha, _ := agent.New(agent.Config{
		Name:        "cyberflow_alpha",
		Description: "Fixes DAST/SAST/CONT vulnerabilities",
		Run:         runCyberflowAlpha,
	})

	parallel, err := parallelagent.New(parallelagent.Config{
		AgentConfig: agent.Config{
			Name:        "alpha_group",
			Description: "Parallel alpha agents solving specific issues",
			SubAgents:   []agent.Agent{buildAlpha, fossAlpha, cqAlpha, cyberflowAlpha},
		},
	})
	if err != nil {
		panic(err)
	}
	return parallel
}

func buildReviewBoard() agent.Agent {
	reviewer1, _ := agent.New(agent.Config{
		Name:        "security_reviewer",
		Description: "Reviews fixes for security correctness",
		Run:         runReviewer(1),
	})
	reviewer2, _ := agent.New(agent.Config{
		Name:        "code_quality_reviewer",
		Description: "Reviews fixes for code quality",
		Run:         runReviewer(2),
	})

	reviewBoard, err := parallelagent.New(parallelagent.Config{
		AgentConfig: agent.Config{
			Name:        "review_board",
			Description: "Multi-agent review panel",
			SubAgents:   []agent.Agent{reviewer1, reviewer2},
		},
	})
	if err != nil {
		panic(err)
	}
	return reviewBoard
}

func runBuildTestAlpha(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		yield(&session.Event{
			LLMResponse: model.LLMResponse{
				Content: &genai.Content{
					Parts: []*genai.Part{{Text: "BuildTestAlpha: Updated blocked dependency version via Nexus"}},
				},
			},
		}, nil)
	}
}

func runFOSSAlpha(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		yield(&session.Event{
			LLMResponse: model.LLMResponse{
				Content: &genai.Content{
					Parts: []*genai.Part{{Text: "FOSSAlpha: Upgraded vulnerable dependency to fixed version"}},
				},
			},
		}, nil)
	}
}

func runCodeQualityAlpha(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		yield(&session.Event{
			LLMResponse: model.LLMResponse{
				Content: &genai.Content{
					Parts: []*genai.Part{{Text: "CodeQualityAlpha: Refactored code to resolve SonarQube issues"}},
				},
			},
		}, nil)
	}
}

func runCyberflowAlpha(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		yield(&session.Event{
			LLMResponse: model.LLMResponse{
				Content: &genai.Content{
					Parts: []*genai.Part{{Text: "CyberflowAlpha: Applied CVE patches for DAST/SAST findings"}},
				},
			},
		}, nil)
	}
}

func runReviewer(id int) func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
		return func(yield func(*session.Event, error) bool) {
			yield(&session.Event{
				LLMResponse: model.LLMResponse{
					Content: &genai.Content{
						Parts: []*genai.Part{{Text: fmt.Sprintf("Reviewer %d: ACCEPT", id)}},
					},
				},
			}, nil)
		}
	}
}
