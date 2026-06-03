package messaging

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// LocalMessager implements Messager using in-memory channels with consumer-group support.
type LocalMessager struct {
	mu       sync.Mutex
	ch       chan *Message
	messages map[string]*Message
	groups   map[string]chan *Message // consumer group → channel
}

// NewLocalMessager creates an in-memory queue with the given buffer size.
func NewLocalMessager(size int) *LocalMessager {
	if size <= 0 {
		size = 1000
	}
	return &LocalMessager{
		ch:       make(chan *Message, size),
		messages: make(map[string]*Message),
		groups:   make(map[string]chan *Message),
	}
}

func (q *LocalMessager) Push(ctx context.Context, msg *Message) error {
	q.mu.Lock()
	q.messages[msg.ID] = msg
	// Fan-out to all consumer groups
	for _, gch := range q.groups {
		select {
		case gch <- msg:
		default:
		}
	}
	q.mu.Unlock()
	select {
	case q.ch <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (q *LocalMessager) Pop(ctx context.Context, timeout time.Duration) (*Message, error) {
	if timeout > 0 {
		ctx2, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		ctx = ctx2
	}
	select {
	case msg := <-q.ch:
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Dequeue blocks until a message arrives, simulating MQTT subscribe semantics.
// The consumerGroup parameter allows multiple logical consumers to each get
// their own copy of every message (fan-out pattern, like MQTT topics).
func (q *LocalMessager) Dequeue(ctx context.Context, consumerGroup string) (*Message, error) {
	q.mu.Lock()
	gch, ok := q.groups[consumerGroup]
	if !ok {
		gch = make(chan *Message, 100)
		q.groups[consumerGroup] = gch
	}
	q.mu.Unlock()

	select {
	case msg := <-gch:
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (q *LocalMessager) Ack(ctx context.Context, msgID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.messages, msgID)
	return nil
}

func (q *LocalMessager) Nack(ctx context.Context, msgID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if msg, ok := q.messages[msgID]; ok {
		msg.Attempts++
		select {
		case q.ch <- msg:
		default:
			return fmt.Errorf("queue full, nack failed for %s", msgID)
		}
	}
	return nil
}

func (q *LocalMessager) PublishHeartbeat(ctx context.Context, hb *Heartbeat) error {
	// In-memory heartbeat is a no-op; sufficient for local mode testing.
	return nil
}

func (q *LocalMessager) ConsumeHeartbeat(ctx context.Context, timeout time.Duration) (*Heartbeat, error) {
	// In-memory mode does not consume heartbeats.
	return nil, nil
}

// Topic returns the default topic prefix.
func (q *LocalMessager) Topic() string { return "flowgent/exec" }

func (q *LocalMessager) Close() error {
	close(q.ch)
	return nil
}
