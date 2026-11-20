package entities

import "time"

// ApprovalInfo represents a pending human approval gate.
// The Status field tracks the approval decision state; it shadows BaseEntity.Status.
type ApprovalInfo struct {
	BaseEntity

	RunID          string         `json:"run_id,omitempty" yaml:"run_id,omitempty"`
	NodeRunID      string         `json:"node_run_id,omitempty" yaml:"node_run_id,omitempty"`
	Type           string         `json:"type" yaml:"type"`
	SubjectType    string         `json:"subject_type" yaml:"subject_type"`
	SubjectID      string         `json:"subject_id" yaml:"subject_id"`
	Request        map[string]any `json:"request" yaml:"request"`
	RequestHash    string         `json:"request_hash" yaml:"request_hash"`
	Status         string         `json:"status" yaml:"status"`
	Decision       map[string]any `json:"decision,omitempty" yaml:"decision,omitempty"`
	DecidedBy      string         `json:"decided_by,omitempty" yaml:"decided_by,omitempty"`
	DecidedAt      *time.Time     `json:"decided_at,omitempty" yaml:"decided_at,omitempty"`
	ExpiresAt      *time.Time     `json:"expires_at,omitempty" yaml:"expires_at,omitempty"`
	ConsumedAt     *time.Time     `json:"consumed_at,omitempty" yaml:"consumed_at,omitempty"`
	IdempotencyKey string         `json:"idempotency_key" yaml:"idempotency_key"`

	TaskRunID      string        `json:"-" yaml:"-" db:"-"`
	AgentFlowRunID string        `json:"-" yaml:"-" db:"-"`
	Token          string        `json:"token,omitempty" yaml:"-" db:"-"`
	Approved       *bool         `json:"-" yaml:"-" db:"-"`
	Comment        string        `json:"-" yaml:"-" db:"-"`
	Timeout        time.Duration `json:"-" yaml:"-" db:"-"`
	ResolvedAt     *time.Time    `json:"-" yaml:"-" db:"-"`
}

func (a *ApprovalInfo) NormalizeAliases() {
	if a.ID == "" {
		a.ID = a.Token
	}
	if a.Token == "" {
		a.Token = a.ID
	}
	if a.RunID == "" {
		a.RunID = a.AgentFlowRunID
	}
	if a.AgentFlowRunID == "" {
		a.AgentFlowRunID = a.RunID
	}
	if a.NodeRunID == "" {
		a.NodeRunID = a.TaskRunID
	}
	if a.TaskRunID == "" {
		a.TaskRunID = a.NodeRunID
	}
	if a.ResolvedAt == nil {
		a.ResolvedAt = a.DecidedAt
	}
	if a.DecidedAt == nil {
		a.DecidedAt = a.ResolvedAt
	}
}
