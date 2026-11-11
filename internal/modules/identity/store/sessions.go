package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
)

// OpenSession registers the device (or updates the existing one with the
// same account and identifier, which keeps its ID and creation time), revokes
// the sessions it had and saves the new one with the hash of its token, all
// atomically. A device keeps a single session.
//
// On return, d holds the device as stored and sess.DeviceID is its ID. The
// revoked sessions are returned.
func (s *Store) OpenSession(ctx context.Context, d *Device, sess *Session, tokenHash string) ([]Session, error) {
	var revoked []Session
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var createdAt int64
		err := tx.QueryRowContext(ctx, upsertDevice,
			d.ID, d.AccountID, d.Identifier, d.Name, d.Client, d.Version,
			nanos(d.CreatedAt), nanos(d.LastSeenAt)).Scan(&d.ID, &createdAt)
		if err != nil {
			return fmt.Errorf("store: upsert device: %w", err)
		}
		d.CreatedAt = fromNanos(createdAt)
		sess.DeviceID = d.ID

		rows, err := tx.QueryContext(ctx, deleteDeviceSessions, d.ID)
		if err != nil {
			return fmt.Errorf("store: revoke device sessions: %w", err)
		}
		if revoked, err = scanSessions(rows); err != nil {
			return err
		}

		_, err = tx.ExecContext(ctx, insertSession,
			sess.ID, tokenHash, sess.AccountID, sess.DeviceID, nullID(sess.ProfileID), nanos(sess.CreatedAt))
		if err != nil {
			return fmt.Errorf("store: insert session: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return revoked, nil
}

// FindSessionByToken returns ErrNotFound for an unknown token hash.
func (s *Store) FindSessionByToken(ctx context.Context, tokenHash string) (*Session, error) {
	rows, err := s.db.QueryContext(ctx, selectSessionByToken, tokenHash)
	if err != nil {
		return nil, fmt.Errorf("store: find session: %w", err)
	}
	return oneSession(rows, "session")
}

// SelectSessionProfile binds a profile to a session and returns the updated
// session. It returns ErrNotFound if the session does not exist or the
// profile does not exist or is not of the session account.
func (s *Store) SelectSessionProfile(ctx context.Context, id, profile uuid.UUID) (*Session, error) {
	rows, err := s.db.QueryContext(ctx, updateSessionProfile, profile, id)
	if err != nil {
		return nil, fmt.Errorf("store: select session profile: %w", err)
	}
	return oneSession(rows, fmt.Sprintf("session %s or profile %s", id, profile))
}

// DeleteSession removes a session and returns it. It returns ErrNotFound if
// the session does not exist.
func (s *Store) DeleteSession(ctx context.Context, id uuid.UUID) (*Session, error) {
	rows, err := s.db.QueryContext(ctx, deleteSession, id)
	if err != nil {
		return nil, fmt.Errorf("store: delete session: %w", err)
	}
	return oneSession(rows, "session "+id.String())
}

// oneSession returns the only session in rows, or ErrNotFound. It closes
// rows.
func oneSession(rows *sql.Rows, what string) (*Session, error) {
	sessions, err := scanSessions(rows)
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, what)
	}
	return &sessions[0], nil
}

// scanSessions reads every session in rows and closes it.
func scanSessions(rows *sql.Rows) ([]Session, error) {
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var (
			sess      Session
			profileID uuid.NullUUID
			createdAt int64
		)
		if err := rows.Scan(&sess.ID, &sess.AccountID, &sess.DeviceID, &profileID, &createdAt); err != nil {
			return nil, fmt.Errorf("store: scan sessions: %w", err)
		}
		sess.ProfileID = profileID.UUID
		sess.CreatedAt = fromNanos(createdAt)
		sessions = append(sessions, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: scan sessions: %w", err)
	}
	return sessions, nil
}
