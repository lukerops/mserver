// Package router exposes the playback service as a REST API. Routes are
// versioned (/v1) and registered at the root, which is shared with the
// other modules. The router only converts HTTP to service calls and back;
// the exported types are the JSON bodies of the API, suffixed with the
// version they belong to (e.g. PlaybackSessionV1).
package router

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/playback/service"
)

type router struct {
	svc service.Service
	log *slog.Logger
}

// New returns the routes of the playback API.
func New(svc service.Service, log *slog.Logger) *http.ServeMux {
	rt := &router{svc: svc, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/playback/sessions", rt.createSession)
	mux.HandleFunc("GET /v1/playback/sessions", rt.listSessions)
	mux.HandleFunc("GET /v1/playback/sessions/{id}", rt.getSession)
	mux.HandleFunc("POST /v1/playback/sessions/{id}/progress", rt.progress)
	mux.HandleFunc("POST /v1/playback/sessions/{id}/heartbeat", rt.heartbeat)
	mux.HandleFunc("POST /v1/playback/sessions/{id}/stop", rt.stop)
	return mux
}

func (rt *router) createSession(w http.ResponseWriter, r *http.Request) {
	var req CreatePlaybackSessionRequestV1
	if !decode(w, r, &req, false) {
		return
	}
	sess, err := rt.svc.Start(r.Context(), service.StartRequest{
		AccountID:  req.AccountID,
		ProfileID:  req.ProfileID,
		DeviceID:   req.DeviceID,
		ItemID:     req.ItemID,
		FileID:     req.FileID,
		PlayMethod: req.PlayMethod,
		PositionMs: req.PositionMs,
		Paused:     req.Paused,
	})
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toPlaybackSessionV1(sess))
}

func (rt *router) listSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := rt.svc.List(r.Context())
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	resp := PlaybackSessionListV1{Sessions: make([]PlaybackSessionV1, len(sessions))}
	for i, s := range sessions {
		resp.Sessions[i] = toPlaybackSessionV1(s)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (rt *router) getSession(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	sess, err := rt.svc.Get(r.Context(), id)
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toPlaybackSessionV1(sess))
}

func (rt *router) progress(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req ProgressRequestV1
	if !decode(w, r, &req, false) {
		return
	}
	sess, err := rt.svc.Progress(r.Context(), id, service.ProgressRequest{
		PositionMs: req.PositionMs,
		Paused:     req.Paused,
	})
	if err != nil {
		rt.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toPlaybackSessionV1(sess))
}

func (rt *router) heartbeat(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := rt.svc.Heartbeat(r.Context(), id); err != nil {
		rt.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (rt *router) stop(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req StopRequestV1
	if !decode(w, r, &req, true) {
		return
	}
	if err := rt.svc.Stop(r.Context(), id, service.StopRequest{PositionMs: req.PositionMs}); err != nil {
		rt.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// decode reads a JSON body of at most 1 MiB; an empty body is accepted
// only when optional. When it fails, the error response is already
// written.
func decode(w http.ResponseWriter, r *http.Request, v any, optional bool) bool {
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v)
	if err == nil || (optional && errors.Is(err, io.EOF)) {
		return true
	}
	writeJSON(w, http.StatusBadRequest, ErrorV1{Error: "invalid body: " + err.Error()})
	return false
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
	default:
		rt.log.ErrorContext(r.Context(), "playback: request failed",
			"method", r.Method, "path", r.URL.Path, "error", err)
		writeJSON(w, http.StatusInternalServerError, ErrorV1{Error: "internal error"})
		return
	}
	writeJSON(w, status, ErrorV1{Error: err.Error()})
}
