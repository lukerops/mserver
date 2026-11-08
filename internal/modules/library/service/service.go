// Package service holds the business rules of the library module: the
// global catalog of media, built from the files of the storage. It knows
// nothing about users or about the APIs that expose the catalog.
package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/infra/outbox"
	"github.com/lukerops/mserver/internal/infra/queue"
	"github.com/lukerops/mserver/internal/modules/library/store"
	storageclient "github.com/lukerops/mserver/internal/modules/storage/client"
)

// Service is the library business logic.
type Service interface {
	// CreateLibrary registers a library and asks the storage to resync
	// its locations, so that their files are cataloged. A location
	// belongs to at most one library. A failed resync is retried a few
	// times and then only logged: the library is created anyway.
	CreateLibrary(ctx context.Context, req CreateLibraryRequest) (Library, error)
	GetLibrary(ctx context.Context, id uuid.UUID) (Library, error)
	// ListLibraries lists the libraries ordered by name.
	ListLibraries(ctx context.Context) ([]Library, error)
	// DeleteLibrary removes a library with its movies, series, seasons and
	// episodes, publishing their removed events. Its locations can then
	// join another library.
	DeleteLibrary(ctx context.Context, id uuid.UUID) error
	// ResyncLibrary publishes again the added event of every movie, series,
	// season and episode of the library, so that consumers can rebuild
	// their projections.
	ResyncLibrary(ctx context.Context, id uuid.UUID) error

	// The catalog is a hierarchy: a library holds movies and series, a
	// series holds seasons and a season holds episodes. The getters take
	// the parents in this order; a nil parent matches any, otherwise the
	// item must belong to it (ErrNotFound if not). The lists fail with
	// ErrNotFound when a parent set in the query does not exist or does
	// not belong to its own parents.

	GetMovie(ctx context.Context, libraryID, id uuid.UUID) (Movie, error)
	ListMovies(ctx context.Context, q MovieQuery) (MoviePage, error)

	GetSeries(ctx context.Context, libraryID, id uuid.UUID) (Series, error)
	ListSeries(ctx context.Context, q SeriesQuery) (SeriesPage, error)
	GetSeason(ctx context.Context, libraryID, seriesID, id uuid.UUID) (Season, error)
	// ListSeasons lists seasons ordered by series and number.
	ListSeasons(ctx context.Context, q SeasonQuery) ([]Season, error)
	GetEpisode(ctx context.Context, libraryID, seriesID, seasonID, id uuid.UUID) (Episode, error)
	ListEpisodes(ctx context.Context, q EpisodeQuery) (EpisodePage, error)

	// HandleStorageEvent updates the catalog with an event of the storage
	// (one of StorageTopics). It is idempotent.
	HandleStorageEvent(ctx context.Context, msg queue.Message) error
}

// Errors of the service. They are wrapped with context: use errors.Is.
var (
	ErrNotFound           = store.ErrNotFound
	ErrAlreadyExists      = store.ErrAlreadyExists
	ErrInvalidArgument    = errors.New("invalid argument")
	ErrFailedPrecondition = errors.New("failed precondition")
	ErrUnavailable        = errors.New("unavailable")
)

type service struct {
	store   store.Store
	outbox  *outbox.Outbox
	storage *storageclient.Client
	log     *slog.Logger
	// resyncBackoff are the waits before each retry of a failed resync of
	// a location.
	resyncBackoff []time.Duration
}

// defaultResyncBackoff retries a failed resync twice, within the request
// that creates the library.
var defaultResyncBackoff = []time.Duration{500 * time.Millisecond, 2 * time.Second}

// New returns the service on st. Events are saved in ob. The locations of
// the libraries are checked and resynced through storage.
func New(st store.Store, ob *outbox.Outbox, storage *storageclient.Client, log *slog.Logger) Service {
	return &service{store: st, outbox: ob, storage: storage, log: log, resyncBackoff: defaultResyncBackoff}
}

func newID() uuid.UUID { return uuid.Must(uuid.NewV7()) }
