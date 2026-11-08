package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/library/store"
)

func (s *Store) InsertLibrary(ctx context.Context, lib store.Library) error {
	return s.WithTx(ctx, func(ctx context.Context) error {
		for _, loc := range lib.LocationIDs {
			var other uuid.UUID
			err := s.conn(ctx).QueryRowContext(ctx,
				`SELECT library_id FROM library_locations WHERE location_id = ?`, loc,
			).Scan(&other)
			if err == nil {
				return fmt.Errorf("%w: location %s belongs to library %s", store.ErrAlreadyExists, loc, other)
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("store: check location %s: %w", loc, err)
			}
		}

		if _, err := s.conn(ctx).ExecContext(ctx,
			`INSERT INTO libraries (id, name, media_type, created_at) VALUES (?, ?, ?, ?)`,
			lib.ID, lib.Name, lib.MediaType, nanos(lib.CreatedAt),
		); err != nil {
			return fmt.Errorf("store: insert library: %w", err)
		}
		for _, loc := range lib.LocationIDs {
			if _, err := s.conn(ctx).ExecContext(ctx,
				`INSERT INTO library_locations (library_id, location_id) VALUES (?, ?)`, lib.ID, loc,
			); err != nil {
				return fmt.Errorf("store: insert library location: %w", err)
			}
		}
		return nil
	})
}

func (s *Store) GetLibrary(ctx context.Context, id uuid.UUID) (store.Library, error) {
	var (
		lib       store.Library
		createdAt int64
	)
	err := s.conn(ctx).QueryRowContext(ctx,
		`SELECT id, name, media_type, created_at FROM libraries WHERE id = ?`, id,
	).Scan(&lib.ID, &lib.Name, &lib.MediaType, &createdAt)
	if err != nil {
		return store.Library{}, notFound(err, "library "+id.String())
	}
	lib.CreatedAt = fromNanos(createdAt)
	if lib.LocationIDs, err = s.libraryLocations(ctx, id); err != nil {
		return store.Library{}, err
	}
	return lib, nil
}

func (s *Store) ListLibraries(ctx context.Context) ([]store.Library, error) {
	rows, err := s.conn(ctx).QueryContext(ctx,
		`SELECT id, name, media_type, created_at FROM libraries ORDER BY name COLLATE NOCASE, id`)
	if err != nil {
		return nil, fmt.Errorf("store: list libraries: %w", err)
	}
	var libraries []store.Library
	for rows.Next() {
		var (
			lib       store.Library
			createdAt int64
		)
		if err := rows.Scan(&lib.ID, &lib.Name, &lib.MediaType, &createdAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("store: list libraries: %w", err)
		}
		lib.CreatedAt = fromNanos(createdAt)
		libraries = append(libraries, lib)
	}
	err = rows.Err()
	rows.Close() // before the next queries: the pool has one connection
	if err != nil {
		return nil, fmt.Errorf("store: list libraries: %w", err)
	}

	for i := range libraries {
		if libraries[i].LocationIDs, err = s.libraryLocations(ctx, libraries[i].ID); err != nil {
			return nil, err
		}
	}
	return libraries, nil
}

func (s *Store) LibraryOfLocation(ctx context.Context, location uuid.UUID) (store.Library, error) {
	var id uuid.UUID
	err := s.conn(ctx).QueryRowContext(ctx,
		`SELECT library_id FROM library_locations WHERE location_id = ?`, location,
	).Scan(&id)
	if err != nil {
		return store.Library{}, notFound(err, "library of location "+location.String())
	}
	return s.GetLibrary(ctx, id)
}

func (s *Store) DeleteLibrary(ctx context.Context, id uuid.UUID) error {
	// The movies and the locations of the library are deleted in cascade.
	res, err := s.conn(ctx).ExecContext(ctx, `DELETE FROM libraries WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete library %s: %w", id, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("store: delete library %s: %w", id, err)
	} else if n == 0 {
		return fmt.Errorf("%w: library %s", store.ErrNotFound, id)
	}
	return nil
}

// libraryLocations returns the locations of a library in insertion order.
func (s *Store) libraryLocations(ctx context.Context, id uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.conn(ctx).QueryContext(ctx,
		`SELECT location_id FROM library_locations WHERE library_id = ? ORDER BY rowid`, id)
	if err != nil {
		return nil, fmt.Errorf("store: library locations: %w", err)
	}
	defer rows.Close()

	var locations []uuid.UUID
	for rows.Next() {
		var loc uuid.UUID
		if err := rows.Scan(&loc); err != nil {
			return nil, fmt.Errorf("store: library locations: %w", err)
		}
		locations = append(locations, loc)
	}
	return locations, rows.Err()
}
