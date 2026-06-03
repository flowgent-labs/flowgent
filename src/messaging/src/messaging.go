package messaging

import (
	"context"
	"time"
)

// ─── Topic Constants ───────────────────────────────────────────
const (
	TopicPrefix       = "flowgent/v1"
	TopicExec         = TopicPrefix + "/exec"
	TopicExecResult   = TopicPrefix + "/exec/result"
	TopicSandboxTrig  = TopicPrefix + "/sandbox/trigger"
	TopicSandboxRes   = TopicPrefix + "/sandbox/result"
	TopicHeartbeat    = TopicPrefix + "/heartbeat"
	TopicCtrlJMCreate = TopicPrefix + "/ctrl/jm/create"
	TopicNotifyEvent  = TopicPrefix + "/notify/event"
	TopicNotifyResult = TopicPrefix + "/notify/result"
	TopicNotifyPodWS  = TopicPrefix + "/notify/pod"
	TopicNotifyQueue  = TopicPrefix + "/notify/queue"
)


// Message represents a queue message for distributed execution.
type Message struct {
	ID        string            `json:"id"`
	Topic     string            `json:"topic"`
	Headers   map[string]string `json:"headers,omitempty"`
	TaskRunID string            `json:"task_run_id"`
	NodeID    string            `json:"node_id"`
	Payload   []byte            `json:"payload,omitempty"`
	Attempts  int               `json:"attempts"`
	Status    string            `json:"status"`
}

// Heartbeat is a TM liveness signal published periodically.
type Heartbeat struct {
	TMID      string    `json:"tm_id"`
	Timestamp time.Time `json:"timestamp"`
	Load      int       `json:"load"`
	Capacity  int       `json:"capacity"`
}

// Messager is the message queue interface for distributed task processing.
type Messager interface {
	// Push enqueues a message for processing.
	Push(ctx context.Context, msg *Message) error

	// Pop polls for a message with a timeout. Returns nil if no message available.
	Pop(ctx context.Context, timeout time.Duration) (*Message, error)

	// Dequeue blocks until a message is available, implementing consumer-group
	// semantics. In local mode this reads from a channel; in MQTT mode this
	// subscribes to a topic and blocks for the next publication.
	Dequeue(ctx context.Context, consumerGroup string) (*Message, error)

	// PublishHeartbeat sends a TM heartbeat to the heartbeat topic.
	PublishHeartbeat(ctx context.Context, hb *Heartbeat) error

	// ConsumeHeartbeat blocks for the next heartbeat from any TM.
	ConsumeHeartbeat(ctx context.Context, timeout time.Duration) (*Heartbeat, error)

	// Ack acknowledges successful processing of a message.
	Ack(ctx context.Context, msgID string) error

	// Nack negatively acknowledges a message (return to queue for retry).
	Nack(ctx context.Context, msgID string) error

	// Topic returns the base topic prefix for plan publication.
	// Publishers use Topic()+"/plans", consumers use $share/{group}/Topic()+"/plans".
	Topic() string

	// Close shuts down the queue.
	Close() error
}
