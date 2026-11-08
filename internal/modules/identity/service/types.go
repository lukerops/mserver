package service

import "github.com/lukerops/mserver/internal/modules/identity/store"

// The records are the ones of the store.
type (
	Account = store.Account
	Profile = store.Profile
	Device  = store.Device
	Session = store.Session
)

// NewProfile describes a profile to create.
type NewProfile struct {
	Handle      string
	DisplayName string
}

// SetupRequest creates the first (administrator) account and its first
// profile.
type SetupRequest struct {
	Username string
	Email    string
	Password string
	Profile  NewProfile
}

// SetupResult is the result of Setup.
type SetupResult struct {
	Account Account
	Profile Profile
}

// DeviceInfo describes the device of an authentication. Identifier is
// chosen by the client and is unique per account.
type DeviceInfo struct {
	Identifier string
	Name       string
	// Client is the application name, e.g. "Jellyfin Web".
	Client  string
	Version string
}

// AuthenticateRequest authenticates an account.
type AuthenticateRequest struct {
	// Login is the username or the email of the account.
	Login    string
	Password string
	Device   DeviceInfo
}

// AuthenticateResult is a new session and its access token. The token is
// only returned here: the identity keeps just its hash.
type AuthenticateResult struct {
	Session Session
	Token   string
}
