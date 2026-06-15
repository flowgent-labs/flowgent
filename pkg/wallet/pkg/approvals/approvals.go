// Package approvals integrates payment approval with Flowgent's existing
// human approval node runtime. When a payment exceeds the configured threshold,
// the workflow pauses in WAITING_HUMAN state until an external API call
// approves or rejects the payment.
package approvals

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg/approval"
	"github.com/flowgent-labs/flowgent/wallet/pkg"
)

// PaymentApprover implements pwf.ApprovalHandler using Flowgent's existing
// human approval infrastructure. No second approval subsystem.
type PaymentApprover struct {
	store   approval.IApprovalStore
	timeout time.Duration
}

// New creates a payment approver that reuses the existing human approval store.
func New(store approval.IApprovalStore, timeout time.Duration) *PaymentApprover {
	if timeout <= 0 {
		timeout = 24 * time.Hour
	}
	return &PaymentApprover{
		store:   store,
		timeout: timeout,
	}
}

// RequestApproval creates a human approval request for a payment intent.
// The workflow pauses until the approval is resolved via the external API.
func (a *PaymentApprover) RequestApproval(ctx context.Context, intent *payments.PaymentIntent) (*payments.PaymentReceipt, error) {
	token := uuid.NewString()
	expiresAt := time.Now().Add(a.timeout)

	approval := &entities.ApprovalInfo{
		TaskRunID: intent.ID,
		Token:     token,
		Status:    "PENDING",
		Timeout:   a.timeout,
		ExpiresAt: &expiresAt,
	}

	if err := a.store.CreateApproval(ctx, approval); err != nil {
		return nil, fmt.Errorf("create payment approval: %w", err)
	}

	// Poll for resolution (in production, this would be event-driven via API callbacks)
	// For the synchronous path, we block and poll
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	deadline := time.After(a.timeout)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline:
			approval.Status = "EXPIRED"
			rejected := false
			approval.Approved = &rejected
			_ = a.store.UpdateApproval(ctx, approval)
			return nil, &payments.PaymentError{
				Code:    "APPROVAL_TIMEOUT",
				Message: fmt.Sprintf("payment approval %s timed out after %s", token, a.timeout),
			}
		case <-ticker.C:
			updated, err := a.store.Get(ctx, token)
			if err != nil {
				continue
			}
			if updated.Status == "APPROVED" {
				return nil, nil // Approved — proceed to payment
			}
			if updated.Status == "REJECTED" {
				return nil, &payments.PaymentError{
					Code:    "APPROVAL_REJECTED",
					Message: "payment approval " + token + " was rejected",
				}
			}
		}
	}
}
