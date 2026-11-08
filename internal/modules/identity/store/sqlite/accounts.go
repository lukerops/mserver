package sqlite

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/identity/store"
)

const accountColumns = `id, username, email, is_admin, disabled, created_at, updated_at`

func scanAccount(row rowScanner, extra ...any) (store.Account, error) {
	var (
		a                    store.Account
		createdAt, updatedAt int64
	)
	dest := append([]any{&a.ID, &a.Username, &a.Email, &a.IsAdmin, &a.Disabled, &createdAt, &updatedAt}, extra...)
	if err := row.Scan(dest...); err != nil {
		return store.Account{}, err
	}
	a.CreatedAt, a.UpdatedAt = fromNanos(createdAt), fromNanos(updatedAt)
	return a, nil
}

func (s *Store) CountAccounts(ctx context.Context) (int, error) {
	var n int
	if err := s.conn(ctx).QueryRowContext(ctx, `SELECT count(*) FROM accounts`).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count accounts: %w", err)
	}
	return n, nil
}

func (s *Store) InsertAccount(ctx context.Context, a store.Account, passwordHash string) error {
	_, err := s.conn(ctx).ExecContext(ctx,
		`INSERT INTO accounts (id, username, username_key, email, email_key, password_hash, is_admin, disabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.Username, key(a.Username), a.Email, key(a.Email), passwordHash,
		a.IsAdmin, a.Disabled, nanos(a.CreatedAt), nanos(a.UpdatedAt),
	)
	if isUniqueViolation(err) {
		return fmt.Errorf("%w: username or email is taken", store.ErrAlreadyExists)
	}
	if err != nil {
		return fmt.Errorf("store: insert account: %w", err)
	}
	return nil
}

func (s *Store) GetAccount(ctx context.Context, id uuid.UUID) (store.Account, error) {
	row := s.conn(ctx).QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE id = ?`, id)
	a, err := scanAccount(row)
	if err != nil {
		return store.Account{}, notFound(err, "account "+id.String())
	}
	return a, nil
}

func (s *Store) FindAccountByLogin(ctx context.Context, login string) (store.Account, string, error) {
	row := s.conn(ctx).QueryRowContext(ctx,
		`SELECT `+accountColumns+`, password_hash FROM accounts WHERE username_key = ?1 OR email_key = ?1`, key(login))
	var hash string
	a, err := scanAccount(row, &hash)
	if err != nil {
		return store.Account{}, "", notFound(err, "account")
	}
	return a, hash, nil
}

func (s *Store) ListAccounts(ctx context.Context) ([]store.Account, error) {
	rows, err := s.conn(ctx).QueryContext(ctx, `SELECT `+accountColumns+` FROM accounts ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list accounts: %w", err)
	}
	defer rows.Close()

	var accounts []store.Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list accounts: %w", err)
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}
