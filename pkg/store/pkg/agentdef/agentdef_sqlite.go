package agentdef

import (
	"context"
	"database/sql"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
)

// AgentDefSQLiteStore wraps store.SQLiteGenericStore[entities.AgentInfo].
type AgentDefSQLiteStore struct {
	inner *store.SQLiteGenericStore[entities.AgentInfo]
}

func NewAgentDefSQLiteStore(conn *sql.DB) *AgentDefSQLiteStore {
	return &AgentDefSQLiteStore{
		inner: &store.SQLiteGenericStore[entities.AgentInfo]{
			Conn: conn, Table: "llm_agent", IDCol: "name",
		},
	}
}

func (s *AgentDefSQLiteStore) Get(ctx context.Context, name string) (*entities.AgentInfo, error) {
	return s.inner.Get(ctx, name)
}
func (s *AgentDefSQLiteStore) Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.AgentInfo], error) {
	return s.inner.Select(ctx, req)
}
func (s *AgentDefSQLiteStore) Save(ctx context.Context, e *entities.AgentInfo) error {
	return s.inner.Save(ctx, e)
}
func (s *AgentDefSQLiteStore) Delete(ctx context.Context, name string) error {
	return s.inner.Delete(ctx, name)
}
