package store

import (
	"context"
	"time"

	"github.com/flowgent-labs/flowgent/model/src"
)

// INotifierStore manages notification channels and subscription routes.
type INotifierStore interface {
	SaveChannel(ctx context.Context, ch *model.NotifierChannel) error
	GetChannel(ctx context.Context, id string) (*model.NotifierChannel, error)
	ListChannels(ctx context.Context, tenantID string) ([]model.NotifierChannel, error)
	DeleteChannel(ctx context.Context, id string) error
	SaveRoute(ctx context.Context, r *model.SubscriptionRoute) error
	GetRoutesByFlow(ctx context.Context, id string) ([]model.SubscriptionRoute, error)
	DeleteRoute(ctx context.Context, id string) error
	DeleteRoutesByPod(ctx context.Context, podID string) error
	CleanupOrphanedRoutes(ctx context.Context, podID string, maxAge time.Duration) (int64, error)
}

// ─── PostgresStore methods — notifier ─────────────────────────

func (s *PostgresStore) SaveChannel(ctx context.Context, ch *model.NotifierChannel) error {
	return nil
}

func (s *PostgresStore) GetChannel(ctx context.Context, id string) (*model.NotifierChannel, error) {
	return nil, nil
}

func (s *PostgresStore) ListChannels(ctx context.Context, tenantID string) ([]model.NotifierChannel, error) {
	return nil, nil
}

func (s *PostgresStore) DeleteChannel(ctx context.Context, id string) error { return nil }

func (s *PostgresStore) SaveRoute(ctx context.Context, r *model.SubscriptionRoute) error {
	return nil
}

func (s *PostgresStore) GetRoutesByFlow(ctx context.Context, id string) ([]model.SubscriptionRoute, error) {
	return nil, nil
}

func (s *PostgresStore) DeleteRoute(ctx context.Context, id string) error { return nil }

func (s *PostgresStore) DeleteRoutesByPod(ctx context.Context, podID string) error {
	return nil
}

func (s *PostgresStore) CleanupOrphanedRoutes(ctx context.Context, podID string, maxAge time.Duration) (int64, error) {
	return 0, nil
}

// ─── SQLiteStore methods — notifier ───────────────────────────

func (s *SQLiteStore) SaveChannel(ctx context.Context, ch *model.NotifierChannel) error {
	return nil
}

func (s *SQLiteStore) GetChannel(ctx context.Context, id string) (*model.NotifierChannel, error) {
	return nil, nil
}

func (s *SQLiteStore) ListChannels(ctx context.Context, tenantID string) ([]model.NotifierChannel, error) {
	return nil, nil
}

func (s *SQLiteStore) DeleteChannel(ctx context.Context, id string) error { return nil }

func (s *SQLiteStore) SaveRoute(ctx context.Context, route *model.SubscriptionRoute) error {
	return nil
}

func (s *SQLiteStore) GetRoutesByFlow(ctx context.Context, id string) ([]model.SubscriptionRoute, error) {
	return nil, nil
}

func (s *SQLiteStore) DeleteRoute(ctx context.Context, id string) error { return nil }

func (s *SQLiteStore) DeleteRoutesByPod(ctx context.Context, podID string) error {
	return nil
}

func (s *SQLiteStore) CleanupOrphanedRoutes(ctx context.Context, podID string, maxAge time.Duration) (int64, error) {
	return 0, nil
}
