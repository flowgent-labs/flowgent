// Package graphrelease persists immutable producer releases, consumer grants,
// and installation provenance. AuthGuard owns authorization decisions; these
// repositories only enforce the resulting FlowgentSqlScope.
package graphrelease

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storage "github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/jackc/pgx/v5/pgxpool"
)

type IRepository interface {
	GetRelease(context.Context, string) (*entities.AgentGraphRelease, error)
	ListAccessibleReleases(context.Context, string) ([]*entities.AgentGraphRelease, error)
	SaveRelease(context.Context, *entities.AgentGraphRelease) error
	RevokeRelease(context.Context, string, string, string) error
	CanAccessRelease(context.Context, string, string) (bool, error)

	ListGrants(context.Context, string, string) ([]*entities.AgentGraphReleaseGrant, error)
	SaveGrant(context.Context, *entities.AgentGraphReleaseGrant) error
	DeleteGrant(context.Context, string, string, string, string) error

	ListInstallations(context.Context, string) ([]*entities.AgentGraphInstallation, error)
	Install(context.Context, *entities.AgentGraph, *entities.AgentGraphInstallation, string) error
}

func NewRepository(store storage.IStorage) (IRepository, error) {
	switch db := store.DB().(type) {
	case *sql.DB:
		return newSQLiteRepository(db), nil
	case *pgxpool.Pool:
		return newPostgresRepository(db), nil
	default:
		return nil, fmt.Errorf("graphrelease: unsupported store %T", store.DB())
	}
}
