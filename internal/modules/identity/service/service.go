// Package service holds the business rules of the identity module:
// accounts, profiles, devices and sessions.
//
// Login happens in two steps: Authenticate checks the account credentials
// and opens a session without profile; SelectProfile then binds a profile
// of the account to the session, keeping its token.
package service

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/infra/outbox"
	"github.com/lukerops/mserver/internal/modules/identity/store"
)

// Service is the identity business logic.
type Service interface {
	// Setup creates the first account, as administrator, with its first
	// profile. It returns ErrAlreadyExists if any account exists.
	Setup(ctx context.Context, req SetupRequest) (SetupResult, error)

	// Authenticate checks the credentials of an account and opens a
	// session on the device, registering it if needed. A previous session
	// of the same device is revoked. Wrong credentials and disabled
	// accounts return ErrUnauthenticated.
	Authenticate(ctx context.Context, req AuthenticateRequest) (AuthenticateResult, error)
	// SelectProfile binds a profile of the session account to the session.
	// It returns ErrNotFound if the profile is not of that account.
	SelectProfile(ctx context.Context, session, profile uuid.UUID) (Session, error)
	// ValidateToken returns the session of an access token, or
	// ErrUnauthenticated.
	ValidateToken(ctx context.Context, token string) (Session, error)
	// RevokeSession ends a session; its token stops being valid.
	RevokeSession(ctx context.Context, session uuid.UUID) error

	GetAccount(ctx context.Context, id uuid.UUID) (Account, error)
	GetProfile(ctx context.Context, id uuid.UUID) (Profile, error)
	// ListProfiles lists the profiles of an account ordered by creation.
	ListProfiles(ctx context.Context, account uuid.UUID) ([]Profile, error)

	// ResyncAccounts and ResyncProfiles publish again the created event of
	// every account or profile, so that consumers can rebuild their
	// projections.
	ResyncAccounts(ctx context.Context) error
	ResyncProfiles(ctx context.Context) error
}

// Errors of the service. They are wrapped with context: use errors.Is.
var (
	ErrNotFound        = store.ErrNotFound
	ErrAlreadyExists   = store.ErrAlreadyExists
	ErrInvalidArgument = errors.New("invalid argument")
	// ErrUnauthenticated is returned for wrong credentials and unknown or
	// revoked tokens. It never tells which part was wrong.
	ErrUnauthenticated = errors.New("unauthenticated")
)

type service struct {
	store  store.Store
	outbox *outbox.Outbox
	log    *slog.Logger
}

// New returns the service on st. Events are saved in ob.
func New(st store.Store, ob *outbox.Outbox, log *slog.Logger) Service {
	return &service{store: st, outbox: ob, log: log}
}

func newID() uuid.UUID { return uuid.Must(uuid.NewV7()) }

func (s *service) GetAccount(ctx context.Context, id uuid.UUID) (Account, error) {
	return s.store.GetAccount(ctx, id)
}

func (s *service) GetProfile(ctx context.Context, id uuid.UUID) (Profile, error) {
	return s.store.GetProfile(ctx, id)
}

func (s *service) ListProfiles(ctx context.Context, account uuid.UUID) ([]Profile, error) {
	return s.store.ListProfiles(ctx, account)
}
