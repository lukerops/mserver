package service

import (
	"time"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/infra/queue"
	"github.com/lukerops/mserver/internal/modules/identity/store"
)

// Source is the CloudEvents source of the identity events.
const Source = "identity"

// Versioned types of the identity events. Events are published on a topic
// with the same name. Delivery is at-least-once: consumers must tolerate
// duplicates.
const (
	EventAccountCreated = "identity.account.created.v1"
	EventAccountUpdated = "identity.account.updated.v1"
	EventAccountDeleted = "identity.account.deleted.v1"
	EventProfileCreated = "identity.profile.created.v1"
	EventProfileUpdated = "identity.profile.updated.v1"
	EventProfileDeleted = "identity.profile.deleted.v1"
	EventSessionRevoked = "identity.session.revoked.v1"
)

// AccountEvent is the data of the account created and updated events. The
// subject is the account id.
type AccountEvent struct {
	AccountID uuid.UUID `json:"account_id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	IsAdmin   bool      `json:"is_admin"`
	Disabled  bool      `json:"disabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AccountDeletedEvent is the data of the account deleted event. The
// subject is the account id.
type AccountDeletedEvent struct {
	AccountID uuid.UUID `json:"account_id"`
}

// ProfileEvent is the data of the profile created and updated events. The
// subject is the profile id.
type ProfileEvent struct {
	ProfileID   uuid.UUID `json:"profile_id"`
	AccountID   uuid.UUID `json:"account_id"`
	Handle      string    `json:"handle"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ProfileDeletedEvent is the data of the profile deleted event. The
// subject is the profile id.
type ProfileDeletedEvent struct {
	ProfileID uuid.UUID `json:"profile_id"`
	AccountID uuid.UUID `json:"account_id"`
}

// SessionRevokedEvent is the data of the session revoked event. The
// subject is the session id.
type SessionRevokedEvent struct {
	SessionID uuid.UUID `json:"session_id"`
	AccountID uuid.UUID `json:"account_id"`
	// ProfileID is omitted when no profile was selected.
	ProfileID uuid.UUID `json:"profile_id,omitzero"`
	DeviceID  uuid.UUID `json:"device_id"`
	RevokedAt time.Time `json:"revoked_at"`
}

func accountEvent(typ string, a store.Account) queue.Envelope[AccountEvent] {
	return queue.NewEnvelope(typ, Source, a.ID.String(), AccountEvent{
		AccountID: a.ID,
		Username:  a.Username,
		Email:     a.Email,
		IsAdmin:   a.IsAdmin,
		Disabled:  a.Disabled,
		CreatedAt: a.CreatedAt,
		UpdatedAt: a.UpdatedAt,
	})
}

func profileEvent(typ string, p store.Profile) queue.Envelope[ProfileEvent] {
	return queue.NewEnvelope(typ, Source, p.ID.String(), ProfileEvent{
		ProfileID:   p.ID,
		AccountID:   p.AccountID,
		Handle:      p.Handle,
		DisplayName: p.DisplayName,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	})
}

func sessionRevokedEvent(s store.Session, at time.Time) queue.Envelope[SessionRevokedEvent] {
	return queue.NewEnvelope(EventSessionRevoked, Source, s.ID.String(), SessionRevokedEvent{
		SessionID: s.ID,
		AccountID: s.AccountID,
		ProfileID: s.ProfileID,
		DeviceID:  s.DeviceID,
		RevokedAt: at,
	})
}
