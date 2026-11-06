// Package sqlite opens per-module SQLite databases and applies embedded
// migrations.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// Open opens (creating if needed) the SQLite database at path. Use
// ":memory:" for an in-memory database.
//
// The pool is limited to a single connection: SQLite serializes writes
// anyway, and this avoids SQLITE_BUSY errors and gives every connection
// the same in-memory database. Callers must not run a query on the *sql.DB
// while holding an open transaction or unconsumed rows.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("sqlite: create directory: %w", err)
		}
	}

	dsn := "file:" + path +
		"?_pragma=foreign_keys(1)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=busy_timeout(5000)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	db.SetConnMaxIdleTime(0)
	db.SetConnMaxLifetime(0)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite: ping %s: %w", path, err)
	}
	return db, nil
}

// Migrate applies, in lexical order, every "*.sql" file in migrations that
// was not applied yet. Each file runs in its own transaction.
func Migrate(ctx context.Context, db *sql.DB, migrations fs.FS) error {
	const createTable = `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`
	if _, err := db.ExecContext(ctx, createTable); err != nil {
		return fmt.Errorf("sqlite: create schema_migrations: %w", err)
	}

	names, err := fs.Glob(migrations, "*.sql")
	if err != nil {
		return fmt.Errorf("sqlite: list migrations: %w", err)
	}
	slices.Sort(names)

	applied, err := appliedVersions(ctx, db)
	if err != nil {
		return err
	}

	for _, name := range names {
		version := strings.TrimSuffix(name, ".sql")
		if applied[version] {
			continue
		}

		script, err := fs.ReadFile(migrations, name)
		if err != nil {
			return fmt.Errorf("sqlite: read migration %s: %w", name, err)
		}
		if err := applyMigration(ctx, db, version, string(script)); err != nil {
			return err
		}
	}
	return nil
}

func appliedVersions(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: read schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("sqlite: read schema_migrations: %w", err)
		}
		applied[version] = true
	}
	return applied, rows.Err()
}

func applyMigration(ctx context.Context, db *sql.DB, version, script string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: migration %s: %w", version, err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, script); err != nil {
		return fmt.Errorf("sqlite: migration %s: %w", version, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
		version, time.Now().UnixNano(),
	); err != nil {
		return fmt.Errorf("sqlite: migration %s: %w", version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: migration %s: %w", version, err)
	}
	return nil
}
