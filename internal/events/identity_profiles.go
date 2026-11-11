package events

import (
	"time"

	"github.com/google/uuid"
)

// Types of the profile events. The subject is the profile id.
const (
	// ProfileCreatedV1 is a new profile (or a profile republished by a
	// resync). Its data is ProfileEventV1.
	ProfileCreatedV1 = "identity.profile.created.v1"
	// ProfileUpdatedV1 is a changed profile. Its data is ProfileEventV1.
	ProfileUpdatedV1 = "identity.profile.updated.v1"
	// ProfileDeletedV1 is a deleted profile. Its data is
	// ProfileDeletedEventV1.
	ProfileDeletedV1 = "identity.profile.deleted.v1"
)

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
