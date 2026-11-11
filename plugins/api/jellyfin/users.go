package jellyfin

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"connectrpc.com/connect/v2"
	"github.com/google/uuid"

	identityv1 "github.com/lukerops/mserver/gen/identity/v1"
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
	loginFailed := func(reason string, attrs ...slog.Attr) {
		attrs = append(attrs,
			slog.String("reason", reason),
			slog.String("client", a.Client),
			slog.String("device", a.Device),
		)
		if p.log.Enabled(ctx, slog.LevelDebug) {
			attrs = append(attrs, slog.String("username", body.Username))
		}
		p.log.LogAttrs(ctx, slog.LevelInfo, "jellyfin: login failed", attrs...)
		unauthorized(w, r, reason)
	}
	login, handle, ok := splitUsername(body.Username)
	if !ok {
		loginFailed("username is not <login>#<profile>")
		return
	}

	res, err := p.identity.Authenticate(ctx, &identityv1.AuthenticateRequest{
		Login:    login,
		Password: body.Pw,
		Device: &identityv1.DeviceInfo{
			Identifier: a.DeviceID,
			Name:       a.Device,
			Client:     a.Client,
			Version:    a.Version,
		},
	})
	switch code := connect.CodeOf(err); {
	case err == nil:
	case code == connect.CodeUnauthenticated:
		// The identity does not tell an unknown login from a wrong password.
		loginFailed("unknown user or wrong password")
		return
	case code == connect.CodeInvalidArgument:
		fail(w, r, http.StatusBadRequest, err.Error(), "login rejected: "+err.Error())
		return
	default:
		p.internalError(w, r, "authenticate", err)
		return
	}

	c, err := p.selectProfile(ctx, res.GetSession(), handle)
	if err != nil {
		// The account session must not outlive a failed login.
		_, revokeErr := p.identity.RevokeSession(context.WithoutCancel(ctx),
			&identityv1.RevokeSessionRequest{SessionId: res.GetSession().GetId()})
		if revokeErr != nil {
			p.log.ErrorContext(ctx, "jellyfin: revoke session",
				slog.String("session_id", res.GetSession().GetId()),
				slog.String("error", revokeErr.Error()),
			)
		}
		if isCode(err, connect.CodeNotFound) {
			loginFailed("unknown profile", slog.String("account_id", res.GetSession().GetAccountId()))
			return
		}
		p.internalError(w, r, "select profile", err)
		return
	}

	p.log.InfoContext(ctx, "jellyfin: user authenticated",
		slog.String("account_id", c.account.GetId()),
		slog.String("profile_id", c.profile.GetId()),
		slog.String("session_id", c.session.GetId()),
		slog.String("client", a.Client),
		slog.String("device", a.Device),
	)
	annotate(ctx,
		slog.String("account_id", c.account.GetId()),
		slog.String("profile_id", c.profile.GetId()),
	)

	writeJSON(w, http.StatusOK, AuthenticationResult{
		User:        p.userDto(c.account, c.profile),
		SessionInfo: p.sessionInfoDto(c.session, c.profile, a),
		AccessToken: res.GetToken(),
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
// insensitive) and binds it to the session. It fails with the NOT_FOUND
// code of the identity when the account has no such profile.
func (p *plugin) selectProfile(ctx context.Context, session *identityv1.Session, handle string) (caller, error) {
	account, err := p.identity.GetAccount(ctx, &identityv1.GetAccountRequest{Id: session.GetAccountId()})
	if err != nil {
		return caller{}, err
	}
	profiles, err := p.identity.ListProfiles(ctx, &identityv1.ListProfilesRequest{AccountId: session.GetAccountId()})
	if err != nil {
		return caller{}, err
	}
	for _, profile := range profiles.GetProfiles() {
		if !strings.EqualFold(profile.GetHandle(), handle) {
			continue
		}
		selected, err := p.identity.SelectProfile(ctx, &identityv1.SelectProfileRequest{
			SessionId: session.GetId(),
			ProfileId: profile.GetId(),
		})
		if err != nil {
			return caller{}, err
		}
		return caller{session: selected.GetSession(), account: account.GetAccount(), profile: profile}, nil
	}
	return caller{}, connect.NewError(connect.CodeNotFound, "profile not found")
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
	res, err := p.identity.GetProfile(r.Context(), &identityv1.GetProfileRequest{Id: id.String()})
	if isCode(err, connect.CodeNotFound) {
		notFound(w, r, "user not found")
		return
	}
	if err != nil {
		p.internalError(w, r, "get profile", err)
		return
	}
	if res.GetProfile().GetAccountId() != c.account.GetId() {
		notFound(w, r, "user of another account")
		return
	}
	writeJSON(w, http.StatusOK, p.userDto(c.account, res.GetProfile()))
}
