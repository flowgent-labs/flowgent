package skill

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SkillPostgresStore wraps storage.PostgresGenericStore[entities.SkillInfo].
type SkillPostgresStore struct {
	inner *storage.PostgresGenericStore[entities.SkillInfo]
}

func NewSkillPostgresStore(pool *pgxpool.Pool) *SkillPostgresStore {
	return &SkillPostgresStore{
		inner: &storage.PostgresGenericStore[entities.SkillInfo]{Pool: pool, Table: "llm_skill", IDCol: "name"},
	}
}

func (s *SkillPostgresStore) Get(ctx context.Context, name string) (*entities.SkillInfo, error) {
	return s.inner.Get(ctx, name)
}
func (s *SkillPostgresStore) Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.SkillInfo], error) {
	return s.inner.Select(ctx, req)
}
func (s *SkillPostgresStore) Save(ctx context.Context, e *entities.SkillInfo) error {
	return s.inner.Save(ctx, e)
}
func (s *SkillPostgresStore) Delete(ctx context.Context, name string) error {
	return s.inner.Delete(ctx, name)
}
