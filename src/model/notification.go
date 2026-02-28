package model

import "time"

// NotificationChannelType enumerates supported notification providers.
type NotificationChannelType string

const (
	NotifTelegram NotificationChannelType = "telegram"
	NotifDingTalk NotificationChannelType = "dingtalk"
	NotifSlack    NotificationChannelType = "slack"
	NotifEmail    NotificationChannelType = "email"
	NotifWebhook  NotificationChannelType = "webhook"
)

// NotificationChannel is a persisted notification provider configuration.
type NotificationChannel struct {
	ID        string                  `json:"id"`
	Name      string                  `json:"name"`
	Type      NotificationChannelType `json:"type"`
	Config    map[string]any          `json:"config"`
	Enabled   bool                    `json:"enabled"`
	TenantID  string                  `json:"tenant_id"`
	CreatedAt time.Time               `json:"created_at"`
	UpdatedAt time.Time               `json:"updated_at"`
}

// NotificationEvent is a transient message sent through a channel.
type NotificationEvent struct {
	ChannelID string `json:"channel_id"`
	Recipient string `json:"recipient"`
	Title     string `json:"title"`
	Body      string `json:"body"`
}

// SubscriptionRoute maps a WebSocket connection to an agentflow for
// clustered push delivery across multiple notification pod instances.
//
// On WS connect, a route is inserted; the pod subscribes to its own
// MQTT topic /flowgent/notify/pod/{pod_id}/ws/+. When a scanner detects
// a pending human approval, it queries routes for the agentflow and
// publishes to the target pod's MQTT topic. The receiving pod pushes
// the message to the local WS connection.
type SubscriptionRoute struct {
	ID          string    `json:"id"`
	AgentFlowID string    `json:"agentflow_id"`
	WSID        string    `json:"ws_id"`
	PodID       string    `json:"pod_id"`
	CreatedAt   time.Time `json:"created_at"`
}
