package polling

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type TraceKey struct{}

type AgentRunTrace struct {
	TraceID      string    `json:"trace_id"`
	AgentID      string    `json:"agent_id"`
	Action       string    `json:"action"`
	Status       string    `json:"status"`
	StartTime    time.Time `json:"start_time"`
	HandoffData  string    `json:"handoff_data"`
}

func WithContext(ctx context.Context) (context.Context, string) {
	id := uuid.New().String()
	return context.WithValue(ctx, TraceKey{}, id), id
}

func GetTraceID(ctx context.Context) string {
	id, ok := ctx.Value(TraceKey{}).(string)
	if !ok || id == "" {
		return uuid.New().String()
	}
	return id
}

func NewTrace(agentID, action string, ctx context.Context) *AgentRunTrace {
	return &AgentRunTrace{
		TraceID:     GetTraceID(ctx),
		AgentID:     agentID,
		Action:      action,
		Status:      "pending",
		StartTime:   time.Now(),
		HandoffData: "",
	}
}