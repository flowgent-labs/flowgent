// Package model defines the shared domain types for the Flowgent engine.
//
// File: notification.go — Notification channel definitions consumed by Notification service.
//   NotifierChannel, NotifierEvent, NotifierMessage, SubscriptionRoute.
package model

import "time"

// NotifierChannelType enumerates supported notification providers.
type NotifierChannelType string

const (
	NotifTelegram NotifierChannelType = "telegram"
	NotifDingTalk NotifierChannelType = "dingtalk"
	NotifSlack    NotifierChannelType = "slack"
	NotifEmail    NotifierChannelType = "email"
	NotifWebhook  NotifierChannelType = "webhook"
)

// NotifierChannel is a persisted notification provider configuration.
type NotifierChannel struct {
	ID        string                  `json:"id"`
	Name      string                  `json:"name"`
	Type      NotifierChannelType `json:"type"`
	Config    map[string]any          `json:"config"`
	Enabled   bool                    `json:"enabled"`
	TenantID  string                  `json:"tenant_id"`
	CreatedAt time.Time               `json:"created_at"`
	UpdatedAt time.Time               `json:"updated_at"`
}

// NotifierEvent is a transient message sent through a channel.
type NotifierEvent struct {
	ChannelID string `json:"channel_id"`
	Recipient string `json:"recipient"`
	Title     string `json:"title"`
	Body      string `json:"body"`
}

// NotifierMessage is a queued notification to be consumed and dispatched.
// Published to MQTT topic /flowgent/notify/queue/{tenantID}/{agentflowID}
// where notification pods consume via shared subscription and post to channels.
type NotifierMessage struct {
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	TenantID    string    `json:"tenant_id"`
	AgentFlowID string    `json:"agentflow_id"`
	Timestamp   time.Time `json:"timestamp"`
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
