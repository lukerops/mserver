// Package store persists the identity records: accounts, profiles, devices
// and sessions. The Store is a concrete type, with hand-written SQL that
// runs on both SQLite and Postgres (placeholders "$1, $2, ..."); only the
// migrations differ between them (migrations/sqlite and migrations/postgres,
// in Migrations).
//
// Usernames and emails are compared ignoring case.
//
// The SQLite pool has a single connection: inside a transaction every query
// must go through the transaction, and rows must be closed before running
// another query. The transactions are internal to the Store methods.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Migrations holds the migrations of the module, laid out as
// "migrations/sqlite/*.sql" and "migrations/postgres/*.sql", as expected by
// database.Open.
//
//go:embed migrations/*/*.sql
var Migrations embed.FS

// Store persists accounts, profiles, devices and sessions. Create it with
// New.
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

// key is the form used to compare usernames and emails.
func key(s string) string { return strings.ToLower(s) }

func nanos(t time.Time) int64     { return t.UnixNano() }
func fromNanos(n int64) time.Time { return time.Unix(0, n).UTC() }

// nullID stores uuid.Nil as NULL.
func nullID(id uuid.UUID) uuid.NullUUID {
	return uuid.NullUUID{UUID: id, Valid: id != uuid.Nil}
}
