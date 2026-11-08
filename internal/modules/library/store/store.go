// Package store defines the persistence of the library catalog: its
// records and the Store interface. Database backends implement Store in
// subpackages (e.g. store/sqlite).
package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// Errors returned by Store implementations, wrapped with context.
var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
)

// Store persists libraries and movies.
type Store interface {
	// WithTx runs fn in a transaction, committing if fn succeeds. Store
	// methods called with the context given to fn join the transaction.
	WithTx(ctx context.Context, fn func(ctx context.Context) error) error

	// InsertLibrary saves a new library. It returns ErrAlreadyExists if
	// one of its locations belongs to another library.
	InsertLibrary(ctx context.Context, lib Library) error
	// GetLibrary returns ErrNotFound if the library does not exist.
	GetLibrary(ctx context.Context, id uuid.UUID) (Library, error)
	// ListLibraries returns every library ordered by name and ID.
	ListLibraries(ctx context.Context) ([]Library, error)
	// LibraryOfLocation returns the library of a location, or ErrNotFound
	// if the location belongs to no library.
	LibraryOfLocation(ctx context.Context, location uuid.UUID) (Library, error)
	// DeleteLibrary removes a library with its movies, freeing its
	// locations. It returns ErrNotFound if the library does not exist.
	DeleteLibrary(ctx context.Context, id uuid.UUID) error

	// GetMovie returns ErrNotFound if the movie does not exist.
	GetMovie(ctx context.Context, id uuid.UUID) (Movie, error)
	// SaveMovie inserts or replaces a movie.
	SaveMovie(ctx context.Context, m Movie) error
	// DeleteMovie removes a movie; removing a missing movie is not an
	// error.
	DeleteMovie(ctx context.Context, id uuid.UUID) error
	// ListMovies returns a page of the movies selected by q and the total
	// number of movies selected (ignoring the offset and limit).
	ListMovies(ctx context.Context, q MovieQuery) ([]Movie, int, error)
}
