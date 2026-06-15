package entities

import "time"

// NotifyChannelType enumerates supported notification providers.
type NotifyChannelType string

const (
	NotifTelegram NotifyChannelType = "telegram"
	NotifDingTalk NotifyChannelType = "dingtalk"
	NotifSlack    NotifyChannelType = "slack"
	NotifEmail    NotifyChannelType = "email"
	NotifWebhook  NotifyChannelType = "webhook"
)

// NotifyChannelInfo is a persisted notification provider configuration.
type NotifyChannelInfo struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Type      NotifyChannelType `json:"type"`
	Config    map[string]any    `json:"config"`
	Enabled   bool              `json:"enabled"`
	TenantID  string            `json:"tenant_id"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}
