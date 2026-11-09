// Package logging builds the logger of the server and ties logs to the
// HTTP request that produced them.
//
// Every request gets an ID (X-Request-Id). The ID is kept in the request
// context, added to every record logged with a *Context method of the
// logger (DebugContext, InfoContext, ...) and forwarded by Transport to the
// calls the request makes to other modules, so one ID follows a request
// across the modules.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// New creates a logger writing to w. level is debug, info, warn or error
// (default info); format is text or json (default text).
func New(w io.Writer, level, format string) (*slog.Logger, error) {
	var l slog.Level
	if level != "" {
		if err := l.UnmarshalText([]byte(level)); err != nil {
			return nil, fmt.Errorf("unknown log level %q", level)
		}
	}
	opts := &slog.HandlerOptions{Level: l}
	var h slog.Handler
	switch strings.ToLower(format) {
	case "", "text":
		h = slog.NewTextHandler(w, opts)
	case "json":
		h = slog.NewJSONHandler(w, opts)
	default:
		return nil, fmt.Errorf("unknown log format %q", format)
	}
	return slog.New(NewHandler(h)), nil
}

// NewHandler wraps h to add the request ID of the context to every record.
func NewHandler(h slog.Handler) slog.Handler {
	return contextHandler{h}
}

type contextHandler struct{ slog.Handler }

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := RequestID(ctx); id != "" {
		r.AddAttrs(slog.String("request_id", id))
	}
	return h.Handler.Handle(ctx, r)
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{h.Handler.WithAttrs(attrs)}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{h.Handler.WithGroup(name)}
}
