package messaging

import (
	"context"
	"testing"
	"time"
)

func TestMQTTQueue_Construction(t *testing.T) {
	q, err := NewMQTTMessager(&MQTTConfig{
		Broker:   "tcp://127.0.0.1:1883",
		ClientID: "ut-test-mqtt",
		Topic:    "flowgent/ut",
	})
	if err != nil {
		t.Skipf("MQTT broker not available (local dev mode): %v", err)
		return
	}
	defer q.Close()

	if q.topic != "flowgent/ut" {
		t.Errorf("expected topic flowgent/ut, got %s", q.topic)
	}
}

func TestMQTTQueue_PushPop(t *testing.T) {
	q, err := NewMQTTMessager(&MQTTConfig{
		Broker:   "tcp://127.0.0.1:1883",
		ClientID: "ut-pushpop",
		Topic:    "flowgent/ut-pushpop",
	})
	if err != nil {
		t.Skipf("MQTT not available: %v", err)
		return
	}
	defer q.Close()

	ctx := context.Background()
	msg := &Message{ID: "mqtt-ut-1", TaskRunID: "r1", NodeID: "n1", Payload: []byte("test")}
	if err := q.Push(ctx, msg); err != nil {
		t.Fatalf("Push: %v", err)
	}

	got, err := q.Pop(ctx, 5*time.Second)
	if err != nil {
		t.Fatalf("Pop: %v", err)
	}
	if got == nil {
		t.Fatal("expected message after push")
	}
	if got.ID != "mqtt-ut-1" {
		t.Errorf("expected mqtt-ut-1, got %s", got.ID)
	}
}

func TestMQTTQueue_Dequeue(t *testing.T) {
	q, err := NewMQTTMessager(&MQTTConfig{
		Broker:   "tcp://127.0.0.1:1883",
		ClientID: "ut-dequeue",
		Topic:    "flowgent/ut-dequeue",
	})
	if err != nil {
		t.Skipf("MQTT not available: %v", err)
		return
	}
	defer q.Close()

	go func() {
		time.Sleep(200 * time.Millisecond)
		q.Push(context.Background(), &Message{ID: "deq-mqtt-1", TaskRunID: "r1"})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	msg, err := q.Dequeue(ctx, "test-group")
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if msg.ID != "deq-mqtt-1" {
		t.Errorf("expected deq-mqtt-1, got %s", msg.ID)
	}
}

func TestMQTTQueue_DefaultTopic(t *testing.T) {
	q, err := NewMQTTMessager(&MQTTConfig{
		Broker:   "tcp://127.0.0.1:1883",
		ClientID: "ut-defaults",
	})
	if err != nil {
		t.Skipf("MQTT not available: %v", err)
		return
	}
	defer q.Close()

	if q.topic != "flowgent/tasks" {
		t.Errorf("expected default topic flowgent/tasks, got %s", q.topic)
	}
}
