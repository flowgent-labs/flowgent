package wallet

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/flowgent-labs/flowgent/wallet/src"
)

type testWallet struct {
	addr    string
	balance decimal.Decimal
}

func (w *testWallet) Address() string { return w.addr }
func (w *testWallet) SignAuthorization(ctx context.Context, data []byte) ([]byte, error) {
	return []byte("sig-" + w.addr), nil
}
func (w *testWallet) Balance(ctx context.Context) (decimal.Decimal, error) { return w.balance, nil }

func TestManager_GetDefault(t *testing.T) {
	w := &testWallet{addr: "0x1234", balance: decimal.NewFromInt(100)}
	mgr := NewManager("0x1234", map[string]Wallet{"0x1234": w})

	got, err := mgr.Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	if got.Address() != "0x1234" {
		t.Errorf("expected 0x1234, got %s", got.Address())
	}
}

func TestManager_GetNotFound(t *testing.T) {
	mgr := NewManager("0xabc", map[string]Wallet{})
	_, err := mgr.Get("0xmissing")
	if err == nil {
		t.Fatal("expected error for missing wallet")
	}
}

func TestManager_SignPaymentAuthorization(t *testing.T) {
	w := &testWallet{addr: "0xsigner"}
	mgr := NewManager("0xsigner", map[string]Wallet{"0xsigner": w})

	intent := &payments.PaymentIntent{
		ID: "int-1", Amount: decimal.NewFromFloat(0.01),
		Asset: "USDC", Recipient: "0xrecv",
	}
	auth, err := mgr.SignPaymentAuthorization(context.Background(), "0xsigner", intent)
	if err != nil {
		t.Fatalf("SignPaymentAuthorization: %v", err)
	}
	if auth.IntentID != "int-1" {
		t.Errorf("expected intent int-1, got %s", auth.IntentID)
	}
	if auth.Wallet != "0xsigner" {
		t.Errorf("expected wallet 0xsigner, got %s", auth.Wallet)
	}
	if auth.Signature == "" {
		t.Error("signature should not be empty")
	}
	if auth.Payload == "" {
		t.Error("payload should not be empty")
	}
}

func TestGenerateID(t *testing.T) {
	id1 := GenerateID()
	id2 := GenerateID()
	if id1 == id2 {
		t.Error("generated IDs should be unique")
	}
	if len(id1) != 32 {
		t.Errorf("expected 32-char hex ID, got %d chars", len(id1))
	}
}

func TestManager_MultipleWallets(t *testing.T) {
	w1 := &testWallet{addr: "0xaaa"}
	w2 := &testWallet{addr: "0xbbb"}
	mgr := NewManager("0xaaa", map[string]Wallet{"0xaaa": w1, "0xbbb": w2})

	a, _ := mgr.Get("0xaaa")
	if a.Address() != "0xaaa" {
		t.Error("should get 0xaaa")
	}
	b, _ := mgr.Get("0xbbb")
	if b.Address() != "0xbbb" {
		t.Error("should get 0xbbb")
	}
}
