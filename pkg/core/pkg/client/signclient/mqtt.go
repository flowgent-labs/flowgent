//go:build x402

package signclient

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type MQTTConfig struct {
	Broker              string
	ClientID            string
	Username            string
	Password            string
	RequestTopicPrefix  string
	ResponseTopicPrefix string
	Timeout             time.Duration
}

type MQTTTransport struct {
	client        mqtt.Client
	requestTopic  string
	responseTopic string
	timeout       time.Duration
	mu            sync.Mutex
	pending       map[string]chan *SignResponse
}

func NewMQTTTransport(config MQTTConfig) (*MQTTTransport, error) {
	if config.Broker == "" {
		return nil, fmt.Errorf("wallet MQTT transport requires messager.mqtt.broker")
	}
	if err := validateIdentifier("client_id", config.ClientID); err != nil {
		return nil, err
	}
	if config.RequestTopicPrefix == "" {
		config.RequestTopicPrefix = DefaultRequestTopicPrefix
	}
	if config.ResponseTopicPrefix == "" {
		config.ResponseTopicPrefix = DefaultResponseTopicPrefix
	}
	if config.Timeout <= 0 {
		config.Timeout = 30 * time.Second
	}
	transport := &MQTTTransport{
		requestTopic:  config.RequestTopicPrefix + "/" + config.ClientID,
		responseTopic: config.ResponseTopicPrefix + "/" + config.ClientID,
		timeout:       config.Timeout,
		pending:       make(map[string]chan *SignResponse),
	}
	options := mqtt.NewClientOptions().
		AddBroker(config.Broker).
		SetClientID(config.ClientID).
		SetCleanSession(true).
		SetKeepAlive(30 * time.Second).
		SetPingTimeout(10 * time.Second).
		SetConnectTimeout(10 * time.Second).
		SetAutoReconnect(true).
		SetResumeSubs(true).
		SetConnectionLostHandler(func(_ mqtt.Client, err error) {})
	if config.Username != "" {
		options.SetUsername(config.Username)
		options.SetPassword(config.Password)
	}
	transport.client = mqtt.NewClient(options)
	if token := transport.client.Connect(); !token.WaitTimeout(config.Timeout) {
		return nil, fmt.Errorf("connect to wallet MQTT broker: timeout")
	} else if token.Error() != nil {
		return nil, fmt.Errorf("connect to wallet MQTT broker: %w", token.Error())
	}
	if token := transport.client.Subscribe(transport.responseTopic, 1, transport.handleResponse); !token.WaitTimeout(config.Timeout) {
		transport.client.Disconnect(250)
		return nil, fmt.Errorf("subscribe to wallet response topic: timeout")
	} else if token.Error() != nil {
		transport.client.Disconnect(250)
		return nil, fmt.Errorf("subscribe to wallet response topic: %w", token.Error())
	}
	return transport, nil
}

func (t *MQTTTransport) RoundTrip(ctx context.Context, request *SignRequest) (*SignResponse, error) {
	result := make(chan *SignResponse, 1)
	t.mu.Lock()
	t.pending[request.RequestID] = result
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		delete(t.pending, request.RequestID)
		t.mu.Unlock()
	}()

	payload, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode wallet signing request: %w", err)
	}
	if token := t.client.Publish(t.requestTopic, 1, false, payload); !token.WaitTimeout(t.timeout) {
		return nil, fmt.Errorf("publish wallet signing request: timeout")
	} else if token.Error() != nil {
		return nil, fmt.Errorf("publish wallet signing request: %w", token.Error())
	}

	timer := time.NewTimer(t.timeout)
	defer timer.Stop()
	select {
	case response := <-result:
		return response, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, fmt.Errorf("wallet signing request timed out after %s", t.timeout)
	}
}

func (t *MQTTTransport) Close() {
	if t.client != nil && t.client.IsConnected() {
		t.client.Disconnect(250)
	}
}

func (t *MQTTTransport) handleResponse(_ mqtt.Client, message mqtt.Message) {
	if len(message.Payload()) > maxResponseBytes {
		return
	}
	var response SignResponse
	if err := json.Unmarshal(message.Payload(), &response); err != nil {
		return
	}
	t.mu.Lock()
	result := t.pending[response.RequestID]
	t.mu.Unlock()
	if result != nil {
		select {
		case result <- &response:
		default:
		}
	}
}

var _ Transport = (*MQTTTransport)(nil)
