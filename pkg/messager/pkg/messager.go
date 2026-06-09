// Package messager defines the inter-component messaging contract for Flowgent.
//
// All inter-component communication uses MQTT topics under the flowgent/v1/ prefix
// with a hierarchical tenant/flow/run structure for observability and multi-tenancy.
//
// Topic hierarchy:
//
//	flowgent/v1/{tenantId}/flows/{flowId}/runs/{runId}/
//	  ├── exec/plans          ← JM→TM: dispatch ExecutionPlans  ($share/tm-pool)
//	  ├── exec/results        ← TM→JM: execution results         (point-to-point)
//	  ├── sandbox/trigger     ← TM→Sandbox: script trigger       ($share/sandbox-pool)
//	  ├── sandbox/result      ← Sandbox→TM: execution result     (point-to-point)
//	  ├── notify/event        ← Publisher→Notifier              ($share/notify-pool)
//	  └── notify/result       ← Notifier→Publisher               (point-to-point)
//
//	flowgent/v1/{tenantId}/flows/{flowId}/
//	  └── ctrl/jm/create      ← Controller→JM leader
//
//	flowgent/v1/heartbeat/{tmId}  ← TM→JM: liveness signals
package messager

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
)

// ─── Topic Prefix ──────────────────────────────────────────────

const (
	// TopicPrefix is the root namespace for all Flowgent MQTT topics.
	TopicPrefix = "flowgent/v1"
)

// ─── Topic Builders ────────────────────────────────────────────
//
// Each function builds a fully-qualified topic string from routing keys.
// Shared-subscription variants prepend $share/{group}/ for load-balanced
// consumption across multiple pods.

// ExecPlansTopic builds the topic for JM→TM execution plan dispatch.
// TMs subscribe with SharedExecPlans() for load-balanced consumption.
func ExecPlansTopic(tenantID, flowID, runID string) string {
	return fmt.Sprintf("%s/%s/flows/%s/runs/%s/exec/plans", TopicPrefix, tenantID, flowID, runID)
}

// SharedExecPlans is the $share subscription for TM slot workers.
func SharedExecPlans() string {
	return "$share/tm-pool/" + TopicPrefix + "/+/flows/+/runs/+/exec/plans"
}

// ExecResultsTopic builds the topic for TM→JM execution result callback.
func ExecResultsTopic(tenantID, flowID, runID string) string {
	return fmt.Sprintf("%s/%s/flows/%s/runs/%s/exec/results", TopicPrefix, tenantID, flowID, runID)
}

// SandboxTriggerTopic builds the topic for TM→Sandbox script trigger dispatch.
// Sandbox pods subscribe with SharedSandboxTrigger() for load-balanced consumption.
func SandboxTriggerTopic(tenantID, flowID, runID string) string {
	return fmt.Sprintf("%s/%s/flows/%s/runs/%s/sandbox/trigger", TopicPrefix, tenantID, flowID, runID)
}

// SharedSandboxTrigger is the $share subscription for sandbox runner pods.
func SharedSandboxTrigger() string {
	return "$share/sandbox-pool/" + TopicPrefix + "/+/flows/+/runs/+/sandbox/trigger"
}

// SandboxResultTopic builds the topic for Sandbox→TM result callback.
func SandboxResultTopic(tenantID, flowID, runID string) string {
	return fmt.Sprintf("%s/%s/flows/%s/runs/%s/sandbox/result", TopicPrefix, tenantID, flowID, runID)
}

// HeartbeatTopic builds the topic for TM→JM liveness heartbeat.
func HeartbeatTopic(tmID string) string {
	return TopicPrefix + "/heartbeat/" + tmID
}

// HeartbeatWildcard is the wildcard subscription for JM to monitor all TMs.
func HeartbeatWildcard() string {
	return TopicPrefix + "/heartbeat/+"
}

// CtrlJMCreateTopic builds the topic for Controller→JM dedicated JM creation.
func CtrlJMCreateTopic(tenantID, flowID string) string {
	return fmt.Sprintf("%s/%s/flows/%s/ctrl/jm/create", TopicPrefix, tenantID, flowID)
}

