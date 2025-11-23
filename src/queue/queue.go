package queue

import (
	"context"
	"time"
)

// Message represents a queue message for distributed execution.
type Message struct {
	ID        string `json:"id"`
	TaskRunID string `json:"task_run_id"`
	NodeID    string `json:"node_id"`
	Payload   []byte `json:"payload,omitempty"`
	Attempts  int    `json:"attempts"`
	Status    string `json:"status"`
}

// Queue is the message queue interface for distributed task processing.
type Queue interface {
	// Push enqueues a message for processing.
	Push(ctx context.Context, msg *Message) error

	// Pop polls for a message with a timeout. Returns nil if no message available.
	Pop(ctx context.Context, timeout time.Duration) (*Message, error)

	// Dequeue blocks until a message is available, implementing consumer-group
	// semantics. In local mode this reads from a channel; in MQTT mode this
	// subscribes to a topic and blocks for the next publication.
	Dequeue(ctx context.Context, consumerGroup string) (*Message, error)

	// Ack acknowledges successful processing of a message.
	Ack(ctx context.Context, msgID string) error

	// Nack negatively acknowledges a message (return to queue for retry).
	Nack(ctx context.Context, msgID string) error

	// Close shuts down the queue.
	Close() error
}
