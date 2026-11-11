package events

import (
	"time"

	"github.com/google/uuid"
)

// SessionRevokedV1 is a revoked session. The subject is the session id; its
// data is SessionRevokedEventV1.
const SessionRevokedV1 = "identity.session.revoked.v1"

// SessionRevokedEventV1 is the data of the session revoked event.
type SessionRevokedEventV1 struct {
	SessionID uuid.UUID `json:"session_id"`
	AccountID uuid.UUID `json:"account_id"`
	// ProfileID is omitted when no profile was selected.
	ProfileID uuid.UUID `json:"profile_id,omitzero"`
	DeviceID  uuid.UUID `json:"device_id"`
	RevokedAt time.Time `json:"revoked_at"`
}
