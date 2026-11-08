package router

import (
	"time"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/identity/service"
)

// ErrorV1 is the body of every error response.
type ErrorV1 struct {
	Error string `json:"error"`
}

type AccountV1 struct {
	ID        uuid.UUID `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	IsAdmin   bool      `json:"is_admin"`
	Disabled  bool      `json:"disabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ProfileV1 struct {
	ID          uuid.UUID `json:"id"`
	AccountID   uuid.UUID `json:"account_id"`
	Handle      string    `json:"handle"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ProfileListV1 struct {
	Profiles []ProfileV1 `json:"profiles"`
}

type SessionV1 struct {
	ID        uuid.UUID `json:"id"`
	AccountID uuid.UUID `json:"account_id"`
	DeviceID  uuid.UUID `json:"device_id"`
	// ProfileID is omitted until a profile is selected.
	ProfileID uuid.UUID `json:"profile_id,omitzero"`
	CreatedAt time.Time `json:"created_at"`
}

type NewProfileV1 struct {
	Handle      string `json:"handle"`
	DisplayName string `json:"display_name"`
}

type SetupRequestV1 struct {
	Username string       `json:"username"`
	Email    string       `json:"email"`
	Password string       `json:"password"`
	Profile  NewProfileV1 `json:"profile"`
}

type SetupResponseV1 struct {
	Account AccountV1 `json:"account"`
	Profile ProfileV1 `json:"profile"`
}

type DeviceInfoV1 struct {
	Identifier string `json:"identifier"`
	Name       string `json:"name"`
	Client     string `json:"client"`
	Version    string `json:"version"`
}

type AuthenticateRequestV1 struct {
	// Login is the username or the email of the account.
	Login    string       `json:"login"`
	Password string       `json:"password"`
	Device   DeviceInfoV1 `json:"device"`
}

type AuthenticateResponseV1 struct {
	Session SessionV1 `json:"session"`
	Token   string    `json:"token"`
}

type ValidateTokenRequestV1 struct {
	Token string `json:"token"`
}

type SelectProfileRequestV1 struct {
	ProfileID uuid.UUID `json:"profile_id"`
}

func toAccountV1(a service.Account) AccountV1 {
	return AccountV1{
		ID:        a.ID,
		Username:  a.Username,
		Email:     a.Email,
		IsAdmin:   a.IsAdmin,
		Disabled:  a.Disabled,
		CreatedAt: a.CreatedAt,
		UpdatedAt: a.UpdatedAt,
	}
}

func toProfileV1(p service.Profile) ProfileV1 {
	return ProfileV1{
		ID:          p.ID,
		AccountID:   p.AccountID,
		Handle:      p.Handle,
		DisplayName: p.DisplayName,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

func toSessionV1(s service.Session) SessionV1 {
	return SessionV1{
		ID:        s.ID,
		AccountID: s.AccountID,
		DeviceID:  s.DeviceID,
		ProfileID: s.ProfileID,
		CreatedAt: s.CreatedAt,
	}
}
