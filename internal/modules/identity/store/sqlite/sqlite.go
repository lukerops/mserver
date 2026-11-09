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
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/database"
	dbsqlite "github.com/lukerops/mserver/internal/database/sqlite"
	"github.com/lukerops/mserver/internal/modules/identity/store"
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
		return fmt.Errorf("%w: %s", store.ErrNotFound, what)
	}
	return fmt.Errorf("store: %s: %w", what, err)
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// key is the form used to compare usernames and emails.
func key(s string) string { return strings.ToLower(s) }

func nanos(t time.Time) int64     { return t.UnixNano() }
func fromNanos(n int64) time.Time { return time.Unix(0, n).UTC() }

// nullID stores uuid.Nil as NULL.
func nullID(id uuid.UUID) uuid.NullUUID {
	return uuid.NullUUID{UUID: id, Valid: id != uuid.Nil}
}
