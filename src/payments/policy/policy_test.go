package policy

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/flowgent-labs/flowgent/src/payments"
)

func testCfg() *payments.PoliciesConfig {
	return &payments.PoliciesConfig{
		MaxSinglePaymentUSD:          1.0,
		MaxDailyBudgetUSD:            10.0,
		AllowedDomains:               []string{"*.googleapis.com", "api.openai.com"},
		BlockedDomains:               []string{"*.evil.xyz"},
		RequireHumanApprovalAboveUSD: 5.0,
		AllowedAssets:                []string{"USDC"},
		AllowedChains:                []string{"base", "solana"},
	}
}

func TestEngine_Allow(t *testing.T) {
	eng := NewEngine(testCfg(), nil)
	ctx := context.Background()

	intent := &payments.PaymentIntent{
		ID: "i1", URL: "https://api.openai.com/v1/chat",
		Asset: "USDC", Amount: decimal.NewFromFloat(0.5),
		Chain: "base", Recipient: "0x1234",
	}

	if err := eng.Allow(ctx, intent); err != nil {
		t.Fatalf("valid intent should pass: %v", err)
	}
}

func TestEngine_DenyBlockedDomain(t *testing.T) {
	eng := NewEngine(testCfg(), nil)
	intent := &payments.PaymentIntent{
		ID: "i2", URL: "https://bad.evil.xyz/api",
		Asset: "USDC", Amount: decimal.NewFromFloat(0.1),
		Chain: "base", Recipient: "0x",
	}
	if err := eng.Allow(context.Background(), intent); err == nil {
		t.Fatal("blocked domain should be denied")
	}
}

func TestEngine_DenyUnknownDomain(t *testing.T) {
	cfg := testCfg()
	cfg.BlockedDomains = nil
	eng := NewEngine(cfg, nil)
	intent := &payments.PaymentIntent{
		ID: "i3", URL: "https://random.unknown.com/api",
		Asset: "USDC", Amount: decimal.NewFromFloat(0.1),
		Chain: "base", Recipient: "0x",
	}
	if err := eng.Allow(context.Background(), intent); err == nil {
		t.Fatal("unknown domain should be denied when allowed list is set")
	}
}

func TestEngine_DenyExceedsMaxSingle(t *testing.T) {
	eng := NewEngine(testCfg(), nil)
	intent := &payments.PaymentIntent{
		ID: "i4", URL: "https://api.openai.com",
		Asset: "USDC", Amount: decimal.NewFromFloat(2.0),
		Chain: "base", Recipient: "0x",
	}
	if err := eng.Allow(context.Background(), intent); err == nil {
		t.Fatal("amount above max single payment should be denied")
	}
}

func TestEngine_DenyWrongAsset(t *testing.T) {
	eng := NewEngine(testCfg(), nil)
	intent := &payments.PaymentIntent{
		ID: "i5", URL: "https://api.openai.com",
		Asset: "ETH", Amount: decimal.NewFromFloat(0.1),
		Chain: "base", Recipient: "0x",
	}
	if err := eng.Allow(context.Background(), intent); err == nil {
		t.Fatal("disallowed asset should be denied")
	}
}

func TestEngine_DenyWrongChain(t *testing.T) {
	eng := NewEngine(testCfg(), nil)
	intent := &payments.PaymentIntent{
		ID: "i6", URL: "https://api.openai.com",
		Asset: "USDC", Amount: decimal.NewFromFloat(0.1),
		Chain: "ethereum", Recipient: "0x",
	}
	if err := eng.Allow(context.Background(), intent); err == nil {
		t.Fatal("disallowed chain should be denied")
	}
}

func TestEngine_DenyDailyBudget(t *testing.T) {
	eng := NewEngine(testCfg(), nil)
	ctx := context.Background()

	// Spend $9.5 first
	eng.RecordSpend(ctx, "0x", decimal.NewFromFloat(9.5))

	// $1.0 would push to $10.5 > $10 budget
	intent := &payments.PaymentIntent{
		ID: "i7", URL: "https://api.openai.com",
		Asset: "USDC", Amount: decimal.NewFromFloat(1.0),
		Chain: "base", Recipient: "0x",
	}
	if err := eng.Allow(ctx, intent); err == nil {
		t.Fatal("should deny when daily budget exceeded")
	}
}

func TestEngine_RequiresHumanApproval(t *testing.T) {
	eng := NewEngine(testCfg(), nil)
	small := &payments.PaymentIntent{Amount: decimal.NewFromFloat(1.0)}
	large := &payments.PaymentIntent{Amount: decimal.NewFromFloat(6.0)}

	if eng.RequiresHumanApproval(small) {
		t.Error("small amount should not require approval")
	}
	if !eng.RequiresHumanApproval(large) {
		t.Error("large amount should require approval")
	}
}

func TestMatchDomain(t *testing.T) {
	tests := []struct {
		pattern  string
		domain   string
		expected bool
	}{
		{"*.example.com", "api.example.com", true},
		{"*.example.com", "example.com", true},
		{"*.example.com", "sub.api.example.com", true},
		{"api.example.com", "api.example.com", true},
		{"api.example.com", "other.example.com", false},
		{"*.evil.xyz", "bad.evil.xyz", true},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+"/"+tt.domain, func(t *testing.T) {
			if got := matchDomain(tt.pattern, tt.domain); got != tt.expected {
				t.Errorf("matchDomain(%q, %q) = %v, want %v", tt.pattern, tt.domain, got, tt.expected)
			}
		})
	}
}

func TestEngine_NilIntent(t *testing.T) {
	eng := NewEngine(testCfg(), nil)
	if err := eng.Allow(context.Background(), nil); err == nil {
		t.Fatal("nil intent should error")
	}
}

func TestEngine_NoApprovalThreshold(t *testing.T) {
	cfg := &payments.PoliciesConfig{}
	eng := NewEngine(cfg, nil)
	intent := &payments.PaymentIntent{Amount: decimal.NewFromFloat(1000)}
	if eng.RequiresHumanApproval(intent) {
		t.Error("zero threshold should never require approval")
	}
}
