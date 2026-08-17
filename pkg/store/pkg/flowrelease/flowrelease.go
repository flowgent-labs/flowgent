// Package flowrelease persists immutable producer releases, consumer grants,
// and installation provenance. Authorization decisions remain in the API.
package flowrelease

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storepkg "github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/jackc/pgx/v5/pgxpool"
)

type IRepository interface {
	GetRelease(context.Context, string) (*entities.FlowRelease, error)
	ListAccessibleReleases(context.Context, string) ([]*entities.FlowRelease, error)
	SaveRelease(context.Context, *entities.FlowRelease) error
	RevokeRelease(context.Context, string, string, string) error
	CanAccessRelease(context.Context, string, string) (bool, error)

	ListGrants(context.Context, string, string) ([]*entities.FlowReleaseGrant, error)
	SaveGrant(context.Context, *entities.FlowReleaseGrant) error
	DeleteGrant(context.Context, string, string, string, string) error

	ListInstallations(context.Context, string) ([]*entities.FlowInstallation, error)
	Install(context.Context, *entities.FlowInfo, *entities.FlowInstallation, string) error
}

func NewRepository(store storepkg.IStore) (IRepository, error) {
	switch db := store.DB().(type) {
	case *sql.DB:
		return newSQLiteRepository(db), nil
	case *pgxpool.Pool:
		return newPostgresRepository(db), nil
	default:
		return nil, fmt.Errorf("flowrelease: unsupported store %T", store.DB())
	}
}
