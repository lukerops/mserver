// Package router exposes the identity service as a REST API. Routes are
// versioned (/v1) and registered at the root, which is shared with the
// other modules. The router only converts HTTP to service calls and back;
// the exported types are the JSON bodies of the API, suffixed with the
// version they belong to (e.g. AccountV1).
package router

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/identity/service"
)

type router struct {
	svc service.Service
	log *slog.Logger
}

// New returns the routes of the identity API.
func New(svc service.Service, log *slog.Logger) *http.ServeMux {
	rt := &router{svc: svc, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/setup", rt.setup)

	mux.HandleFunc("POST /v1/sessions", rt.authenticate)
	mux.HandleFunc("POST /v1/sessions/validate", rt.validateToken)
	mux.HandleFunc("PUT /v1/sessions/{id}/profile", rt.selectProfile)
	mux.HandleFunc("DELETE /v1/sessions/{id}", rt.revokeSession)

	mux.HandleFunc("POST /v1/accounts/resync", rt.resyncAccounts)
	mux.HandleFunc("GET /v1/accounts/{id}", rt.getAccount)
	mux.HandleFunc("GET /v1/accounts/{id}/profiles", rt.listProfiles)

	mux.HandleFunc("POST /v1/profiles/resync", rt.resyncProfiles)
	mux.HandleFunc("GET /v1/profiles/{id}", rt.getProfile)
	return mux
}

// decode reads a JSON body of at most 1 MiB. When it fails, the error
// response is already written.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorV1{Error: "invalid body: " + err.Error()})
		return false
	}
	return true
}

// pathID parses the {id} of the path. When it fails, the error response
// is already written.
func pathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorV1{Error: "invalid id " + r.PathValue("id")})
		return uuid.Nil, false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError maps a service error to its status code.
func (rt *router) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var status int
	switch {
	case errors.Is(err, service.ErrInvalidArgument):
		status = http.StatusBadRequest
	case errors.Is(err, service.ErrUnauthenticated):
		status = http.StatusUnauthorized
	case errors.Is(err, service.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, service.ErrAlreadyExists):
		status = http.StatusConflict
	default:
		rt.log.ErrorContext(r.Context(), "identity: request failed",
			"method", r.Method, "path", r.URL.Path, "error", err)
		writeJSON(w, http.StatusInternalServerError, ErrorV1{Error: "internal error"})
		return
	}
	writeJSON(w, status, ErrorV1{Error: err.Error()})
}
