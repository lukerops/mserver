// Package service holds the business rules of the playback module: the
// playback sessions, from start to stop, and their events. It knows
// nothing about the APIs that expose them.
//
// The sessions live in a kv.Store shared by every instance of the module
// (key "sessions/{id}", JSON value), so any instance can serve any
// session. A session ends when the client stops it or when it receives no
// report for the session timeout; the reaper of every instance looks for
// idle sessions and the one that deletes the key publishes the stopped
// event. The key also gets a TTL of twice the timeout, renewed on every
// write, so that sessions never leak if no reaper runs.
//
// Writes are not transactional: concurrent reports of the same session
// race and the last write wins.
//
// Events are published straight to the pubsub right after the change they
// describe is saved (there is no outbox). A failed publication is logged
// and does not undo the change.
package service

import (
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/lukerops/mserver/internal/kv"
	"github.com/lukerops/mserver/internal/pubsub"
)

// keyPrefix is the prefix of the session keys in the store.
const keyPrefix = "sessions/"

// Errors of the service. They are wrapped with context: use errors.Is.
var (
	ErrNotFound        = errors.New("not found")
	ErrInvalidArgument = errors.New("invalid argument")
)

// Service is the playback business logic.
//
// The values received and returned are read-only: they may be shared with
// the caller (in-process calls do not copy them).
type Service struct {
	kv      kv.Store
	pub     pubsub.Publisher
	log     *slog.Logger
	timeout time.Duration
	now     func() time.Time

	// mu serializes the read-modify-write of the sessions in this
	// instance. Other instances may still race (last write wins).
	mu sync.Mutex
}

// New returns the service on st. Events are published on pub. Sessions
// without reports for timeout are stopped by the reaper.
func New(st kv.Store, pub pubsub.Publisher, timeout time.Duration, log *slog.Logger) *Service {
	return &Service{kv: st, pub: pub, log: log, timeout: timeout, now: time.Now}
}

// ttl is the expiration of the keys: a safety net, longer than the
// timeout, for when no reaper runs.
func (s *Service) ttl() time.Duration { return 2 * s.timeout }
