package sqlite

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/identity/store"
)

const profileColumns = `id, account_id, handle, display_name, created_at, updated_at`

func scanProfile(row rowScanner) (store.Profile, error) {
	var (
		p                    store.Profile
		createdAt, updatedAt int64
	)
	if err := row.Scan(&p.ID, &p.AccountID, &p.Handle, &p.DisplayName, &createdAt, &updatedAt); err != nil {
		return store.Profile{}, err
	}
	p.CreatedAt, p.UpdatedAt = fromNanos(createdAt), fromNanos(updatedAt)
	return p, nil
}

func (s *Store) InsertProfile(ctx context.Context, p store.Profile) error {
	_, err := s.conn(ctx).ExecContext(ctx,
		`INSERT INTO profiles (`+profileColumns+`) VALUES (?, ?, ?, ?, ?, ?)`,
		p.ID, p.AccountID, p.Handle, p.DisplayName, nanos(p.CreatedAt), nanos(p.UpdatedAt),
	)
	if isUniqueViolation(err) {
		return fmt.Errorf("%w: profile handle %q is taken", store.ErrAlreadyExists, p.Handle)
	}
	if err != nil {
		return fmt.Errorf("store: insert profile: %w", err)
	}
	return nil
}

func (s *Store) GetProfile(ctx context.Context, id uuid.UUID) (store.Profile, error) {
	row := s.conn(ctx).QueryRowContext(ctx, `SELECT `+profileColumns+` FROM profiles WHERE id = ?`, id)
	p, err := scanProfile(row)
	if err != nil {
		return store.Profile{}, notFound(err, "profile "+id.String())
	}
	return p, nil
}

func (s *Store) ListProfiles(ctx context.Context, account uuid.UUID) ([]store.Profile, error) {
	return s.listProfiles(ctx, `SELECT `+profileColumns+` FROM profiles WHERE account_id = ? ORDER BY id`, account)
}

func (s *Store) ListAllProfiles(ctx context.Context) ([]store.Profile, error) {
	return s.listProfiles(ctx, `SELECT `+profileColumns+` FROM profiles ORDER BY id`)
}

func (s *Store) listProfiles(ctx context.Context, query string, args ...any) ([]store.Profile, error) {
	rows, err := s.conn(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list profiles: %w", err)
	}
	defer rows.Close()

	var profiles []store.Profile
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list profiles: %w", err)
		}
		profiles = append(profiles, p)
	}
	return profiles, rows.Err()
}
