package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// GetProfile returns ErrNotFound if the profile does not exist.
func (s *Store) GetProfile(ctx context.Context, id uuid.UUID) (*Profile, error) {
	profiles, err := s.queryProfiles(ctx, selectProfile, id)
	if err != nil {
		return nil, err
	}
	if len(profiles) == 0 {
		return nil, fmt.Errorf("%w: profile %s", ErrNotFound, id)
	}
	return &profiles[0], nil
}

// ListProfiles returns the profiles of an account ordered by ID.
func (s *Store) ListProfiles(ctx context.Context, account uuid.UUID) ([]Profile, error) {
	return s.queryProfiles(ctx, selectAccountProfiles, account)
}

// ListAllProfiles returns every profile ordered by ID.
func (s *Store) ListAllProfiles(ctx context.Context) ([]Profile, error) {
	return s.queryProfiles(ctx, selectProfiles)
}

func (s *Store) queryProfiles(ctx context.Context, query string, args ...any) ([]Profile, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: query profiles: %w", err)
	}
	defer rows.Close()

	var profiles []Profile
	for rows.Next() {
		var (
			p                    Profile
			createdAt, updatedAt int64
		)
		if err := rows.Scan(&p.ID, &p.AccountID, &p.Handle, &p.DisplayName, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("store: query profiles: %w", err)
		}
		p.CreatedAt, p.UpdatedAt = fromNanos(createdAt), fromNanos(updatedAt)
		profiles = append(profiles, p)
	}
	return profiles, rows.Err()
}
