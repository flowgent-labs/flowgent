package notifier

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/flowgent-labs/flowgent/core/pkg/client"
	messager "github.com/flowgent-labs/flowgent/messager/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// MQTTClient is the interface for publishing notification messages to MQTT.
type MQTTClient interface {
	Subscribe(ctx context.Context, topic string, handler func(topic string, payload []byte)) error
	Publish(ctx context.Context, topic string, payload []byte) error
}

// NotifierServer is the notification & WebSocket push service. It runs:
//   - A scanner goroutine that detects pending human approvals
//   - A WebSocket hub that manages client connections with MQTT-based routing
//     for clustered multi-pod deployment
//
// Each pod subscribes to its own MQTT channel /flowgent/notify/pod/{podID}/ws/+
// and pushes messages to local WS connections. The scanner publishes to
// the correct pod's channel based on the subscription routing table.
type NotifierServer struct {
	client    *client.NotifierClient
	mqtt      MQTTClient
	senders   map[string]Sender
	podID     string
	wsClients map[string]*wsConn
	mu        sync.RWMutex
	logger    *slog.Logger
}

// WSConn is an active WebSocket client connection.
type WSConn interface {
	ReadMessages() <-chan []byte
	Done() <-chan struct{}
}

type wsConn struct {
	ID          string
	AgentFlowID string
	MsgCh       chan []byte
	done        chan struct{}
}

// ReadMessages returns a receive-only channel for incoming WS messages.
func (c *wsConn) ReadMessages() <-chan []byte { return c.MsgCh }

// Done returns a channel that is closed when the connection is terminated.
func (c *wsConn) Done() <-chan struct{} { return c.done }

// Close signals the connection to shut down.
func (c *wsConn) Close() { close(c.done) }

// NewNotifierServer creates a notification service with the given client and optional MQTT client.
func NewNotifierServer(c *client.NotifierClient, mqtt MQTTClient) *NotifierServer {
	hostname, _ := os.Hostname()
	podID := fmt.Sprintf("%s-%s", hostname, uuid.New().String()[:8])

	svc := &NotifierServer{
		client:    c,
		mqtt:      mqtt,
		senders:   make(map[string]Sender),
		podID:     podID,
		wsClients: make(map[string]*wsConn),
		logger:    slog.Default().With("component", "notification"),
	}

	// Register built-in senders
	svc.senders["telegram"] = &TelegramSender{}
	svc.senders["dingtalk"] = &DingTalkSender{}
	svc.senders["slack"] = &SlackSender{}
	svc.senders["email"] = &EmailSender{}
	svc.senders["webhook"] = &WebhookSender{}

	return svc
}

// PodID returns the unique pod identifier for MQTT routing.
func (s *NotifierServer) PodID() string { return s.podID }

// Start begins the scanner goroutine, MQTT listener, queue consumer, and cleanup loop.
func (s *NotifierServer) Start(ctx context.Context) error {
	if s.mqtt != nil {
		topicWS := messager.NotifyPodWSWildcard(s.podID)
		if err := s.mqtt.Subscribe(ctx, topicWS, s.onMQTTMessage); err != nil {
			s.logger.Warn("mqtt WS subscribe failed", "topic", topicWS, "error", err)
		} else {
			s.logger.Info("notification WS routing subscribed", "topic", topicWS)
		}

		topicQueue := messager.NotifyQueueWildcard()
		if err := s.mqtt.Subscribe(ctx, topicQueue, s.onQueueMessage); err != nil {
			s.logger.Warn("mqtt queue subscribe failed", "topic", topicQueue, "error", err)
		} else {
			s.logger.Info("notification queue consumer subscribed", "topic", topicQueue)
		}
	}

	go s.scanHumanApprovals(ctx)
	go s.cleanupLoop(ctx)

	s.logger.Info("notification service started", "pod_id", s.podID)
	return nil
}

// RegisterSender adds or overrides a named sender implementation.
func (s *NotifierServer) RegisterSender(name string, sender Sender) {
	s.senders[name] = sender
}

func (s *NotifierServer) onQueueMessage(topic string, payload []byte) {
	s.logger.Debug("queue message received", "topic", topic)

	var tenantID, flowID string
	if n, _ := fmt.Sscanf(topic, messager.TopicPrefix+"/%s/flows/%s/runs/", &tenantID, &flowID); n < 2 {
		s.logger.Warn("invalid queue topic format", "topic", topic)
		return
	}

	var msg model.NotifierMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		s.logger.Warn("queue message unmarshal", "error", err)
		return
	}

	s.logger.Info("processing queue notification",
		"tenant", tenantID,
		"flow", flowID,
		"title", msg.Title)

	s.notifyChannels(context.Background(), "", msg.Title, msg.Body)
}

