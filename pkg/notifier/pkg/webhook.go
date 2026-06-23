package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/flowgent-labs/flowgent/core/pkg/client"
	model "github.com/flowgent-labs/flowgent/model/pkg"
)

// WebhookSender sends notifications to a generic HTTP webhook endpoint.
type WebhookSender struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	client  model.IFlowgentHttpClient
}

func (s *WebhookSender) Type() string { return "webhook" }

func (s *WebhookSender) SetHTTPClient(c model.IFlowgentHttpClient) { s.client = c }

func (s *WebhookSender) Validate() error {
	if s.URL == "" {
		return fmt.Errorf("webhook: url is required")
	}
	return nil
}

func (s *WebhookSender) Send(ctx context.Context, recipient, title, body string) error {
	payload := map[string]any{
		"title":     title,
		"body":      body,
		"recipient": recipient,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	b, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range s.Headers {
		req.Header.Set(k, v)
	}

	if s.client == nil {
		s.client = client.NewGenericHttpClient(10 * time.Second)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook: HTTP %d", resp.StatusCode)
	}
	return nil
}
