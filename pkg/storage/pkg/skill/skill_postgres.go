package skill

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SkillPostgresStore wraps storage.PostgresGenericStore[entities.SkillInfo].
type SkillPostgresStore struct {
	byName *storage.PostgresGenericStore[entities.SkillInfo]
	byID   *storage.PostgresGenericStore[entities.SkillInfo]
}

func NewSkillPostgresStore(pool *pgxpool.Pool) *SkillPostgresStore {
	return &SkillPostgresStore{
		byName: &storage.PostgresGenericStore[entities.SkillInfo]{Pool: pool, Table: "llm_skill", IDCol: "name"},
		byID:   &storage.PostgresGenericStore[entities.SkillInfo]{Pool: pool, Table: "llm_skill", IDCol: "id"},
	}
}

func (s *SkillPostgresStore) Get(ctx context.Context, namespace, name string) (*entities.SkillInfo, error) {
	return s.byName.GetScoped(ctx, namespace, name)
}
func (s *SkillPostgresStore) List(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[entities.SkillInfo], error) {
	return s.byID.SelectScoped(ctx, namespace, req)
}
func (s *SkillPostgresStore) Save(ctx context.Context, e *entities.SkillInfo) error {
	return s.byID.Save(ctx, e)
}
func (s *SkillPostgresStore) Delete(ctx context.Context, namespace, name string) error {
	return s.byName.DeleteScoped(ctx, namespace, name)
}
