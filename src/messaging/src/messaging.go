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


// Message is a message with routing metadata.
type Message struct {
	ID      string            `json:"id"`
	Headers map[string]string `json:"headers,omitempty"`
	Payload []byte            `json:"payload,omitempty"`
}

// Heartbeat is a TM liveness signal.
type Heartbeat struct {
	TMID      string    `json:"tm_id"`
	Timestamp time.Time `json:"timestamp"`
	Load      int       `json:"load"`
	Capacity  int       `json:"capacity"`
}

// SubHandler receives messages from a subscribed topic.
type SubHandler func(topic string, payload []byte)

// Messager is the unified message queue interface.
type Messager interface {
	// Publish sends msg to the given topic.
	Publish(ctx context.Context, topic string, msg *Message) error

	// Subscribe registers handler for the given topic. Non-blocking.
	Subscribe(ctx context.Context, topic string, handler SubHandler) error

	// Ack confirms successful processing.
	Ack(ctx context.Context, msgID string) error

	// Nack returns msg to queue for retry.
	Nack(ctx context.Context, msgID string) error

	// Close shuts down the messager.
	Close() error
}
