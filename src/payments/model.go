// Package payments provides the optional economic runtime layer for Flowgent.
// It implements x402 payment protocol client-side support, spending policies,
// wallet abstraction, and facilitator integration.
//
// Flowgent acts as a consumer-side economic runtime. Settlement is delegated
// to facilitator providers (e.g. Coinbase x402 facilitator).
package payments

import (
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

// ─── x402 types ────────────────────────────────────────────────

// X402PaymentRequest represents a parsed x402 payment request from an HTTP 402 response.
type X402PaymentRequest struct {
	Asset       string          `json:"asset"`
	Amount      decimal.Decimal `json:"amount"`
	Chain       string          `json:"chain"`
	Recipient   string          `json:"recipient"`
	Settlement  string          `json:"settlement"`
	Facilitator string          `json:"facilitator"`
}

// Validate checks that all required fields are present and valid.
func (pr *X402PaymentRequest) Validate() error {
	if pr.Asset == "" {
		return fmt.Errorf("x402: asset is required")
	}
	if pr.Amount.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("x402: amount must be positive")
	}
	if pr.Chain == "" {
		return fmt.Errorf("x402: chain is required")
	}
	if pr.Recipient == "" {
		return fmt.Errorf("x402: recipient is required")
	}
	if pr.Settlement != "" && pr.Settlement != "x402" {
		return fmt.Errorf("x402: unsupported settlement protocol: %s", pr.Settlement)
	}
	if pr.Facilitator == "" {
		return fmt.Errorf("x402: facilitator URL is required")
	}
	return nil
}

// ─── Payment intent ────────────────────────────────────────────

// PaymentIntent is created after parsing a 402 response. It is evaluated
// by the policy engine before any funds are authorized.
type PaymentIntent struct {
	ID          string          `json:"id"`
	URL         string          `json:"url"`
	Asset       string          `json:"asset"`
	Amount      decimal.Decimal `json:"amount"`
	Chain       string          `json:"chain"`
	Recipient   string          `json:"recipient"`
	Facilitator string          `json:"facilitator"`
	Status      IntentStatus    `json:"status"`
	CreatedAt   time.Time       `json:"created_at"`
}

type IntentStatus string

const (
	IntentPending   IntentStatus = "PENDING"
	IntentApproved  IntentStatus = "APPROVED"
	IntentDenied    IntentStatus = "DENIED"
	IntentPaid      IntentStatus = "PAID"
	IntentFailed    IntentStatus = "FAILED"
	IntentExpired   IntentStatus = "EXPIRED"
)

// ─── Payment receipt ───────────────────────────────────────────

// PaymentReceipt is the receipt returned by the facilitator after successful payment.
type PaymentReceipt struct {
	ID            string          `json:"id"`
	IntentID      string          `json:"intent_id"`
	TxHash        string          `json:"tx_hash,omitempty"`
	Asset         string          `json:"asset"`
	Amount        decimal.Decimal `json:"amount"`
	Chain         string          `json:"chain"`
	Facilitator   string          `json:"facilitator"`
	Authorization string          `json:"authorization"`
	PaidAt        time.Time       `json:"paid_at"`
	ExpiresAt     *time.Time      `json:"expires_at,omitempty"`
}

// ─── Payment authorization ─────────────────────────────────────

// PaymentAuthorization is the signed authorization sent to the facilitator.
type PaymentAuthorization struct {
	IntentID  string `json:"intent_id"`
	Wallet    string `json:"wallet"`
	Signature string `json:"signature"`
	Payload   string `json:"payload"`
}

// ─── Errors ────────────────────────────────────────────────────

// ErrPaymentDenied is returned when the policy engine denies a payment.
var ErrPaymentDenied = &PaymentError{Code: "PAYMENT_DENIED", Message: "payment denied by policy"}

// ErrPaymentRequiresApproval is returned when a payment requires human approval.
var ErrPaymentRequiresApproval = &PaymentError{Code: "PAYMENT_REQUIRES_APPROVAL", Message: "payment requires human approval"}

// PaymentError represents a payment-specific error.
type PaymentError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *PaymentError) Error() string {
	return e.Code + ": " + e.Message
}
