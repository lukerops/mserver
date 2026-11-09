// Package database has helpers shared by the database/sql backends.
package database

import (
	"context"
	"database/sql"
	"fmt"
)

// Conn is what both *sql.DB and *sql.Tx implement.
type Conn interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// txKey keys the open transaction of a database in a context.
type txKey struct{ db *sql.DB }

// WithTx runs fn in a transaction on db, committing if fn succeeds. The
// transaction travels in the context given to fn, so that every code that
// uses ConnFrom with the same db joins it. A nested call reuses the open
// transaction.
func WithTx(ctx context.Context, db *sql.DB, fn func(ctx context.Context) error) error {
	if _, ok := ctx.Value(txKey{db}).(*sql.Tx); ok {
		return fn(ctx)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("database: begin: %w", err)
	}
	defer tx.Rollback()

	if err := fn(context.WithValue(ctx, txKey{db}, tx)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("database: commit: %w", err)
	}
	return nil
}

// ConnFrom returns the transaction on db open in ctx, or db itself.
func ConnFrom(ctx context.Context, db *sql.DB) Conn {
	if tx, ok := ctx.Value(txKey{db}).(*sql.Tx); ok {
		return tx
	}
	return db
}
