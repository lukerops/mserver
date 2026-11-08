// Package service holds the business rules of the storage module: the VFS
// that owns every physical path and indexes files into IDs. Files are
// referred to by ID; paths only appear relative to a location.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/infra/outbox"
	"github.com/lukerops/mserver/internal/modules/storage/analyzer"
	"github.com/lukerops/mserver/internal/modules/storage/driver"
	"github.com/lukerops/mserver/internal/modules/storage/driver/local"
	"github.com/lukerops/mserver/internal/modules/storage/store"
)

// Service is the storage business logic.
type Service interface {
	// CreateLocation registers a location. It fails if the directory is
	// inside, or contains, another location.
	CreateLocation(ctx context.Context, req CreateLocationRequest) (Location, error)
	GetLocation(ctx context.Context, id uuid.UUID) (Location, error)
	ListLocations(ctx context.Context) ([]Location, error)
	// DeleteLocation removes a location and its files from the index.
	DeleteLocation(ctx context.Context, id uuid.UUID) error
	// ScanLocation synchronizes the index with the files of the location.
	// If the location is not accessible, the index is left untouched.
	ScanLocation(ctx context.Context, id uuid.UUID) (ScanResult, error)
	// ScanAll scans every location, logging failures.
	ScanAll(ctx context.Context)
	// ResyncLocation publishes again the indexed event of every file of
	// the location and the analyzed event of every analysis of their
	// current content, so that consumers can rebuild their projections.
	ResyncLocation(ctx context.Context, id uuid.UUID) error

	// AnalyzePending runs the analyzers on the files they accept that
	// were not analyzed in their current state and are unmodified for
	// StableAfter. It returns how many files were analyzed. It stops at
	// the first failure that is not about the file itself (e.g. the
	// analyzer is not installed).
	AnalyzePending(ctx context.Context) (int, error)
	// RunAnalysis runs AnalyzePending every interval and after every scan
	// that changes the index, until ctx is canceled.
	RunAnalysis(ctx context.Context, interval time.Duration)

	GetFile(ctx context.Context, id uuid.UUID) (File, error)
	// ListFiles lists the files of a location ordered by path.
	ListFiles(ctx context.Context, location uuid.UUID) ([]File, error)
	// ListSiblings lists the other indexed files in the same directory
	// (e.g. external subtitles).
	ListSiblings(ctx context.Context, id uuid.UUID) ([]Sibling, error)
	// Resolve returns the path of the content of the file in the data
	// plane of the storage ("/locations/{location}/files/{file}"), to be
	// joined to the address of the storage.
	Resolve(ctx context.Context, id uuid.UUID) (string, error)
	// OpenFile opens the content of a file of a location. The caller
	// closes it. It returns ErrNotFound if the file is not indexed in the
	// location or is missing from it, and ErrUnavailable if the location
	// is not accessible.
	OpenFile(ctx context.Context, location, id uuid.UUID) (File, driver.File, error)
}

// Errors of the service. They are wrapped with context: use errors.Is.
var (
	ErrNotFound           = store.ErrNotFound
	ErrAlreadyExists      = store.ErrAlreadyExists
	ErrInvalidArgument    = errors.New("invalid argument")
	ErrFailedPrecondition = errors.New("failed precondition")
	ErrUnavailable        = errors.New("unavailable")
)

// factories are the available drivers.
var factories = map[string]driver.Factory{
	local.Name: local.New,
}

type service struct {
	store     store.Store
	outbox    *outbox.Outbox
	log       *slog.Logger
	analyzers []analyzer.Analyzer

	// scanMu serializes the changes of the index: scans, location
	// deletions, saved analyses and resyncs.
	scanMu sync.Mutex
	// analysisKick wakes up RunAnalysis.
	analysisKick chan struct{}
}

// New returns the service on st. Events are saved in ob. Files are
// analyzed by the given analyzers.
func New(st store.Store, ob *outbox.Outbox, log *slog.Logger, analyzers ...analyzer.Analyzer) Service {
	return &service{
		store:        st,
		outbox:       ob,
		log:          log,
		analyzers:    analyzers,
		analysisKick: make(chan struct{}, 1),
	}
}

func newID() uuid.UUID { return uuid.Must(uuid.NewV7()) }

func newDriver(name string, options map[string]string) (driver.Driver, error) {
	factory, ok := factories[name]
	if !ok {
		return nil, fmt.Errorf("%w: unknown driver %q", ErrInvalidArgument, name)
	}
	d, err := factory(options)
	if errors.Is(err, driver.ErrInvalidOptions) {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	return d, err
}
