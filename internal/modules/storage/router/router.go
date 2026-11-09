// Package router exposes the storage service as a REST API. Routes are
// versioned (/v1) and registered at the root, which is shared with the
// other modules; the content of the files is served apart, at
// /locations/{location_id}/files/{file_id}. The router only converts HTTP
// to service calls and back; the exported types are the JSON bodies of
// the API, suffixed with the version they belong to (e.g. LocationV1).
package router

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/infra/logging"
	"github.com/lukerops/mserver/internal/modules/storage/service"
)

type router struct {
	svc service.Service
	log *slog.Logger
}

// New returns the routes of the storage API.
func New(svc service.Service, log *slog.Logger) *http.ServeMux {
	rt := &router{svc: svc, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/locations", rt.createLocation)
	mux.HandleFunc("GET /v1/locations", rt.listLocations)
	mux.HandleFunc("GET /v1/locations/{id}", rt.getLocation)
	mux.HandleFunc("DELETE /v1/locations/{id}", rt.deleteLocation)
	mux.HandleFunc("POST /v1/locations/{id}/scan", rt.scanLocation)
	mux.HandleFunc("POST /v1/locations/{id}/resync", rt.resyncLocation)
	mux.HandleFunc("GET /v1/locations/{id}/files", rt.listFiles)

	mux.HandleFunc("GET /v1/files/{id}", rt.getFile)
	mux.HandleFunc("GET /v1/files/{id}/siblings", rt.listSiblings)
	mux.HandleFunc("GET /v1/files/{id}/resolve", rt.resolve)

	// The data plane: the content of the files. It is not versioned, as
	// it is plain HTTP. GET also serves HEAD.
	mux.HandleFunc("GET /locations/{location_id}/files/{file_id}", rt.fileContent)
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

// pathID parses the {id} of the path. When it fails, the error response
// is already written.
func pathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		badRequest(w, r, "invalid id "+r.PathValue("id"))
		return uuid.Nil, false
	}
	return id, true
}

// badRequest answers 400 with msg, also added to the access log.
func badRequest(w http.ResponseWriter, r *http.Request, msg string) {
	logging.Annotate(r.Context(), "error", msg)
	writeJSON(w, http.StatusBadRequest, ErrorV1{Error: msg})
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
		rt.log.ErrorContext(r.Context(), "storage: request failed",
			"method", r.Method, "path", r.URL.Path, "error", err)
		writeJSON(w, http.StatusInternalServerError, ErrorV1{Error: "internal error"})
		return
	}
	logging.Annotate(r.Context(), "error", err.Error())
	writeJSON(w, status, ErrorV1{Error: err.Error()})
}
