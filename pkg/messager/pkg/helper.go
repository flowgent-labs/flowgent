package messager

import (
	"log"
	"log/slog"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
)

// NewQueueFromConfig creates a messager from config or env, with fallback to local.
func NewQueueFromConfig(cfg *config.FlowgentConfig, clientID string) IMessager {
	distributed := cfg != nil && cfg.Messager.Type == "mqtt"

	qc := cfg.Messager
	slog.Debug("mqtt config", "distributed", distributed, "type", qc.Type, "broker", qc.MQTT.Broker)
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
			log.Fatalf("FATAL: MQTT connect failed: %v — broker=%s", err, qc.MQTT.Broker)
		}
		slog.Warn("MQTT connect failed, falling back to memory queue", "err", err)
	}
	if distributed {
		log.Fatalf("FATAL: MQTT broker not configured. Set messager.mqtt.broker in flowgent.yaml or FLOWGENT__MESSAGER__MQTT__BROKER env var.")
	}
	slog.Warn("Using in-memory queue (local dev mode — not suitable for distributed deployment)")
	return NewLocalMessager(1000)
}
