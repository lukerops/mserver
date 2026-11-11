package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/database"
)

// CreateFirstAccount saves the first account, with its password hash, and
// its first profile, atomically. It returns ErrAlreadyExists if any account
// exists, or if the username, the email or the handle is taken.
func (s *Store) CreateFirstAccount(ctx context.Context, a *Account, passwordHash string, p *Profile) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, countAccounts).Scan(&n); err != nil {
			return fmt.Errorf("store: count accounts: %w", err)
		}
		if n > 0 {
			return fmt.Errorf("%w: setup was already done", ErrAlreadyExists)
		}

		_, err := tx.ExecContext(ctx, insertAccount,
			a.ID, a.Username, key(a.Username), a.Email, key(a.Email), passwordHash,
			a.IsAdmin, a.Disabled, nanos(a.CreatedAt), nanos(a.UpdatedAt))
		if database.IsUniqueViolation(err) {
			return fmt.Errorf("%w: username or email is taken", ErrAlreadyExists)
		}
		if err != nil {
			return fmt.Errorf("store: insert account: %w", err)
		}

		_, err = tx.ExecContext(ctx, insertProfile,
			p.ID, p.AccountID, p.Handle, p.DisplayName, nanos(p.CreatedAt), nanos(p.UpdatedAt))
		if database.IsUniqueViolation(err) {
			return fmt.Errorf("%w: profile handle %q is taken", ErrAlreadyExists, p.Handle)
		}
		if err != nil {
			return fmt.Errorf("store: insert profile: %w", err)
		}
		return nil
	})
}

// GetAccount returns ErrNotFound if the account does not exist.
func (s *Store) GetAccount(ctx context.Context, id uuid.UUID) (*Account, error) {
	accounts, err := s.queryAccounts(ctx, selectAccount, id)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, fmt.Errorf("%w: account %s", ErrNotFound, id)
	}
	return &accounts[0], nil
}

// FindAccountByLogin finds an account by username or email and returns it
// with its password hash, or ErrNotFound.
func (s *Store) FindAccountByLogin(ctx context.Context, login string) (*Account, string, error) {
	var (
		a                    Account
		hash                 string
		createdAt, updatedAt int64
	)
	err := s.db.QueryRowContext(ctx, selectAccountByLogin, key(login)).
		Scan(&a.ID, &a.Username, &a.Email, &a.IsAdmin, &a.Disabled, &createdAt, &updatedAt, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", fmt.Errorf("%w: account", ErrNotFound)
	}
	if err != nil {
		return nil, "", fmt.Errorf("store: find account: %w", err)
	}
	a.CreatedAt, a.UpdatedAt = fromNanos(createdAt), fromNanos(updatedAt)
	return &a, hash, nil
}

// ListAccounts returns every account ordered by ID.
func (s *Store) ListAccounts(ctx context.Context) ([]Account, error) {
	return s.queryAccounts(ctx, selectAccounts)
}

func (s *Store) queryAccounts(ctx context.Context, query string, args ...any) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: query accounts: %w", err)
	}
	defer rows.Close()

	var accounts []Account
	for rows.Next() {
		var (
			a                    Account
			createdAt, updatedAt int64
		)
		if err := rows.Scan(&a.ID, &a.Username, &a.Email, &a.IsAdmin, &a.Disabled, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("store: query accounts: %w", err)
		}
		a.CreatedAt, a.UpdatedAt = fromNanos(createdAt), fromNanos(updatedAt)
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}
