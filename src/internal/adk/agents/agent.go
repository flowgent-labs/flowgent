package agents

import (
	"context"

	"cyberbot/src/internal/adk/polling"
)

type BaseAgent struct {
	ID           string
	Model        string
	Temperature  float64
	SystemPrompt string
}

func (a *BaseAgent) Execute(ctx context.Context, input string) (string, error) {
	tr := polling.NewTrace(a.ID, "base_execute", ctx)
	return "", nil
}