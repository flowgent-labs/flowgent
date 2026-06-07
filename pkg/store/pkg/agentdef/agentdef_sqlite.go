package agentdef

import (
	"context"
	"database/sql"

	"github.com/flowgent-labs/flowgent/model/src"
	"github.com/flowgent-labs/flowgent/store/src"
)

// AgentDefSQLiteStore wraps store.SQLiteGenericStore[model.AgentDef].
type AgentDefSQLiteStore struct {
	inner *store.SQLiteGenericStore[model.AgentDef]
}

func NewAgentDefSQLiteStore(conn *sql.DB) *AgentDefSQLiteStore {
	return &AgentDefSQLiteStore{
		inner: &store.SQLiteGenericStore[model.AgentDef]{
			Conn: conn, Table: "agents", IDCol: "name",
		},
	}
}

func (s *AgentDefSQLiteStore) Get(ctx context.Context, name string) (*model.AgentDef, error) {
	return s.inner.Get(ctx, name)
}
func (s *AgentDefSQLiteStore) Select(ctx context.Context, req model.PageRequest) (*model.Page[model.AgentDef], error) {
	return s.inner.Select(ctx, req)
}
func (s *AgentDefSQLiteStore) Save(ctx context.Context, e *model.AgentDef) error {
	return s.inner.Save(ctx, e)
}
func (s *AgentDefSQLiteStore) Delete(ctx context.Context, name string) error {
	return s.inner.Delete(ctx, name)
}
