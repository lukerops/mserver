package store

import (
	"time"

	"github.com/google/uuid"
)

// Records are identified by UUIDv7.

// Account holds the credentials and administrative data. Profiles belong
// to an account. The password hash is never part of it.
type Account struct {
	ID        uuid.UUID
	Username  string
	Email     string
	IsAdmin   bool
	Disabled  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Profile is the identity used to consume media. Its handle is unique
// within the account.
type Profile struct {
	ID          uuid.UUID
	AccountID   uuid.UUID
	Handle      string
	DisplayName string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Device is a device registered by an account on authentication.
// Identifier is chosen by the client and is unique per account.
type Device struct {
	ID         uuid.UUID
	AccountID  uuid.UUID
	Identifier string
	Name       string
	Client     string
	Version    string
	CreatedAt  time.Time
	LastSeenAt time.Time
}

// Session is an authenticated session of an account on a device.
// ProfileID is uuid.Nil until a profile is selected.
type Session struct {
	ID        uuid.UUID
	AccountID uuid.UUID
	DeviceID  uuid.UUID
	ProfileID uuid.UUID
	CreatedAt time.Time
}
