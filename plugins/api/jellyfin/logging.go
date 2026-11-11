package jellyfin

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// maxBodySize bounds the JSON bodies the plugin reads.
const maxBodySize = 1 << 20

// maxLoggedBody bounds the part of an invalid body logged at debug level.
const maxLoggedBody = 4 << 10

// fail answers an error with body, as http.Error, and adds the reason to
// the access log of the request.
func fail(w http.ResponseWriter, r *http.Request, status int, body, reason string) {
	annotate(r.Context(), slog.String("error", reason))
	http.Error(w, body, status)
}

// badRequest answers 400 with msg, which is also the logged reason.
func badRequest(w http.ResponseWriter, r *http.Request, msg string) {
	fail(w, r, http.StatusBadRequest, msg, msg)
}

// unauthorized answers 401 "Unauthorized"; the reason is only logged.
func unauthorized(w http.ResponseWriter, r *http.Request, reason string) {
	fail(w, r, http.StatusUnauthorized, "Unauthorized", reason)
}

// notFound answers 404 as http.NotFound; the reason is only logged.
func notFound(w http.ResponseWriter, r *http.Request, reason string) {
	annotate(r.Context(), slog.String("error", reason))
	http.NotFound(w, r)
}

// unavailable answers 503 "Service Unavailable"; the reason is only logged.
func unavailable(w http.ResponseWriter, r *http.Request, reason string) {
	fail(w, r, http.StatusServiceUnavailable, "Service Unavailable", reason)
}

// internalError logs err and answers 500.
func (p *plugin) internalError(w http.ResponseWriter, r *http.Request, op string, err error) {
	p.log.ErrorContext(r.Context(), "jellyfin: "+op, slog.String("error", err.Error()))
	fail(w, r, http.StatusInternalServerError, "Internal Server Error", op+": "+err.Error())
}

// decodeBody decodes the JSON body of r into v. An empty body is accepted
// and leaves v as is. When the body is invalid, it answers 400 and returns
// false.
func (p *plugin) decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	return p.decode(w, r, v, true, true)
}

// decodeCredentials decodes a JSON body that carries credentials, like
// decodeBody, but an empty body is invalid and the body is never logged.
func (p *plugin) decodeCredentials(w http.ResponseWriter, r *http.Request, v any) bool {
	return p.decode(w, r, v, false, false)
}

func (p *plugin) decode(w http.ResponseWriter, r *http.Request, v any, allowEmpty, logBody bool) bool {
	ctx := r.Context()
	logBody = logBody && p.log.Enabled(ctx, slog.LevelDebug)
	var body io.Reader = http.MaxBytesReader(w, r.Body, maxBodySize)
	var raw prefixWriter
	if logBody {
		body = io.TeeReader(body, &raw)
	}
	err := json.NewDecoder(body).Decode(v)
	if err == nil || (allowEmpty && errors.Is(err, io.EOF)) {
		return true
	}

	attrs := []slog.Attr{
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.String("error", err.Error()),
	}
	if logBody {
		// The decoder stops at the error: read the rest of what is logged.
		_, _ = io.CopyN(&raw, body, int64(maxLoggedBody-raw.Len()))
		s := raw.String()
		if raw.truncated || raw.Len() >= maxLoggedBody {
			s += "…"
		}
		attrs = append(attrs, slog.String("body", s))
	}
	p.log.LogAttrs(ctx, slog.LevelWarn, "jellyfin: invalid request body", attrs...)
	fail(w, r, http.StatusBadRequest, "invalid body", "invalid body: "+err.Error())
	return false
}

// prefixWriter keeps the first maxLoggedBody bytes written to it.
type prefixWriter struct {
	bytes.Buffer
	truncated bool
}

func (b *prefixWriter) Write(p []byte) (int, error) {
	if room := maxLoggedBody - b.Len(); len(p) > room {
		b.truncated = true
		b.Buffer.Write(p[:max(room, 0)])
	} else {
		b.Buffer.Write(p)
	}
	return len(p), nil
}

// annotateClient adds the Jellyfin client of a request to its access log.
// The token is never logged.
func annotateClient(r *http.Request, a authorization) {
	var attrs []slog.Attr
	for _, kv := range [][2]string{
		{"client", a.Client},
		{"device", a.Device},
		{"device_id", a.DeviceID},
		{"client_version", a.Version},
	} {
		if kv[1] != "" {
			attrs = append(attrs, slog.String(kv[0], kv[1]))
		}
	}
	if attrs != nil {
		annotate(r.Context(), attrs...)
	}
}

// probeMethods are the methods tried to tell a route served with other
// methods from a missing one.
var probeMethods = []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}

// ignoredEndpoints are the endpoints the plugin does not implement on
// purpose, that clients call all the time and work without (lower case
// paths): they are logged at debug level.
var ignoredEndpoints = map[string]bool{
	// The WebSocket of the server notifications. Clients fall back to
	// polling.
	"/socket": true,
}

// checkRoute logs the requests mux has no route for: an endpoint the
// plugin does not implement (404), or a method it does not implement on
// an existing endpoint (405). path is the path as the client sent it.
func (p *plugin) checkRoute(mux *http.ServeMux, r *http.Request, path string, a authorization) {
	if _, pattern := mux.Handler(r); pattern != "" {
		return
	}
	var allowed []string
	for _, m := range probeMethods {
		if m == r.Method {
			continue
		}
		probe := r.WithContext(r.Context())
		probe.Method = m
		if _, pattern := mux.Handler(probe); pattern != "" {
			allowed = append(allowed, m)
		}
	}
	ctx := r.Context()
	if allowed != nil {
		p.log.WarnContext(ctx, "jellyfin: method not allowed",
			slog.String("method", r.Method),
			slog.String("path", path),
			slog.String("allowed", strings.Join(allowed, ", ")),
			slog.String("client", a.Client),
		)
		annotate(ctx, slog.String("error", "method not allowed"))
		return
	}
	level := slog.LevelWarn
	if ignoredEndpoints[r.URL.Path] {
		level = slog.LevelDebug
		quiet(ctx)
	}
	p.log.LogAttrs(ctx, level, "jellyfin: endpoint not implemented",
		slog.String("method", r.Method),
		slog.String("path", path),
		slog.String("client", a.Client),
	)
	annotate(ctx, slog.String("error", "endpoint not implemented"))
}
