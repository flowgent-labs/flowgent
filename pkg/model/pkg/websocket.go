// Package model defines the shared domain types for the Flowgent engine.
//
// File: websocket.go — WebSocket push message envelope consumed by API Server.
//
//	WSMessage, WSMessageType enum.
package model

// WSMessageType enumerates the kinds of push messages sent over WebSocket.
type WSMessageType string

const (
	WSHumanApprovalCreated  WSMessageType = "human_approval_created"
	WSHumanApprovalResolved WSMessageType = "human_approval_resolved"
	WSRunStatusChanged      WSMessageType = "run_status_changed"
	WSTaskStatusChanged     WSMessageType = "task_status_changed"
	WSNotification          WSMessageType = "notification"
)

// WSMessage is the JSON envelope pushed to WebSocket clients.
type WSMessage struct {
	Type           WSMessageType  `json:"type"`
	AgentGraphName string         `json:"agent_graph_name,omitempty"`
	GraphRunID     string         `json:"graph_run_id,omitempty"`
	NodeRunID      string         `json:"node_run_id,omitempty"`
	Payload        map[string]any `json:"payload"`
}
