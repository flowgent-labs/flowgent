package messager

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type MQTTMessager struct {
	mu     sync.Mutex
	client mqtt.Client
	subs   map[string]struct{}
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
	return &MQTTMessager{client: client, subs: make(map[string]struct{})}, nil
}

func (q *MQTTMessager) Publish(ctx context.Context, topic string, msg *Message) error {
	data, _ := json.Marshal(msg)
	token := q.client.Publish(topic, 1, false, data)
	if token.WaitTimeout(5*time.Second) && token.Error() != nil {
		return token.Error()
	}
	return nil
}

func (q *MQTTMessager) Subscribe(ctx context.Context, topic string, handler SubHandler) error {
	q.mu.Lock()
	if _, ok := q.subs[topic]; ok {
		q.mu.Unlock()
		return nil
	}
	q.subs[topic] = struct{}{}
	q.mu.Unlock()
	token := q.client.Subscribe(topic, 1, func(c mqtt.Client, m mqtt.Message) {
		var msg Message
		if json.Unmarshal(m.Payload(), &msg) == nil {
			handler(topic, msg.Payload)
		}
	})
	if !token.WaitTimeout(5 * time.Second) {
		return nil
	}
	if token.Error() != nil {
		return fmt.Errorf("mqtt subscribe %s: %w", topic, token.Error())
	}
	return nil
}

func (q *MQTTMessager) Ack(ctx context.Context, msgID string) error   { return nil }
func (q *MQTTMessager) Nack(ctx context.Context, msgID string) error  { return nil }
func (q *MQTTMessager) Close() error { q.client.Disconnect(250); return nil }
