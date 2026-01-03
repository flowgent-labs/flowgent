package payments

import "testing"

func TestPaymentsConfig_Defaults(t *testing.T) {
	cfg := &PaymentsConfig{}
	if cfg.Enabled {
		t.Error("payments should be disabled by default")
	}
}

func TestPoliciesConfig_Thresholds(t *testing.T) {
	cfg := &PoliciesConfig{
		MaxSinglePaymentUSD:          1.0,
		MaxDailyBudgetUSD:            50.0,
		RequireHumanApprovalAboveUSD: 5.0,
		AllowedAssets:                []string{"USDC", "USDT"},
		AllowedChains:                []string{"base", "solana"},
	}
	if cfg.MaxSinglePaymentUSD != 1.0 {
		t.Error("max single payment should be 1.0")
	}
	if len(cfg.AllowedAssets) != 2 {
		t.Error("should have 2 allowed assets")
	}
}

func TestSecretStoreConfig_Provider(t *testing.T) {
	cfg := &SecretStoreConfig{Provider: "default"}
	if cfg.Provider != "default" {
		t.Error("provider should be default")
	}
	cfg2 := &SecretStoreConfig{Provider: "vault"}
	if cfg2.Provider != "vault" {
		t.Error("provider should be vault")
	}
}
