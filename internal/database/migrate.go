package database

import (
	"context"
	"database/sql"
	"fmt"
	"hash/fnv"
	"io/fs"
	"slices"
	"strings"
	"time"
)

// createMigrationsTable is valid on both drivers (BIGINT has INTEGER
// affinity on SQLite).
const createMigrationsTable = `CREATE TABLE IF NOT EXISTS schema_migrations (
	version    TEXT PRIMARY KEY,
	applied_at BIGINT NOT NULL
)`

const (
	selectMigration = `SELECT 1 FROM schema_migrations WHERE version = $1`
	insertMigration = `INSERT INTO schema_migrations (version, applied_at) VALUES ($1, $2)`
)

// migrate applies, in lexical order, every "*.sql" file in migrations that
// was not applied yet. Each file runs in its own transaction, together with
// the record in schema_migrations.
//
// On Postgres the transaction first takes an advisory lock, so replicas of
// the same module starting at once apply each migration only once.
func migrate(ctx context.Context, db *sql.DB, driver Driver, module string, migrations fs.FS) error {
	if _, err := db.ExecContext(ctx, createMigrationsTable); err != nil {
		return fmt.Errorf("database: create schema_migrations: %w", err)
	}

	names, err := fs.Glob(migrations, "*.sql")
	if err != nil {
		return fmt.Errorf("database: list migrations: %w", err)
	}
	slices.Sort(names)

	for _, name := range names {
		script, err := fs.ReadFile(migrations, name)
		if err != nil {
			return fmt.Errorf("database: read migration %s: %w", name, err)
		}
		version := strings.TrimSuffix(name, ".sql")
		if err := applyMigration(ctx, db, driver, module, version, string(script)); err != nil {
			return fmt.Errorf("database: migration %s: %w", version, err)
		}
	}
	return nil
}

func applyMigration(ctx context.Context, db *sql.DB, driver Driver, module, version, script string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if driver == Postgres {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, lockKey(module)); err != nil {
			return err
		}
	}

	var applied int
	err = tx.QueryRowContext(ctx, selectMigration, version).Scan(&applied)
	switch {
	case err == nil:
		return nil // already applied
	case err != sql.ErrNoRows:
		return err
	}

	if _, err := tx.ExecContext(ctx, script); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, insertMigration, version, time.Now().UTC().UnixNano()); err != nil {
		return err
	}
	return tx.Commit()
}

// lockKey derives the advisory lock key of a module's migrations.
func lockKey(module string) int64 {
	h := fnv.New64a()
	h.Write([]byte("mserver.migrate." + module))
	return int64(h.Sum64())
}
