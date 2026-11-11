package jellyfin

import (
	"io"
	"log/slog"
	"net/http"

	identityv1 "github.com/lukerops/mserver/gen/identity/v1"
)

// capabilities accepts the client capabilities. They are not used yet.
func (p *plugin) capabilities(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.authenticate(w, r); !ok {
		return
	}
	_, _ = io.Copy(io.Discard, http.MaxBytesReader(w, r.Body, 1<<20))
	w.WriteHeader(http.StatusNoContent)
}

// logout revokes the session of the request token.
func (p *plugin) logout(w http.ResponseWriter, r *http.Request) {
	c, ok := p.authenticate(w, r)
	if !ok {
		return
	}
	if _, err := p.identity.RevokeSession(r.Context(), &identityv1.RevokeSessionRequest{SessionId: c.session.GetId()}); err != nil {
		p.internalError(w, r, "logout", err)
		return
	}
	p.log.InfoContext(r.Context(), "jellyfin: user logged out",
		slog.String("account_id", c.account.GetId()),
		slog.String("profile_id", c.profile.GetId()),
		slog.String("session_id", c.session.GetId()),
	)
	w.WriteHeader(http.StatusNoContent)
}