// PublishNotification enqueues a notification to the MQTT queue.
func (s *NotifierServer) PublishNotification(ctx context.Context, tenantID, agentflowID, title, body string) error {
	if s.mqtt == nil {
		return fmt.Errorf("notification: mqtt not configured")
	}

	msg := model.NotifierMessage{
		Title:       title,
		Body:        body,
		TenantID:    tenantID,
		AgentFlowID: agentflowID,
		Timestamp:   time.Now(),
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	topic := messager.NotifyEventTopic(tenantID, agentflowID, "")
	return s.mqtt.Publish(ctx, topic, payload)
}

func (s *NotifierServer) onMQTTMessage(topic string, payload []byte) {
	var msg model.WSMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		s.logger.Warn("mqtt message unmarshal", "error", err)
		return
	}

	s.mu.RLock()
	conn, ok := s.wsClients[msg.TaskID]
	s.mu.RUnlock()

	if ok {
		b, _ := json.Marshal(msg)
		select {
		case conn.MsgCh <- b:
		default:
			s.logger.Debug("ws client buffer full, dropping", "ws_id", conn.ID)
		}
	}
}

// RegisterWS adds a WebSocket client and creates a subscription route.
func (s *NotifierServer) RegisterWS(ctx context.Context, agentFlowID string) (WSConn, error) {
	conn := &wsConn{
		ID:          uuid.New().String(),
		AgentFlowID: agentFlowID,
		MsgCh:       make(chan []byte, 64),
		done:        make(chan struct{}),
	}

	route := &model.SubscriptionRoute{
		ID:          uuid.New().String(),
		AgentFlowID: agentFlowID,
		WSID:        conn.ID,
		PodID:       s.podID,
	}
	if err := s.client.SaveRoute(ctx, route); err != nil {
		return nil, fmt.Errorf("save subscription route: %w", err)
	}

	s.mu.Lock()
	s.wsClients[conn.ID] = conn
	s.mu.Unlock()

	s.logger.Info("ws client registered", "ws_id", conn.ID, "agentflow_id", agentFlowID)
	return conn, nil
}

// UnregisterWS removes a WebSocket client and its subscription route.
func (s *NotifierServer) UnregisterWS(ctx context.Context, wsID string) {
	s.mu.Lock()
	delete(s.wsClients, wsID)
	s.mu.Unlock()

	_ = s.client.DeleteRoute(ctx, wsID)
}

func (s *NotifierServer) scanHumanApprovals(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	seen := make(map[string]bool)

	for {
		select {
		case <-ticker.C:
			approvals, err := s.client.ListPendingApprovals(ctx)
			if err != nil {
				s.logger.Error("scan pending approvals", "error", err)
				continue
			}

			for _, a := range approvals {
				if seen[a.Token] {
					continue
				}
				seen[a.Token] = true

				afRunID := a.AgentFlowRunID
				if afRunID == "" {
					afRunID = a.TaskRunID
				}

				msg := model.WSMessage{
					Type:   model.WSHumanApprovalCreated,
					RunID:  afRunID,
					TaskID: a.TaskRunID,
					Payload: map[string]any{
						"token":  a.Token,
						"status": a.Status,
					},
				}

				if s.mqtt != nil {
					s.pushToSubscribers(ctx, afRunID, &msg)
				}

				s.notifyChannels(ctx, a.Token, "Human Approval Required",
					fmt.Sprintf("A human approval is pending for task %s. Token: %s", a.TaskRunID, a.Token))
			}
		case <-ctx.Done():
			return
		}
	}
}

func (s *NotifierServer) pushToSubscribers(ctx context.Context, agentFlowID string, msg *model.WSMessage) {
	routes, err := s.client.GetRoutesByFlow(ctx, agentFlowID)
	if err != nil {
		s.logger.Error("lookup subscription routes", "error", err)
		return
	}

	b, _ := json.Marshal(msg)
	for _, route := range routes {
		topic := messager.NotifyPodWSTopic(route.PodID, route.WSID)
		if err := s.mqtt.Publish(ctx, topic, b); err != nil {
			s.logger.Warn("mqtt publish", "topic", topic, "error", err)
		}
	}
}

func (s *NotifierServer) notifyChannels(ctx context.Context, recipient, title, body string) {
	channels, err := s.client.ListChannels(ctx, "")
	if err != nil {
		s.logger.Error("list notification channels", "error", err)
		return
	}

	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		sender, ok := s.senders[string(ch.Type)]
		if !ok {
			s.logger.Warn("unknown channel type", "type", ch.Type)
			continue
		}

		if err := configureSender(sender, ch.Config); err != nil {
			s.logger.Warn("configure sender", "channel", ch.Name, "error", err)
			continue
		}

		go func(ch entities.NotifyChannelInfo, sender Sender) {
			if err := sender.Send(ctx, recipient, title, body); err != nil {
				s.logger.Error("send notification", "channel", ch.Name, "error", err)
			}
		}(ch, sender)
	}
}

func (s *NotifierServer) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			n, err := s.client.CleanupOrphanedRoutes(ctx, s.podID, 5*time.Minute)
			if err != nil {
				s.logger.Error("cleanup orphaned routes", "error", err)
			} else if n > 0 {
				s.logger.Info("cleaned up orphaned routes", "count", n)
			}
		case <-ctx.Done():
			return
		}
	}
}

// Shutdown gracefully stops the notification service.
func (s *NotifierServer) Shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, conn := range s.wsClients {
		conn.Close()
		delete(s.wsClients, id)
	}
	s.logger.Info("notification service shut down")
}

func configureSender(sender Sender, config map[string]any) error {
	b, err := json.Marshal(config)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, sender)
}
