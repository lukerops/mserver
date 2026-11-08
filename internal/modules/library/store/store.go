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

// Store persists libraries, movies, series, seasons and episodes.
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
	// DeleteLibrary removes a library with its movies, series, seasons and
	// episodes, freeing its locations. It returns ErrNotFound if the
	// library does not exist.
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

	// GetSeries returns ErrNotFound if the series does not exist.
	GetSeries(ctx context.Context, id uuid.UUID) (Series, error)
	// FindSeries returns the series of a library with a key and year, or
	// ErrNotFound.
	FindSeries(ctx context.Context, library uuid.UUID, key string, year int) (Series, error)
	// InsertSeries saves a new series.
	InsertSeries(ctx context.Context, s Series) error
	// DeleteSeries removes a series with its seasons and episodes;
	// removing a missing series is not an error.
	DeleteSeries(ctx context.Context, id uuid.UUID) error
	// ListSeries returns a page of the series selected by q and the total
	// number of series selected (ignoring the offset and limit).
	ListSeries(ctx context.Context, q SeriesQuery) ([]Series, int, error)

	// GetSeason returns ErrNotFound if the season does not exist.
	GetSeason(ctx context.Context, id uuid.UUID) (Season, error)
	// FindSeason returns the season of a series with a number, or
	// ErrNotFound.
	FindSeason(ctx context.Context, series uuid.UUID, number int) (Season, error)
	// InsertSeason saves a new season.
	InsertSeason(ctx context.Context, s Season) error
	// DeleteSeason removes a season with its episodes; removing a missing
	// season is not an error.
	DeleteSeason(ctx context.Context, id uuid.UUID) error
	// ListSeasons returns the seasons selected by q, ordered by series and
	// number.
	ListSeasons(ctx context.Context, q SeasonQuery) ([]Season, error)

	// GetEpisode returns ErrNotFound if the episode does not exist.
	GetEpisode(ctx context.Context, id uuid.UUID) (Episode, error)
	// SaveEpisode inserts or replaces an episode. Its series and season
	// must exist.
	SaveEpisode(ctx context.Context, e Episode) error
	// DeleteEpisode removes an episode; removing a missing episode is not
	// an error.
	DeleteEpisode(ctx context.Context, id uuid.UUID) error
	// ListEpisodes returns a page of the episodes selected by q and the
	// total number of episodes selected (ignoring the offset and limit).
	ListEpisodes(ctx context.Context, q EpisodeQuery) ([]Episode, int, error)
}
