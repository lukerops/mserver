package sqlite

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/identity/store"
)

const sessionColumns = `id, account_id, device_id, profile_id, created_at`

func scanSession(row rowScanner) (store.Session, error) {
	var (
		s         store.Session
		profileID uuid.NullUUID
		createdAt int64
	)
	if err := row.Scan(&s.ID, &s.AccountID, &s.DeviceID, &profileID, &createdAt); err != nil {
		return store.Session{}, err
	}
	s.ProfileID = profileID.UUID
	s.CreatedAt = fromNanos(createdAt)
	return s, nil
}

func (s *Store) InsertSession(ctx context.Context, sess store.Session, tokenHash string) error {
	if _, err := s.conn(ctx).ExecContext(ctx,
		`INSERT INTO sessions (id, token_hash, account_id, device_id, profile_id, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		sess.ID, tokenHash, sess.AccountID, sess.DeviceID, nullID(sess.ProfileID), nanos(sess.CreatedAt),
	); err != nil {
		return fmt.Errorf("store: insert session: %w", err)
	}
	return nil
}

func (s *Store) GetSession(ctx context.Context, id uuid.UUID) (store.Session, error) {
	row := s.conn(ctx).QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE id = ?`, id)
	sess, err := scanSession(row)
	if err != nil {
		return store.Session{}, notFound(err, "session "+id.String())
	}
	return sess, nil
}

func (s *Store) FindSessionByToken(ctx context.Context, tokenHash string) (store.Session, error) {
	row := s.conn(ctx).QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE token_hash = ?`, tokenHash)
	sess, err := scanSession(row)
	if err != nil {
		return store.Session{}, notFound(err, "session")
	}
	return sess, nil
}

func (s *Store) ListDeviceSessions(ctx context.Context, device uuid.UUID) ([]store.Session, error) {
	rows, err := s.conn(ctx).QueryContext(ctx,
		`SELECT `+sessionColumns+` FROM sessions WHERE device_id = ? ORDER BY id`, device)
	if err != nil {
		return nil, fmt.Errorf("store: list device sessions: %w", err)
	}
	defer rows.Close()

	var sessions []store.Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list device sessions: %w", err)
		}
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

func (s *Store) SetSessionProfile(ctx context.Context, id, profile uuid.UUID) error {
	res, err := s.conn(ctx).ExecContext(ctx,
		`UPDATE sessions SET profile_id = ? WHERE id = ?`, nullID(profile), id)
	if err != nil {
		return fmt.Errorf("store: set session profile: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: session %s", store.ErrNotFound, id)
	}
	return nil
}

func (s *Store) DeleteSession(ctx context.Context, id uuid.UUID) error {
	res, err := s.conn(ctx).ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete session: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: session %s", store.ErrNotFound, id)
	}
	return nil
}
