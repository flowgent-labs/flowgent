package notifier

import (
	"context"
	"database/sql"

	"github.com/flowgent-labs/flowgent/model/src"
	"github.com/flowgent-labs/flowgent/store/src"
	"github.com/flowgent-labs/flowgent/common/src/utils"
)

// NotifierSQLiteStore wraps store.SQLiteGenericStore[model.NotifierChannel].
type NotifierSQLiteStore struct {
	inner *store.SQLiteGenericStore[model.NotifierChannel]
}

func NewNotifierSQLiteStore(conn *sql.DB) *NotifierSQLiteStore {
	return &NotifierSQLiteStore{
		inner: &store.SQLiteGenericStore[model.NotifierChannel]{
			Conn: conn, Table: "notification_channels", IDCol: "id",
		},
	}
}

func (s *NotifierSQLiteStore) Get(ctx context.Context, id string) (*model.NotifierChannel, error) {
	return s.inner.Get(ctx, id)
}
func (s *NotifierSQLiteStore) Select(ctx context.Context, page, pageSize int) (*utils.Page[model.NotifierChannel], error) {
	return s.inner.Select(ctx, page, pageSize)
}
func (s *NotifierSQLiteStore) Save(ctx context.Context, e *model.NotifierChannel) error {
	return s.inner.Save(ctx, e)
}
func (s *NotifierSQLiteStore) Delete(ctx context.Context, id string) error {
	return s.inner.Delete(ctx, id)
}
