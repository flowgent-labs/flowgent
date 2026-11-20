package trigger

import (
	"context"
	"log/slog"
	"sync"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/robfig/cron/v3"
)

// ScheduleTrigger manages cron-based agentgraph triggers.
type ScheduleTrigger struct {
	mu       sync.Mutex
	cron     *cron.Cron
	specs    map[string]string
	triggers map[string]func(context.Context, string)
}

func NewScheduleTrigger() *ScheduleTrigger {
	return &ScheduleTrigger{
		cron:     cron.New(),
		specs:    make(map[string]string),
		triggers: make(map[string]func(context.Context, string)),
	}
}

func (s *ScheduleTrigger) RegisterAgentGraphs(graphs []entities.AgentGraph, triggerFn func(context.Context, string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range graphs {
		for _, t := range f.Triggers {
			if t.Type == "schedule" && t.Cron != "" {
				s.specs[f.ID] = t.Cron
				s.triggers[f.ID] = triggerFn
				graphID := f.ID
				cronExpr := t.Cron
				s.cron.AddFunc(cronExpr, func() {
					slog.Info("cron trigger fired", "subgraph", graphID, "cron", cronExpr)
					triggerFn(context.Background(), graphID)
				})
				slog.Info("registered cron schedule", "subgraph", graphID, "cron", cronExpr)
			}
		}
	}
}

func (s *ScheduleTrigger) Start() {
	s.cron.Start()
}

func (s *ScheduleTrigger) Stop() {
	s.cron.Stop()
}

func (s *ScheduleTrigger) Clear() {
	s.cron.Stop()
	s.cron = cron.New()
	s.specs = make(map[string]string)
	s.triggers = make(map[string]func(context.Context, string))
}
