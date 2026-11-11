package jellyfin

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

// requestIDHeader carries the ID of a request. The plugin answers it, and
// takes it from the request when the client sends one.
const requestIDHeader = "X-Request-Id"

type requestCtxKey struct{}

// request is the logging state of an HTTP request.
type request struct {
	id string

	mu    sync.Mutex
	attrs []slog.Attr
	quiet bool
}

func requestFrom(ctx context.Context) *request {
	req, _ := ctx.Value(requestCtxKey{}).(*request)
	return req
}

// annotate adds attributes to the access log line of the request of ctx:
// the reason of an error response, the client that sent it, ... Outside a
// request it does nothing.
func annotate(ctx context.Context, attrs ...slog.Attr) {
	req := requestFrom(ctx)
	if req == nil {
		return
	}
	req.mu.Lock()
	req.attrs = append(req.attrs, attrs...)
	req.mu.Unlock()
}

// quiet makes the access log line of the request of ctx debug level even
// for an error status: for requests that are expected to fail.
func quiet(ctx context.Context) {
	req := requestFrom(ctx)
	if req == nil {
		return
	}
	req.mu.Lock()
	req.quiet = true
	req.mu.Unlock()
}

// requestIDHandler adds the ID of the request of the context to every
// record logged with a *Context method of the logger.
type requestIDHandler struct{ slog.Handler }

func (h requestIDHandler) Handle(ctx context.Context, r slog.Record) error {
	if req := requestFrom(ctx); req != nil {
		r.AddAttrs(slog.String("request_id", req.id))
	}
	return h.Handler.Handle(ctx, r)
}

func (h requestIDHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return requestIDHandler{h.Handler.WithAttrs(attrs)}
}

func (h requestIDHandler) WithGroup(name string) slog.Handler {
	return requestIDHandler{h.Handler.WithGroup(name)}
}

// accessLog logs one line per request after it is served, at debug level
// for successes, info for client errors (4xx) and error for server errors
// (5xx). It gives each request an ID (the X-Request-Id of the request when
// present) and answers it in X-Request-Id. A panic in next is logged with
// its stack and answered 500.
func accessLog(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := r.Header.Get(requestIDHeader)
		if !validRequestID(id) {
			id = newRequestID()
		}
		req := &request{id: id}
		ctx := context.WithValue(r.Context(), requestCtxKey{}, req)
		r = r.WithContext(ctx)
		w.Header().Set(requestIDHeader, id)
		rw := &responseWriter{ResponseWriter: w}

		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				log.ErrorContext(ctx, "jellyfin: panic",
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.String("panic", fmt.Sprint(v)),
					slog.String("stack", string(debug.Stack())),
				)
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
			req.mu.Lock()
			quiet := req.quiet
			req.mu.Unlock()
			level := slog.LevelDebug
			switch {
			case quiet:
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
				slog.Int64("duration", int64(time.Since(start))),
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

func newRequestID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// validRequestID accepts IDs of up to 64 printable ASCII characters.
func validRequestID(id string) bool {
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
