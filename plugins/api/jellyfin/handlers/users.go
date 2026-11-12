package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/plugins/api/jellyfin/service"
)

// publicUsers lists no user, so that jellyfin-web shows the manual login
// form: profiles are not public.
func (h *Handler) publicUsers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, []service.User{})
}

// authenticateByName logs in with "<login>#<profile>": it authenticates the
// account, then selects the profile on the same session. The profile is
// required.
func (h *Handler) authenticateByName(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body service.AuthenticateUserByName
	if !h.decodeCredentials(w, r, &body) {
		return
	}
	a := requestAuthorization(r)
	device := requestDevice(a)
	// loginFailed logs a refused login, with the username typed only at
	// debug level. The password is never logged.
	loginFailed := func(reason string, attrs ...slog.Attr) {
		attrs = append(attrs,
			slog.String("reason", reason),
			slog.String("client", a.Client),
			slog.String("device", a.Device),
		)
		if h.log.Enabled(ctx, slog.LevelDebug) {
			attrs = append(attrs, slog.String("username", body.Username))
		}
		h.log.LogAttrs(ctx, slog.LevelInfo, "jellyfin: login failed", attrs...)
		unauthorized(w, r, reason)
	}
	login, handle, ok := splitUsername(body.Username)
	if !ok {
		loginFailed("username is not <login>#<profile>")
		return
	}

	c, token, err := h.svc.Login(ctx, login, body.Pw, handle, device)
	switch {
	case err == nil:
	case errors.Is(err, service.ErrUnauthenticated):
		// The identity does not tell an unknown login from a wrong password.
		loginFailed(err.Error())
		return
	case errors.Is(err, service.ErrInvalidArgument):
		fail(w, r, http.StatusBadRequest, err.Error(), "login rejected: "+err.Error())
		return
	default:
		h.internalError(w, r, "authenticate", err)
		return
	}

	annotate(ctx,
		slog.String("account_id", c.AccountID),
		slog.String("profile_id", c.ProfileID),
	)

	writeJSON(w, http.StatusOK, service.AuthenticationResult{
		User:        h.svc.User(c),
		SessionInfo: h.svc.SessionInfo(c, device),
		AccessToken: token,
		ServerId:    service.ServerID,
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

func (h *Handler) currentUser(w http.ResponseWriter, r *http.Request) {
	c, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, h.svc.User(c))
}

// userByID returns a profile of the caller account. The id may come with
// or without hyphens.
func (h *Handler) userByID(w http.ResponseWriter, r *http.Request) {
	c, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("userId"))
	if err != nil {
		badRequest(w, r, "invalid user id")
		return
	}
	user, err := h.svc.UserByID(r.Context(), c, id)
	if err != nil {
		h.serviceError(w, r, "get profile", err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}
