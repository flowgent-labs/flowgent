package model

import (
	"context"

	"github.com/shopspring/decimal"
)

// Wallet is the core wallet interface for signing payment authorizations.
type Wallet interface {
	Address() string
	SignAuthorization(ctx context.Context, data []byte) ([]byte, error)
	Balance(ctx context.Context) (decimal.Decimal, error)
}
