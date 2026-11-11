package handler

import (
	"errors"
	"fmt"
	"log/slog"

	"connectrpc.com/connect/v2"
	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/library/service"
)

// connectError translates an error of the service to a Connect error. The
// errors the service does not know are logged and hidden from the caller.
func (h *Handler) connectError(err error) error {
	var code connect.Code
	switch {
	case errors.Is(err, service.ErrInvalidArgument):
		code = connect.CodeInvalidArgument
	case errors.Is(err, service.ErrNotFound):
		code = connect.CodeNotFound
	case errors.Is(err, service.ErrAlreadyExists):
		code = connect.CodeAlreadyExists
	case errors.Is(err, service.ErrFailedPrecondition):
		code = connect.CodeFailedPrecondition
	case errors.Is(err, service.ErrUnavailable):
		code = connect.CodeUnavailable
	default:
		var cerr *connect.Error
		if errors.As(err, &cerr) {
			return err
		}
		h.log.Error("library: request failed", slog.String("error", err.Error()))
		return connect.NewError(connect.CodeInternal, "internal error").WithCause(err)
	}
	return connect.NewError(code, err.Error()).WithCause(err)
}

func invalid(format string, args ...any) error {
	return connect.NewError(connect.CodeInvalidArgument, fmt.Sprintf(format, args...))
}

// parseID parses the UUID in the field of a request.
func parseID(field, s string) (uuid.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, invalid("invalid %s %q", field, s)
	}
	return id, nil
}

// parseOptionalID parses the UUID in the field of a request, which may be
// empty (uuid.Nil).
func parseOptionalID(field, s string) (uuid.UUID, error) {
	if s == "" {
		return uuid.Nil, nil
	}
	return parseID(field, s)
}

// parseIDs parses the UUIDs in the field of a request.
func parseIDs(field string, ss []string) ([]uuid.UUID, error) {
	if len(ss) == 0 {
		return nil, nil
	}
	ids := make([]uuid.UUID, len(ss))
	for i, s := range ss {
		id, err := parseID(field, s)
		if err != nil {
			return nil, err
		}
		ids[i] = id
	}
	return ids, nil
}

// page returns the limit and offset of a list request. A missing or too
// large limit is the maximum page.
func page(limit, offset int32) (int, int, error) {
	if limit < 0 || offset < 0 {
		return 0, 0, invalid("negative limit or offset")
	}
	if limit == 0 || limit > maxListLimit {
		limit = maxListLimit
	}
	return int(limit), int(offset), nil
}

func toInts(ns []int32) []int {
	if len(ns) == 0 {
		return nil
	}
	out := make([]int, len(ns))
	for i, n := range ns {
		out[i] = int(n)
	}
	return out
}
