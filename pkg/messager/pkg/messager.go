// Package messager defines the inter-component messaging contract for Flowgent.
//
// All inter-component communication uses MQTT topics under the flowgent/v1/ prefix
// with a hierarchical namespace/flow/run structure for observability and multi-tenancy.
//
// Topic hierarchy:
//
//	flowgent/v1/{namespaceId}/flows/{flowId}/runs/{runId}/
//	  ├── exec/plans          ← JM→TM: dispatch ExecutionPlans  ($share/tm-pool)
//	  ├── exec/results        ← TM→JM: execution results         (point-to-point)
//	  ├── sandbox/trigger     ← TM→Sandbox: script trigger       ($share/sandbox-pool)
//	  ├── sandbox/result      ← Sandbox→TM: execution result     (point-to-point)
//	  ├── notify/event        ← Publisher→Notifier              ($share/notify-pool)
//	  └── notify/result       ← Notifier→Publisher               (point-to-point)
//
//	flowgent/v1/{namespaceId}/flows/{flowId}/
//	  └── ctrl/jm/create      ← Controller→JM leader
//
//	flowgent/v1/{namespaceId}/flows/{flowId}/runs/{runId}/
//	  ├── sign/request         ← TM→Wallet: unsigned payment     ($share/wallet-pool)
//	  └── sign/response        ← Wallet→TM: signed result        (point-to-point)
//
//	flowgent/v1/heartbeat/{tmId}  ← TM→JM: liveness signals
package messager

