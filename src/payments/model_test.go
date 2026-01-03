package payments

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestX402PaymentRequest_Validate(t *testing.T) {
	valid := &X402PaymentRequest{
		Asset: "USDC", Amount: decimal.NewFromFloat(0.01),
		Chain: "base", Recipient: "0x1234",
		Settlement: "x402", Facilitator: "https://facilitator.example.com",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid request should pass: %v", err)
	}
}

func TestX402PaymentRequest_ValidateErrors(t *testing.T) {
	tests := []struct {
		name string
		pr   *X402PaymentRequest
	}{
		{"empty asset", &X402PaymentRequest{Amount: decimal.NewFromInt(1), Chain: "base", Recipient: "0x", Facilitator: "http://f"}},
		{"zero amount", &X402PaymentRequest{Asset: "USDC", Amount: decimal.Zero, Chain: "base", Recipient: "0x", Facilitator: "http://f"}},
		{"empty chain", &X402PaymentRequest{Asset: "USDC", Amount: decimal.NewFromInt(1), Recipient: "0x", Facilitator: "http://f"}},
		{"empty recipient", &X402PaymentRequest{Asset: "USDC", Amount: decimal.NewFromInt(1), Chain: "base", Facilitator: "http://f"}},
		{"empty facilitator", &X402PaymentRequest{Asset: "USDC", Amount: decimal.NewFromInt(1), Chain: "base", Recipient: "0x"}},
		{"bad settlement", &X402PaymentRequest{Asset: "USDC", Amount: decimal.NewFromInt(1), Chain: "base", Recipient: "0x", Settlement: "eth", Facilitator: "http://f"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.pr.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestPaymentIntent_Lifecycle(t *testing.T) {
	intent := &PaymentIntent{
		ID: "int-1", URL: "https://api.example.com/data",
		Asset: "USDC", Amount: decimal.NewFromFloat(0.05),
		Chain: "base", Recipient: "0xabc",
		Facilitator: "https://facilitator.example.com",
		Status: IntentPending,
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
