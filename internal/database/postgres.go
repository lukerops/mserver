package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// openPostgres connects to url, creates the schema named after the module
// if needed and returns a pool whose connections have it as the only entry
// of their search_path. This keeps the tables (and schema_migrations) of
// each module isolated from the others inside the same cluster.
func openPostgres(ctx context.Context, url, module string) (*sql.DB, error) {
	if url == "" {
		return nil, fmt.Errorf("database: postgres: URL is not set")
	}
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("database: postgres: parse URL: %w", err)
	}

	if err := createSchema(ctx, cfg.Copy(), module); err != nil {
		return nil, err
	}

	cfg.RuntimeParams["search_path"] = module
	db := stdlib.OpenDB(*cfg)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("database: postgres: ping: %w", err)
	}
	return db, nil
}

// createSchema runs CREATE SCHEMA IF NOT EXISTS on a throwaway connection.
// The module name was validated by Open, so it is safe to quote it into the
// statement (DDL does not accept placeholders).
func createSchema(ctx context.Context, cfg *pgx.ConnConfig, module string) error {
	db := stdlib.OpenDB(*cfg)
	defer db.Close()

	_, err := db.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS "`+module+`"`)
	// Two replicas creating the same schema at once may collide on the
	// catalog's unique index, which means the schema exists.
	if err != nil && !IsUniqueViolation(err) {
		return fmt.Errorf("database: postgres: create schema %s: %w", module, err)
	}
	return nil
}
