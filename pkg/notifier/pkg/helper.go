package notifier

import (
	"context"
	"sync"
	"time"

	"github.com/flowgent-labs/flowgent/api/pkg/handler"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/model/pkg"
)

// ─── Adapters ─────────────────────────────────────────────────────

// NotifToWSAdapter adapts notifier.Service to the api.WSBridge interface.
type NotifToWSAdapter struct {
	Svc *Service
}

func (a *NotifToWSAdapter) RegisterWS(ctx context.Context, agentFlowID string) (handler.WSConn, error) {
	conn, err := a.Svc.RegisterWS(ctx, agentFlowID)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (a *NotifToWSAdapter) PodID() string { return a.Svc.PodID() }

// NotifierStoreAdapter satisfies notifier.Store using the apiserver client.
// Subscription routes are kept in-memory (transient, no REST API for them).
type NotifierStoreAdapter struct {
	API      *client.FlowgentClient
	Tenant   string
	RoutesMu sync.Mutex
	Routes   map[string]*model.SubscriptionRoute
}

// NewNotifierStoreAdapter creates a client-backed NotifierStoreAdapter.
func NewNotifierStoreAdapter(api *client.FlowgentClient, tenant string) *NotifierStoreAdapter {
	return &NotifierStoreAdapter{
		API:    api,
		Tenant: tenant,
		Routes: make(map[string]*model.SubscriptionRoute),
	}
}

func (a *NotifierStoreAdapter) ListPendingApprovals(ctx context.Context) ([]model.HumanApproval, error) {
	return a.API.ListPendingApprovals(ctx)
}

func (a *NotifierStoreAdapter) ListChannels(ctx context.Context, tenantID string) ([]model.NotifierChannel, error) {
	tid := tenantID
	if tid == "" {
		tid = a.Tenant
	}
	return a.API.ListChannels(ctx, tid)
}

func (a *NotifierStoreAdapter) SaveRoute(ctx context.Context, route *model.SubscriptionRoute) error {
	a.RoutesMu.Lock()
	defer a.RoutesMu.Unlock()
	a.Routes[route.ID] = route
	return nil
}

func (a *NotifierStoreAdapter) GetRoutesByFlow(ctx context.Context, agentFlowID string) ([]model.SubscriptionRoute, error) {
	a.RoutesMu.Lock()
	defer a.RoutesMu.Unlock()
	var result []model.SubscriptionRoute
	for _, route := range a.Routes {
		if route.AgentFlowID == agentFlowID {
			result = append(result, *route)
		}
	}
	return result, nil
}

func (a *NotifierStoreAdapter) DeleteRoute(ctx context.Context, id string) error {
	a.RoutesMu.Lock()
	defer a.RoutesMu.Unlock()
	delete(a.Routes, id)
	return nil
}

func (a *NotifierStoreAdapter) CleanupOrphanedRoutes(ctx context.Context, podID string, maxAge time.Duration) (int64, error) {
	a.RoutesMu.Lock()
	defer a.RoutesMu.Unlock()
	var deleted int64
	for id, route := range a.Routes {
		if route.PodID == podID && time.Since(route.CreatedAt) > maxAge {
			delete(a.Routes, id)
			deleted++
		}
	}
	return deleted, nil
}

// CreateNotifierService builds a notifier.Service from config, or nil if disabled.
func CreateNotifierService(api *client.FlowgentClient, cfg *config.FlowgentConfig) *Service {
	if !cfg.Notifier.Enabled {
		return nil
	}
	tenant := cfg.Tenant.DefaultTenant
	if tenant == "" {
		tenant = "default"
	}
	adapter := NewNotifierStoreAdapter(api, tenant)
	return NewService(adapter, nil)
}
