// Package events is the event API of the identity module: the types and
// the data of the events it publishes. Like the DTOs of the router
// package, every type carries the version of the event.
//
// Events are wrapped in queue.Envelope and published on the topic named
// after their type. Delivery is at-least-once: consumers must tolerate
// duplicates.
package events

import (
	"time"

	"github.com/google/uuid"
)

// Source is the CloudEvents source of the identity events.
const Source = "identity"

// Types of the identity events.
const (
	// AccountCreatedV1 is a new account (or an account republished by a
	// resync). The subject is the account id; the data is AccountEventV1.
	AccountCreatedV1 = "identity.account.created.v1"
	// AccountUpdatedV1 is a changed account. The subject is the account
	// id; the data is AccountEventV1.
	AccountUpdatedV1 = "identity.account.updated.v1"
	// AccountDeletedV1 is a deleted account. The subject is the account
	// id; the data is AccountDeletedEventV1.
	AccountDeletedV1 = "identity.account.deleted.v1"
	// ProfileCreatedV1 is a new profile (or a profile republished by a
	// resync). The subject is the profile id; the data is ProfileEventV1.
	ProfileCreatedV1 = "identity.profile.created.v1"
	// ProfileUpdatedV1 is a changed profile. The subject is the profile
	// id; the data is ProfileEventV1.
	ProfileUpdatedV1 = "identity.profile.updated.v1"
	// ProfileDeletedV1 is a deleted profile. The subject is the profile
	// id; the data is ProfileDeletedEventV1.
	ProfileDeletedV1 = "identity.profile.deleted.v1"
	// SessionRevokedV1 is a revoked session. The subject is the session
	// id; the data is SessionRevokedEventV1.
	SessionRevokedV1 = "identity.session.revoked.v1"
)

// AccountEventV1 is the data of the account created and updated events.
type AccountEventV1 struct {
	AccountID uuid.UUID `json:"account_id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	IsAdmin   bool      `json:"is_admin"`
	Disabled  bool      `json:"disabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AccountDeletedEventV1 is the data of the account deleted event.
type AccountDeletedEventV1 struct {
	AccountID uuid.UUID `json:"account_id"`
}

// ProfileEventV1 is the data of the profile created and updated events.
type ProfileEventV1 struct {
	ProfileID   uuid.UUID `json:"profile_id"`
	AccountID   uuid.UUID `json:"account_id"`
	Handle      string    `json:"handle"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ProfileDeletedEventV1 is the data of the profile deleted event.
type ProfileDeletedEventV1 struct {
	ProfileID uuid.UUID `json:"profile_id"`
	AccountID uuid.UUID `json:"account_id"`
}

// SessionRevokedEventV1 is the data of the session revoked event.
type SessionRevokedEventV1 struct {
	SessionID uuid.UUID `json:"session_id"`
	AccountID uuid.UUID `json:"account_id"`
	// ProfileID is omitted when no profile was selected.
	ProfileID uuid.UUID `json:"profile_id,omitzero"`
	DeviceID  uuid.UUID `json:"device_id"`
	RevokedAt time.Time `json:"revoked_at"`
}
