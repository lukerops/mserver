package database

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func testMigrations() fstest.MapFS {
	return fstest.MapFS{
		"migrations/sqlite/0001_init.sql": {Data: []byte(
			`CREATE TABLE items (id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE);`)},
		"migrations/sqlite/0002_extra.sql": {Data: []byte(
			`CREATE TABLE extras (id TEXT PRIMARY KEY); CREATE INDEX extras_id ON extras (id);`)},
		"migrations/postgres/0001_init.sql": {Data: []byte(
			`CREATE TABLE items (id UUID PRIMARY KEY, name TEXT NOT NULL UNIQUE);`)},
		"migrations/postgres/0002_extra.sql": {Data: []byte(
			`CREATE TABLE extras (id UUID PRIMARY KEY); CREATE INDEX extras_id ON extras (id);`)},
	}
}

func openSQLiteTest(t *testing.T, dir string) *sql.DB {
	t.Helper()
	db, err := Open(t.Context(), Config{Driver: SQLite, DataDir: dir}, "test", testMigrations())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestParseDriver(t *testing.T) {
	tests := []struct {
		in      string
		want    Driver
		wantErr bool
	}{
		{"", SQLite, false},
		{"sqlite", SQLite, false},
		{"postgres", Postgres, false},
		{"mysql", "", true},
		{"SQLite", "", true},
	}
	for _, tt := range tests {
		got, err := ParseDriver(tt.in)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("ParseDriver(%q) = %q, %v; want %q, error=%v", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestOpenInvalidModule(t *testing.T) {
	for _, module := range []string{"", "../x", "Identity", "a b", `a"b`, "1a"} {
		if _, err := Open(t.Context(), Config{DataDir: ":memory:"}, module, nil); err == nil {
			t.Errorf("Open(module=%q): want error", module)
		}
	}
}

func TestOpenSQLiteErrors(t *testing.T) {
	if _, err := Open(t.Context(), Config{Driver: SQLite}, "test", nil); err == nil {
		t.Error("empty data dir: want error")
	}
	if _, err := Open(t.Context(), Config{Driver: "mysql"}, "test", nil); err == nil {
		t.Error("unknown driver: want error")
	}
	if _, err := Open(t.Context(), Config{Driver: Postgres}, "test", nil); err == nil {
		t.Error("empty postgres URL: want error")
	}
}

func TestOpenSQLiteCreatesModuleFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "data")
	openSQLiteTest(t, dir)
	if _, err := os.Stat(filepath.Join(dir, "test.db")); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	db := openSQLiteTest(t, dir)
	db.Close()

	// Reopening must not reapply the migrations.
	db = openSQLiteTest(t, dir)
	var n int
	if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("applied migrations = %d, want 2", n)
	}
	if _, err := db.ExecContext(t.Context(), `INSERT INTO extras (id) VALUES ($1)`, "a"); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateRollsBackFailedMigration(t *testing.T) {
	migrations := fstest.MapFS{
		"migrations/sqlite/0001_bad.sql": {Data: []byte(
			`CREATE TABLE ok (id TEXT); INSERT INTO missing VALUES (1);`)},
	}
	dir := t.TempDir()
	if _, err := Open(t.Context(), Config{DataDir: dir}, "test", migrations); err == nil {
		t.Fatal("want error")
	}

	// The failed migration must leave nothing behind.
	db, err := Open(t.Context(), Config{DataDir: dir}, "test", fstest.MapFS{
		"migrations/sqlite/0001_good.sql": {Data: []byte(`CREATE TABLE ok (id TEXT);`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
}

func TestIsUniqueViolation(t *testing.T) {
	db := openSQLiteTest(t, ":memory:")
	ctx := t.Context()
	const insert = `INSERT INTO items (id, name) VALUES ($1, $2)`

	if _, err := db.ExecContext(ctx, insert, "1", "a"); err != nil {
		t.Fatal(err)
	}
	_, err := db.ExecContext(ctx, insert, "2", "a")
	if !IsUniqueViolation(err) {
		t.Errorf("duplicate name: IsUniqueViolation(%v) = false", err)
	}
	_, err = db.ExecContext(ctx, insert, "1", "b")
	if !IsUniqueViolation(err) {
		t.Errorf("duplicate primary key: IsUniqueViolation(%v) = false", err)
	}
	if IsUniqueViolation(nil) {
		t.Error("IsUniqueViolation(nil) = true")
	}
	if IsUniqueViolation(errors.New("boom")) {
		t.Error("IsUniqueViolation(other error) = true")
	}
}
