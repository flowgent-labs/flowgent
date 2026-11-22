// Package messager defines the inter-component messaging contract for Flowgent.
//
// All inter-component communication uses MQTT topics under the flowgent/v1/ prefix
// with a hierarchical namespace/cluster/graph/run structure for work dispatch and a
// namespace/graph/run structure for point-to-point callbacks and control events.
//
// Topic hierarchy:
//
//	flowgent/v1/{namespaceId}/clusters/{clusterId}/agent-graphs/{graphId}/graph-runs/{runId}/
//	  ├── exec/plans          ← JM→TM: cluster-routed dispatch       ($share/tm-{namespaceId}-{clusterId})
//	  └── sandbox/trigger     ← TM→Sandbox: cluster-routed trigger   ($share/sandbox-{namespaceId}-{clusterId})
//
//	flowgent/v1/{namespaceId}/agent-graphs/{graphId}/graph-runs/{runId}/
//	  ├── exec/results        ← TM→JM: execution results          (point-to-point)
//	  ├── sandbox/result      ← Sandbox→TM: execution result      (point-to-point)
//	  ├── notify/event        ← Publisher→Notifier                ($share/notify-pool)
//	  └── notify/result       ← Notifier→Publisher                (point-to-point)
//
//	flowgent/v1/{namespaceId}/agent-graphs/{graphId}/
//	  └── ctrl/jm/create      ← Controller→JM leader
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

// ExecPlansTopic builds the cluster-routed JM→TM dispatch topic. AgentGraph and Run
// remain in the path for observability while cluster is the consumer boundary.
func ExecPlansTopic(namespaceID, clusterID, graphID, runID string) string {
	return fmt.Sprintf("%s/%s/clusters/%s/agent-graphs/%s/graph-runs/%s/exec/plans", TopicPrefix, namespaceID, clusterID, graphID, runID)
}

// SharedExecPlans load-balances only within one runtime cluster.
func SharedExecPlans(namespaceID, clusterID string) string {
	group := fmt.Sprintf("tm-%s-%s", namespaceID, clusterID)
	return fmt.Sprintf("$share/%s/%s/%s/clusters/%s/agent-graphs/+/graph-runs/+/exec/plans", group, TopicPrefix, namespaceID, clusterID)
}

// ExecResultsTopic builds the topic for TM→JM execution result callback.
func ExecResultsTopic(namespaceID, graphID, runID string) string {
	return fmt.Sprintf("%s/%s/agent-graphs/%s/graph-runs/%s/exec/results", TopicPrefix, namespaceID, graphID, runID)
}

// SandboxTriggerTopic builds the topic for TM→Sandbox script trigger dispatch.
// Sandbox pods subscribe with SharedSandboxTrigger() for load-balanced consumption.
func SandboxTriggerTopic(namespaceID, clusterID, graphID, runID string) string {
	return fmt.Sprintf("%s/%s/clusters/%s/agent-graphs/%s/graph-runs/%s/sandbox/trigger", TopicPrefix, namespaceID, clusterID, graphID, runID)
}

// SharedSandboxTrigger load-balances only within one runtime cluster.
func SharedSandboxTrigger(namespaceID, clusterID string) string {
	group := fmt.Sprintf("sandbox-%s-%s", namespaceID, clusterID)
	return fmt.Sprintf("$share/%s/%s/%s/clusters/%s/agent-graphs/+/graph-runs/+/sandbox/trigger", group, TopicPrefix, namespaceID, clusterID)
}

// SandboxResultTopic builds the topic for Sandbox→TM result callback.
func SandboxResultTopic(namespaceID, graphID, runID string) string {
	return fmt.Sprintf("%s/%s/agent-graphs/%s/graph-runs/%s/sandbox/result", TopicPrefix, namespaceID, graphID, runID)
}

// HeartbeatTopic builds the topic for TM→JM liveness heartbeat.
func HeartbeatTopic(tmID string) string {
	return TopicPrefix + "/heartbeat/" + tmID
}

// HeartbeatWildcard is the wildcard subscription for JM to monitor all TMs.
func HeartbeatWildcard() string {
	return TopicPrefix + "/heartbeat/+"
}

// RuntimeReadyTopic is published by a runtime worker only after its work
// subscription is active. JobManager subscribes before scaling from zero, so
// the first execution plan cannot race ahead of its consumer.
func RuntimeReadyTopic(namespaceID, clusterID, role, workerID string) string {
	return fmt.Sprintf("%s/%s/clusters/%s/runtime/%s/%s/ready", TopicPrefix, namespaceID, clusterID, role, workerID)
}

