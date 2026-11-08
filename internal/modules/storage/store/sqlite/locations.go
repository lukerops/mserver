package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/storage/store"
)

const locationColumns = `id, driver, options, created_at`

func scanLocation(row rowScanner) (store.Location, error) {
	var (
		loc       store.Location
		options   string
		createdAt int64
	)
	if err := row.Scan(&loc.ID, &loc.Driver, &options, &createdAt); err != nil {
		return store.Location{}, err
	}
	if err := json.Unmarshal([]byte(options), &loc.Options); err != nil {
		return store.Location{}, err
	}
	loc.CreatedAt = fromNanos(createdAt)
	return loc, nil
}

func (s *Store) InsertLocation(ctx context.Context, loc store.Location, root string) error {
	options, err := json.Marshal(loc.Options)
	if err != nil {
		return fmt.Errorf("store: location options: %w", err)
	}

	return s.WithTx(ctx, func(ctx context.Context) error {
		if err := s.checkOverlap(ctx, root); err != nil {
			return err
		}
		if _, err := s.conn(ctx).ExecContext(ctx,
			`INSERT INTO locations (id, driver, options, root, created_at) VALUES (?, ?, ?, ?, ?)`,
			loc.ID, loc.Driver, string(options), root, nanos(loc.CreatedAt),
		); err != nil {
			return fmt.Errorf("store: insert location: %w", err)
		}
		return nil
	})
}

func (s *Store) checkOverlap(ctx context.Context, root string) error {
	rows, err := s.conn(ctx).QueryContext(ctx, `SELECT id, root FROM locations`)
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
			return fmt.Errorf("%w: directory overlaps location %s", store.ErrAlreadyExists, id)
		}
	}
	return rows.Err()
}

func isUnder(p, dir string) bool {
	return strings.HasPrefix(p, strings.TrimSuffix(dir, "/")+"/")
}

func (s *Store) GetLocation(ctx context.Context, id uuid.UUID) (store.Location, error) {
	row := s.conn(ctx).QueryRowContext(ctx, `SELECT `+locationColumns+` FROM locations WHERE id = ?`, id)
	loc, err := scanLocation(row)
	if err != nil {
		return store.Location{}, notFound(err, "location "+id.String())
	}
	return loc, nil
}

func (s *Store) ListLocations(ctx context.Context) ([]store.Location, error) {
	rows, err := s.conn(ctx).QueryContext(ctx, `SELECT `+locationColumns+` FROM locations ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list locations: %w", err)
	}
	defer rows.Close()

	var locations []store.Location
	for rows.Next() {
		loc, err := scanLocation(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list locations: %w", err)
		}
		locations = append(locations, loc)
	}
	return locations, rows.Err()
}

// DeleteLocation removes a location and, by cascade, its files.
func (s *Store) DeleteLocation(ctx context.Context, id uuid.UUID) error {
	res, err := s.conn(ctx).ExecContext(ctx, `DELETE FROM locations WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete location %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: location %s", store.ErrNotFound, id)
	}
	return nil
}
