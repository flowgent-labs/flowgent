package notifier

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NotifierPostgresStore wraps store.PostgresGenericStore[entities.NotifyChannelInfo].
type NotifierPostgresStore struct {
	inner *store.PostgresGenericStore[entities.NotifyChannelInfo]
}

func NewNotifierPostgresStore(pool *pgxpool.Pool) *NotifierPostgresStore {
	return &NotifierPostgresStore{
		inner: &store.PostgresGenericStore[entities.NotifyChannelInfo]{
			Pool: pool, Table: "nfy_channel", IDCol: "id",
		},
	}
}

func (s *NotifierPostgresStore) Get(ctx context.Context, id string) (*entities.NotifyChannelInfo, error) {
	return s.inner.Get(ctx, id)
}
func (s *NotifierPostgresStore) Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.NotifyChannelInfo], error) {
	return s.inner.Select(ctx, req)
}
func (s *NotifierPostgresStore) Save(ctx context.Context, e *entities.NotifyChannelInfo) error {
	return s.inner.Save(ctx, e)
}
func (s *NotifierPostgresStore) Delete(ctx context.Context, id string) error {
	return s.inner.Delete(ctx, id)
}
