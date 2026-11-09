package jellyfin

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/infra/logging"
	identity "github.com/lukerops/mserver/internal/modules/identity/client"
	identityrouter "github.com/lukerops/mserver/internal/modules/identity/router"
)

// authorization is the content of the Authorization (or
// X-Emby-Authorization) header sent by Jellyfin clients:
//
//	MediaBrowser Client="Jellyfin Web", Device="Firefox", DeviceId="...", Version="10.10.0", Token="..."
type authorization struct {
	Client   string
	Device   string
	DeviceID string
	Version  string
	Token    string
}

// requestAuthorization reads the authorization of a request. The token
// may also come in X-Emby-Token or in the api_key query parameter.
func requestAuthorization(r *http.Request) authorization {
	header := r.Header.Get("Authorization")
	if header == "" {
		header = r.Header.Get("X-Emby-Authorization")
	}
	var a authorization
	if scheme, params, ok := strings.Cut(strings.TrimSpace(header), " "); ok &&
		(strings.EqualFold(scheme, "MediaBrowser") || strings.EqualFold(scheme, "Emby")) {
		a = parseParams(params)
	}

	for _, token := range []string{
		a.Token,
		r.Header.Get("X-Emby-Token"),
		r.Header.Get("X-MediaBrowser-Token"),
		r.URL.Query().Get("api_key"),
		r.URL.Query().Get("ApiKey"),
	} {
		if token != "" {
			a.Token = token
			break
		}
	}
	return a
}

// parseParams parses `Key="value", ...`. Values may be quoted and
// URL-encoded; commas inside quotes do not split.
func parseParams(s string) authorization {
	var (
		a      authorization
		quoted bool
		start  int
	)
	for i := 0; i <= len(s); i++ {
		if i < len(s) {
			if s[i] == '"' {
				quoted = !quoted
			}
			if s[i] != ',' || quoted {
				continue
			}
		}
		key, value, _ := strings.Cut(s[start:i], "=")
		start = i + 1

		value = strings.Trim(strings.TrimSpace(value), `"`)
		if unescaped, err := url.QueryUnescape(value); err == nil {
			value = unescaped
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "client":
			a.Client = value
		case "device":
			a.Device = value
		case "deviceid":
			a.DeviceID = value
		case "version":
			a.Version = value
		case "token":
			a.Token = value
		}
	}
	return a
}

// caller is the authenticated user of a request: a session with a
// selected profile.
type caller struct {
	session identityrouter.SessionV1
	account identityrouter.AccountV1
	profile identityrouter.ProfileV1
}

// authenticate checks the token of the request. When it fails, the error
// response is already written and ok is false.
func (p *plugin) authenticate(w http.ResponseWriter, r *http.Request) (c caller, ok bool) {
	ctx := r.Context()
	token := requestAuthorization(r).Token
	var err error
	c.session, err = p.identity.ValidateToken(ctx, token)
	if err != nil {
		switch {
		case token == "" && errors.Is(err, identity.ErrUnauthenticated):
			unauthorized(w, r, "missing token")
		case errors.Is(err, identity.ErrUnauthenticated), errors.Is(err, identity.ErrNotFound):
			unauthorized(w, r, "invalid token: "+err.Error())
		default:
			p.internalError(w, r, "validate token", err)
		}
		return caller{}, false
	}
	if c.session.ProfileID == uuid.Nil {
		unauthorized(w, r, "session has no profile selected")
		return caller{}, false
	}

	c.account, err = p.identity.GetAccount(ctx, c.session.AccountID)
	switch {
	case errors.Is(err, identity.ErrUnauthenticated), errors.Is(err, identity.ErrNotFound):
		unauthorized(w, r, "account not found")
		return caller{}, false
	case err != nil:
		p.internalError(w, r, "get account", err)
		return caller{}, false
	case c.account.Disabled:
		unauthorized(w, r, "account disabled")
		return caller{}, false
	}

	c.profile, err = p.identity.GetProfile(ctx, c.session.ProfileID)
	switch {
	case errors.Is(err, identity.ErrUnauthenticated), errors.Is(err, identity.ErrNotFound):
		unauthorized(w, r, "profile not found")
		return caller{}, false
	case err != nil:
		p.internalError(w, r, "get profile", err)
		return caller{}, false
	}
	logging.Annotate(ctx, "account_id", c.account.ID, "profile_id", c.profile.ID)
	return c, true
}
