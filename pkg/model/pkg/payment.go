package model

import (
	"time"

	"github.com/shopspring/decimal"
)

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
	IntentPending  IntentStatus = "PENDING"
	IntentApproved IntentStatus = "APPROVED"
	IntentDenied   IntentStatus = "DENIED"
	IntentPaid     IntentStatus = "PAID"
	IntentFailed   IntentStatus = "FAILED"
	IntentExpired  IntentStatus = "EXPIRED"
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
