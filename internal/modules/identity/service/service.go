// Package service holds the business rules of the identity module:
// accounts, profiles, devices and sessions.
//
// Login happens in two steps: Authenticate checks the account credentials
// and opens a session without profile; SelectProfile then binds a profile
// of the account to the session, keeping its token.
//
// Events are published straight to the pubsub right after the change they
// describe is committed (there is no outbox). A failed publication is
// logged and does not undo the change: ResyncAccounts and ResyncProfiles
// republish the state, so the consumers heal.
package service

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/identity/store"
	"github.com/lukerops/mserver/internal/pubsub"
)

// Errors of the service. They are wrapped with context: use errors.Is.
var (
	ErrNotFound        = store.ErrNotFound
	ErrAlreadyExists   = store.ErrAlreadyExists
	ErrInvalidArgument = errors.New("invalid argument")
	// ErrUnauthenticated is returned for wrong credentials and unknown or
	// revoked tokens. It never tells which part was wrong.
	ErrUnauthenticated = errors.New("unauthenticated")
)

// Service is the identity business logic.
//
// The values received and returned are read-only: they may be shared with
// the caller (in-process calls do not copy them).
type Service struct {
	store *store.Store
	pub   pubsub.Publisher
	log   *slog.Logger
}

// New returns the service on st. Events are published on pub.
func New(st *store.Store, pub pubsub.Publisher, log *slog.Logger) *Service {
	return &Service{store: st, pub: pub, log: log}
}

func newID() uuid.UUID { return uuid.Must(uuid.NewV7()) }

// GetAccount returns ErrNotFound if the account does not exist.
func (s *Service) GetAccount(ctx context.Context, id uuid.UUID) (*Account, error) {
	return s.store.GetAccount(ctx, id)
}

// GetProfile returns ErrNotFound if the profile does not exist.
func (s *Service) GetProfile(ctx context.Context, id uuid.UUID) (*Profile, error) {
	return s.store.GetProfile(ctx, id)
}

// ListProfiles lists the profiles of an account ordered by creation.
func (s *Service) ListProfiles(ctx context.Context, account uuid.UUID) ([]Profile, error) {
	return s.store.ListProfiles(ctx, account)
}
