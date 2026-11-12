// Package store persists the library catalog: libraries, movies, series,
// seasons and episodes. The Store is a concrete type, with hand-written SQL
// that runs on both SQLite and Postgres (placeholders "$1, $2, ..."); only
// the migrations differ between them (migrations/sqlite and
// migrations/postgres, in Migrations).
//
// The audio and subtitle languages of the media info are also saved in
// their own columns (see languages.go), so that the lists can filter by
// language with a plain LIKE.
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

// Store persists the catalog. Create it with New.
type Store struct {
	db *sql.DB
}

// queryer is the subset of *sql.DB and *sql.Tx used by the query helpers.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// New returns the Store on a database already migrated (see database.Open
// and Migrations).
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// withTx runs fn in a transaction, committing if fn succeeds. fn must run
// its queries on the given transaction.
func (s *Store) withTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	return s.withTxOptions(ctx, nil, fn)
}

// withReadTx runs fn in a read-only transaction with the strongest
// isolation both drivers support: on Postgres the Repeatable Read level,
// so every statement of a read (the count and the page of a list) sees the
// same snapshot; on SQLite the level is ignored, and its single connection
// already serializes the reads.
func (s *Store) withReadTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	return s.withTxOptions(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}, fn)
}

func (s *Store) withTxOptions(ctx context.Context, opts *sql.TxOptions, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, opts)
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

// inClause appends vals to args and returns the placeholders of the IN
// list, e.g. "($1, $2)". vals must not be empty.
func inClause[T any](args *[]any, vals []T) string {
	ph := make([]string, len(vals))
	for i, v := range vals {
		ph[i] = arg(args, v)
	}
	return "(" + strings.Join(ph, ", ") + ")"
}

// paginate appends LIMIT and OFFSET to query, only when they are set (a
// "LIMIT -1" is invalid on Postgres). SQLite does not accept an OFFSET
// without a LIMIT, so an offset alone gets the largest possible limit.
func paginate(query string, args *[]any, limit, offset int) string {
	var b strings.Builder
	b.WriteString(query)
	switch {
	case limit > 0:
		b.WriteString(" LIMIT ")
		b.WriteString(arg(args, limit))
	case offset > 0:
		b.WriteString(" LIMIT ")
		b.WriteString(arg(args, int64(math.MaxInt64)))
	}
	if offset > 0 {
		b.WriteString(" OFFSET ")
		b.WriteString(arg(args, offset))
	}
	return b.String()
}

// where joins the conditions into a WHERE clause, empty when there are
// none.
func where(conds []string) string {
	if len(conds) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(conds, " AND ")
}

func nanos(t time.Time) int64     { return t.UnixNano() }
func fromNanos(n int64) time.Time { return time.Unix(0, n).UTC() }
