package skill

import (
	"context"
	"database/sql"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
)

// SkillSQLiteStore wraps storage.SQLiteGenericStore[entities.SkillInfo].
type SkillSQLiteStore struct {
	byName *storage.SQLiteGenericStore[entities.SkillInfo]
	byID   *storage.SQLiteGenericStore[entities.SkillInfo]
}

func NewSkillSQLiteStore(conn *sql.DB) *SkillSQLiteStore {
	return &SkillSQLiteStore{
		byName: &storage.SQLiteGenericStore[entities.SkillInfo]{Conn: conn, Table: "llm_skill", IDCol: "name"},
		byID:   &storage.SQLiteGenericStore[entities.SkillInfo]{Conn: conn, Table: "llm_skill", IDCol: "id"},
	}
}

func (s *SkillSQLiteStore) Get(ctx context.Context, namespace, name string) (*entities.SkillInfo, error) {
	return s.byName.GetScoped(ctx, namespace, name)
}
func (s *SkillSQLiteStore) List(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[entities.SkillInfo], error) {
	return s.byID.SelectScoped(ctx, namespace, req)
}
func (s *SkillSQLiteStore) Save(ctx context.Context, e *entities.SkillInfo) error {
	return s.byID.Save(ctx, e)
}
func (s *SkillSQLiteStore) Delete(ctx context.Context, namespace, name string) error {
	return s.byName.DeleteScoped(ctx, namespace, name)
}
