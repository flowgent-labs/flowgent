// Package e2e provides wallet daemon integration tests.
// These tests cover key generation, secret store encryption round-trip,
// and wallet manager signing flows.
package e2e

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"os"
	"testing"

	"github.com/shopspring/decimal"

	_ "modernc.org/sqlite"

	"github.com/flowgent-labs/flowgent/pkg/payments"
	"github.com/flowgent-labs/flowgent/pkg/payments/providers"
	"github.com/flowgent-labs/flowgent/pkg/payments/wallet"
)

// ─── E2E: Key Generation + Secret Store + Signing ─────────────

func TestE2E_WalletKeyGenStoreSignVerify(t *testing.T) {
	// 1. Generate an Ed25519 keypair
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	// 2. Encrypt and store the private key in the secret store
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	os.Setenv("FLOWGENT_MASTER_KEY", "e2e-wallet-test-master-key-32bytes!")
	defer os.Unsetenv("FLOWGENT_MASTER_KEY")

	store, err := providers.NewDefaultSecretStoreProvider(db, "", "")
	if err != nil {
		t.Fatalf("NewDefaultSecretStoreProvider: %v", err)
	}

	ctx := context.Background()
	walletAddr := hex.EncodeToString(pub)

	if err := store.PutSecret(ctx, "wallet:"+walletAddr, priv); err != nil {
		t.Fatalf("PutSecret: %v", err)
	}

	// 3. Retrieve and verify
	retrieved, err := store.GetSecret(ctx, "wallet:"+walletAddr)
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if string(retrieved) != string(priv) {
		t.Fatal("retrieved key does not match original")
	}

	// 4. Sign a test payload with the retrieved key
	testPayload := []byte("test-payment-intent:0.01:USDC:0x-recipient")
	sig := ed25519.Sign(ed25519.PrivateKey(retrieved), testPayload)

	// 5. Verify the signature with the public key
	if !ed25519.Verify(pub, testPayload, sig) {
		t.Fatal("signature verification failed")
	}

	t.Logf("Wallet address: 0x%s", walletAddr)
}

// ─── E2E: Wallet Manager Multi-Wallet ─────────────────────────

func TestE2E_WalletManagerSigning(t *testing.T) {
	// Create two wallets
	pub1, priv1, _ := ed25519.GenerateKey(rand.Reader)
	pub2, priv2, _ := ed25519.GenerateKey(rand.Reader)

	addr1 := hex.EncodeToString(pub1)
	addr2 := hex.EncodeToString(pub2)

	// Wrap in wallet interface
	w1 := &e2eSigningWallet{addr: addr1, priv: priv1}
	w2 := &e2eSigningWallet{addr: addr2, priv: priv2}

	mgr := wallet.NewManager(addr1, map[string]wallet.Wallet{
		addr1: w1,
		addr2: w2,
	})

	// Sign with wallet 1
	auth1, err := mgr.SignPaymentAuthorization(context.Background(), addr1, &payments.PaymentIntent{
		ID: "e2e-1", Amount: decimal.NewFromFloat(0.01), Asset: "USDC", Recipient: "0x-rec",
	})
	if err != nil {
		t.Fatalf("Sign with wallet1: %v", err)
	}
	if auth1.Wallet != addr1 {
		t.Errorf("expected wallet %s, got %s", addr1, auth1.Wallet)
	}

	// Sign with wallet 2
	auth2, err := mgr.SignPaymentAuthorization(context.Background(), addr2, &payments.PaymentIntent{
		ID: "e2e-2", Amount: decimal.NewFromFloat(0.02), Asset: "USDC", Recipient: "0x-rec2",
	})
	if err != nil {
		t.Fatalf("Sign with wallet2: %v", err)
	}
	if auth2.Wallet != addr2 {
		t.Errorf("expected wallet %s, got %s", addr2, auth2.Wallet)
	}

	// Signatures should differ
	if auth1.Signature == auth2.Signature {
		t.Error("different wallets should produce different signatures")
	}

	// Verify both signatures
	payload1 := []byte(auth1.Payload)
	sig1, _ := hex.DecodeString(auth1.Signature)
	if !ed25519.Verify(pub1, payload1, sig1) {
		t.Error("wallet1 signature verification failed")
	}

	payload2 := []byte(auth2.Payload)
	sig2, _ := hex.DecodeString(auth2.Signature)
	if !ed25519.Verify(pub2, payload2, sig2) {
		t.Error("wallet2 signature verification failed")
	}
}

// ─── Helpers ──────────────────────────────────────────────────

type e2eSigningWallet struct {
	addr string
	priv ed25519.PrivateKey
}

func (w *e2eSigningWallet) Address() string { return w.addr }

func (w *e2eSigningWallet) SignAuthorization(ctx context.Context, data []byte) ([]byte, error) {
	return ed25519.Sign(w.priv, data), nil
}

func (w *e2eSigningWallet) Balance(ctx context.Context) (decimal.Decimal, error) {
	return decimal.NewFromInt(1000), nil
}
