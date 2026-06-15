package model

import "time"

// NotifierEvent is a transient message sent through a channel.
type NotifierEvent struct {
	ChannelID string `json:"channel_id"`
	Recipient string `json:"recipient"`
	Title     string `json:"title"`
	Body      string `json:"body"`
}

// NotifierMessage is a queued notification to be consumed and dispatched.
type NotifierMessage struct {
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	TenantID    string    `json:"tenant_id"`
	AgentFlowID string    `json:"agentflow_id"`
	Timestamp   time.Time `json:"timestamp"`
}

// SubscriptionRoute maps a WebSocket connection to an agentflow for
// clustered push delivery across multiple notification pod instances.
type SubscriptionRoute struct {
	ID          string    `json:"id"`
	AgentFlowID string    `json:"agentflow_id"`
	WSID        string    `json:"ws_id"`
	PodID       string    `json:"pod_id"`
	CreatedAt   time.Time `json:"created_at"`
}
