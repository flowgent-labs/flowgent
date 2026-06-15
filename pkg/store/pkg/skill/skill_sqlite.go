package skill

import (
	"context"
	"database/sql"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
)

// SkillSQLiteStore wraps store.SQLiteGenericStore[entities.SkillInfo].
type SkillSQLiteStore struct {
	inner *store.SQLiteGenericStore[entities.SkillInfo]
}

func NewSkillSQLiteStore(conn *sql.DB) *SkillSQLiteStore {
	return &SkillSQLiteStore{
		inner: &store.SQLiteGenericStore[entities.SkillInfo]{Conn: conn, Table: "llm_skill", IDCol: "name"},
	}
}

func (s *SkillSQLiteStore) Get(ctx context.Context, name string) (*entities.SkillInfo, error) {
	return s.inner.Get(ctx, name)
}
func (s *SkillSQLiteStore) Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.SkillInfo], error) {
	return s.inner.Select(ctx, req)
}
func (s *SkillSQLiteStore) Save(ctx context.Context, e *entities.SkillInfo) error {
	return s.inner.Save(ctx, e)
}
func (s *SkillSQLiteStore) Delete(ctx context.Context, name string) error {
	return s.inner.Delete(ctx, name)
}
