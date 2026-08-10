package messager

import (
	"context"
	"sync"
)

// LocalMessager implements Messager using in-memory handler dispatch.
// Handlers registered via Subscribe are called synchronously from Publish.
// Supports MQTT-style single-level wildcards (+) and $share/ prefix strips.
type LocalMessager struct {
	mu   sync.Mutex
	subs map[string][]SubHandler
	next map[string]int
}

// NewLocalMessager creates an in-memory messager.
func NewLocalMessager(size int) *LocalMessager {
	return &LocalMessager{
		subs: make(map[string][]SubHandler),
		next: make(map[string]int),
	}
}

func (q *LocalMessager) Publish(ctx context.Context, topic string, msg *InterMessage) error {
	q.mu.Lock()
	handlers := q.matchHandlersLocked(topic)
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

// matchHandlers returns all handlers whose subscription topic matches the
// published topic. Supports MQTT single-level wildcards (+). Subscriptions
// with a $share/ prefix are normalized before matching.
func (q *LocalMessager) matchHandlersLocked(topic string) []SubHandler {
	var handlers []SubHandler
	topicParts := splitTopic(topic)
	for sub, hs := range q.subs {
		subNormalized := stripSharePrefix(sub)
		if topicMatch(subNormalized, topicParts) {
			if isSharedTopic(sub) && len(hs) > 0 {
				idx := q.next[sub] % len(hs)
				q.next[sub] = idx + 1
				handlers = append(handlers, hs[idx])
			} else {
				handlers = append(handlers, hs...)
			}
		}
	}
	return handlers
}

func stripSharePrefix(topic string) string {
	if len(topic) > 7 && topic[:7] == "$share/" {
		if idx := indexByteAfterSlash(topic, 7); idx > 0 {
			return topic[idx+1:]
		}
	}
	return topic
}

func indexByteAfterSlash(s string, start int) int {
	for i := start; i < len(s); i++ {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}

func splitTopic(topic string) []string {
	if topic == "" {
		return nil
	}
	parts := make([]string, 0, 8)
	start := 0
	for i := 0; i < len(topic); i++ {
		if topic[i] == '/' {
			parts = append(parts, topic[start:i])
			start = i + 1
		}
	}
	parts = append(parts, topic[start:])
	return parts
}

func topicMatch(subPattern string, pubParts []string) bool {
	subParts := splitTopic(subPattern)
	if len(subParts) != len(pubParts) {
		return false
	}
	for i, sp := range subParts {
		if sp == "+" {
			continue // single-level wildcard: match anything at this level
		}
		if sp != pubParts[i] {
			return false
		}
	}
	return true
}

func (q *LocalMessager) Ack(ctx context.Context, msgID string) error  { return nil }
func (q *LocalMessager) Nack(ctx context.Context, msgID string) error { return nil }

func (q *LocalMessager) Close() error {
	q.mu.Lock()
	q.subs = nil
	q.mu.Unlock()
	return nil
}
