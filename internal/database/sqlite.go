package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// memoryDir is the DataDir that selects in-memory databases.
const memoryDir = ":memory:"

// openSQLite opens (creating if needed) <dataDir>/<module>.db.
//
// The pool is limited to a single connection: SQLite serializes writes
// anyway, and this avoids SQLITE_BUSY errors and gives every use the same
// in-memory database. Callers must not run a query on the *sql.DB while
// holding an open transaction or unconsumed rows; inside a transaction,
// use ConnFrom.
func openSQLite(ctx context.Context, dataDir, module string) (*sql.DB, error) {
	file := memoryDir
	if dataDir != memoryDir {
		if dataDir == "" {
			return nil, fmt.Errorf("database: sqlite: data directory is not set")
		}
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
			return nil, fmt.Errorf("database: sqlite: create data directory: %w", err)
		}
		file = filepath.Join(dataDir, module+".db")
	}

	dsn := "file:" + file +
		"?_pragma=foreign_keys(1)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=busy_timeout(5000)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("database: sqlite: open %s: %w", file, err)
	}
	db.SetMaxOpenConns(1)
	db.SetConnMaxIdleTime(0)
	db.SetConnMaxLifetime(0)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("database: sqlite: ping %s: %w", file, err)
	}
	return db, nil
}
