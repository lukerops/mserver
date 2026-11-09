package logging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

// RequestIDHeader carries the request ID between the modules.
const RequestIDHeader = "X-Request-Id"

type ctxKey struct{}

// request is the logging state of an HTTP request.
type request struct {
	id string

	mu    sync.Mutex
	attrs []slog.Attr
}

func fromContext(ctx context.Context) *request {
	req, _ := ctx.Value(ctxKey{}).(*request)
	return req
}

// RequestID returns the ID of the request of ctx, or "" outside a request.
func RequestID(ctx context.Context) string {
	if req := fromContext(ctx); req != nil {
		return req.id
	}
	return ""
}

// WithRequestID returns a context with a request ID, for work started
// outside an HTTP request that should be traced the same way.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, &request{id: id})
}

// Annotate adds attributes, as key-value pairs like slog, to the access
// log line of the request of ctx: the reason of an error response, the
// client that sent it, ... Outside a request it does nothing.
func Annotate(ctx context.Context, args ...any) {
	req := fromContext(ctx)
	if req == nil {
		return
	}
	attrs := slog.Group("", args...).Value.Group()
	req.mu.Lock()
	req.attrs = append(req.attrs, attrs...)
	req.mu.Unlock()
}

// Handler logs one line per request after it is served, at debug level
// for successes, info for client errors (4xx) and error for server errors
// (5xx). It gives each request an ID (the X-Request-Id of the request
// when present, so that the calls between modules keep the ID of the
// request that made them) and answers it in X-Request-Id. A panic in next
// is logged with its stack and answered 500.
func Handler(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := r.Header.Get(RequestIDHeader)
		if !validID(id) {
			id = newID()
		}
		req := &request{id: id}
		ctx := context.WithValue(r.Context(), ctxKey{}, req)
		r = r.WithContext(ctx)
		w.Header().Set(RequestIDHeader, id)
		rw := &responseWriter{ResponseWriter: w}

		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				log.ErrorContext(ctx, "http: panic", "method", r.Method, "path", r.URL.Path,
					"panic", fmt.Sprint(v), "stack", string(debug.Stack()))
				if !rw.wroteHeader {
					http.Error(rw, "Internal Server Error", http.StatusInternalServerError)
				} else {
					// Part of the response is sent: abort the connection so the
					// client does not take it as complete.
					defer panic(http.ErrAbortHandler)
				}
			}

			status := rw.status
			if !rw.wroteHeader {
				status = http.StatusOK
			}
			level := slog.LevelDebug
			switch {
			case status >= 500:
				level = slog.LevelError
			case status >= 400:
				level = slog.LevelInfo
			}
			if !log.Enabled(ctx, level) {
				return
			}
			attrs := []slog.Attr{
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
			}
			if q := redactQuery(r.URL.RawQuery); q != "" {
				attrs = append(attrs, slog.String("query", q))
			}
			attrs = append(attrs,
				slog.Int("status", status),
				slog.Int64("bytes", rw.bytes),
				slog.Duration("duration", time.Since(start)),
				slog.String("remote", r.RemoteAddr),
			)
			if ua := r.UserAgent(); ua != "" {
				attrs = append(attrs, slog.String("user_agent", ua))
			}
			if rg := r.Header.Get("Range"); rg != "" {
				attrs = append(attrs, slog.String("range", rg))
			}
			req.mu.Lock()
			attrs = append(attrs, req.attrs...)
			req.mu.Unlock()
			log.LogAttrs(ctx, level, "http: request", attrs...)
		}()

		next.ServeHTTP(rw, r)
	})
}

// secretParams are query parameters that carry credentials (lower case).
var secretParams = map[string]bool{"api_key": true, "apikey": true, "token": true, "x-emby-token": true}

// redactQuery returns the query with the values of credentials hidden.
func redactQuery(raw string) string {
	if raw == "" {
		return ""
	}
	q, _ := url.ParseQuery(raw)
	redacted := false
	for k := range q {
		if secretParams[strings.ToLower(k)] {
			q[k] = []string{"REDACTED"}
			redacted = true
		}
	}
	if !redacted {
		if s, err := url.QueryUnescape(raw); err == nil {
			return s
		}
		return raw
	}
	s := q.Encode()
	if u, err := url.QueryUnescape(s); err == nil {
		return u
	}
	return s
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// validID accepts IDs of up to 64 printable ASCII characters.
func validID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		if id[i] <= ' ' || id[i] > '~' {
			return false
		}
	}
	return true
}

// responseWriter records the status and the size of a response. It keeps
// the optional interfaces used to stream files: Flush (through Unwrap and
// http.ResponseController) and ReadFrom (sendfile).
type responseWriter struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool
}

func (w *responseWriter) WriteHeader(status int) {
	if !w.wroteHeader && status >= 200 {
		w.status = status
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

func (w *responseWriter) ReadFrom(r io.Reader) (int64, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	var n int64
	var err error
	if rf, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		n, err = rf.ReadFrom(r)
	} else {
		n, err = io.Copy(struct{ io.Writer }{w.ResponseWriter}, r)
	}
	w.bytes += n
	return n, err
}

func (w *responseWriter) Flush() {
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Transport forwards the request ID of the context of each request in
// X-Request-Id, so that the module that serves it logs the same ID.
type Transport struct {
	// Base does the requests; nil means http.DefaultTransport.
	Base http.RoundTripper
}

func (t Transport) RoundTrip(r *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	if id := RequestID(r.Context()); id != "" && r.Header.Get(RequestIDHeader) == "" {
		r = r.Clone(r.Context())
		r.Header.Set(RequestIDHeader, id)
	}
	return base.RoundTrip(r)
}
