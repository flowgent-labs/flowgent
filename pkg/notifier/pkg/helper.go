package notifier

import (
	"context"

	"github.com/flowgent-labs/flowgent/api/pkg/handler"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
)

// NotifToWSAdapter adapts notifier.NotifierServer to the api.WSBridge interface.
type NotifToWSAdapter struct {
	Svc *NotifierServer
}

func (a *NotifToWSAdapter) RegisterWS(ctx context.Context, agentFlowID string) (handler.WSConn, error) {
	conn, err := a.Svc.RegisterWS(ctx, agentFlowID)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (a *NotifToWSAdapter) PodID() string { return a.Svc.PodID() }

// CreateNotifierService builds a notifier.NotifierServer from config, or nil if disabled.
func CreateNotifierService(api *client.FlowgentClient, cfg *config.FlowgentConfig) *NotifierServer {
	if !cfg.Notifier.Enabled {
		return nil
	}
	tenant := cfg.Tenant.DefaultTenant
	if tenant == "" {
		tenant = "default"
	}
	notifierClient := client.NewNotifierClient(api, tenant)
	return NewNotifierServer(notifierClient, nil)
}
