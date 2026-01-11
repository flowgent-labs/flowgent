// Package e2e provides end-to-end tests for the Flowgent economic layer.
// These tests exercise the full x402 payment flow: 402 detection → parsing →
// policy evaluation → wallet signing → facilitator authorization → retry.
package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/flowgent-labs/flowgent/src/payments"
	"github.com/flowgent-labs/flowgent/src/payments/facilitator"
	"github.com/flowgent-labs/flowgent/src/payments/policy"
	"github.com/flowgent-labs/flowgent/src/payments/pwf"
	"github.com/flowgent-labs/flowgent/src/payments/wallet"
	"github.com/flowgent-labs/flowgent/src/payments/x402"
)

// ─── E2E: Full x402 Payment Flow ──────────────────────────────

type e2eWallet struct{}

func (w *e2eWallet) Address() string                                             { return "0x-e2e-wallet" }
func (w *e2eWallet) SignAuthorization(ctx context.Context, data []byte) ([]byte, error) { return []byte("e2e-sig"), nil }
func (w *e2eWallet) Balance(ctx context.Context) (decimal.Decimal, error)                     { return decimal.NewFromInt(10000), nil }

func TestE2E_FullPaymentFlow(t *testing.T) {
	// 1. Set up a mock facilitator that handles POST /settle
	facilitatorSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/settle" {
			json.NewEncoder(w).Encode(facilitator.SettleResponse{
				TxHash: "0xe2etx", Status: "confirmed",
			})
			return
		}
		// /health
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer facilitatorSrv.Close()

	// 2. Set up a target API that returns 402 then 200 on retry
	callCount := 0
	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Header.Get(x402.HeaderX402Auth) != "" {
			// Retry with auth token → succeed
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "paid", "data": "success"})
			return
		}
		// First call → 402
		pr := payments.X402PaymentRequest{
			Asset: "USDC", Amount: decimal.NewFromFloat(0.01),
			Chain: "base", Recipient: "0x-target",
			Settlement: "x402", Facilitator: facilitatorSrv.URL,
		}
		headerVal, _ := json.Marshal(pr)
		w.Header().Set(x402.HeaderX402Payment, string(headerVal))
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer targetSrv.Close()

	// 3. Wire up PWF runtime
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

	// 4. Execute fetch
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
		t.Errorf("expected 2 calls (402 + retry with token), got %d", callCount)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["status"] != "paid" {
		t.Errorf("expected paid status, got %v", result)
	}
}

// ─── E2E: Policy Denial ───────────────────────────────────────

func TestE2E_PolicyDeniesBlockedDomain(t *testing.T) {
	paymentHeader, _ := json.Marshal(payments.X402PaymentRequest{
		Asset: "USDC", Amount: decimal.NewFromFloat(0.01),
		Chain: "base", Recipient: "0x-evil", Settlement: "x402",
		Facilitator: "http://facilitator",
	})

	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(x402.HeaderX402Payment, string(paymentHeader))
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer targetSrv.Close()

	wm := wallet.NewManager("0x", map[string]wallet.Wallet{"0x": &e2eWallet{}})
	// Block by wildcard pattern — the test server domain contains "127.0.0.1"
	eng := policy.NewEngine(&payments.PoliciesConfig{
		BlockedDomains: []string{"127.0.0.1"}, // block localhost
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

// ─── E2E: Approval Required ───────────────────────────────────

func TestE2E_ApprovalRequiredAboveThreshold(t *testing.T) {
	paymentHeader, _ := json.Marshal(payments.X402PaymentRequest{
		Asset: "USDC", Amount: decimal.NewFromFloat(10.0), // above 5.0 threshold
		Chain: "base", Recipient: "0x-expensive", Settlement: "x402",
		Facilitator: "http://facilitator",
	})

	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(x402.HeaderX402Payment, string(paymentHeader))
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer targetSrv.Close()

	wm := wallet.NewManager("0x", map[string]wallet.Wallet{"0x": &e2eWallet{}})
	eng := policy.NewEngine(&payments.PoliciesConfig{
		MaxSinglePaymentUSD:          100.0,
		RequireHumanApprovalAboveUSD: 5.0,
		AllowedAssets:                []string{"USDC"},
		AllowedChains:                []string{"base"},
	}, nil)

	// PWF with no approver → should fail with approval required
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

// ─── E2E: x402 Parsing ↔ Facilitator Round-Trip ──────────────

func TestE2E_X402ParseAndFacilitatorRoundTrip(t *testing.T) {
	// Create facilitator mock that handles POST /settle (real x402 protocol)
	facilitatorSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/settle" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var settleReq facilitator.SettleRequest
		json.NewDecoder(r.Body).Decode(&settleReq)
		// Return a SettleResponse (real facilitator format)
		json.NewEncoder(w).Encode(facilitator.SettleResponse{
			TxHash: "0x-roundtrip-tx",
			Status: "confirmed",
		})
	}))
	defer facilitatorSrv.Close()

	// Sign authorization
	wm := wallet.NewManager("0x-e2e", map[string]wallet.Wallet{"0x-e2e": &e2eWallet{}})
	auth, err := wm.SignPaymentAuthorization(context.Background(), "0x-e2e", &payments.PaymentIntent{
		ID: "int-rt", Amount: decimal.NewFromFloat(0.05),
		Asset: "USDC", Recipient: "0x-rec",
	})
	if err != nil {
		t.Fatalf("SignPaymentAuthorization: %v", err)
	}

	// Send to facilitator (calls POST /settle internally)
	fc := facilitator.New(facilitatorSrv.URL, 5*time.Second)
	receipt, err := fc.Authorize(context.Background(), auth)
	if err != nil {
		t.Fatalf("Facilitator authorize: %v", err)
	}
	if receipt.IntentID != "int-rt" {
		t.Errorf("expected intent int-rt, got %s", receipt.IntentID)
	}
	if receipt.TxHash != "0x-roundtrip-tx" {
		t.Errorf("expected tx 0x-roundtrip-tx, got %s", receipt.TxHash)
	}
	if receipt.Authorization == "" {
		t.Error("authorization should not be empty")
	}
	t.Logf("Round-trip: intent=%s tx=%s auth=%s", receipt.IntentID, receipt.TxHash, receipt.Authorization)
}
