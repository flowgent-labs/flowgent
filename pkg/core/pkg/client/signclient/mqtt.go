//go:build x402

// Package signclient provides async payment signing via MQTT messaging.
// In production, the TaskManager (TM) publishes unsigned payment payloads to
// the sign/request MQTT topic. The wallet daemon subscribes, signs using its
// private key, and publishes the signed result to sign/response.
package signclient

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	model "github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/messager/pkg"
)

// MqttSignClient sends unsigned payment payloads to the wallet daemon
// for signing via MQTT, and waits for the signed result.
type MqttSignClient struct {
	messager messager.IMessager
	namespaceID string
	flowID   string
	runID    string
	timeout  time.Duration

	mu      sync.Mutex
	pending map[string]chan signResult
}

type signResult struct {
	signature string
	err       error
}

// NewMqttSignClient creates an async sign client that communicates with the
// wallet daemon via MQTT. It subscribes to the sign/response topic for the
// given run context and dispatches responses to waiting callers by request ID.
func NewMqttSignClient(m messager.IMessager, namespaceID, flowID, runID string, timeout time.Duration) (*MqttSignClient, error) {
	c := &MqttSignClient{
		messager: m,
		namespaceID: namespaceID,
		flowID:   flowID,
		runID:    runID,
		timeout:  timeout,
		pending:  make(map[string]chan signResult),
	}
	if err := m.Subscribe(context.Background(), messager.SignResponseTopic(namespaceID, flowID, runID), c.handleResponse); err != nil {
		return nil, fmt.Errorf("subscribe sign response: %w", err)
	}
	return c, nil
}

func (c *MqttSignClient) handleResponse(_ string, payload []byte) {
	var resp messager.SignResponse
	if err := json.Unmarshal(payload, &resp); err != nil {
		return
	}
	c.mu.Lock()
	ch, ok := c.pending[resp.RequestID]
	delete(c.pending, resp.RequestID)
	c.mu.Unlock()
	if ok {
		var err error
		if resp.Error != "" {
			err = fmt.Errorf("%s", resp.Error)
		}
		ch <- signResult{signature: resp.Signature, err: err}
	}
}

// Sign sends an unsigned payload to the wallet daemon via MQTT and waits for
// the signed result. The wallet daemon handles key management and signing.
func (c *MqttSignClient) Sign(ctx context.Context, walletAddr string, payload []byte) ([]byte, error) {
	reqID := uuid.New().String()

	ch := make(chan signResult, 1)
	c.mu.Lock()
	c.pending[reqID] = ch
	c.mu.Unlock()

	req := messager.SignRequest{
		RequestID: reqID,
		Wallet:    walletAddr,
		Payload:   string(payload),
		Namespace:  c.namespaceID,
		FlowID:    c.flowID,
		RunID:     c.runID,
	}
	body, _ := json.Marshal(req)

	if err := c.messager.Publish(ctx, messager.SignRequestTopic(c.namespaceID, c.flowID, c.runID), &messager.InterMessage{
		ID:      reqID,
		Payload: body,
	}); err != nil {
		c.mu.Lock()
		delete(c.pending, reqID)
		c.mu.Unlock()
		return nil, fmt.Errorf("publish sign request: %w", err)
	}

	select {
	case result := <-ch:
		if result.err != nil {
			return nil, result.err
		}
		return []byte(result.signature), nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, reqID)
		c.mu.Unlock()
		return nil, ctx.Err()
	case <-time.After(c.timeout):
		c.mu.Lock()
		delete(c.pending, reqID)
		c.mu.Unlock()
		return nil, fmt.Errorf("sign request %s timed out after %s", reqID, c.timeout)
	}
}

var _ model.SignClient = (*MqttSignClient)(nil)
