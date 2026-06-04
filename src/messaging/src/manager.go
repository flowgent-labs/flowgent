package messaging

import (
	"log"
	"os"
)

// MessagingManager is the unified entry point for messaging implementations.
// It embeds Messager so all messaging operations are directly available.
type MessagingManager struct {
	Messager
}

// MessagingManagerConfig mirrors config.MessagingConfig, decoupled from config.
type MessagingManagerConfig struct {
	Type        string // "memory" | "mqtt"
	ClientID    string
	Broker      string
	Username    string
	Password    string
	Distributed bool // session/application mode: MQTT mandatory
}

// NewMessagingManager creates the correct Messager implementation from config.
func NewMessagingManager(cfg *MessagingManagerConfig) *MessagingManager {
	if cfg.Type == "mqtt" && cfg.Broker != "" {
		mq, err := NewMQTTMessager(&MQTTConfig{
			Broker:   cfg.Broker,
			ClientID: cfg.ClientID,
			Username: cfg.Username,
			Password: cfg.Password,
		})
		if err == nil {
			return &MessagingManager{Messager: mq}
		}
		if cfg.Distributed {
			log.Fatalf("FATAL: MQTT connect failed in distributed mode: %v — broker=%s", err, cfg.Broker)
		}
		log.Printf("WARNING: MQTT connect failed (%v), falling back to memory", err)
	}

	if broker := os.Getenv("FLOWGENT_MQTT_BROKER"); broker != "" {
		mq, err := NewMQTTMessager(&MQTTConfig{Broker: broker, ClientID: cfg.ClientID})
		if err == nil {
			return &MessagingManager{Messager: mq}
		}
		if cfg.Distributed {
			log.Fatalf("FATAL: MQTT (env) connect failed in distributed mode: %v — broker=%s", err, broker)
		}
		log.Printf("WARNING: MQTT (env) connect failed (%v), using memory", err)
	}

	if cfg.Distributed {
		log.Fatalf("FATAL: MQTT broker not configured. Set messaging.mqtt.broker or FLOWGENT_MQTT_BROKER env var.")
	}

	log.Printf("WARNING: Using in-memory queue (local dev mode)")
	return &MessagingManager{Messager: NewLocalMessager(1000)}
}

// Ensure MessagingManager satisfies Messager.
var _ Messager = (*MessagingManager)(nil)
