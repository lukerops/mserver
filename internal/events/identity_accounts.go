package events

import (
	"time"

	"github.com/google/uuid"
)

// SourceIdentity is the CloudEvents source of the identity events.
const SourceIdentity = "identity"

// Types of the account events. The subject is the account id.
const (
	// AccountCreatedV1 is a new account (or an account republished by a
	// resync). Its data is AccountEventV1.
	AccountCreatedV1 = "identity.account.created.v1"
	// AccountUpdatedV1 is a changed account. Its data is AccountEventV1.
	AccountUpdatedV1 = "identity.account.updated.v1"
	// AccountDeletedV1 is a deleted account. Its data is
	// AccountDeletedEventV1.
	AccountDeletedV1 = "identity.account.deleted.v1"
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
