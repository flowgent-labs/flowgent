// Package model defines the shared domain types for the Flowgent engine.
//
// File: approval.go — Human-in-the-loop approval consumed by API Server, HumanExecutor.
//   HumanApproval, HumanApprovalStore interface.
package model

import (
	"context"
	"time"
)

// HumanApprovalStore is the minimal interface for human approval persistence.
// Both the engine and the payments module consume this interface,
// ensuring a single approval subsystem.
type HumanApprovalStore interface {
	CreateHumanApproval(ctx context.Context, approval *HumanApproval) error
	GetHumanApproval(ctx context.Context, token string) (*HumanApproval, error)
	UpdateHumanApproval(ctx context.Context, approval *HumanApproval) error
}

// HumanApproval represents a pending human approval gate.
type HumanApproval struct {
	TaskRunID      string        `json:"task_run_id" yaml:"task_run_id"`
	AgentFlowRunID string        `json:"agentflow_run_id" yaml:"agentflow_run_id"`
	Token          string        `json:"token" yaml:"token"`
	Status         string        `json:"status" yaml:"status"`
	Approved       *bool         `json:"approved" yaml:"approved"`
	Comment        string        `json:"comment" yaml:"comment"`
	Timeout        time.Duration `json:"timeout" yaml:"timeout"`
	CreatedAt      time.Time     `json:"created_at" yaml:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at" yaml:"updated_at"`
	ExpiresAt      *time.Time    `json:"expires_at" yaml:"expires_at"`
	ResolvedAt     *time.Time    `json:"resolved_at" yaml:"resolved_at"`
}
