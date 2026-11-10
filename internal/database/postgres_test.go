package database

import (
	"os"
	"testing"
)

// TestPostgres runs only when MSERVER_TEST_POSTGRES_URL points to a
// disposable Postgres database.
func TestPostgres(t *testing.T) {
	url := os.Getenv("MSERVER_TEST_POSTGRES_URL")
	if url == "" {
		t.Skip("MSERVER_TEST_POSTGRES_URL is not set")
	}
	ctx := t.Context()
	cfg := Config{Driver: Postgres, PostgresURL: url}

	db, err := Open(ctx, cfg, "dbtest", testMigrations())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.ExecContext(ctx, `DROP SCHEMA dbtest CASCADE`)
		db.Close()
	})

	// Reopening must find the schema and skip the applied migrations.
	again, err := Open(ctx, cfg, "dbtest", testMigrations())
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()

	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("applied migrations = %d, want 2", n)
	}

	const insert = `INSERT INTO items (id, name) VALUES ($1, $2)`
	id := "018f0000-0000-7000-8000-000000000001"
	if _, err := db.ExecContext(ctx, insert, id, "a"); err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, insert, "018f0000-0000-7000-8000-000000000002", "a")
	if !IsUniqueViolation(err) {
		t.Errorf("IsUniqueViolation(%v) = false", err)
	}
}
