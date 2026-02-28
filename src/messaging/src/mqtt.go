package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// MQTTQueue implements Queue using EMQX/Mosquitto MQTT broker.
type MQTTQueue struct {
	mu       sync.Mutex
	client   mqtt.Client
	topic    string
	ch       chan *Message
	messages map[string]chan *Message
}

// MQTTConfig holds MQTT broker connection parameters.
type MQTTConfig struct {
	Broker   string `json:"broker" yaml:"broker"`
	ClientID string `json:"client_id" yaml:"client_id"`
	Username string `json:"username" yaml:"username"`
	Password string `json:"password" yaml:"password"`
	Topic    string `json:"topic" yaml:"topic"`
}

// NewMQTTQueue creates an MQTT-backed queue.
func NewMQTTQueue(cfg *MQTTConfig) (*MQTTQueue, error) {
	if cfg.Topic == "" {
		cfg.Topic = "flowgent/tasks"
	}

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

	q := &MQTTQueue{
		client:   client,
		topic:    cfg.Topic,
		ch:       make(chan *Message, 100),
		messages: make(map[string]chan *Message),
	}

	// Subscribe to default topic
	if token := client.Subscribe(cfg.Topic, 1, q.onMessage); token.WaitTimeout(5*time.Second) && token.Error() != nil {
		return nil, fmt.Errorf("mqtt subscribe: %w", token.Error())
	}

	return q, nil
}

func (q *MQTTQueue) onMessage(_ mqtt.Client, msg mqtt.Message) {
	var m Message
	if err := json.Unmarshal(msg.Payload(), &m); err != nil {
		return
	}
	// Route to all waiting consumers
	q.mu.Lock()
	for _, ch := range q.messages {
		select {
		case ch <- &m:
		default:
		}
	}
	q.mu.Unlock()
	// Also route to pop channel
	select {
	case q.ch <- &m:
	default:
	}
}

func (q *MQTTQueue) Push(ctx context.Context, msg *Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	// Use msg.Topic if set (allows per-message topic routing), fallback to base topic.
	topic := msg.Topic
	if topic == "" {
		topic = q.topic
	}
	token := q.client.Publish(topic, 1, false, data)
	if token.WaitTimeout(5*time.Second) && token.Error() != nil {
		return token.Error()
	}
	return nil
}

func (q *MQTTQueue) Pop(ctx context.Context, timeout time.Duration) (*Message, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx2, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case msg := <-q.ch:
		return msg, nil
	case <-ctx2.Done():
		return nil, nil // timeout = no message, not an error
	}
}

// Dequeue blocks until a message arrives on the consumer group's subscribed topic.
// This mirrors MQTT's persistent subscribe semantics: each consumer group gets
// its own topic subscription and all members of the group receive every message
// (fan-out, not competing consumer).
func (q *MQTTQueue) Dequeue(ctx context.Context, consumerGroup string) (*Message, error) {
	// All TMs subscribe to the same fan-out topic. Leasing ensures only one TM
	// processes each plan (first to acquire lease wins, others skip).
	// Publisher (K8sRM) → {q.topic}/tasks/plans
	// Consumer (TM slots) → {q.topic}/tasks/plans (all receive, lease-based dedup)
	topic := q.topic
	if consumerGroup != "" {
		topic = q.topic + "/tasks/plans"
	}

	ch := make(chan *Message, 10)
	consumerID := fmt.Sprintf("%s-%d", consumerGroup, time.Now().UnixNano())

	q.mu.Lock()
	q.messages[consumerID] = ch
	q.mu.Unlock()

	// Subscribe to the consumer-group-specific topic
	if consumerGroup != "" {
		if token := q.client.Subscribe(topic, 1, func(c mqtt.Client, m mqtt.Message) {
			var msg Message
			if json.Unmarshal(m.Payload(), &msg) == nil {
				ch <- &msg
			}
		}); token.WaitTimeout(5*time.Second) && token.Error() != nil {
			q.mu.Lock()
			delete(q.messages, consumerID)
			q.mu.Unlock()
			return nil, fmt.Errorf("mqtt subscribe topic %s: %w", topic, token.Error())
		}
	}

	defer func() {
		q.mu.Lock()
		delete(q.messages, consumerID)
		q.mu.Unlock()
	}()

	select {
	case msg := <-ch:
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (q *MQTTQueue) PublishHeartbeat(ctx context.Context, hb *Heartbeat) error {
	data, err := json.Marshal(hb)
	if err != nil {
		return err
	}
	token := q.client.Publish("flowgent/heartbeat/"+hb.TMID, 0, false, data)
	if token.WaitTimeout(3*time.Second) && token.Error() != nil {
		return token.Error()
	}
	return nil
}

func (q *MQTTQueue) ConsumeHeartbeat(ctx context.Context, timeout time.Duration) (*Heartbeat, error) {
	// Heartbeat consumption is handled by the HeartbeatMonitor subscribing
	// to the heartbeat topic. For simplicity, this returns nil.
	// Production: use shared MQTT subscription to flowgent/heartbeat/#
	return nil, nil
}

func (q *MQTTQueue) Ack(ctx context.Context, msgID string) error {
	return nil // MQTT QoS 1 handles ack
}

func (q *MQTTQueue) Nack(ctx context.Context, msgID string) error {
	return nil // republish handled by caller
}

// Topic returns the base topic prefix for plan execution.
func (q *MQTTQueue) Topic() string { return q.topic }

func (q *MQTTQueue) Close() error {
	q.client.Disconnect(250)
	return nil
}
