package engine

import (
	"context"
	"log/slog"
	"sync"

	"github.com/flowgent-labs/flowgent/src/model"
	"github.com/robfig/cron/v3"
)

// ScheduleTrigger manages cron-based agentflow triggers.
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

func (s *ScheduleTrigger) RegisterAgentFlows(flows []model.AgentFlowSpec, triggerFn func(context.Context, string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range flows {
		for _, t := range f.Triggers {
			if t.Type == "schedule" && t.Cron != "" {
				s.specs[f.ID] = t.Cron
				s.triggers[f.ID] = triggerFn
				flowID := f.ID
				cronExpr := t.Cron
				s.cron.AddFunc(cronExpr, func() {
					slog.Info("cron trigger fired", "agentflow", flowID, "cron", cronExpr)
					triggerFn(context.Background(), flowID)
				})
				slog.Info("registered cron schedule", "agentflow", flowID, "cron", cronExpr)
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
