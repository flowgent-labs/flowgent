package agentdef

import (
	"context"
	"database/sql"

	"github.com/flowgent-labs/flowgent/model/src"
	"github.com/flowgent-labs/flowgent/store/src"
)

// AgentSQLiteStore wraps store.SQLiteGenericStore[model.AgentDef].
type AgentSQLiteStore struct {
	inner *store.SQLiteGenericStore[model.AgentDef]
}

func NewAgentSQLiteStore(conn *sql.DB) *AgentSQLiteStore {
	return &AgentSQLiteStore{
		inner: &store.SQLiteGenericStore[model.AgentDef]{
			Conn: conn, Table: "agents", IDCol: "name",
		},
	}
}

func (s *AgentSQLiteStore) Get(ctx context.Context, name string) (*model.AgentDef, error) {
	return s.inner.Get(ctx, name)
}
func (s *AgentSQLiteStore) Select(ctx context.Context, offset, limit int) ([]*model.AgentDef, error) {
	return s.inner.Select(ctx, offset, limit)
}
func (s *AgentSQLiteStore) Save(ctx context.Context, e *model.AgentDef) error {
	return s.inner.Save(ctx, e)
}
func (s *AgentSQLiteStore) Delete(ctx context.Context, name string) error {
	return s.inner.Delete(ctx, name)
}
