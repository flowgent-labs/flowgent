// Package runtimeconfig persists namespace and Flow runtime configuration.
// Inheritance and encryption remain API-domain responsibilities.
package runtimeconfig

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storage "github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("runtime configuration not found")

type IRepository interface {
	Get(context.Context, string, string, string) (*entities.RuntimeConfiguration, error)
	Upsert(context.Context, *entities.RuntimeConfiguration) error
}

func NewRepository(store storage.IStorage) (IRepository, error) {
	switch db := store.DB().(type) {
	case *sql.DB:
		return &sqliteRepository{db: db}, nil
	case *pgxpool.Pool:
		return &postgresRepository{db: db}, nil
	default:
		return nil, fmt.Errorf("runtimeconfig: unsupported store %T", store.DB())
	}
}

func normalizeNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
