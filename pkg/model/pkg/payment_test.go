package model

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestPaymentIntent_Lifecycle(t *testing.T) {
	intent := &PaymentIntent{
		ID: "int-1", URL: "https://api.example.com/data",
		Asset: "USDC", Amount: decimal.NewFromFloat(0.05),
		Chain: "base", Recipient: "0xabc",
		Facilitator: "https://facilitator.example.com",
		Status:      IntentPending,
	}
	if intent.Status != IntentPending {
		t.Error("new intent should be PENDING")
	}
	intent.Status = IntentApproved
	if intent.Status != IntentApproved {
		t.Error("should transition to APPROVED")
	}
	intent.Status = IntentPaid
	if intent.Status != IntentPaid {
		t.Error("should transition to PAID")
	}
}

func TestPaymentError(t *testing.T) {
	err := &PaymentError{Code: "TEST_ERR", Message: "test message"}
	if err.Error() != "TEST_ERR: test message" {
		t.Errorf("unexpected error format: %s", err.Error())
	}
	if ErrPaymentDenied.Code != "PAYMENT_DENIED" {
		t.Error("ErrPaymentDenied should have code PAYMENT_DENIED")
	}
}
