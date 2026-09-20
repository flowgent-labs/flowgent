package notifier

import (
	"context"
	"database/sql"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
)

// NotifierSQLiteStore wraps storage.SQLiteGenericStore[entities.NotifyChannelInfo].
type NotifierSQLiteStore struct {
	inner *storage.SQLiteGenericStore[entities.NotifyChannelInfo]
}

func NewNotifierSQLiteStore(conn *sql.DB) *NotifierSQLiteStore {
	return &NotifierSQLiteStore{
		inner: &storage.SQLiteGenericStore[entities.NotifyChannelInfo]{
			Conn: conn, Table: "nfy_channel", IDCol: "id",
		},
	}
}

func (s *NotifierSQLiteStore) Get(ctx context.Context, namespace, id string) (*entities.NotifyChannelInfo, error) {
	return s.inner.GetScoped(ctx, namespace, id)
}
func (s *NotifierSQLiteStore) List(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[entities.NotifyChannelInfo], error) {
	return s.inner.SelectScoped(ctx, namespace, req)
}
func (s *NotifierSQLiteStore) Save(ctx context.Context, e *entities.NotifyChannelInfo) error {
	return s.inner.Save(ctx, e)
}
func (s *NotifierSQLiteStore) Delete(ctx context.Context, namespace, id string) error {
	return s.inner.DeleteScoped(ctx, namespace, id)
}
