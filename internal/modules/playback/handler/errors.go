package handler

import (
	"errors"
	"fmt"
	"log/slog"

	"connectrpc.com/connect/v2"
	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/playback/service"
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
	default:
		var cerr *connect.Error
		if errors.As(err, &cerr) {
			return err
		}
		h.log.Error("playback: request failed", slog.String("error", err.Error()))
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
