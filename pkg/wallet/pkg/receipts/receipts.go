// Package receipts provides payment receipt persistence and retrieval.
package receipts

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/flowgent-labs/flowgent/wallet/pkg"
)

// Store persists payment receipts.
type Store struct {
	db *sql.DB
}

// NewStore creates a new receipt store with the given database connection.
func NewStore(db *sql.DB) (*Store, error) {
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		return nil, fmt.Errorf("migrate receipts: %w", err)
	}
	return s, nil
}

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS payment_receipts (
			id            TEXT PRIMARY KEY,
			intent_id     TEXT NOT NULL,
			tx_hash       TEXT,
			asset         TEXT NOT NULL,
			amount        TEXT NOT NULL,
			chain         TEXT NOT NULL,
			facilitator   TEXT NOT NULL,
			authorization TEXT NOT NULL,
			paid_at       TIMESTAMP NOT NULL,
			expires_at    TIMESTAMP
		)
	`)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_receipts_intent ON payment_receipts(intent_id)
	`)
	return err
}

// Save persists a payment receipt.
func (s *Store) Save(ctx context.Context, r *payments.PaymentReceipt) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO payment_receipts (id, intent_id, tx_hash, asset, amount, chain, facilitator, authorization, paid_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, r.ID, r.IntentID, r.TxHash, r.Asset, r.Amount.String(), r.Chain, r.Facilitator, r.Authorization, r.PaidAt, r.ExpiresAt)
	return err
}

// GetByIntent retrieves a receipt by payment intent ID.
func (s *Store) GetByIntent(ctx context.Context, intentID string) (*payments.PaymentReceipt, error) {
	var r payments.PaymentReceipt
	var amountStr string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, intent_id, tx_hash, asset, amount, chain, facilitator, authorization, paid_at, expires_at
		FROM payment_receipts WHERE intent_id = $1
	`, intentID).Scan(&r.ID, &r.IntentID, &r.TxHash, &r.Asset, &amountStr, &r.Chain, &r.Facilitator, &r.Authorization, &r.PaidAt, &r.ExpiresAt)
	if err != nil {
		return nil, err
	}
	r.Amount, _ = decimal.NewFromString(amountStr)
	return &r, nil
}

// ListByDate returns all receipts within a date range.
func (s *Store) ListByDate(ctx context.Context, from, to time.Time) ([]payments.PaymentReceipt, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, intent_id, tx_hash, asset, amount, chain, facilitator, authorization, paid_at, expires_at
		FROM payment_receipts WHERE paid_at BETWEEN $1 AND $2 ORDER BY paid_at DESC
	`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []payments.PaymentReceipt
	for rows.Next() {
		var r payments.PaymentReceipt
		var amountStr string
		if err := rows.Scan(&r.ID, &r.IntentID, &r.TxHash, &r.Asset, &amountStr, &r.Chain, &r.Facilitator, &r.Authorization, &r.PaidAt, &r.ExpiresAt); err != nil {
			return nil, err
		}
		r.Amount, _ = decimal.NewFromString(amountStr)
		results = append(results, r)
	}
	return results, rows.Err()
}
