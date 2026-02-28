package executor

import (
	"encoding/json"
	"github.com/flowgent-labs/flowgent/src/model"
)

// ─── Helpers ───────────────────────────────────────────

func formatPlanInput(plan *model.ExecutionPlan) string {
	b, _ := json.Marshal(plan.Input)
	return string(b)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen { return s }
	return s[:maxLen] + "..."
}

