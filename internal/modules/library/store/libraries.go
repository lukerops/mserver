package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/database"
)

// InsertLibrary saves a new library with its locations, atomically. It
// returns ErrAlreadyExists if one of its locations belongs to another
// library.
func (s *Store) InsertLibrary(ctx context.Context, lib *Library) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		for _, loc := range lib.LocationIDs {
			var other uuid.UUID
			err := tx.QueryRowContext(ctx, selectLocationOwner, loc).Scan(&other)
			if err == nil {
				return fmt.Errorf("%w: location %s belongs to library %s", ErrAlreadyExists, loc, other)
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("store: check location %s: %w", loc, err)
			}
		}

		if _, err := tx.ExecContext(ctx, insertLibrary,
			lib.ID, lib.Name, lib.MediaType, nanos(lib.CreatedAt)); err != nil {
			return fmt.Errorf("store: insert library: %w", err)
		}
		for i, loc := range lib.LocationIDs {
			_, err := tx.ExecContext(ctx, insertLibraryLocation, lib.ID, loc, i)
			if database.IsUniqueViolation(err) {
				// Another replica took the location since the check.
				return fmt.Errorf("%w: location %s belongs to another library", ErrAlreadyExists, loc)
			}
			if err != nil {
				return fmt.Errorf("store: insert library location: %w", err)
			}
		}
		return nil
	})
}

// GetLibrary returns ErrNotFound if the library does not exist.
func (s *Store) GetLibrary(ctx context.Context, id uuid.UUID) (*Library, error) {
	libraries, err := s.queryLibraries(ctx, selectLibrary, id)
	if err != nil {
		return nil, err
	}
	if len(libraries) == 0 {
		return nil, fmt.Errorf("%w: library %s", ErrNotFound, id)
	}
	return &libraries[0], nil
}

// ListLibraries returns every library ordered by name and ID.
func (s *Store) ListLibraries(ctx context.Context) ([]Library, error) {
	return s.queryLibraries(ctx, selectLibraries+orderLibraries)
}

// LibraryOfLocation returns the library of a location, or ErrNotFound if
// the location belongs to no library.
func (s *Store) LibraryOfLocation(ctx context.Context, location uuid.UUID) (*Library, error) {
	libraries, err := s.queryLibraries(ctx, selectLibraryOf, location)
	if err != nil {
		return nil, err
	}
	if len(libraries) == 0 {
		return nil, fmt.Errorf("%w: library of location %s", ErrNotFound, location)
	}
	return &libraries[0], nil
}

// DeleteLibrary removes a library with its movies, series, seasons and
// episodes, freeing its locations. It returns ErrNotFound if the library
// does not exist.
func (s *Store) DeleteLibrary(ctx context.Context, id uuid.UUID) error {
	// The locations and the catalog of the library go in cascade.
	res, err := s.db.ExecContext(ctx, deleteLibrary, id)
	if err != nil {
		return fmt.Errorf("store: delete library %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: library %s", ErrNotFound, id)
	}
	return nil
}

// queryLibraries runs a query of selectLibraries and fills the locations of
// the libraries it returns.
func (s *Store) queryLibraries(ctx context.Context, query string, args ...any) ([]Library, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: query libraries: %w", err)
	}
	defer rows.Close()

	var libraries []Library
	for rows.Next() {
		var (
			lib       Library
			createdAt int64
		)
		if err := rows.Scan(&lib.ID, &lib.Name, &lib.MediaType, &createdAt); err != nil {
			return nil, fmt.Errorf("store: query libraries: %w", err)
		}
		lib.CreatedAt = fromNanos(createdAt)
		libraries = append(libraries, lib)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: query libraries: %w", err)
	}
	rows.Close() // before the next query: the SQLite pool has one connection
	if len(libraries) == 0 {
		return nil, nil
	}

	locations, err := s.libraryLocations(ctx)
	if err != nil {
		return nil, err
	}
	for i := range libraries {
		libraries[i].LocationIDs = locations[libraries[i].ID]
	}
	return libraries, nil
}

// libraryLocations returns the locations of every library, in the order
// they were given.
func (s *Store) libraryLocations(ctx context.Context) (map[uuid.UUID][]uuid.UUID, error) {
	rows, err := s.db.QueryContext(ctx, selectAllLocation)
	if err != nil {
		return nil, fmt.Errorf("store: library locations: %w", err)
	}
	defer rows.Close()

	locations := make(map[uuid.UUID][]uuid.UUID)
	for rows.Next() {
		var lib, loc uuid.UUID
		if err := rows.Scan(&lib, &loc); err != nil {
			return nil, fmt.Errorf("store: library locations: %w", err)
		}
		locations[lib] = append(locations[lib], loc)
	}
	return locations, rows.Err()
}
