package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
)

// ListDirMatches returns the matches of a directory of a location, ordered
// by ID.
func (s *Store) ListDirMatches(ctx context.Context, location uuid.UUID, dir string) ([]Match, error) {
	return s.queryMatches(ctx, selectDirMatches, location, dir)
}

// ListMatches returns a page of every match, ordered by ID. limit must be
// positive.
func (s *Store) ListMatches(ctx context.Context, limit, offset int) ([]Match, error) {
	return s.queryMatches(ctx, selectMatches, limit, offset)
}

// ApplyMatches saves the matches (inserting or replacing them by ID) and
// removes the ones with the IDs in removed, atomically.
func (s *Store) ApplyMatches(ctx context.Context, saved []Match, removed []uuid.UUID) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		for _, id := range removed {
			if _, err := tx.ExecContext(ctx, deleteMatch, id); err != nil {
				return fmt.Errorf("store: delete match %s: %w", id, err)
			}
		}
		for i := range saved {
			m := &saved[i]
			if _, err := tx.ExecContext(ctx, upsertMatch,
				m.ID, m.ItemID, m.ItemType, m.FileID, m.LocationID, m.Dir, m.Format,
				m.Language, m.Title, m.Forced, m.HearingImpaired, m.Source); err != nil {
				return fmt.Errorf("store: save match %s: %w", m.ID, err)
			}
		}
		return nil
	})
}

func (s *Store) queryMatches(ctx context.Context, query string, args ...any) ([]Match, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: query matches: %w", err)
	}
	defer rows.Close()

	var matches []Match
	for rows.Next() {
		var m Match
		if err := rows.Scan(&m.ID, &m.ItemID, &m.ItemType, &m.FileID, &m.LocationID, &m.Dir, &m.Format,
			&m.Language, &m.Title, &m.Forced, &m.HearingImpaired, &m.Source); err != nil {
			return nil, fmt.Errorf("store: query matches: %w", err)
		}
		matches = append(matches, m)
	}
	return matches, rows.Err()
}
