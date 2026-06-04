package messager

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/flowgent-labs/flowgent/config/src/config"
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
type IMessager interface {
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




// MessagerManager is the unified entry point for messaging implementations.
// It embeds IMessager so all messaging operations are directly available.
type MessagerManager struct {
	IMessager
}

// NewMessagerManager creates the correct IMessager implementation from FlowgentConfig.
func NewMessagerManager(cfg *config.FlowgentConfig, clientID string) *MessagerManager {
	if cfg.Messaging.Type == "mqtt" && cfg.Messaging.MQTT.Broker != "" {
		mq, err := NewMQTTMessager(&MQTTConfig{
			Broker:   cfg.Messaging.MQTT.Broker,
			ClientID: clientID,
			Username: cfg.Messaging.MQTT.Username,
			Password: cfg.Messaging.MQTT.Password,
		})
		if err == nil {
			return &MessagerManager{IMessager: mq}
		}
		if cfg.Deployment.Mode != "" {
			log.Fatalf("FATAL: MQTT connect failed in distributed mode: %v — broker=%s", err, cfg.Messaging.MQTT.Broker)
		}
		log.Printf("WARNING: MQTT connect failed (%v), falling back to memory", err)
	}

	if broker := os.Getenv("FLOWGENT_MQTT_BROKER"); broker != "" {
		mq, err := NewMQTTMessager(&MQTTConfig{Broker: broker, ClientID: clientID})
		if err == nil {
			return &MessagerManager{IMessager: mq}
		}
		if cfg.Deployment.Mode != "" {
			log.Fatalf("FATAL: MQTT (env) connect failed in distributed mode: %v — broker=%s", err, broker)
		}
		log.Printf("WARNING: MQTT (env) connect failed (%v), using memory", err)
	}

	if cfg.Deployment.Mode != "" {
		log.Fatalf("FATAL: MQTT broker not configured. Set messager.mqtt.broker or FLOWGENT_MQTT_BROKER env var.")
	}

	log.Printf("WARNING: Using in-memory queue (local dev mode)")
	return &MessagerManager{IMessager: NewLocalMessager(1000)}
}

// Ensure MessagerManager satisfies IMessager.
var _ IMessager = (*MessagerManager)(nil)
