package storage_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	storage "github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/google/uuid"
)

// Opt-in PostgreSQL integration, also run against the isolated live scenario
// schema. The fake transport here verifies SQL claiming/ack, not EMQX uptime.
func TestPostgresOutboxProtocol(t *testing.T) {
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set")
	}
	ctx := context.Background()
	pool := storage.NewPostgresPool(ctx, dsn, os.Getenv("TEST_PG_SCHEMA"))
	defer pool.Close()
	aggregate := uuid.NewString()
	topic := "test/outbox/" + aggregate
	err := storage.WithSQLTransaction(ctx, pool, func(tx *storage.SQLTransaction) error {
		for i := int64(1); i <= 2; i++ {
			if err := storage.AppendOutbox(ctx, tx, storage.OutboxEvent{ID: uuid.NewString(), Namespace: "default", AggregateID: aggregate, AggregateSeq: i, Type: "integration_test", Topic: topic, Payload: json.RawMessage(`{"committed":true}`)}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for i := 0; i < 128 && count < 2; i++ {
		found, err := storage.PublishOutboxOnce(ctx, pool, func(_ context.Context, current string, data []byte) error {
			if current == topic {
				count++
			}
			if !json.Valid(data) {
				t.Fatal("invalid committed payload")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			break
		}
	}
	if count != 2 {
		t.Fatalf("PostgreSQL publisher acknowledged %d/2 records", count)
	}
}
