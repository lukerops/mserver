// Package store defines the persistence of the identity module: its
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

// Store persists accounts, profiles, devices and sessions. Usernames and
// emails are compared ignoring case.
type Store interface {
	// WithTx runs fn in a transaction, committing if fn succeeds. Store
	// methods called with the context given to fn join the transaction.
	WithTx(ctx context.Context, fn func(ctx context.Context) error) error

	CountAccounts(ctx context.Context) (int, error)
	// InsertAccount returns ErrAlreadyExists if the username or the email
	// is taken.
	InsertAccount(ctx context.Context, a Account, passwordHash string) error
	// GetAccount returns ErrNotFound if the account does not exist.
	GetAccount(ctx context.Context, id uuid.UUID) (Account, error)
	// FindAccountByLogin finds an account by username or email and returns
	// it with its password hash, or ErrNotFound.
	FindAccountByLogin(ctx context.Context, login string) (Account, string, error)
	// ListAccounts returns every account ordered by ID.
	ListAccounts(ctx context.Context) ([]Account, error)

	// InsertProfile returns ErrAlreadyExists if the handle is taken in the
	// account.
	InsertProfile(ctx context.Context, p Profile) error
	// GetProfile returns ErrNotFound if the profile does not exist.
	GetProfile(ctx context.Context, id uuid.UUID) (Profile, error)
	// ListProfiles returns the profiles of an account ordered by ID.
	ListProfiles(ctx context.Context, account uuid.UUID) ([]Profile, error)
	// ListAllProfiles returns every profile ordered by ID.
	ListAllProfiles(ctx context.Context) ([]Profile, error)

	// UpsertDevice saves a device identified by its account and
	// identifier. An existing device keeps its ID and creation time, which
	// are returned.
	UpsertDevice(ctx context.Context, d Device) (Device, error)

	InsertSession(ctx context.Context, s Session, tokenHash string) error
	// GetSession returns ErrNotFound if the session does not exist.
	GetSession(ctx context.Context, id uuid.UUID) (Session, error)
	// FindSessionByToken returns ErrNotFound for an unknown token hash.
	FindSessionByToken(ctx context.Context, tokenHash string) (Session, error)
	// ListDeviceSessions returns the sessions of a device.
	ListDeviceSessions(ctx context.Context, device uuid.UUID) ([]Session, error)
	// SetSessionProfile returns ErrNotFound if the session does not exist.
	SetSessionProfile(ctx context.Context, id, profile uuid.UUID) error
	// DeleteSession returns ErrNotFound if the session does not exist.
	DeleteSession(ctx context.Context, id uuid.UUID) error
}
