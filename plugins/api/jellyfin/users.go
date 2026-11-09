package jellyfin

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/infra/logging"
	identity "github.com/lukerops/mserver/internal/modules/identity/client"
	identityrouter "github.com/lukerops/mserver/internal/modules/identity/router"
)

// publicUsers lists no user, so that jellyfin-web shows the manual login
// form: profiles are not public.
func (p *plugin) publicUsers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, []UserDto{})
}

// authenticateByName logs in with "<login>#<profile>": it authenticates the
// account, then selects the profile on the same session. The profile is
// required.
func (p *plugin) authenticateByName(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body AuthenticateUserByName
	if !p.decodeCredentials(w, r, &body) {
		return
	}
	a := requestAuthorization(r)
	// loginFailed logs a refused login, with the username typed only at
	// debug level. The password is never logged.
	loginFailed := func(reason string, args ...any) {
		args = append(args, "reason", reason, "client", a.Client, "device", a.Device)
		if p.log.Enabled(ctx, slog.LevelDebug) {
			args = append(args, "username", body.Username)
		}
		p.log.InfoContext(ctx, "jellyfin: login failed", args...)
		unauthorized(w, r, reason)
	}
	login, handle, ok := splitUsername(body.Username)
	if !ok {
		loginFailed("username is not <login>#<profile>")
		return
	}

	res, err := p.identity.Authenticate(ctx, identityrouter.AuthenticateRequestV1{
		Login:    login,
		Password: body.Pw,
		Device: identityrouter.DeviceInfoV1{
			Identifier: a.DeviceID,
			Name:       a.Device,
			Client:     a.Client,
			Version:    a.Version,
		},
	})
	switch {
	case errors.Is(err, identity.ErrUnauthenticated):
		// The identity does not tell an unknown login from a wrong password.
		loginFailed("unknown user or wrong password")
		return
	case errors.Is(err, identity.ErrInvalidArgument):
		fail(w, r, http.StatusBadRequest, err.Error(), "login rejected: "+err.Error())
		return
	case err != nil:
		p.internalError(w, r, "authenticate", err)
		return
	}

	c, err := p.selectProfile(ctx, res.Session, handle)
	if err != nil {
		// The account session must not outlive a failed login.
		if err := p.identity.RevokeSession(context.WithoutCancel(ctx), res.Session.ID); err != nil {
			p.log.ErrorContext(ctx, "jellyfin: revoke session", "session_id", res.Session.ID, "error", err)
		}
		if errors.Is(err, identity.ErrNotFound) {
			loginFailed("unknown profile", "account_id", res.Session.AccountID)
			return
		}
		p.internalError(w, r, "select profile", err)
		return
	}

	p.log.InfoContext(ctx, "jellyfin: user authenticated", "account_id", c.account.ID, "profile_id", c.profile.ID,
		"session_id", c.session.ID, "client", a.Client, "device", a.Device)
	logging.Annotate(ctx, "account_id", c.account.ID, "profile_id", c.profile.ID)

	writeJSON(w, http.StatusOK, AuthenticationResult{
		User:        p.userDto(c.account, c.profile),
		SessionInfo: p.sessionInfoDto(c.session, c.profile, a),
		AccessToken: res.Token,
		ServerId:    serverID,
	})
}

// splitUsername splits "<username or email>#<profile handle>" on the last
// "#".
func splitUsername(username string) (login, handle string, ok bool) {
	i := strings.LastIndex(username, "#")
	if i < 0 {
		return "", "", false
	}
	login, handle = strings.TrimSpace(username[:i]), strings.TrimSpace(username[i+1:])
	return login, handle, login != "" && handle != ""
}

// selectProfile finds the profile of the session account by handle (case
// insensitive) and binds it to the session.
func (p *plugin) selectProfile(ctx context.Context, session identityrouter.SessionV1, handle string) (caller, error) {
	account, err := p.identity.GetAccount(ctx, session.AccountID)
	if err != nil {
		return caller{}, err
	}
	profiles, err := p.identity.ListProfiles(ctx, session.AccountID)
	if err != nil {
		return caller{}, err
	}
	for _, profile := range profiles {
		if !strings.EqualFold(profile.Handle, handle) {
			continue
		}
		session, err := p.identity.SelectProfile(ctx, session.ID, profile.ID)
		if err != nil {
			return caller{}, err
		}
		return caller{session: session, account: account, profile: profile}, nil
	}
	return caller{}, identity.ErrNotFound
}

func (p *plugin) currentUser(w http.ResponseWriter, r *http.Request) {
	c, ok := p.authenticate(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, p.userDto(c.account, c.profile))
}

// userByID returns a profile of the caller account. The id may come with
// or without hyphens.
func (p *plugin) userByID(w http.ResponseWriter, r *http.Request) {
	c, ok := p.authenticate(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("userId"))
	if err != nil {
		badRequest(w, r, "invalid user id")
		return
	}
	profile, err := p.identity.GetProfile(r.Context(), id)
	if errors.Is(err, identity.ErrNotFound) {
		notFound(w, r, "user not found")
		return
	}
	if err == nil && profile.AccountID != c.account.ID {
		notFound(w, r, "user of another account")
		return
	}
	if err != nil {
		p.internalError(w, r, "get profile", err)
		return
	}
	writeJSON(w, http.StatusOK, p.userDto(c.account, profile))
}
