// Package store persists what the subtitles module knows: the subtitle
// files of the storage, the movies and episodes of the library, and the
// subtitles matched with them. The Store is a concrete type, with
// hand-written SQL that runs on both SQLite and Postgres (placeholders
// "$1, $2, ..."); only the migrations differ between them
// (migrations/sqlite and migrations/postgres, in Migrations).
//
// The SQLite pool has a single connection: inside a transaction every query
// must go through the transaction. The transactions are internal to the
// Store methods.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
)

// Migrations holds the migrations of the module, laid out as
// "migrations/sqlite/*.sql" and "migrations/postgres/*.sql", as expected by
// database.Open.
//
//go:embed migrations/*/*.sql
var Migrations embed.FS

// Errors returned by the Store, wrapped with context: use errors.Is.
var ErrNotFound = errors.New("not found")

// Store persists the state of the module. Create it with New.
type Store struct {
	db *sql.DB
}

// New returns the Store on a database already migrated (see database.Open
// and Migrations).
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// withTx runs fn in a transaction, committing if fn succeeds. fn must run
// its queries on the given transaction.
func (s *Store) withTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin transaction: %w", err)
	}
	defer tx.Rollback()

	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit transaction: %w", err)
	}
	return nil
}
