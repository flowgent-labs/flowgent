package notifier

import (
	"context"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NotifierPostgresStore wraps storage.PostgresGenericStore[entities.NotifyChannelInfo].
type NotifierPostgresStore struct {
	inner *storage.PostgresGenericStore[entities.NotifyChannelInfo]
}

func NewNotifierPostgresStore(pool *pgxpool.Pool) *NotifierPostgresStore {
	return &NotifierPostgresStore{
		inner: &storage.PostgresGenericStore[entities.NotifyChannelInfo]{
			Pool: pool, Table: "nfy_channel", IDCol: "id",
		},
	}
}

func (s *NotifierPostgresStore) Get(ctx context.Context, namespace, id string) (*entities.NotifyChannelInfo, error) {
	return s.inner.GetScoped(ctx, namespace, id)
}
func (s *NotifierPostgresStore) List(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[entities.NotifyChannelInfo], error) {
	return s.inner.SelectScoped(ctx, namespace, req)
}
func (s *NotifierPostgresStore) Save(ctx context.Context, e *entities.NotifyChannelInfo) error {
	if e.RowVersion == 0 {
		e.RowVersion = 1
	}
	if e.Status == "" {
		e.Status = "ACTIVE"
	}
	now := time.Now().UTC()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	e.UpdatedAt = now
	if _, err := s.inner.Pool.Exec(ctx, `INSERT INTO orh_namespace(id,name,description,created_by,updated_by) VALUES($1::varchar(64),$1::text,'Flowgent namespace',$2::varchar(255),$2::varchar(255)) ON CONFLICT(id) DO NOTHING`, e.Namespace, e.CreatedBy); err != nil {
		return err
	}
	return s.inner.Save(ctx, e)
}
func (s *NotifierPostgresStore) Delete(ctx context.Context, namespace, id string) error {
	return s.inner.DeleteScoped(ctx, namespace, id)
}