// NotifyEventTopic builds the topic for publisher→Notifier event dispatch.
// Notifier pods subscribe with SharedNotifyEvent() for load-balanced consumption.
func NotifyEventTopic(tenantID, flowID, runID string) string {
	return fmt.Sprintf("%s/%s/flows/%s/runs/%s/notify/event", TopicPrefix, tenantID, flowID, runID)
}

// SharedNotifyEvent is the $share subscription for notifier pods.
func SharedNotifyEvent() string {
	return "$share/notify-pool/" + TopicPrefix + "/+/flows/+/runs/+/notify/event"
}

// NotifyResultTopic builds the topic for Notifier→Publisher delivery confirmation.
func NotifyResultTopic(tenantID, flowID, runID string) string {
	return fmt.Sprintf("%s/%s/flows/%s/runs/%s/notify/result", TopicPrefix, tenantID, flowID, runID)
}

// NotifyPodWSTopic builds the topic for cross-pod WebSocket message routing.
func NotifyPodWSTopic(podID, wsID string) string {
	return fmt.Sprintf("%s/notify/pod/%s/ws/%s", TopicPrefix, podID, wsID)
}

// NotifyPodWSWildcard builds the per-pod WS wildcard subscription.
func NotifyPodWSWildcard(podID string) string {
	return fmt.Sprintf("%s/notify/pod/%s/ws/+", TopicPrefix, podID)
}

// NotifyQueueWildcard builds the wildcard subscription for notifier queue consumers.
func NotifyQueueWildcard() string {
	return "$share/notify-pool/" + TopicPrefix + "/+/flows/+/runs/+/notify/event"
}

// ─── InterMessage ──────────────────────────────────────────────

// InterMessage is the standard envelope for all inter-component MQTT messages.
type InterMessage struct {
	ID      string            `json:"id"`
	Headers map[string]string `json:"headers,omitempty"`
	Payload []byte            `json:"payload,omitempty"`
}

// ─── Controller Lifecycle Topics ───────────────────────────────
//
// Apiserver publishes lifecycle events after successful DB writes. Controller
// subscribes via SharedCtrlEvents() for hash-mod-shard dispatching. Events use
// InterMessage with a JSON-encoded FlowEvent or RunEvent payload.

// CtrlFlowUpdatedTopic is published by apiserver after flow create/update/reload.
func CtrlFlowUpdatedTopic(tenantID, flowID string) string {
	return fmt.Sprintf("%s/%s/flows/%s/ctrl/flow/updated", TopicPrefix, tenantID, flowID)
}

// CtrlFlowDeletedTopic is published by apiserver after flow deletion.
func CtrlFlowDeletedTopic(tenantID, flowID string) string {
	return fmt.Sprintf("%s/%s/flows/%s/ctrl/flow/deleted", TopicPrefix, tenantID, flowID)
}

// CtrlRunCreatedTopic is published by apiserver after a new PENDING run is created.
func CtrlRunCreatedTopic(tenantID, flowID, runID string) string {
	return fmt.Sprintf("%s/%s/flows/%s/runs/%s/ctrl/run/created", TopicPrefix, tenantID, flowID, runID)
}

// CtrlRunStatusTopic is published by apiserver after a run status changes.
func CtrlRunStatusTopic(tenantID, flowID, runID string) string {
	return fmt.Sprintf("%s/%s/flows/%s/runs/%s/ctrl/run/status", TopicPrefix, tenantID, flowID, runID)
}

// SharedCtrlEvents is the $share subscription for controller pods.
func SharedCtrlEvents() string {
	return "$share/ctrl-pool/" + TopicPrefix + "/+/flows/+/ctrl/#"
}

// FlowEvent is published by apiserver when a flow definition changes.
type FlowEvent struct {
	EventType string `json:"event_type"` // CREATED | UPDATED | DELETED
	FlowID    string `json:"flow_id"`
	TenantID  string `json:"tenant_id"`
	Version   int64  `json:"version,omitempty"`
}

