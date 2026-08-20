package agent

import (
	"context"
	"database/sql"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
)

// AgentSQLiteStore wraps store.SQLiteGenericStore[entities.AgentInfo].
type AgentSQLiteStore struct {
	inner *store.SQLiteGenericStore[entities.AgentInfo]
}

func NewAgentSQLiteStore(conn *sql.DB) *AgentSQLiteStore {
	return &AgentSQLiteStore{
		inner: &store.SQLiteGenericStore[entities.AgentInfo]{
			Conn: conn, Table: "llm_agent", IDCol: "id",
		},
	}
}

func (s *AgentSQLiteStore) Get(ctx context.Context, namespace, name string) (*entities.AgentInfo, error) {
	lookup := *s.inner
	lookup.IDCol = "name"
	return lookup.GetScoped(ctx, namespace, name)
}
func (s *AgentSQLiteStore) List(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[entities.AgentInfo], error) {
	return s.inner.SelectScoped(ctx, namespace, req)
}
func (s *AgentSQLiteStore) Save(ctx context.Context, e *entities.AgentInfo) error {
	return s.inner.Save(ctx, e)
}
func (s *AgentSQLiteStore) Delete(ctx context.Context, namespace, name string) error {
	lookup := *s.inner
	lookup.IDCol = "name"
	return lookup.DeleteScoped(ctx, namespace, name)
}
