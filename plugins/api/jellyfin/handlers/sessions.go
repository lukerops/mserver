package handlers

import (
	"io"
	"net/http"
)

// capabilities accepts the client capabilities. They are not used yet.
func (h *Handler) capabilities(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticate(w, r); !ok {
		return
	}
	_, _ = io.Copy(io.Discard, http.MaxBytesReader(w, r.Body, 1<<20))
	w.WriteHeader(http.StatusNoContent)
}

// logout revokes the session of the request token.
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	c, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	if err := h.svc.Logout(r.Context(), c); err != nil {
		h.internalError(w, r, "logout", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
