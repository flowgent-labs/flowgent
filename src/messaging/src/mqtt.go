package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
)

// MQTTQueue implements Queue using MQTT 5 (paho.golang) with native $share support.
type MQTTQueue struct {
	mu       sync.Mutex
	cm       *autopaho.ConnectionManager
	topic    string
	ch       chan *Message
	messages map[string]chan *Message
	subs     map[string]struct{} // tracks active subscriptions
	ctx      context.Context
	cancel   context.CancelFunc
}

// MQTTConfig holds MQTT broker connection parameters.
type MQTTConfig struct {
	Broker   string `json:"broker" yaml:"broker"`
	ClientID string `json:"client_id" yaml:"client_id"`
	Username string `json:"username" yaml:"username"`
	Password string `json:"password" yaml:"password"`
	Topic    string `json:"topic" yaml:"topic"`
}

// NewMQTTQueue creates an MQTT 5-backed queue with auto-reconnection.
func NewMQTTQueue(cfg *MQTTConfig) (*MQTTQueue, error) {
	if cfg.Topic == "" {
		cfg.Topic = "flowgent/tasks"
	}

	brokerURL, err := url.Parse(strings.Replace(cfg.Broker, "tcp://", "mqtt://", 1))
	if err != nil {
		return nil, fmt.Errorf("mqtt parse broker %q: %w", cfg.Broker, err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	q := &MQTTQueue{
		topic:    cfg.Topic,
		ch:       make(chan *Message, 100),
		messages: make(map[string]chan *Message),
		subs:     make(map[string]struct{}),
		ctx:      ctx,
		cancel:   cancel,
	}

	cliCfg := autopaho.ClientConfig{
		ServerUrls:                    []*url.URL{brokerURL},
		KeepAlive:                     30,
		CleanStartOnInitialConnection: true,
		ConnectTimeout:                10 * time.Second,
		OnConnectionUp: func(cm *autopaho.ConnectionManager, _ *paho.Connack) {
			// Re-subscribe to all active topics on reconnect.
			q.mu.Lock()
			defer q.mu.Unlock()
			for topic := range q.subs {
				if _, err := cm.Subscribe(ctx, &paho.Subscribe{
					Subscriptions: []paho.SubscribeOptions{{Topic: topic, QoS: 1}},
				}); err != nil {
					// Logged by autopaho
				}
			}
		},
		OnConnectError: func(err error) {},
	}
	if cfg.ClientID != "" {
		cliCfg.ClientID = cfg.ClientID
	}
	if cfg.Username != "" {
		cliCfg.ConnectUsername = cfg.Username
	}
	if cfg.Password != "" {
		cliCfg.ConnectPassword = []byte(cfg.Password)
	}

	cm, err := autopaho.NewConnection(ctx, cliCfg)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("mqtt connect: %w", err)
	}

	if err := cm.AwaitConnection(ctx); err != nil {
		cancel()
		return nil, fmt.Errorf("mqtt await connection: %w", err)
	}

	q.cm = cm

	// Base handler: routes messages to all dequeue/pop channels.
	cm.AddOnPublishReceived(func(pr autopaho.PublishReceived) (bool, error) {
		var m Message
		if err := json.Unmarshal(pr.Packet.Payload, &m); err != nil {
			return true, nil
		}
		q.mu.Lock()
		for _, ch := range q.messages {
			select {
			case ch <- &m:
			default:
			}
		}
		q.mu.Unlock()
		select {
		case q.ch <- &m:
		default:
		}
		return true, nil
	})

	return q, nil
}

func (q *MQTTQueue) Push(ctx context.Context, msg *Message) error {
	topic := msg.Topic
	if topic == "" {
		topic = q.topic
	}
	_, err := q.cm.Publish(ctx, &paho.Publish{
		Topic:   topic,
		QoS:     1,
		Payload: mustMarshal(msg),
	})
	return err
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

// Dequeue subscribes to the plan execution topic via MQTT 5 $share for
// load-balanced consumption across TM slot workers.
// Topic: $share/{consumerGroup}/{q.topic}/tasks/plans
func (q *MQTTQueue) Dequeue(ctx context.Context, consumerGroup string) (*Message, error) {
	topic := q.topic
	if consumerGroup != "" {
		topic = fmt.Sprintf("$share/%s/%s/tasks/plans", consumerGroup, q.topic)
	}

	// Subscribe once per unique topic.
	q.mu.Lock()
	if _, ok := q.subs[topic]; !ok {
		q.subs[topic] = struct{}{}
		q.mu.Unlock()
		if _, err := q.cm.Subscribe(ctx, &paho.Subscribe{
			Subscriptions: []paho.SubscribeOptions{{Topic: topic, QoS: 1}},
		}); err != nil {
			return nil, fmt.Errorf("mqtt subscribe %s: %w", topic, err)
		}
	} else {
		q.mu.Unlock()
	}

	ch := make(chan *Message, 10)
	consumerID := fmt.Sprintf("%s-%d", consumerGroup, time.Now().UnixNano())

	q.mu.Lock()
	q.messages[consumerID] = ch
	q.mu.Unlock()

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
	_, err := q.cm.Publish(ctx, &paho.Publish{
		Topic:   "flowgent/heartbeat/" + hb.TMID,
		QoS:     0,
		Payload: mustMarshal(hb),
	})
	return err
}

func (q *MQTTQueue) ConsumeHeartbeat(ctx context.Context, timeout time.Duration) (*Heartbeat, error) {
	return nil, nil // heartbeat monitoring via separate subscription
}

func (q *MQTTQueue) Ack(ctx context.Context, msgID string) error   { return nil }
func (q *MQTTQueue) Nack(ctx context.Context, msgID string) error { return nil }

func (q *MQTTQueue) Topic() string { return q.topic }

func (q *MQTTQueue) Close() error {
	q.cancel()
	return q.cm.Disconnect(context.Background())
}

func mustMarshal(v any) []byte {
	data, _ := json.Marshal(v)
	return data
}
