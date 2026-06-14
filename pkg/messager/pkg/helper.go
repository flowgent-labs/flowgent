package messager

import (
	"log"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
)

// NewQueueFromConfig creates a messager from config or env, with fallback to local.
func NewQueueFromConfig(cfg *config.FlowgentConfig, clientID string) IMessager {
	distributed := cfg != nil && (cfg.Deployment.Mode == "session" || cfg.Deployment.Mode == "application")

	qc := cfg.Messaging
	if qc.Type == "mqtt" && qc.MQTT.Broker != "" {
		mqc := &MQTTConfig{
			Broker:   qc.MQTT.Broker,
			ClientID: clientID,
			Username: qc.MQTT.Username,
			Password: qc.MQTT.Password,
		}
		mq, err := NewMQTTMessager(mqc)
		if err == nil {
			return mq
		}
		if distributed {
			log.Fatalf("FATAL: MQTT connect failed in %s mode: %v — broker=%s", cfg.Deployment.Mode, err, qc.MQTT.Broker)
		}
		log.Printf("WARNING: MQTT connect failed (%v), falling back to memory queue", err)
	}
	if distributed {
		log.Fatalf("FATAL: MQTT broker not configured. In %s mode, set messager.mqtt.broker in flowgent.yaml or FLOWGENT__MESSAGING__MQTT__BROKER env var.", cfg.Deployment.Mode)
	}
	log.Printf("WARNING: Using in-memory queue (local dev mode — not suitable for distributed deployment)")
	return NewLocalMessager(1000)
}