// RuntimeReadyWildcard matches all workers for one runtime cluster role.
func RuntimeReadyWildcard(namespaceID, clusterID, role string) string {
	return RuntimeReadyTopic(namespaceID, clusterID, role, "+")
}

// CtrlJMCreateTopic builds the topic for Controller→JM dedicated JM creation.
func CtrlJMCreateTopic(namespaceID, graphID string) string {
	return fmt.Sprintf("%s/%s/agent-graphs/%s/ctrl/jm/create", TopicPrefix, namespaceID, graphID)
}

// NotifyEventTopic builds the topic for publisher→Notifier event dispatch.
// Notifier pods subscribe with SharedNotifyEvent() for load-balanced consumption.
func NotifyEventTopic(namespaceID, graphID, runID string) string {
	return fmt.Sprintf("%s/%s/agent-graphs/%s/graph-runs/%s/notify/event", TopicPrefix, namespaceID, graphID, runID)
}

// SharedNotifyEvent is the $share subscription for notifier pods.
func SharedNotifyEvent() string {
	return "$share/notify-pool/" + TopicPrefix + "/+/agent-graphs/+/graph-runs/+/notify/event"
}

// NotifyResultTopic builds the topic for Notifier→Publisher delivery confirmation.
func NotifyResultTopic(namespaceID, graphID, runID string) string {
	return fmt.Sprintf("%s/%s/agent-graphs/%s/graph-runs/%s/notify/result", TopicPrefix, namespaceID, graphID, runID)
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
	return "$share/notify-pool/" + TopicPrefix + "/+/agent-graphs/+/graph-runs/+/notify/event"
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
// InterMessage with a JSON-encoded AgentGraphEvent or RunEvent payload.

// CtrlAgentGraphUpdatedTopic is published by apiserver after graph create/update/reload.
func CtrlAgentGraphUpdatedTopic(namespaceID, graphID string) string {
	return fmt.Sprintf("%s/%s/agent-graphs/%s/ctrl/graph/updated", TopicPrefix, namespaceID, graphID)
}

// CtrlAgentGraphDeletedTopic is published by apiserver after graph deletion.
func CtrlAgentGraphDeletedTopic(namespaceID, graphID string) string {
	return fmt.Sprintf("%s/%s/agent-graphs/%s/ctrl/graph/deleted", TopicPrefix, namespaceID, graphID)
}

// CtrlRunCreatedTopic is published by apiserver after a new PENDING run is created.
func CtrlRunCreatedTopic(namespaceID, graphID, runID string) string {
	return fmt.Sprintf("%s/%s/agent-graphs/%s/graph-runs/%s/ctrl/graph-run/created", TopicPrefix, namespaceID, graphID, runID)
}

// CtrlRunStatusTopic is published by apiserver after a run status changes.
func CtrlRunStatusTopic(namespaceID, graphID, runID string) string {
	return fmt.Sprintf("%s/%s/agent-graphs/%s/graph-runs/%s/ctrl/graph-run/status", TopicPrefix, namespaceID, graphID, runID)
}

// SharedCtrlEvents is the $share subscription for controller pods.
func SharedCtrlEvents() string {
	return "$share/ctrl-pool/" + TopicPrefix + "/+/agent-graphs/+/ctrl/#"
}

// AgentGraphEvent is published by apiserver when a graph definition changes.
type AgentGraphEvent struct {
	EventType    string `json:"event_type"` // CREATED | UPDATED | DELETED
	AgentGraphID string `json:"agent_graph_id"`
	Namespace    string `json:"namespace_id"`
	Version      int64  `json:"version,omitempty"`
}

// RunEvent is published by apiserver when a run lifecycle changes.
type RunEvent struct {
	EventType    string `json:"event_type"` // CREATED | STATUS_CHANGED
	GraphRunID   string `json:"graph_run_id"`
	AgentGraphID string `json:"agent_graph_id"`
	Namespace    string `json:"namespace_id"`
	Status       string `json:"status"`
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

// RuntimeReady is a scoped readiness lease for TaskManager and Sandbox
// consumers. Workers refresh it periodically after their MQTT work
// subscription succeeds; Deployment Ready alone is not a messaging barrier.
type RuntimeReady struct {
	WorkerID  string    `json:"worker_id"`
	Role      string    `json:"role"`
	Namespace string    `json:"namespace_id"`
	ClusterID string    `json:"runtime_cluster_id"`
	Timestamp time.Time `json:"timestamp"`
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
