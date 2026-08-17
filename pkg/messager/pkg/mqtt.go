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
	connMu   sync.Mutex
	cfg      MQTTConfig
	client   mqtt.Client
	handlers map[string][]SubHandler // multiple handlers per topic (one MQTT sub)
	next     map[string]int
}

type MQTTConfig struct {
	Broker   string
	ClientID string
	Username string
	Password string
}

func NewMQTTMessager(cfg *MQTTConfig) (*MQTTMessager, error) {
	q := &MQTTMessager{
		cfg:      *cfg,
		handlers: make(map[string][]SubHandler),
		next:     make(map[string]int),
	}
	q.client = mqtt.NewClient(q.clientOptions())
	if err := q.connect(15 * time.Second); err != nil {
		return nil, err
	}
	return q, nil
}

func (q *MQTTMessager) clientOptions() *mqtt.ClientOptions {
	cfg := q.cfg
	opts := mqtt.NewClientOptions().
		AddBroker(cfg.Broker).
		SetClientID(cfg.ClientID).
		SetCleanSession(true).
		SetKeepAlive(30 * time.Second).
		SetPingTimeout(10 * time.Second).
		SetConnectTimeout(10 * time.Second).
		SetAutoReconnect(false).
		SetMaxReconnectInterval(30 * time.Second)
	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
	}
	if cfg.Password != "" {
		opts.SetPassword(cfg.Password)
	}
	opts.SetConnectionLostHandler(func(c mqtt.Client, err error) {
		slog.Warn("mqtt connection lost", "client_id", cfg.ClientID, "err", err)
	})
	return opts
}

func (q *MQTTMessager) connect(timeout time.Duration) error {
	token := q.client.Connect()
	if !token.WaitTimeout(timeout) {
		return fmt.Errorf("mqtt connect: timeout")
	}
	if token.Error() != nil {
		return fmt.Errorf("mqtt connect: %w", token.Error())
	}
	return nil
}

func (q *MQTTMessager) ensureConnected(ctx context.Context) error {
	q.connMu.Lock()
	defer q.connMu.Unlock()
	return q.ensureConnectedLocked(ctx)
}

func (q *MQTTMessager) ensureConnectedLocked(ctx context.Context) error {
	if q.client != nil && q.client.IsConnected() {
		return nil
	}
	return q.dialLocked(ctx)
}

func (q *MQTTMessager) reconnect(ctx context.Context) error {
	q.connMu.Lock()
	defer q.connMu.Unlock()
	if q.client != nil && q.client.IsConnected() {
		return nil
	}
	return q.dialLocked(ctx)
}

func (q *MQTTMessager) dialLocked(ctx context.Context) error {
	if q.client != nil {
		q.client.Disconnect(250)
	}
	q.client = mqtt.NewClient(q.clientOptions())
	done := make(chan error, 1)
	go func() { done <- q.connect(15 * time.Second) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		if err != nil {
			return err
		}
	}
	q.resubscribe(q.client)
	return nil
}

func (q *MQTTMessager) Publish(ctx context.Context, topic string, msg *InterMessage) error {
	data, _ := json.Marshal(msg)
	var lastErr error
	for attempt := 1; attempt <= 2; attempt++ {
		q.connMu.Lock()
		if err := q.ensureConnectedLocked(ctx); err != nil {
			q.connMu.Unlock()
			return err
		}
		slog.Debug("mqtt publish", "topic", topic, "len", len(data), "connected", q.client.IsConnected())
		token := q.client.Publish(topic, 1, false, data)
		if !token.WaitTimeout(5 * time.Second) {
			lastErr = fmt.Errorf("mqtt publish timeout")
		} else if token.Error() != nil {
			lastErr = token.Error()
		} else {
			slog.Debug("mqtt publish ok", "topic", topic)
			q.connMu.Unlock()
			return nil
		}
		slog.Error("mqtt publish failed", "topic", topic, "attempt", attempt, "err", lastErr)
		_ = q.dialLocked(ctx)
		q.connMu.Unlock()
	}
	return lastErr
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

	q.connMu.Lock()
	defer q.connMu.Unlock()
	if err := q.ensureConnectedLocked(ctx); err != nil {
		return err
	}
	return q.subscribeTopic(ctx, q.client, topic)
}

func (q *MQTTMessager) subscribeTopic(ctx context.Context, client mqtt.Client, topic string) error {
	slog.Debug("mqtt subscribe first handler", "topic", topic, "connected", q.client.IsConnected())
	token := client.Subscribe(topic, 1, q.messageHandler(topic))
	if !token.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("mqtt subscribe %s: timeout", topic)
	}
	if token.Error() != nil {
		slog.Error("mqtt subscribe failed", "topic", topic, "err", token.Error())
		return fmt.Errorf("mqtt subscribe %s: %w", topic, token.Error())
	}
	slog.Debug("mqtt subscribe ok", "topic", topic)
	return nil
}

func (q *MQTTMessager) resubscribe(client mqtt.Client) {
	q.mu.RLock()
	topics := make([]string, 0, len(q.handlers))
	for topic := range q.handlers {
		topics = append(topics, topic)
	}
	q.mu.RUnlock()

	for _, topic := range topics {
		token := client.Subscribe(topic, 1, q.messageHandler(topic))
		if !token.WaitTimeout(5 * time.Second) {
			slog.Error("mqtt resubscribe timeout", "topic", topic)
			continue
		}
		if err := token.Error(); err != nil {
			slog.Error("mqtt resubscribe failed", "topic", topic, "err", err)
			continue
		}
		slog.Debug("mqtt resubscribe ok", "topic", topic)
	}
}

func (q *MQTTMessager) messageHandler(topic string) mqtt.MessageHandler {
	return func(c mqtt.Client, m mqtt.Message) {
		handlers := q.dispatchHandlers(topic)
		slog.Debug("mqtt message received", "topic", m.Topic(), "len", len(m.Payload()), "handlers", len(handlers))
		var msg InterMessage
		payload := m.Payload()
		// Lifecycle publishers predate InterMessage and intentionally send raw
		// JSON. Unknown JSON fields unmarshal successfully into an empty struct,
		// so Payload must be non-nil before treating a message as an envelope.
		if json.Unmarshal(m.Payload(), &msg) == nil && msg.Payload != nil {
			payload = msg.Payload
		}
		for i, h := range handlers {
			slog.Debug("mqtt dispatch handler", "topic", topic, "handler", i+1, "total", len(handlers))
			go h(m.Topic(), payload)
		}
	}
}

func (q *MQTTMessager) dispatchHandlers(topic string) []SubHandler {
	q.mu.Lock()
	defer q.mu.Unlock()
	handlers := q.handlers[topic]
	if len(handlers) == 0 {
		return nil
	}
	if isSharedTopic(topic) {
		idx := q.next[topic] % len(handlers)
		q.next[topic] = idx + 1
		return []SubHandler{handlers[idx]}
	}
	out := make([]SubHandler, len(handlers))
	copy(out, handlers)
	return out
}

func isSharedTopic(topic string) bool {
	return len(topic) > len("$share/") && topic[:len("$share/")] == "$share/"
}

func (q *MQTTMessager) Ack(ctx context.Context, msgID string) error  { return nil }
func (q *MQTTMessager) Nack(ctx context.Context, msgID string) error { return nil }
func (q *MQTTMessager) Close() error {
	q.connMu.Lock()
	defer q.connMu.Unlock()
	if q.client != nil {
		q.client.Disconnect(250)
	}
	return nil
}
