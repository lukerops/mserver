// Package database opens the per-module databases (SQLite or Postgres),
// applies their embedded migrations and has the helpers shared by the
// module stores.
//
// Every module has its own isolated database: a "<module>.db" file under
// Config.DataDir on SQLite, or a "<module>" schema on Postgres. The stores
// run hand-written SQL with "$1, $2, ..." placeholders, which both drivers
// understand, so the only thing that differs between the two is the set of
// migrations.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
)

// Driver selects the database backend.
type Driver string

const (
	// SQLite is the self-sufficient backend, used by the monolith.
	SQLite Driver = "sqlite"
	// Postgres is the backend of the distributed deployment.
	Postgres Driver = "postgres"
)

// ParseDriver converts the value of MSERVER_DB_DRIVER. An empty value
// selects SQLite.
func ParseDriver(s string) (Driver, error) {
	switch Driver(s) {
	case "", SQLite:
		return SQLite, nil
	case Postgres:
		return Postgres, nil
	}
	return "", fmt.Errorf("database: unknown driver %q (want %q or %q)", s, SQLite, Postgres)
}

// Config selects and configures the backend.
type Config struct {
	// Driver is the backend. Empty means SQLite.
	Driver Driver

	// DataDir is the directory of the "<module>.db" files (SQLite only).
	// The special value ":memory:" opens in-memory databases.
	DataDir string

	// PostgresURL is the connection URL (Postgres only), such as
	// postgres://user:pass@host:5432/mserver.
	PostgresURL string
}

// moduleName is what a module name must look like, since it becomes a file
// name and a Postgres schema name.
var moduleName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Open opens the database of module and applies its migrations.
//
// migrations is the module's embedded tree, laid out as
// "migrations/sqlite/*.sql" and "migrations/postgres/*.sql" (that is, the
// result of //go:embed migrations/*/*.sql). It may be nil for a module
// without migrations.
func Open(ctx context.Context, cfg Config, module string, migrations fs.FS) (*sql.DB, error) {
	if !moduleName.MatchString(module) {
		return nil, fmt.Errorf("database: invalid module name %q", module)
	}

	driver := cfg.Driver
	if driver == "" {
		driver = SQLite
	}

	var (
		db  *sql.DB
		err error
	)

	switch driver {
	case SQLite:
		db, err = openSQLite(ctx, cfg.DataDir, module)
	case Postgres:
		db, err = openPostgres(ctx, cfg.PostgresURL, module)
	default:
		return nil, fmt.Errorf("database: unknown driver %q", driver)
	}
	if err != nil {
		return nil, err
	}

	if migrations == nil {
		return db, nil
	}

	sub, err := fs.Sub(migrations, path.Join("migrations", string(driver)))
	if err != nil {
		return nil, errors.Join(fmt.Errorf("database: %s migrations: %w", driver, err), db.Close())
	}

	if err := migrate(ctx, db, driver, module, sub); err != nil {
		return nil, errors.Join(err, db.Close())
	}

	return db, nil
}
