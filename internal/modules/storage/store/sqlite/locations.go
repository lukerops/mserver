package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lukerops/mserver/internal/modules/storage/api"
)

const locationColumns = `id, driver, options, created_at`

func scanLocation(row rowScanner) (api.Location, error) {
	var (
		loc            api.Location
		id, options    string
		createdAtNanos int64
	)
	if err := row.Scan(&id, &loc.Driver, &options, &createdAtNanos); err != nil {
		return api.Location{}, err
	}
	var err error
	if loc.ID, err = api.ParseLocationID(id); err != nil {
		return api.Location{}, err
	}
	if err := json.Unmarshal([]byte(options), &loc.Options); err != nil {
		return api.Location{}, err
	}
	loc.CreatedAt = time.Unix(0, createdAtNanos).UTC()
	return loc, nil
}

func (s *Store) InsertLocation(ctx context.Context, loc api.Location, root string) error {
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
			loc.ID.String(), loc.Driver, string(options), root, loc.CreatedAt.UnixNano(),
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
		var id, other string
		if err := rows.Scan(&id, &other); err != nil {
			return fmt.Errorf("store: check overlap: %w", err)
		}
		if root == other || isUnder(root, other) || isUnder(other, root) {
			return fmt.Errorf("%w: directory overlaps location %s", api.ErrAlreadyExists, id)
		}
	}
	return rows.Err()
}

func isUnder(p, dir string) bool {
	return strings.HasPrefix(p, strings.TrimSuffix(dir, "/")+"/")
}

func (s *Store) GetLocation(ctx context.Context, id api.LocationID) (api.Location, error) {
	row := s.conn(ctx).QueryRowContext(ctx, `SELECT `+locationColumns+` FROM locations WHERE id = ?`, id.String())
	loc, err := scanLocation(row)
	if err != nil {
		return api.Location{}, notFound(err, "location "+id.String())
	}
	return loc, nil
}

func (s *Store) ListLocations(ctx context.Context) ([]api.Location, error) {
	rows, err := s.conn(ctx).QueryContext(ctx, `SELECT `+locationColumns+` FROM locations ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list locations: %w", err)
	}
	defer rows.Close()

	var locations []api.Location
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
func (s *Store) DeleteLocation(ctx context.Context, id api.LocationID) error {
	res, err := s.conn(ctx).ExecContext(ctx, `DELETE FROM locations WHERE id = ?`, id.String())
	if err != nil {
		return fmt.Errorf("store: delete location %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: location %s", api.ErrNotFound, id)
	}
	return nil
}
