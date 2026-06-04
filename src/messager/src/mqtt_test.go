package messager

import (
	"context"
	"testing"
	"time"
)

func TestMQTTMessager_PublishSubscribe(t *testing.T) {
	q, err := NewMQTTMessager(&MQTTConfig{
		Broker:   "tcp://127.0.0.1:1883",
		ClientID: "ut-pubsub",
	})
	if err != nil {
		t.Skipf("MQTT broker not available: %v", err)
		return
	}
	defer q.Close()

	ctx := context.Background()
	done := make(chan []byte, 1)

	q.Subscribe(ctx, "flowgent/ut/test", func(topic string, payload []byte) {
		done <- payload
	})

	time.Sleep(100 * time.Millisecond) // let subscription settle

	if err := q.Publish(ctx, "flowgent/ut/test", &Message{ID: "m1", Payload: []byte("hello-mqtt")}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case payload := <-done:
		if string(payload) != "hello-mqtt" {
			t.Errorf("expected hello-mqtt, got %s", string(payload))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestMQTTMessager_NoBroker(t *testing.T) {
	_, err := NewMQTTMessager(&MQTTConfig{
		Broker:   "tcp://127.0.0.1:1883",
		ClientID: "ut-nobroker",
	})
	if err != nil {
		t.Skipf("no broker (expected): %v", err)
	}
}
