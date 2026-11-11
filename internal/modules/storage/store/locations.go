package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/database"
)

// InsertLocation saves a new location. It returns ErrAlreadyExists if root
// is inside, contains or equals the root of another location.
func (s *Store) InsertLocation(ctx context.Context, loc *Location, root string) error {
	options, err := json.Marshal(loc.Options)
	if err != nil {
		return fmt.Errorf("store: location options: %w", err)
	}

	return s.withTx(ctx, func(tx *sql.Tx) error {
		if err := checkOverlap(ctx, tx, root); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, insertLocation,
			loc.ID, loc.Driver, string(options), root, nanos(loc.CreatedAt))
		if database.IsUniqueViolation(err) {
			return fmt.Errorf("%w: directory is already a location", ErrAlreadyExists)
		}
		if err != nil {
			return fmt.Errorf("store: insert location: %w", err)
		}
		return nil
	})
}

func checkOverlap(ctx context.Context, tx *sql.Tx, root string) error {
	rows, err := tx.QueryContext(ctx, selectLocationRoots)
	if err != nil {
		return fmt.Errorf("store: check overlap: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id    uuid.UUID
			other string
		)
		if err := rows.Scan(&id, &other); err != nil {
			return fmt.Errorf("store: check overlap: %w", err)
		}
		if root == other || isUnder(root, other) || isUnder(other, root) {
			return fmt.Errorf("%w: directory overlaps location %s", ErrAlreadyExists, id)
		}
	}
	return rows.Err()
}

func isUnder(p, dir string) bool {
	return strings.HasPrefix(p, strings.TrimSuffix(dir, "/")+"/")
}

// GetLocation returns ErrNotFound if the location does not exist.
func (s *Store) GetLocation(ctx context.Context, id uuid.UUID) (*Location, error) {
	locations, err := s.queryLocations(ctx, selectLocation, id)
	if err != nil {
		return nil, err
	}
	if len(locations) == 0 {
		return nil, fmt.Errorf("%w: location %s", ErrNotFound, id)
	}
	return &locations[0], nil
}

// ListLocations returns every location ordered by ID.
func (s *Store) ListLocations(ctx context.Context) ([]Location, error) {
	return s.queryLocations(ctx, selectLocations)
}

func (s *Store) queryLocations(ctx context.Context, query string, args ...any) ([]Location, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: query locations: %w", err)
	}
	defer rows.Close()

	var locations []Location
	for rows.Next() {
		var (
			loc       Location
			options   []byte
			createdAt int64
		)
		if err := rows.Scan(&loc.ID, &loc.Driver, &options, &createdAt); err != nil {
			return nil, fmt.Errorf("store: query locations: %w", err)
		}
		if err := json.Unmarshal(options, &loc.Options); err != nil {
			return nil, fmt.Errorf("store: location %s options: %w", loc.ID, err)
		}
		loc.CreatedAt = fromNanos(createdAt)
		locations = append(locations, loc)
	}
	return locations, rows.Err()
}

// DeleteLocation removes a location and, by cascade, its files. It returns
// ErrNotFound if the location does not exist.
func (s *Store) DeleteLocation(ctx context.Context, id uuid.UUID) error {
	res, err := s.db.ExecContext(ctx, deleteLocation, id)
	if err != nil {
		return fmt.Errorf("store: delete location %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: location %s", ErrNotFound, id)
	}
	return nil
}
