package notifier

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/flowgent-labs/flowgent/core/pkg/client"
	model "github.com/flowgent-labs/flowgent/model/pkg"
)

// DingTalkSender sends notifications via DingTalk custom bot webhook.
type DingTalkSender struct {
	WebhookURL string `json:"webhook_url"`
	Secret     string `json:"secret"`
	client     model.IFlowgentHttpClient
}

func (s *DingTalkSender) Type() string { return "dingtalk" }

func (s *DingTalkSender) SetHTTPClient(c model.IFlowgentHttpClient) { s.client = c }

func (s *DingTalkSender) Validate() error {
	if s.WebhookURL == "" {
		return fmt.Errorf("dingtalk: webhook_url is required")
	}
	return nil
}

func (s *DingTalkSender) Send(ctx context.Context, recipient, title, body string) error {
	targetURL := s.WebhookURL
	text := body
	if title != "" {
		text = fmt.Sprintf("### %s\n\n%s", title, body)
	}

	// Sign with HMAC-SHA256 if secret is configured
	if s.Secret != "" {
		timestamp := fmt.Sprintf("%d", time.Now().UnixMilli())
		mac := hmac.New(sha256.New, []byte(s.Secret))
		mac.Write([]byte(timestamp + "\n" + s.Secret))
		sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))
		targetURL = fmt.Sprintf("%s&timestamp=%s&sign=%s", s.WebhookURL, url.QueryEscape(timestamp), url.QueryEscape(sign))
	}

	payload := map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]any{
			"title": title,
			"text":  text,
		},
	}
	b, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	if s.client == nil {
		s.client = client.NewGenericHttpClient(10 * time.Second)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("dingtalk send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("dingtalk: HTTP %d", resp.StatusCode)
	}
	return nil
}
