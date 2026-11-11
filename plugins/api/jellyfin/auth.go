package jellyfin

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"connectrpc.com/connect/v2"

	identityv1 "github.com/lukerops/mserver/gen/identity/v1"
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
	session *identityv1.Session
	account *identityv1.Account
	profile *identityv1.Profile
}

// authenticate checks the token of the request. When it fails, the error
// response is already written and ok is false.
func (p *plugin) authenticate(w http.ResponseWriter, r *http.Request) (c caller, ok bool) {
	ctx := r.Context()
	token := requestAuthorization(r).Token
	res, err := p.identity.ValidateToken(ctx, &identityv1.ValidateTokenRequest{Token: token})
	if err != nil {
		switch code := connect.CodeOf(err); {
		case token == "" && code == connect.CodeUnauthenticated:
			unauthorized(w, r, "missing token")
		case code == connect.CodeUnauthenticated, code == connect.CodeNotFound:
			unauthorized(w, r, "invalid token: "+err.Error())
		default:
			p.internalError(w, r, "validate token", err)
		}
		return caller{}, false
	}
	c.session = res.GetSession()
	if c.session.GetProfileId() == "" {
		unauthorized(w, r, "session has no profile selected")
		return caller{}, false
	}

	account, err := p.identity.GetAccount(ctx, &identityv1.GetAccountRequest{Id: c.session.GetAccountId()})
	switch {
	case isCode(err, connect.CodeUnauthenticated), isCode(err, connect.CodeNotFound):
		unauthorized(w, r, "account not found")
		return caller{}, false
	case err != nil:
		p.internalError(w, r, "get account", err)
		return caller{}, false
	case account.GetAccount().GetDisabled():
		unauthorized(w, r, "account disabled")
		return caller{}, false
	}
	c.account = account.GetAccount()

	profile, err := p.identity.GetProfile(ctx, &identityv1.GetProfileRequest{Id: c.session.GetProfileId()})
	switch {
	case isCode(err, connect.CodeUnauthenticated), isCode(err, connect.CodeNotFound):
		unauthorized(w, r, "profile not found")
		return caller{}, false
	case err != nil:
		p.internalError(w, r, "get profile", err)
		return caller{}, false
	}
	c.profile = profile.GetProfile()
	annotate(ctx,
		slog.String("account_id", c.account.GetId()),
		slog.String("profile_id", c.profile.GetId()),
	)
	return c, true
}
