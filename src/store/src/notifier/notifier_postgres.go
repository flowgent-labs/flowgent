package notifier

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/flowgent-labs/flowgent/model/src"
	"github.com/flowgent-labs/flowgent/store/src"
)

// NotifierPostgresStore wraps store.PostgresGenericStore[model.NotifierChannel].
type NotifierPostgresStore struct {
	inner *store.PostgresGenericStore[model.NotifierChannel]
}

func NewNotifierPostgresStore(pool *pgxpool.Pool) *NotifierPostgresStore {
	return &NotifierPostgresStore{
		inner: &store.PostgresGenericStore[model.NotifierChannel]{
			Pool: pool, Table: "notification_channels", IDCol: "id",
		},
	}
}

func (s *NotifierPostgresStore) Get(ctx context.Context, id string) (*model.NotifierChannel, error) {
	return s.inner.Get(ctx, id)
}
func (s *NotifierPostgresStore) Select(ctx context.Context, offset, limit int) ([]*model.NotifierChannel, error) {
	return s.inner.Select(ctx, offset, limit)
}
func (s *NotifierPostgresStore) Save(ctx context.Context, e *model.NotifierChannel) error {
	return s.inner.Save(ctx, e)
}
func (s *NotifierPostgresStore) Delete(ctx context.Context, id string) error {
	return s.inner.Delete(ctx, id)
}
