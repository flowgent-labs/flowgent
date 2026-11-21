package storage

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// OutboxEvent reuses orh_outbox. The transport envelope remains in payload;
// topic and claiming lease live in its existing metadata/available_at fields.
type OutboxEvent struct {
	ID           string          `json:"message_id"`
	Namespace    string          `json:"namespace_id"`
	AggregateSeq int64           `json:"aggregate_seq"`
	AggregateID  string          `json:"aggregate_id"`
	Type         string          `json:"event_type"`
	Topic        string          `json:"topic"`
	Payload      json.RawMessage `json:"payload"`
}

func AppendOutbox(ctx context.Context, tx *SQLTransaction, e OutboxEvent) error {
	if e.ID == "" || e.Topic == "" || !json.Valid(e.Payload) {
		return fmt.Errorf("invalid outbox event")
	}
	metadata, _ := json.Marshal(map[string]any{"topic": e.Topic, "aggregate_seq": e.AggregateSeq})
	_, err := tx.Exec(ctx, `INSERT INTO orh_outbox(id,namespace_id,aggregate_type,aggregate_id,event_type,payload,idempotency_key,metadata,created_at) VALUES(?,?,'execution',?,?,?,?,?,?) ON CONFLICT(namespace_id,idempotency_key) DO NOTHING`, e.ID, e.Namespace, e.AggregateID, e.Type, string(e.Payload), e.ID, string(metadata), time.Now().UTC())
	return err
}

// PublishOutboxOnce claims one oldest event per aggregate. Claiming commits
// before network I/O; a crash leaves a bounded lease and is safe to redeliver.
func PublishOutboxOnce(ctx context.Context, db any, publish func(context.Context, string, []byte) error) (bool, error) {
	var event OutboxEvent
	var payload, metadata string
	var attempts int
	var version int64
	now := time.Now().UTC()
	lease := now.Add(30 * time.Second)
	err := WithSQLTransaction(ctx, db, func(tx *SQLTransaction) error {
		sequence := func(alias string) string {
			if tx.sqlite != nil {
				return "COALESCE(json_extract(" + alias + ".metadata,'$.aggregate_seq'),0)"
			}
			return "COALESCE((" + alias + ".metadata->>'aggregate_seq')::bigint,0)"
		}
		predecessor := "(" + sequence("p") + "<" + sequence("o") + " OR (" + sequence("p") + "=" + sequence("o") + " AND (p.created_at<o.created_at OR(p.created_at=o.created_at AND p.id<o.id))))"
		q := `SELECT id,namespace_id,aggregate_id,event_type,payload,metadata,attempts,row_version FROM orh_outbox o WHERE status='pending' AND CAST(metadata AS TEXT) LIKE '%"topic"%' AND available_at<=? AND NOT EXISTS(SELECT 1 FROM orh_outbox p WHERE p.namespace_id=o.namespace_id AND p.aggregate_id=o.aggregate_id AND p.status='pending' AND CAST(p.metadata AS TEXT) LIKE '%"topic"%' AND ` + predecessor + `) ORDER BY created_at,id LIMIT 1` + tx.LockSuffix()
		if e := tx.QueryRow(ctx, q, now).Scan(&event.ID, &event.Namespace, &event.AggregateID, &event.Type, &payload, &metadata, &attempts, &version); e != nil {
			return e
		}
		var meta map[string]any
		if e := json.Unmarshal([]byte(metadata), &meta); e != nil {
			return e
		}
		event.Topic, _ = meta["topic"].(string)
		if event.Topic == "" {
			return fmt.Errorf("outbox event has no transport topic")
		}
		n, e := tx.Exec(ctx, `UPDATE orh_outbox SET available_at=?,attempts=attempts+1 WHERE id=? AND row_version=? AND available_at<=?`, lease, event.ID, version, now)
		if e != nil {
			return e
		}
		if n != 1 {
			return fmt.Errorf("outbox claim conflict")
		}
		return nil
	})
	if err != nil {
		if IsNoRows(err) {
			return false, nil
		}
		return false, err
	}
	// A publisher may have spent its entire lease blocked by the transport.
	// Completion uses the exact claimed deadline, so an old publisher cannot ack
	// another publisher's claim. Duplicate publication remains possible (QoS 1).
	publishErr := publish(ctx, event.Topic, []byte(payload))
	err = WithSQLTransaction(ctx, db, func(tx *SQLTransaction) error {
		if publishErr == nil {
			_, e := tx.Exec(ctx, `UPDATE orh_outbox SET status='published',published_at=?,last_error=NULL WHERE id=? AND available_at=? AND status='pending'`, time.Now().UTC(), event.ID, lease)
			return e
		}
		delay := time.Second * time.Duration(1<<min(attempts, 8))
		_, e := tx.Exec(ctx, `UPDATE orh_outbox SET available_at=?,last_error=? WHERE id=? AND available_at=? AND status='pending'`, time.Now().UTC().Add(delay), publishErr.Error(), event.ID, lease)
		return e
	})
	if err != nil {
		return true, err
	}
	return true, publishErr
}
func StableEventID(parts ...string) string {
	data, _ := json.Marshal(parts)
	sum := sha256.Sum256(data)
	return fmt.Sprintf("evt-%x", sum[:24])
}

// RedeliverUnclaimed closes the MQTT PUBACK/consumer-crash gap. The durable
// dispatch remains authoritative even if EMQX loses an already-acked message.
func RedeliverUnclaimed(ctx context.Context, db any) error {
	return WithSQLTransaction(ctx, db, func(tx *SQLTransaction) error {
		_, err := tx.Exec(ctx, `UPDATE orh_outbox SET status='pending',available_at=? WHERE event_type='node_dispatch' AND status='published' AND published_at<? AND aggregate_id IN(SELECT n.id FROM orh_node_run n JOIN orh_dispatch d ON d.node_run_id=n.id WHERE n.status='PENDING' AND d.status='pending')`, time.Now().UTC(), time.Now().UTC().Add(-5*time.Second))
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE orh_outbox SET status='pending',available_at=? WHERE event_type='sandbox_dispatch' AND status='published' AND published_at<? AND aggregate_id IN(SELECT id FROM orh_node_run WHERE status='RUNNING' AND checkpoint IS NULL)`, time.Now().UTC(), time.Now().UTC().Add(-5*time.Second))
		return err
	})
}

// RunOutboxPublisher is hosted by the API process (including all-in-one).
func RunOutboxPublisher(ctx context.Context, db any, publish func(context.Context, string, []byte) error) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := RedeliverUnclaimed(ctx, db); err != nil {
				slog.Warn("dispatch recovery deferred", "error", err)
			}
			for i := 0; i < 64; i++ {
				found, err := PublishOutboxOnce(ctx, db, publish)
				if err != nil {
					slog.Warn("outbox publication deferred", "error", err)
					break
				}
				if !found {
					break
				}
			}
		}
	}
}
