package entities

// ExecutionMode defines whether a flow runs on a shared cluster or a dedicated one.
type ExecutionMode string

const (
	ModeSession     ExecutionMode = "session"
	ModeApplication ExecutionMode = "application"
)

// Priority defines the scheduling precedence of an agentflow.
type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityMedium Priority = "medium"
	PriorityHigh   Priority = "high"
	PriorityGrade  Priority = "grade"
)

// IsApplication returns true if the priority demands a dedicated cluster.
func (p Priority) IsApplication() bool { return p == PriorityGrade }
