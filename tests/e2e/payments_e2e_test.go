package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/x402-foundation/x402/go/types"

	x402sdk "github.com/x402-foundation/x402/go"

	"github.com/flowgent-labs/flowgent/src/payments"
	"github.com/flowgent-labs/flowgent/src/payments/facilitator"
	"github.com/flowgent-labs/flowgent/src/payments/policy"
	"github.com/flowgent-labs/flowgent/src/payments/pwf"
	"github.com/flowgent-labs/flowgent/src/payments/wallet"
)

// ─── Mock Wallet ────────────────────────────────────────

type e2eWallet struct{}

func (w *e2eWallet) Address() string                                      { return "0x-e2e-wallet" }
func (w *e2eWallet) SignAuthorization(ctx context.Context, data []byte) ([]byte, error) { return []byte("e2e-sig"), nil }
func (w *e2eWallet) Balance(ctx context.Context) (decimal.Decimal, error)              { return decimal.NewFromInt(10000), nil }

// ─── E2E: Full x402 Payment Flow ────────────────────────

func TestE2E_FullPaymentFlow(t *testing.T) {
	facilitatorSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/settle" {
			json.NewEncoder(w).Encode(x402sdk.SettleResponse{
				Transaction: "0xe2etx", Success: true,
			})
			return
		}
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer facilitatorSrv.Close()

	// Target API: first call → 402 (V2 body), retry with auth → 200
	callCount := 0
	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Header.Get("X402-Authorization") != "" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "paid", "data": "success"})
			return
		}
		// V2 402 response body (uses official SDK PaymentRequired type)
		pr := types.PaymentRequired{
			X402Version: 2,
			Accepts: []types.PaymentRequirements{{
				Scheme:  "x402",
				Network: "base",
				Asset:   "USDC",
				Amount:  "0.01",
				PayTo:   "0x-target",
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		json.NewEncoder(w).Encode(pr)
	}))
	defer targetSrv.Close()

	wm := wallet.NewManager("0x-e2e-wallet", map[string]wallet.Wallet{
		"0x-e2e-wallet": &e2eWallet{},
	})
	eng := policy.NewEngine(&payments.PoliciesConfig{
		MaxSinglePaymentUSD:          1.0,
		MaxDailyBudgetUSD:            100.0,
		AllowedAssets:                []string{"USDC"},
		AllowedChains:                []string{"base"},
		RequireHumanApprovalAboveUSD: 5.0,
	}, nil)
	fc := facilitator.New(facilitatorSrv.URL, 5*time.Second)
	rt := pwf.New(pwf.Config{HTTPTimeout: 5 * time.Second}, eng, wm, fc, nil)
	rt.SetDefaultWallet("0x-e2e-wallet")

	req, _ := http.NewRequest(http.MethodGet, targetSrv.URL+"/api/data", nil)
	resp, err := rt.Fetch(context.Background(), req)
	if err != nil {
		t.Fatalf("E2E Fetch failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 after payment, got %d", resp.StatusCode)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls (402 + retry), got %d", callCount)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["status"] != "paid" {
		t.Errorf("expected paid status, got %v", result)
	}
}

// ─── E2E: Policy Denial ─────────────────────────────────

func TestE2E_PolicyDeniesBlockedDomain(t *testing.T) {
	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pr := types.PaymentRequired{
			X402Version: 2,
			Accepts: []types.PaymentRequirements{{
				Scheme: "x402", Network: "base", Asset: "USDC",
				Amount: "0.01", PayTo: "0x-evil",
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		json.NewEncoder(w).Encode(pr)
	}))
	defer targetSrv.Close()

	wm := wallet.NewManager("0x", map[string]wallet.Wallet{"0x": &e2eWallet{}})
	eng := policy.NewEngine(&payments.PoliciesConfig{
		BlockedDomains: []string{"127.0.0.1"},
		AllowedAssets:  []string{"USDC"},
	}, nil)
	fc := facilitator.New("http://localhost:8085", 5*time.Second)
	rt := pwf.New(pwf.Config{HTTPTimeout: 5 * time.Second}, eng, wm, fc, nil)

	req, _ := http.NewRequest(http.MethodGet, targetSrv.URL, nil)
	_, err := rt.Fetch(context.Background(), req)
	if err == nil {
		t.Fatal("expected policy denial for blocked domain")
	}
	t.Logf("Correctly denied: %v", err)
}

// ─── E2E: Approval Required ─────────────────────────────

func TestE2E_ApprovalRequiredAboveThreshold(t *testing.T) {
	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pr := types.PaymentRequired{
			X402Version: 2,
			Accepts: []types.PaymentRequirements{{
				Scheme: "x402", Network: "base", Asset: "USDC",
				Amount: "10.0", PayTo: "0x-expensive",
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		json.NewEncoder(w).Encode(pr)
	}))
	defer targetSrv.Close()

	wm := wallet.NewManager("0x", map[string]wallet.Wallet{"0x": &e2eWallet{}})
	eng := policy.NewEngine(&payments.PoliciesConfig{
		MaxSinglePaymentUSD:          100.0,
		RequireHumanApprovalAboveUSD: 5.0,
		AllowedAssets:                []string{"USDC"},
		AllowedChains:                []string{"base"},
	}, nil)

	fc := facilitator.New("http://localhost:8085", 5*time.Second)
	rt := pwf.New(pwf.Config{HTTPTimeout: 5 * time.Second}, eng, wm, fc, nil)

	req, _ := http.NewRequest(http.MethodGet, targetSrv.URL, nil)
	_, err := rt.Fetch(context.Background(), req)
	if err == nil {
		t.Fatal("expected PAYMENT_REQUIRES_APPROVAL error")
	}
	if err.Error() != payments.ErrPaymentRequiresApproval.Error() {
		t.Logf("Approval required error: %v", err)
	}
}

// ─── E2E: x402 Payload → Facilitator Round-Trip ─────────

func TestE2E_X402ParseAndFacilitatorRoundTrip(t *testing.T) {
	facilitatorSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/settle" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var payload types.PaymentPayload
		json.NewDecoder(r.Body).Decode(&payload)
		json.NewEncoder(w).Encode(x402sdk.SettleResponse{
			Transaction: "0x-roundtrip-tx",
			Success:     true,
		})
	}))
	defer facilitatorSrv.Close()

	fc := facilitator.New(facilitatorSrv.URL, 5*time.Second)

	payload := &types.PaymentPayload{
		X402Version: 2,
		Payload:     map[string]interface{}{"intent_id": "int-rt"},
		Accepted: types.PaymentRequirements{
			Scheme: "x402", Network: "base", Asset: "USDC",
			Amount: "0.05", PayTo: "0x-rec",
		},
	}

	receipt, err := fc.Authorize(context.Background(), payload)
	if err != nil {
		t.Fatalf("Facilitator authorize: %v", err)
	}
	if receipt.TxHash != "0x-roundtrip-tx" {
		t.Errorf("expected tx 0x-roundtrip-tx, got %s", receipt.TxHash)
	}
	t.Logf("Round-trip: intent=%s tx=%s auth=%s", receipt.IntentID, receipt.TxHash, receipt.Authorization)
}
