package messaging

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestLocalMessager_PublishSubscribe(t *testing.T) {
	q := NewLocalMessager(10)
	defer q.Close()
	ctx := context.Background()

	var gotTopic string
	var gotPayload []byte
	done := make(chan struct{})

	q.Subscribe(ctx, "test/topic", func(topic string, payload []byte) {
		gotTopic = topic
		gotPayload = payload
		close(done)
	})

	q.Publish(ctx, "test/topic", &Message{ID: "msg-1", Payload: []byte("hello")})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler not called")
	}
	if gotTopic != "test/topic" {
		t.Errorf("expected test/topic, got %s", gotTopic)
	}
	if string(gotPayload) != "hello" {
		t.Errorf("expected hello, got %s", string(gotPayload))
	}
}

func TestLocalMessager_MultipleSubscribers(t *testing.T) {
	q := NewLocalMessager(10)
	defer q.Close()
	ctx := context.Background()

	var mu sync.Mutex
	var received []string

	q.Subscribe(ctx, "topic/a", func(topic string, payload []byte) {
		mu.Lock()
		received = append(received, "a:"+string(payload))
		mu.Unlock()
	})
	q.Subscribe(ctx, "topic/a", func(topic string, payload []byte) {
		mu.Lock()
		received = append(received, "b:"+string(payload))
		mu.Unlock()
	})

	q.Publish(ctx, "topic/a", &Message{ID: "fanout-1", Payload: []byte("x")})

	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Errorf("expected 2, got %d: %v", len(received), received)
	}
}

func TestLocalMessager_TopicIsolation(t *testing.T) {
	q := NewLocalMessager(10)
	defer q.Close()
	ctx := context.Background()

	var called bool
	q.Subscribe(ctx, "topic/a", func(topic string, payload []byte) {
		called = true
	})

	q.Publish(ctx, "topic/b", &Message{ID: "other", Payload: []byte("x")})

	time.Sleep(50 * time.Millisecond)
	if called {
		t.Error("handler should not be called for different topic")
	}
}

func TestLocalMessager_AckNack(t *testing.T) {
	q := NewLocalMessager(10)
	defer q.Close()
	ctx := context.Background()

	// Ack/Nack are no-ops for LocalMessager; verify they don't panic.
	if err := q.Ack(ctx, "msg-1"); err != nil {
		t.Errorf("Ack: %v", err)
	}
	if err := q.Nack(ctx, "msg-1"); err != nil {
		t.Errorf("Nack: %v", err)
	}
}
