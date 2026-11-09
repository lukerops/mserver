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

	"github.com/lukerops/mserver/internal/infra/logging"
	"github.com/lukerops/mserver/internal/modules/library/service"
)

type router struct {
	svc service.Service
	log *slog.Logger
}

// New returns the routes of the library API. The catalog is nested under
// its library: /v1/libraries/{library_id}/movies/{movie_id} and
// /v1/libraries/{library_id}/series/{series_id}/seasons/{season_id}/episodes/{episode_id}.
// The parents in the paths of the catalog may be the Wildcard.
func New(svc service.Service, log *slog.Logger) *http.ServeMux {
	rt := &router{svc: svc, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/libraries", rt.createLibrary)
	mux.HandleFunc("GET /v1/libraries", rt.listLibraries)
	mux.HandleFunc("GET /v1/libraries/{library_id}", rt.getLibrary)
	mux.HandleFunc("DELETE /v1/libraries/{library_id}", rt.deleteLibrary)
	mux.HandleFunc("POST /v1/libraries/{library_id}/resync", rt.resyncLibrary)

	mux.HandleFunc("GET /v1/libraries/{library_id}/movies", rt.listMovies)
	mux.HandleFunc("GET /v1/libraries/{library_id}/movies/{movie_id}", rt.getMovie)

	mux.HandleFunc("GET /v1/libraries/{library_id}/series", rt.listSeries)
	mux.HandleFunc("GET /v1/libraries/{library_id}/series/{series_id}", rt.getSeries)
	mux.HandleFunc("GET /v1/libraries/{library_id}/series/{series_id}/seasons", rt.listSeasons)
	mux.HandleFunc("GET /v1/libraries/{library_id}/series/{series_id}/seasons/{season_id}", rt.getSeason)
	mux.HandleFunc("GET /v1/libraries/{library_id}/series/{series_id}/seasons/{season_id}/episodes", rt.listEpisodes)
	mux.HandleFunc("GET /v1/libraries/{library_id}/series/{series_id}/seasons/{season_id}/episodes/{episode_id}", rt.getEpisode)
	return mux
}

// decode reads a JSON body of at most 1 MiB. When it fails, the error
// response is already written.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); err != nil {
		badRequest(w, r, "invalid body: "+err.Error())
		return false
	}
	return true
}

// Wildcard is the path segment that stands for any parent, as in
// /v1/libraries/-/series/-/seasons/-/episodes (all the episodes).
const Wildcard = "-"

// pathUUID parses the path value name. When wildcard is true, "-" is
// accepted and returned as uuid.Nil. When it fails, the error response is
// already written.
func pathUUID(w http.ResponseWriter, r *http.Request, name string, wildcard bool) (uuid.UUID, bool) {
	s := r.PathValue(name)
	if wildcard && s == Wildcard {
		return uuid.Nil, true
	}
	id, err := uuid.Parse(s)
	if err != nil {
		badRequest(w, r, "invalid "+name+" "+s)
		return uuid.Nil, false
	}
	return id, true
}

// pathParents parses the parents name of the path, in order, which may
// be the Wildcard (uuid.Nil). When it fails, the error response is
// already written.
func pathParents(w http.ResponseWriter, r *http.Request, names ...string) ([]uuid.UUID, bool) {
	ids := make([]uuid.UUID, len(names))
	for i, name := range names {
		var ok bool
		if ids[i], ok = pathUUID(w, r, name, true); !ok {
			return nil, false
		}
	}
	return ids, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// badRequest answers 400 with msg, which is also the error of the access
// log line of the request.
func badRequest(w http.ResponseWriter, r *http.Request, msg string) {
	logging.Annotate(r.Context(), "error", msg)
	writeJSON(w, http.StatusBadRequest, ErrorV1{Error: msg})
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
	logging.Annotate(r.Context(), "error", err.Error())
	writeJSON(w, status, ErrorV1{Error: err.Error()})
}
