package receipts

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/shopspring/decimal"

	"github.com/flowgent-labs/flowgent/src/payments"
)

func TestStore_SaveAndGet(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	ctx := context.Background()
	receipt := &payments.PaymentReceipt{
		ID: "rec-1", IntentID: "int-1", TxHash: "0xtx",
		Asset: "USDC", Amount: decimal.NewFromFloat(0.01),
		Chain: "base", Facilitator: "https://facilitator.example.com",
		Authorization: "tok-abc", PaidAt: time.Now(),
	}

	if err := store.Save(ctx, receipt); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.GetByIntent(ctx, "int-1")
	if err != nil {
		t.Fatalf("GetByIntent: %v", err)
	}
	if got.ID != "rec-1" {
		t.Errorf("expected rec-1, got %s", got.ID)
	}
	if !got.Amount.Equals(decimal.NewFromFloat(0.01)) {
		t.Errorf("expected 0.01, got %s", got.Amount)
	}
}

func TestStore_GetMissing(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	store, _ := NewStore(db)
	_, err = store.GetByIntent(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing receipt")
	}
}

func TestStore_ListByDate(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	store, _ := NewStore(db)
	ctx := context.Background()
	now := time.Now()

	store.Save(ctx, &payments.PaymentReceipt{
		ID: "r1", IntentID: "i1", Asset: "USDC",
		Amount: decimal.NewFromFloat(0.1), Chain: "base",
		Facilitator: "f1", Authorization: "tok1", PaidAt: now,
	})
	store.Save(ctx, &payments.PaymentReceipt{
		ID: "r2", IntentID: "i2", Asset: "USDC",
		Amount: decimal.NewFromFloat(0.2), Chain: "base",
		Facilitator: "f1", Authorization: "tok2", PaidAt: now,
	})

	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Hour)
	receipts, err := store.ListByDate(ctx, from, to)
	if err != nil {
		t.Fatalf("ListByDate: %v", err)
	}
	if len(receipts) != 2 {
		t.Errorf("expected 2 receipts, got %d", len(receipts))
	}
}

func TestStore_ListByDate_Empty(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	store, _ := NewStore(db)
	receipts, err := store.ListByDate(context.Background(), time.Now(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("ListByDate: %v", err)
	}
	if len(receipts) != 0 {
		t.Errorf("expected 0 receipts, got %d", len(receipts))
	}
}
