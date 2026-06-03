package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type MQTTQueue struct {
	mu       sync.Mutex
	client   mqtt.Client
	topic    string
	ch       chan *Message
	messages map[string]chan *Message
	subs     map[string]struct{}
}

type MQTTConfig struct {
	Broker   string
	ClientID string
	Username string
	Password string
	Topic    string
}

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
		subs:     make(map[string]struct{}),
	}
	// Subscribe to base topic for Pop()
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
	q.mu.Lock()
	for _, ch := range q.messages {
		select { case ch <- &m: default: }
	}
	q.mu.Unlock()
	select { case q.ch <- &m: default: }
}

// Push publishes to msg.Topic if set, otherwise base topic.
func (q *MQTTQueue) Push(ctx context.Context, msg *Message) error {
	data, _ := json.Marshal(msg)
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
		return nil, nil
	}
}

// Dequeue subscribes to {topic}/tasks/plans — same topic K8sRM publishes to.
func (q *MQTTQueue) Dequeue(ctx context.Context, consumerGroup string) (*Message, error) {
	topic := q.topic + "/tasks/plans"
	q.mu.Lock()
	if _, ok := q.subs[topic]; !ok {
		q.subs[topic] = struct{}{}
		q.mu.Unlock()
		if token := q.client.Subscribe(topic, 1, func(c mqtt.Client, m mqtt.Message) {
			var msg Message
			if json.Unmarshal(m.Payload(), &msg) == nil {
				q.mu.Lock()
				for _, ch := range q.messages {
					select { case ch <- &msg: default: }
				}
				q.mu.Unlock()
				select { case q.ch <- &msg: default: }
			}
		}); token.WaitTimeout(5*time.Second) && token.Error() != nil {
			return nil, fmt.Errorf("mqtt subscribe %s: %w", topic, token.Error())
		}
	} else {
		q.mu.Unlock()
	}
	ch := make(chan *Message, 10)
	cid := fmt.Sprintf("%s-%d", consumerGroup, time.Now().UnixNano())
	q.mu.Lock()
	q.messages[cid] = ch
	q.mu.Unlock()
	defer func() {
		q.mu.Lock()
		delete(q.messages, cid)
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
	data, _ := json.Marshal(hb)
	token := q.client.Publish("/flowgent/v1/heartbeat/"+hb.TMID, 0, false, data)
	if token.WaitTimeout(3*time.Second) && token.Error() != nil {
		return token.Error()
	}
	return nil
}

func (q *MQTTQueue) ConsumeHeartbeat(ctx context.Context, timeout time.Duration) (*Heartbeat, error) {
	return nil, nil
}

func (q *MQTTQueue) Ack(ctx context.Context, msgID string) error   { return nil }
func (q *MQTTQueue) Nack(ctx context.Context, msgID string) error { return nil }
func (q *MQTTQueue) Topic() string { return q.topic }

func (q *MQTTQueue) Close() error {
	q.client.Disconnect(250)
	return nil
}
