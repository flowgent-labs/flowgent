package entities

// ExecutionMode defines whether a flow runs on a shared cluster or a dedicated one.
type ExecutionMode string

const (
	ModeApplication ExecutionMode = "application"
)

// Priority defines the scheduling precedence of an agentflow.
//
// Session mode (priorities low/medium/high routed to a shared, Helm-managed
// JM/TM pool via a PENDING run with namespace="") has been temporarily
// disabled to simplify troubleshooting — see docs/01-L1-Engine-Architecture.md
// §1.1/§4.3. Every flow currently runs in Application mode (a dedicated
// per-flow JM Deployment created by the Controller).
//
// The Priority field itself is intentionally kept on AgentFlowInfo/FlowRunInfo
// (not removed) — this reserves the config surface so Session mode can be
// reintroduced later without an API/schema break. For now PriorityHigh is the
// only value the API accepts (see handler.NormalizePriority); the old
// PriorityLow/PriorityMedium/PriorityGrade constants have been removed since
// nothing in the current codebase branches on them.
type Priority string

const (
	// PriorityHigh is the only priority value currently accepted by the API.
	// Every flow runs in Application mode regardless.
	PriorityHigh Priority = "high"
)
