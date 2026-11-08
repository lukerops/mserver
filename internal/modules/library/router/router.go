// Package router exposes the library service as a REST API. Routes are
// versioned (/v1) and registered at the root, which is shared with the
// other modules. The router only converts HTTP to service calls and back;
// the exported types are the JSON bodies of the API, suffixed with the
// version they belong to (e.g. LibraryV1).
package router

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/library/service"
)

type router struct {
	svc service.Service
	log *slog.Logger
}

// New returns the routes of the library API.
func New(svc service.Service, log *slog.Logger) *http.ServeMux {
	rt := &router{svc: svc, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/libraries", rt.createLibrary)
	mux.HandleFunc("GET /v1/libraries", rt.listLibraries)
	mux.HandleFunc("GET /v1/libraries/{id}", rt.getLibrary)
	mux.HandleFunc("DELETE /v1/libraries/{id}", rt.deleteLibrary)
	mux.HandleFunc("POST /v1/libraries/{id}/resync", rt.resyncLibrary)

	mux.HandleFunc("GET /v1/movies", rt.listMovies)
	mux.HandleFunc("GET /v1/movies/{id}", rt.getMovie)
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
	case errors.Is(err, service.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, service.ErrAlreadyExists):
		status = http.StatusConflict
	case errors.Is(err, service.ErrFailedPrecondition):
		status = http.StatusPreconditionFailed
	case errors.Is(err, service.ErrUnavailable):
		status = http.StatusServiceUnavailable
	default:
		rt.log.ErrorContext(r.Context(), "library: request failed",
			"method", r.Method, "path", r.URL.Path, "error", err)
		writeJSON(w, http.StatusInternalServerError, ErrorV1{Error: "internal error"})
		return
	}
	writeJSON(w, status, ErrorV1{Error: err.Error()})
}
