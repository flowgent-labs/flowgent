// Package resourcepool persists namespace-scoped worker capacity definitions.
package resourcepool

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storepkg "github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound      = errors.New("resource pool not found")
	ErrAlreadyExists = errors.New("resource pool already exists")
)

type IRepository interface {
	List(context.Context, string) ([]*entities.ResourcePoolInfo, error)
	Get(context.Context, string, string) (*entities.ResourcePoolInfo, error)
	Create(context.Context, *entities.ResourcePoolInfo) error
	Update(context.Context, *entities.ResourcePoolInfo) error
	Delete(context.Context, string, string, string) error
}

func NewRepository(store storepkg.IStore) (IRepository, error) {
	switch db := store.DB().(type) {
	case *sql.DB:
		return &sqliteRepository{db: db}, nil
	case *pgxpool.Pool:
		return &postgresRepository{db: db}, nil
	default:
		return nil, fmt.Errorf("resourcepool: unsupported store %T", store.DB())
	}
}

func normalizeError(err error) error {
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
