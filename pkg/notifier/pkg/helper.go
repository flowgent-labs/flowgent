package notifier

import (
	"context"
	"fmt"
	"os"

	"github.com/flowgent-labs/flowgent/api/pkg/handler"
	"github.com/flowgent-labs/flowgent/common/pkg/secretbox"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	messager "github.com/flowgent-labs/flowgent/messager/pkg"
	model "github.com/flowgent-labs/flowgent/model/pkg"
)

// NotifToWSAdapter adapts notifier.FlowgentNotifierManager to the api.WSBridge interface.
type NotifToWSAdapter struct {
	Svc *FlowgentNotifierManager
}

func (a *NotifToWSAdapter) RegisterWS(ctx context.Context, agentFlowID string) (handler.WSConn, error) {
	conn, err := a.Svc.RegisterWS(ctx, agentFlowID)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (a *NotifToWSAdapter) PodID() string { return a.Svc.PodID() }

// CreateNotifierService builds a notifier.FlowgentNotifierManager from config.
func CreateNotifierService(api *client.FlowgentClient, cfg *config.FlowgentConfig, httpClient model.IFlowgentAPIClient) (*FlowgentNotifierManager, error) {
	namespace := cfg.Runtime.Namespace.DefaultNamespace
	if namespace == "" {
		namespace = "default"
	}
	notifierClient := client.NewNotifierClient(api, namespace)
	cipher, err := secretbox.NewAESGCMSecretCipher(
		cfg.Notifier.SecretEncryption.ActiveKeyID,
		cfg.Notifier.SecretEncryption.Keys,
	)
	if err != nil {
		return nil, fmt.Errorf("notification secret encryption: %w", err)
	}
	var queue MQTTClient
	if cfg.Messager.Type == "mqtt" && cfg.Messager.MQTT.Broker != "" {
		host, _ := os.Hostname()
		mq, mqErr := messager.NewMQTTMessager(&messager.MQTTConfig{
			Broker: cfg.Messager.MQTT.Broker, ClientID: "flowgent-notifier-" + host,
			Username: cfg.Messager.MQTT.Username, Password: cfg.Messager.MQTT.Password,
		})
		if mqErr != nil {
			return nil, fmt.Errorf("notification MQTT: %w", mqErr)
		}
		queue = &notificationMQTTAdapter{inner: mq}
	}
	return NewFlowgentNotifierManager(notifierClient, queue, httpClient, &cfg.Notifier, cipher), nil
}

type notificationMQTTAdapter struct{ inner *messager.MQTTMessager }

func (a *notificationMQTTAdapter) Subscribe(ctx context.Context, topic string, handler func(string, []byte)) error {
	return a.inner.Subscribe(ctx, topic, handler)
}

func (a *notificationMQTTAdapter) Publish(ctx context.Context, topic string, payload []byte) error {
	return a.inner.Publish(ctx, topic, &messager.InterMessage{Payload: payload})
}

func (a *notificationMQTTAdapter) Close() error { return a.inner.Close() }
