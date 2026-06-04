package store

import (
	"context"
	"time"

	"github.com/flowgent-labs/flowgent/model/src"
)

// INotifierStore manages NotifierChannel and SubscriptionRoute entities.
type INotifierStore interface {
	SaveChannel(ctx context.Context, ch *model.NotifierChannel) error
	GetChannel(ctx context.Context, id string) (*model.NotifierChannel, error)
	ListChannels(ctx context.Context, tenantID string) ([]model.NotifierChannel, error)
	DeleteChannel(ctx context.Context, id string) error

	SaveRoute(ctx context.Context, route *model.SubscriptionRoute) error
	GetRoutesByFlow(ctx context.Context, agentFlowID string) ([]model.SubscriptionRoute, error)
	DeleteRoute(ctx context.Context, id string) error
	DeleteRoutesByPod(ctx context.Context, podID string) error
	CleanupOrphanedRoutes(ctx context.Context, podID string, maxAge time.Duration) (int64, error)
}
