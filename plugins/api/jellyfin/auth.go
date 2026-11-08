package jellyfin

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

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
	var err error
	c.session, err = p.identity.ValidateToken(ctx, requestAuthorization(r).Token)
	if err == nil && c.session.ProfileID == uuid.Nil {
		err = identity.ErrUnauthenticated
	}
	if err == nil {
		c.account, err = p.identity.GetAccount(ctx, c.session.AccountID)
	}
	if err == nil && c.account.Disabled {
		err = identity.ErrUnauthenticated
	}
	if err == nil {
		c.profile, err = p.identity.GetProfile(ctx, c.session.ProfileID)
	}

	switch {
	case err == nil:
		return c, true
	case errors.Is(err, identity.ErrUnauthenticated), errors.Is(err, identity.ErrNotFound):
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	default:
		p.log.ErrorContext(ctx, "jellyfin: authenticate", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
	return caller{}, false
}
