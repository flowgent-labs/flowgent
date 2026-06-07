package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// TelegramSender sends notifications via Telegram Bot API.
type TelegramSender struct {
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
	client   *http.Client
}

func (s *TelegramSender) Type() string { return "telegram" }

func (s *TelegramSender) Validate() error {
	if s.BotToken == "" {
		return fmt.Errorf("telegram: bot_token is required")
	}
	if s.ChatID == "" {
		return fmt.Errorf("telegram: chat_id is required")
	}
	return nil
}

func (s *TelegramSender) Send(ctx context.Context, recipient, title, body string) error {
	chatID := s.ChatID
	if recipient != "" {
		chatID = recipient
	}
	text := body
	if title != "" {
		text = fmt.Sprintf("*%s*\n\n%s", title, body)
	}
	payload := map[string]any{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "Markdown",
	}
	b, _ := json.Marshal(payload)

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", s.BotToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	if s.client == nil {
		s.client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("telegram: HTTP %d", resp.StatusCode)
	}
	return nil
}