// RunEvent is published by apiserver when a run lifecycle changes.
type RunEvent struct {
	EventType string `json:"event_type"` // CREATED | STATUS_CHANGED
	RunID     string `json:"run_id"`
	FlowID    string `json:"flow_id"`
	TenantID  string `json:"tenant_id"`
	Status    string `json:"status"`
}

// ─── Heartbeat ─────────────────────────────────────────────────

// Heartbeat is a TM liveness signal published periodically.
type Heartbeat struct {
	TMID      string    `json:"tm_id"`
	Timestamp time.Time `json:"timestamp"`
	Load      int       `json:"load"`
	Capacity  int       `json:"capacity"`
}

// SandboxHeartbeat is a sandbox pod liveness signal.
type SandboxHeartbeat struct {
	PodID     string    `json:"pod_id"`
	Timestamp time.Time `json:"timestamp"`
	Load      int       `json:"load"`
	Capacity  int       `json:"capacity"`
}

// ─── SubHandler ────────────────────────────────────────────────

// SubHandler receives messages from a subscribed topic.
type SubHandler func(topic string, payload []byte)

// ─── IMessager Interface ───────────────────────────────────────

// IMessager is the unified message queue interface for inter-component communication.
type IMessager interface {
	// Publish sends msg to the given topic.
	Publish(ctx context.Context, topic string, msg *InterMessage) error

	// Subscribe registers handler for the given topic. Non-blocking.
	Subscribe(ctx context.Context, topic string, handler SubHandler) error

	// Ack confirms successful processing.
	Ack(ctx context.Context, msgID string) error

	// Nack returns msg to queue for retry.
	Nack(ctx context.Context, msgID string) error

	// Close shuts down the messager.
	Close() error
}

// ─── MessagerManager ───────────────────────────────────────────

// MessagerManager is the unified entry point for messaging implementations.
// It embeds IMessager so all messaging operations are directly available.
type MessagerManager struct {
	IMessager
}

// NewMessagerManager creates the correct IMessager implementation from FlowgentConfig.
func NewMessagerManager(cfg *config.FlowgentConfig, clientID string) *MessagerManager {
	if cfg.Messaging.Type == "mqtt" && cfg.Messaging.MQTT.Broker != "" {
		mq, err := NewMQTTMessager(&MQTTConfig{
			Broker:   cfg.Messaging.MQTT.Broker,
			ClientID: clientID,
			Username: cfg.Messaging.MQTT.Username,
			Password: cfg.Messaging.MQTT.Password,
		})
		if err == nil {
			return &MessagerManager{IMessager: mq}
		}
		if cfg.Deployment.Mode != "" {
			log.Fatalf("FATAL: MQTT connect failed in distributed mode: %v — broker=%s", err, cfg.Messaging.MQTT.Broker)
		}
		log.Printf("WARNING: MQTT connect failed (%v), falling back to memory", err)
	}

	if broker := os.Getenv("FLOWGENT__MESSAGER__MQTT__BROKER"); broker != "" {
		mq, err := NewMQTTMessager(&MQTTConfig{Broker: broker, ClientID: clientID})
		if err == nil {
			return &MessagerManager{IMessager: mq}
		}
		if cfg.Deployment.Mode != "" {
			log.Fatalf("FATAL: MQTT (env) connect failed in distributed mode: %v — broker=%s", err, broker)
		}
		log.Printf("WARNING: MQTT (env) connect failed (%v), using memory", err)
	}

	if cfg.Deployment.Mode != "" {
		log.Fatalf("FATAL: MQTT broker not configured. Set messager.mqtt.broker or FLOWGENT__MESSAGER__MQTT__BROKER env var.")
	}

	log.Printf("WARNING: Using in-memory queue (local dev mode)")
	return &MessagerManager{IMessager: NewLocalMessager(1000)}
}

// Ensure MessagerManager satisfies IMessager.
var _ IMessager = (*MessagerManager)(nil)
