package jellyfin

import (
	"io"
	"net/http"
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
	if err := p.identity.RevokeSession(r.Context(), c.session.ID); err != nil {
		p.log.ErrorContext(r.Context(), "jellyfin: logout", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
