// Package service holds the business rules of the subtitles module: it
// matches the subtitle files of the storage with the movies and episodes of
// the library, and publishes the matches. It knows nothing about the APIs
// that expose them.
//
// The module keeps a projection of what it needs from the storage (the
// subtitle files) and from the library (the movies and episodes), built
// from their events, so it calls neither of them. Every change recalculates
// the matches of the directory it happens in (see reconcile.go).
//
// Events are published straight to the pubsub right after the change they
// describe is committed (there is no outbox). A failed publication is
// logged and does not undo the change: ResyncSubtitles republishes the
// state, so the consumers heal.
package service

import (
	"log/slog"
	"sync"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/subtitles/store"
	"github.com/lukerops/mserver/internal/pubsub"
)

// Service is the subtitles business logic.
type Service struct {
	store *store.Store
	pub   pubsub.Publisher
	log   *slog.Logger

	// mu serializes the changes of the projection and the events that
	// describe them: the storage and library events and the resyncs.
	mu sync.Mutex
}

// New returns the service on st. Events are published on pub.
func New(st *store.Store, pub pubsub.Publisher, log *slog.Logger) *Service {
	return &Service{store: st, pub: pub, log: log}
}

// subtitleNamespace derives the IDs of the subtitles.
var subtitleNamespace = uuid.MustParse("3b8f8f4e-6f1e-5d0b-9a52-0b6a7a3d3c11")

// subtitleID is the ID of the subtitle of a file for an item: the same pair
// always gets the same ID.
func subtitleID(item, file uuid.UUID) uuid.UUID {
	var b [32]byte
	copy(b[:16], item[:])
	copy(b[16:], file[:])
	return uuid.NewSHA1(subtitleNamespace, b[:])
}
