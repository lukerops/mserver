// Package sqlite implements store.Store on the module's SQLite database.
//
// The SQLite pool has a single connection: inside a transaction, every
// query must go through the transaction (see conn), and rows must be
// closed before running another query.
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"github.com/lukerops/mserver/internal/infra/database"
	dbsqlite "github.com/lukerops/mserver/internal/infra/database/sqlite"
	"github.com/lukerops/mserver/internal/modules/storage/api"
	"github.com/lukerops/mserver/internal/modules/storage/store"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Store implements store.Store.
type Store struct {
	db *sql.DB
}

var _ store.Store = (*Store)(nil)

// New migrates db and returns a store on it.
func New(ctx context.Context, db *sql.DB) (*Store, error) {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return nil, err
	}
	if err := dbsqlite.Migrate(ctx, db, sub); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return database.WithTx(ctx, s.db, fn)
}

// conn returns the transaction open in ctx, or the database.
func (s *Store) conn(ctx context.Context) database.Conn {
	return database.ConnFrom(ctx, s.db)
}

type rowScanner interface{ Scan(dest ...any) error }

func notFound(err error, what string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", api.ErrNotFound, what)
	}
	return fmt.Errorf("store: %s: %w", what, err)
}
