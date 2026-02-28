package pwf

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/x402-foundation/x402/go/types"

	"github.com/flowgent-labs/flowgent/src/payments"
	"github.com/flowgent-labs/flowgent/src/payments/facilitator"
	"github.com/flowgent-labs/flowgent/src/payments/policy"
	"github.com/flowgent-labs/flowgent/src/payments/wallet"
)

type testWallet struct{}

func (w *testWallet) Address() string                                       { return "0x-test-wallet" }
func (w *testWallet) SignAuthorization(ctx context.Context, data []byte) ([]byte, error) { return []byte("test-signature"), nil }
func (w *testWallet) Balance(ctx context.Context) (decimal.Decimal, error)                     { return decimal.NewFromInt(1000), nil }

type testApprover struct {
	approved bool
}

func (a *testApprover) RequestApproval(ctx context.Context, intent *payments.PaymentIntent) (*payments.PaymentReceipt, error) {
	if a.approved {
		return nil, nil
	}
	return nil, &payments.PaymentError{Code: "APPROVAL_REJECTED", Message: "test rejection"}
}

func TestRuntime_Fetch_NormalResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":"ok"}`))
	}))
	defer server.Close()

	wm := wallet.NewManager("0x", map[string]wallet.Wallet{"0x": &testWallet{}})
	eng := policy.NewEngine(&payments.PoliciesConfig{}, nil)
	fc := facilitator.New("http://localhost:8085", 5*time.Second)

	rt := New(Config{HTTPTimeout: 5 * time.Second}, eng, wm, fc, nil)

	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	resp, err := rt.Fetch(context.Background(), req)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestRuntime_Fetch_402PolicyDenies(t *testing.T) {
	// Server returns V2 402 with a $100 payment requirement
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if retry with auth header
		if r.Header.Get("X402-Authorization") != "" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"paid":true}`))
			return
		}
		pr := types.PaymentRequired{
			X402Version: 2,
			Accepts: []types.PaymentRequirements{{
				Scheme: "x402", Network: "base", Asset: "USDC",
				Amount: "100.0", PayTo: "0xbad",
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		json.NewEncoder(w).Encode(pr)
	}))
	defer server.Close()

	wm := wallet.NewManager("0x", map[string]wallet.Wallet{"0x": &testWallet{}})
	// Policy: max $1, but payment is $100 → denied
	eng := policy.NewEngine(&payments.PoliciesConfig{
		MaxSinglePaymentUSD: 1.0,
	}, nil)
	fc := facilitator.New("http://localhost:8085", 5*time.Second)
	rt := New(Config{HTTPTimeout: 5 * time.Second}, eng, wm, fc, nil)

	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	_, err := rt.Fetch(context.Background(), req)
	if err == nil {
		t.Fatal("expected policy denial error for $100 payment with $1 limit")
	}
}

func TestRuntime_SetDefaultWallet(t *testing.T) {
	rt := New(Config{}, nil, nil, nil, nil)
	rt.SetDefaultWallet("0x-my-wallet")
	if rt.defaultAddr != "0x-my-wallet" {
		t.Errorf("expected 0x-my-wallet, got %s", rt.defaultAddr)
	}
}

func TestRuntime_Fetch_NoPaymentNeeded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	wm := wallet.NewManager("0x", map[string]wallet.Wallet{"0x": &testWallet{}})
	eng := policy.NewEngine(&payments.PoliciesConfig{}, nil)
	fc := facilitator.New("http://localhost:8085", 5*time.Second)
	rt := New(Config{HTTPTimeout: 5 * time.Second}, eng, wm, fc, nil)

	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	resp, err := rt.Fetch(context.Background(), req)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

var _ = httptest.NewServer