import (
	"context"
	"fmt"
	"log"
	"log/slog"
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
func ExecPlansTopic(namespaceID, flowID, runID string) string {
	return fmt.Sprintf("%s/%s/flows/%s/runs/%s/exec/plans", TopicPrefix, namespaceID, flowID, runID)
}

// SharedExecPlans is the $share subscription for TM slot workers.
func SharedExecPlans() string {
	return "$share/tm-pool/" + TopicPrefix + "/+/flows/+/runs/+/exec/plans"
}

// ExecResultsTopic builds the topic for TM→JM execution result callback.
func ExecResultsTopic(namespaceID, flowID, runID string) string {
	return fmt.Sprintf("%s/%s/flows/%s/runs/%s/exec/results", TopicPrefix, namespaceID, flowID, runID)
}

// SandboxTriggerTopic builds the topic for TM→Sandbox script trigger dispatch.
// Sandbox pods subscribe with SharedSandboxTrigger() for load-balanced consumption.
func SandboxTriggerTopic(namespaceID, flowID, runID string) string {
	return fmt.Sprintf("%s/%s/flows/%s/runs/%s/sandbox/trigger", TopicPrefix, namespaceID, flowID, runID)
}

// SharedSandboxTrigger is the $share subscription for sandbox runner pods.
func SharedSandboxTrigger() string {
	return "$share/sandbox-pool/" + TopicPrefix + "/+/flows/+/runs/+/sandbox/trigger"
}

// SandboxResultTopic builds the topic for Sandbox→TM result callback.
func SandboxResultTopic(namespaceID, flowID, runID string) string {
	return fmt.Sprintf("%s/%s/flows/%s/runs/%s/sandbox/result", TopicPrefix, namespaceID, flowID, runID)
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
func CtrlJMCreateTopic(namespaceID, flowID string) string {
	return fmt.Sprintf("%s/%s/flows/%s/ctrl/jm/create", TopicPrefix, namespaceID, flowID)
}

// NotifyEventTopic builds the topic for publisher→Notifier event dispatch.
// Notifier pods subscribe with SharedNotifyEvent() for load-balanced consumption.
func NotifyEventTopic(namespaceID, flowID, runID string) string {
	return fmt.Sprintf("%s/%s/flows/%s/runs/%s/notify/event", TopicPrefix, namespaceID, flowID, runID)
}

// SharedNotifyEvent is the $share subscription for notifier pods.
func SharedNotifyEvent() string {
	return "$share/notify-pool/" + TopicPrefix + "/+/flows/+/runs/+/notify/event"
}

// NotifyResultTopic builds the topic for Notifier→Publisher delivery confirmation.
func NotifyResultTopic(namespaceID, flowID, runID string) string {
	return fmt.Sprintf("%s/%s/flows/%s/runs/%s/notify/result", TopicPrefix, namespaceID, flowID, runID)
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

// SignRequestTopic builds the topic for TM→Wallet payment signing requests.
// Wallet daemons subscribe with SharedSignRequest() for load-balanced consumption.
func SignRequestTopic(namespaceID, flowID, runID string) string {
	return fmt.Sprintf("%s/%s/flows/%s/runs/%s/sign/request", TopicPrefix, namespaceID, flowID, runID)
}

// SharedSignRequest is the $share subscription for wallet daemon pods.
func SharedSignRequest() string {
	return "$share/wallet-pool/" + TopicPrefix + "/+/flows/+/runs/+/sign/request"
}

// SignResponseTopic builds the topic for Wallet→TM signed payment result.
func SignResponseTopic(namespaceID, flowID, runID string) string {
	return fmt.Sprintf("%s/%s/flows/%s/runs/%s/sign/response", TopicPrefix, namespaceID, flowID, runID)
}

// SignResponseSubscription is the per-run subscription for TM to receive the signed result.
func SignResponseSubscription(namespaceID, flowID, runID string) string {
	return TopicPrefix + "/" + namespaceID + "/flows/" + flowID + "/runs/" + runID + "/sign/response"
}

// ─── Payment Signing Types ──────────────────────────────────────

// SignRequest is the MQTT payload for an unsigned payment authorization request.
type SignRequest struct {
	RequestID string `json:"request_id"`
	Wallet    string `json:"wallet"`
	Payload   string `json:"payload"`
	Namespace  string `json:"namespace_id"`
	FlowID    string `json:"flow_id"`
	RunID     string `json:"run_id"`
}

// SignResponse is the MQTT payload for a signed payment authorization result.
type SignResponse struct {
	RequestID string `json:"request_id"`
	Wallet    string `json:"wallet"`
	Signature string `json:"signature,omitempty"`
	Error     string `json:"error,omitempty"`
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
func CtrlFlowUpdatedTopic(namespaceID, flowID string) string {
	return fmt.Sprintf("%s/%s/flows/%s/ctrl/flow/updated", TopicPrefix, namespaceID, flowID)
}

// CtrlFlowDeletedTopic is published by apiserver after flow deletion.
func CtrlFlowDeletedTopic(namespaceID, flowID string) string {
	return fmt.Sprintf("%s/%s/flows/%s/ctrl/flow/deleted", TopicPrefix, namespaceID, flowID)
}

// CtrlRunCreatedTopic is published by apiserver after a new PENDING run is created.
func CtrlRunCreatedTopic(namespaceID, flowID, runID string) string {
	return fmt.Sprintf("%s/%s/flows/%s/runs/%s/ctrl/run/created", TopicPrefix, namespaceID, flowID, runID)
}

// CtrlRunStatusTopic is published by apiserver after a run status changes.
func CtrlRunStatusTopic(namespaceID, flowID, runID string) string {
	return fmt.Sprintf("%s/%s/flows/%s/runs/%s/ctrl/run/status", TopicPrefix, namespaceID, flowID, runID)
}

// SharedCtrlEvents is the $share subscription for controller pods.
func SharedCtrlEvents() string {
	return "$share/ctrl-pool/" + TopicPrefix + "/+/flows/+/ctrl/#"
}

// FlowEvent is published by apiserver when a flow definition changes.
type FlowEvent struct {
	EventType string `json:"event_type"` // CREATED | UPDATED | DELETED
	FlowID    string `json:"flow_id"`
	Namespace  string `json:"namespace_id"`
	Version   int64  `json:"version,omitempty"`
}

// RunEvent is published by apiserver when a run lifecycle changes.
type RunEvent struct {
	EventType string `json:"event_type"` // CREATED | STATUS_CHANGED
	RunID     string `json:"run_id"`
	FlowID    string `json:"flow_id"`
	Namespace  string `json:"namespace_id"`
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
	if cfg.Messager.Type == "mqtt" && cfg.Messager.MQTT.Broker != "" {
		mq, err := NewMQTTMessager(&MQTTConfig{
			Broker:   cfg.Messager.MQTT.Broker,
			ClientID: clientID,
			Username: cfg.Messager.MQTT.Username,
			Password: cfg.Messager.MQTT.Password,
		})
		if err == nil {
			return &MessagerManager{IMessager: mq}
		}
		if cfg.Messager.MQTT.Broker != "" {
			log.Fatalf("FATAL: MQTT connect failed: %v — broker=%s", err, cfg.Messager.MQTT.Broker)
		}
		slog.Warn("MQTT connect failed, falling back to memory", "err", err)
	}

	if cfg.Messager.Type == "mqtt" && cfg.Messager.MQTT.Broker == "" {
		log.Fatalf("FATAL: MQTT broker not configured. Set messager.mqtt.broker in flowgent.yaml or FLOWGENT__MESSAGER__MQTT__BROKER env var.")
	}

	slog.Warn("Using in-memory queue (local dev mode)")
	return &MessagerManager{IMessager: NewLocalMessager(1000)}
}

// Ensure MessagerManager satisfies IMessager.
var _ IMessager = (*MessagerManager)(nil)
