package notifier

import (
	"context"
	"database/sql"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
)

// NotifierSQLiteStore wraps store.SQLiteGenericStore[entities.NotifyChannelInfo].
type NotifierSQLiteStore struct {
	inner *store.SQLiteGenericStore[entities.NotifyChannelInfo]
}

func NewNotifierSQLiteStore(conn *sql.DB) *NotifierSQLiteStore {
	return &NotifierSQLiteStore{
		inner: &store.SQLiteGenericStore[entities.NotifyChannelInfo]{
			Conn: conn, Table: "nfy_channel", IDCol: "id",
		},
	}
}

func (s *NotifierSQLiteStore) Get(ctx context.Context, id string) (*entities.NotifyChannelInfo, error) {
	return s.inner.Get(ctx, id)
}
func (s *NotifierSQLiteStore) Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.NotifyChannelInfo], error) {
	return s.inner.Select(ctx, req)
}
func (s *NotifierSQLiteStore) Save(ctx context.Context, e *entities.NotifyChannelInfo) error {
	return s.inner.Save(ctx, e)
}
func (s *NotifierSQLiteStore) Delete(ctx context.Context, id string) error {
	return s.inner.Delete(ctx, id)
}
