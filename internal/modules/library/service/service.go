// Package service holds the business rules of the library module: the
// global catalog of media, built from the files of the storage. It knows
// nothing about users or about the APIs that expose the catalog.
//
// Events are published straight to the pubsub right after the change they
// describe is committed (there is no outbox). A failed publication is
// logged and does not undo the change: ResyncLibrary republishes the
// state, so the consumers heal.
package service

import (
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/gen/storage/v1/storagev1connect"
	"github.com/lukerops/mserver/internal/modules/library/store"
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

// Service is the library business logic.
//
// The catalog is a hierarchy: a library holds movies and series, a series
// holds seasons and a season holds episodes. The getters take the parents
// in this order; a nil parent matches any, otherwise the item must belong to
// it (ErrNotFound if not). The lists fail with ErrNotFound when a parent
// set in the query does not exist or does not belong to its own parents.
//
// The values received and returned are read-only: they may be shared with
// the caller (in-process calls do not copy them).
type Service struct {
	store   *store.Store
	pub     pubsub.Publisher
	storage storagev1connect.StorageServiceClient
	log     *slog.Logger

	// mu serializes the changes of the catalog and the events that describe
	// them: the storage events, library deletions and resyncs.
	mu sync.Mutex
	// resyncBackoff are the waits before each retry of a failed resync of
	// a location.
	resyncBackoff []time.Duration
}

// defaultResyncBackoff retries a failed resync twice, within the request
// that creates the library.
var defaultResyncBackoff = []time.Duration{500 * time.Millisecond, 2 * time.Second}

// New returns the service on st. Events are published on pub. The locations
// of the libraries are checked and resynced through storage.
func New(st *store.Store, pub pubsub.Publisher, storage storagev1connect.StorageServiceClient, log *slog.Logger) *Service {
	return &Service{store: st, pub: pub, storage: storage, log: log, resyncBackoff: defaultResyncBackoff}
}

func newID() uuid.UUID { return uuid.Must(uuid.NewV7()) }
