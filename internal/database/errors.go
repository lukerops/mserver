package database

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// pgUniqueViolation is the Postgres SQLSTATE of a unique violation.
const pgUniqueViolation = "23505"

// IsUniqueViolation reports whether err is a unique (or primary key)
// constraint violation, on either SQLite ("UNIQUE constraint failed") or
// Postgres (SQLSTATE 23505).
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == pgUniqueViolation
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
