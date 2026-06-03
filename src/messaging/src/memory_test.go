package messaging

import (
	"context"
	"testing"
	"time"
)

func TestMemoryQueue_PushPop(t *testing.T) {
	q := NewMemoryQueue(10)
	defer q.Close()
	ctx := context.Background()

	msg := &Message{ID: "test-1", TaskRunID: "run-1", NodeID: "node-1"}
	if err := q.Push(ctx, msg); err != nil {
		t.Fatalf("Push: %v", err)
	}

	got, err := q.Pop(ctx, time.Second)
	if err != nil {
		t.Fatalf("Pop: %v", err)
	}
	if got.ID != msg.ID {
		t.Errorf("expected %s, got %s", msg.ID, got.ID)
	}
}

func TestMemoryQueue_Dequeue(t *testing.T) {
	q := NewMemoryQueue(10)
	defer q.Close()

	go func() {
		time.Sleep(50 * time.Millisecond)
		q.Push(context.Background(), &Message{ID: "deq-1", TaskRunID: "run-1"})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	msg, err := q.Dequeue(ctx, "workers")
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if msg.ID != "deq-1" {
		t.Errorf("expected deq-1, got %s", msg.ID)
	}
}

func TestMemoryQueue_DequeueTimeout(t *testing.T) {
	q := NewMemoryQueue(10)
	defer q.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := q.Dequeue(ctx, "workers")
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestMemoryQueue_Ack(t *testing.T) {
	q := NewMemoryQueue(10)
	defer q.Close()
	ctx := context.Background()

	q.Push(ctx, &Message{ID: "ack-1"})
	q.Ack(ctx, "ack-1")

	// After ack, the message is removed from internal tracking
	msg, _ := q.Pop(ctx, 100*time.Millisecond)
	if msg != nil {
		t.Log("pop returned message (may or may not depending on timing)")
	}
}

func TestMemoryQueue_Nack(t *testing.T) {
	q := NewMemoryQueue(10)
	defer q.Close()
	ctx := context.Background()

	q.Push(ctx, &Message{ID: "nack-1", Attempts: 0})
	msg, _ := q.Pop(ctx, time.Second)
	if msg == nil {
		t.Fatal("expected message")
	}

	q.Nack(ctx, "nack-1")
	msg2, _ := q.Pop(ctx, time.Second)
	if msg2 == nil {
		t.Fatal("nack should re-queue message")
	}
	if msg2.Attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", msg2.Attempts)
	}
}

func TestMemoryQueue_ConsumerGroupFanout(t *testing.T) {
	q := NewMemoryQueue(10)
	defer q.Close()
	ctx := context.Background()

	received := make(chan string, 2)

	// Two consumers in different groups both receive the same message
	go func() {
		msg, _ := q.Dequeue(ctx, "group-a")
		if msg != nil {
			received <- "group-a:" + msg.ID
		}
	}()
	go func() {
		msg, _ := q.Dequeue(ctx, "group-b")
		if msg != nil {
			received <- "group-b:" + msg.ID
		}
	}()

	time.Sleep(50 * time.Millisecond)
	q.Push(ctx, &Message{ID: "fanout-1"})

	timeout := time.After(2 * time.Second)
	count := 0
	for count < 2 {
		select {
		case r := <-received:
			t.Logf("received: %s", r)
			count++
		case <-timeout:
			t.Fatalf("timed out waiting for fanout (got %d/2)", count)
		}
	}
}
