package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// SlackSender sends notifications via Slack incoming webhook.
type SlackSender struct {
	WebhookURL string `json:"webhook_url"`
	Channel    string `json:"channel"`
	client     *http.Client
}

func (s *SlackSender) Type() string { return "slack" }

func (s *SlackSender) Validate() error {
	if s.WebhookURL == "" {
		return fmt.Errorf("slack: webhook_url is required")
	}
	return nil
}

func (s *SlackSender) Send(ctx context.Context, recipient, title, body string) error {
	channel := s.Channel
	if recipient != "" {
		channel = recipient
	}
	payload := map[string]any{
		"channel": channel,
		"blocks": []map[string]any{
			{
				"type": "header",
				"text": map[string]any{"type": "plain_text", "text": title},
			},
			{
				"type": "section",
				"text": map[string]any{"type": "mrkdwn", "text": body},
			},
		},
	}
	if title == "" {
		payload["text"] = body
	}
	b, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.WebhookURL, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	if s.client == nil {
		s.client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("slack send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("slack: HTTP %d", resp.StatusCode)
	}
	return nil
}
