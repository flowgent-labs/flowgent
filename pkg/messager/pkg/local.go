package messager

import (
	"context"
	"sync"
)

// LocalMessager implements Messager using in-memory handler dispatch.
// Handlers registered via Subscribe are called synchronously from Publish.
type LocalMessager struct {
	mu   sync.Mutex
	subs map[string][]SubHandler
}

// NewLocalMessager creates an in-memory messager.
func NewLocalMessager(size int) *LocalMessager {
	return &LocalMessager{
		subs: make(map[string][]SubHandler),
	}
}

func (q *LocalMessager) Publish(ctx context.Context, topic string, msg *InterMessage) error {
	q.mu.Lock()
	handlers := q.subs[topic]
	q.mu.Unlock()
	for _, h := range handlers {
		h(topic, msg.Payload)
	}
	return nil
}

func (q *LocalMessager) Subscribe(ctx context.Context, topic string, handler SubHandler) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, ok := q.subs[topic]; !ok {
		q.subs[topic] = make([]SubHandler, 0, 1)
	}
	q.subs[topic] = append(q.subs[topic], handler)
	return nil
}

func (q *LocalMessager) Ack(ctx context.Context, msgID string) error  { return nil }
func (q *LocalMessager) Nack(ctx context.Context, msgID string) error { return nil }

func (q *LocalMessager) Close() error {
	q.mu.Lock()
	q.subs = nil
	q.mu.Unlock()
	return nil
}
