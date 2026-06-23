// Package wallet provides the wallet abstraction for payment signing and key management.
// The wallet subsystem is isolated from the orchestration runtime. Flowgent runtime
// MUST never directly store or access raw private keys.
package wallet

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	model "github.com/flowgent-labs/flowgent/model/pkg"
)

// Manager manages multiple wallets and signs payment authorizations.
type Manager struct {
	wallets  map[string]model.Wallet
	default_ string
}

// NewManager creates a wallet manager with the given wallets.
func NewManager(defaultWallet string, wallets map[string]model.Wallet) *Manager {
	return &Manager{
		wallets:  wallets,
		default_: defaultWallet,
	}
}

// Get returns the wallet with the given address, or the default wallet if address is empty.
func (m *Manager) Get(address string) (model.Wallet, error) {
	if address == "" {
		address = m.default_
	}
	w, ok := m.wallets[address]
	if !ok {
		return nil, &model.PaymentError{Code: "WALLET_NOT_FOUND", Message: "wallet not found: " + address}
	}
	return w, nil
}

// Default returns the default wallet.
func (m *Manager) Default() (model.Wallet, error) {
	return m.Get(m.default_)
}

// SignPaymentAuthorization signs a payment intent for the given wallet address.
func (m *Manager) SignPaymentAuthorization(ctx context.Context, walletAddr string, intent *model.PaymentIntent) (*model.PaymentAuthorization, error) {
	w, err := m.Get(walletAddr)
	if err != nil {
		return nil, err
	}

	payload := intent.ID + ":" + intent.Amount.String() + ":" + intent.Asset + ":" + intent.Recipient
	payloadBytes := []byte(payload)

	sig, err := w.SignAuthorization(ctx, payloadBytes)
	if err != nil {
		return nil, &model.PaymentError{
			Code:    "SIGN_FAILED",
			Message: "failed to sign payment authorization: " + err.Error(),
		}
	}

	return &model.PaymentAuthorization{
		IntentID:  intent.ID,
		Wallet:    w.Address(),
		Signature: hex.EncodeToString(sig),
		Payload:   payload,
	}, nil
}

// GenerateID generates a random hex ID for payment intents or receipts.
func GenerateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
