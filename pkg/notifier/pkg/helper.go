package notifier

import (
	"context"

	"github.com/flowgent-labs/flowgent/api/pkg/handler"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
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
func CreateNotifierService(api *client.FlowgentClient, cfg *config.FlowgentConfig, httpClient model.IFlowgentAPIClient) *FlowgentNotifierManager {
	namespace := cfg.Runtime.Namespace.DefaultNamespace
	if namespace == "" {
		namespace = "default"
	}
	notifierClient := client.NewNotifierClient(api, namespace)
	return NewFlowgentNotifierManager(notifierClient, nil, httpClient, &cfg.Notifier)
}
