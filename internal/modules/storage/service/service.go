// Package service holds the business rules of the storage module: the VFS
// that owns every physical path and indexes files into IDs. Files are
// referred to by ID; paths only appear relative to a location.
//
// Events are published straight to the pubsub right after the change they
// describe is committed (there is no outbox). A failed publication is
// logged and does not undo the change: ResyncLocation republishes the
// state, so the consumers heal.
package service

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/storage/analyzer"
	"github.com/lukerops/mserver/internal/modules/storage/driver"
	"github.com/lukerops/mserver/internal/modules/storage/driver/local"
	"github.com/lukerops/mserver/internal/modules/storage/store"
	"github.com/lukerops/mserver/internal/pubsub"
)

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

// Service is the storage business logic.
//
// The values received and returned are read-only: they may be shared with
// the caller (in-process calls do not copy them).
type Service struct {
	store     *store.Store
	pub       pubsub.Publisher
	log       *slog.Logger
	analyzers []analyzer.Analyzer

	// scanMu serializes the changes of the index and the events that
	// describe them: scans, location deletions, saved analyses and
	// resyncs.
	scanMu sync.Mutex
	// analysisKick wakes up RunAnalysis.
	analysisKick chan struct{}
}

// New returns the service on st. Events are published on pub. Files are
// analyzed by the given analyzers.
func New(st *store.Store, pub pubsub.Publisher, log *slog.Logger, analyzers ...analyzer.Analyzer) *Service {
	return &Service{
		store:        st,
		pub:          pub,
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
