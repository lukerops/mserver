// Package store persists the storage index: locations, files and the
// results of the analyzers. The Store is a concrete type, with hand-written
// SQL that runs on both SQLite and Postgres (placeholders "$1, $2, ...");
// only the migrations differ between them (migrations/sqlite and
// migrations/postgres, in Migrations).
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
	"math"
	"strings"
	"time"
)

// Migrations holds the migrations of the module, laid out as
// "migrations/sqlite/*.sql" and "migrations/postgres/*.sql", as expected by
// database.Open.
//
//go:embed migrations/*/*.sql
var Migrations embed.FS

// Store persists locations, files and analyses. Create it with New.
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

// arg appends v to args and returns its placeholder.
func arg(args *[]any, v any) string {
	*args = append(*args, v)
	return fmt.Sprintf("$%d", len(*args))
}

// paginate appends LIMIT and OFFSET to query, only when they are set (a
// "LIMIT -1" is invalid on Postgres). SQLite does not accept an OFFSET
// without a LIMIT, so an offset alone gets the largest possible limit.
func paginate(query string, args *[]any, p Page) string {
	var b strings.Builder
	b.WriteString(query)
	switch {
	case p.Limit > 0:
		b.WriteString(" LIMIT ")
		b.WriteString(arg(args, p.Limit))
	case p.Offset > 0:
		b.WriteString(" LIMIT ")
		b.WriteString(arg(args, int64(math.MaxInt64)))
	}
	if p.Offset > 0 {
		b.WriteString(" OFFSET ")
		b.WriteString(arg(args, p.Offset))
	}
	return b.String()
}

func nanos(t time.Time) int64     { return t.UnixNano() }
func fromNanos(n int64) time.Time { return time.Unix(0, n).UTC() }
