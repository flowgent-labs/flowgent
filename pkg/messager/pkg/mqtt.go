package messager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type MQTTMessager struct {
	mu       sync.RWMutex
	client   mqtt.Client
	handlers map[string][]SubHandler // multiple handlers per topic (one MQTT sub)
}

type MQTTConfig struct {
	Broker   string
	ClientID string
	Username string
	Password string
}

func NewMQTTMessager(cfg *MQTTConfig) (*MQTTMessager, error) {
	opts := mqtt.NewClientOptions().
		AddBroker(cfg.Broker).
		SetClientID(cfg.ClientID).
		SetCleanSession(true).
		SetKeepAlive(30 * time.Second).
		SetPingTimeout(10 * time.Second).
		SetConnectTimeout(10 * time.Second).
		SetAutoReconnect(true).
		SetMaxReconnectInterval(30 * time.Second)
	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
	}
	if cfg.Password != "" {
		opts.SetPassword(cfg.Password)
	}
	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.WaitTimeout(15*time.Second) && token.Error() != nil {
		return nil, fmt.Errorf("mqtt connect: %w", token.Error())
	}
	return &MQTTMessager{client: client, handlers: make(map[string][]SubHandler)}, nil
}

func (q *MQTTMessager) Publish(ctx context.Context, topic string, msg *InterMessage) error {
	data, _ := json.Marshal(msg)
	slog.Debug("mqtt publish", "topic", topic, "len", len(data), "connected", q.client.IsConnected())
	token := q.client.Publish(topic, 1, false, data)
	if token.WaitTimeout(5*time.Second) && token.Error() != nil {
		slog.Error("mqtt publish failed", "topic", topic, "err", token.Error())
		return token.Error()
	}
	slog.Debug("mqtt publish ok", "topic", topic)
	return nil
}

func (q *MQTTMessager) Subscribe(ctx context.Context, topic string, handler SubHandler) error {
	q.mu.Lock()
	_, exists := q.handlers[topic]
	q.handlers[topic] = append(q.handlers[topic], handler)
	count := len(q.handlers[topic])
	q.mu.Unlock()

	if exists {
		slog.Debug("mqtt subscribe handler appended", "topic", topic, "handlers", count)
		return nil
	}

	slog.Debug("mqtt subscribe first handler", "topic", topic, "connected", q.client.IsConnected())
	token := q.client.Subscribe(topic, 1, func(c mqtt.Client, m mqtt.Message) {
		q.mu.RLock()
		handlers := q.handlers[topic]
		q.mu.RUnlock()
		slog.Debug("mqtt message received", "topic", m.Topic(), "len", len(m.Payload()), "handlers", len(handlers))
		var msg InterMessage
		if json.Unmarshal(m.Payload(), &msg) == nil {
			for i, h := range handlers {
				slog.Debug("mqtt dispatch handler", "topic", topic, "handler", i+1, "total", len(handlers))
				go h(topic, msg.Payload)
			}
		}
	})
	if !token.WaitTimeout(5 * time.Second) {
		slog.Warn("mqtt subscribe timeout", "topic", topic)
		return nil
	}
	if token.Error() != nil {
		slog.Error("mqtt subscribe failed", "topic", topic, "err", token.Error())
		return fmt.Errorf("mqtt subscribe %s: %w", topic, token.Error())
	}
	slog.Debug("mqtt subscribe ok", "topic", topic)
	return nil
}

func (q *MQTTMessager) Ack(ctx context.Context, msgID string) error  { return nil }
func (q *MQTTMessager) Nack(ctx context.Context, msgID string) error { return nil }
func (q *MQTTMessager) Close() error                                 { q.client.Disconnect(250); return nil }
