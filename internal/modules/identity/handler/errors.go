package handler

import (
	"errors"
	"fmt"
	"log/slog"

	"connectrpc.com/connect/v2"
	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/identity/service"
)

// connectError translates an error of the service to a Connect error. The
// errors the service does not know are logged and hidden from the caller.
func (h *Handler) connectError(err error) error {
	var code connect.Code
	switch {
	case errors.Is(err, service.ErrInvalidArgument):
		code = connect.CodeInvalidArgument
	case errors.Is(err, service.ErrUnauthenticated):
		code = connect.CodeUnauthenticated
	case errors.Is(err, service.ErrNotFound):
		code = connect.CodeNotFound
	case errors.Is(err, service.ErrAlreadyExists):
		code = connect.CodeAlreadyExists
	default:
		var cerr *connect.Error
		if errors.As(err, &cerr) {
			return err
		}
		h.log.Error("identity: request failed", slog.String("error", err.Error()))
		return connect.NewError(connect.CodeInternal, "internal error").WithCause(err)
	}
	return connect.NewError(code, err.Error()).WithCause(err)
}

// parseID parses the UUID in the field of a request.
func parseID(field, s string) (uuid.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, connect.NewError(connect.CodeInvalidArgument, fmt.Sprintf("invalid %s %q", field, s))
	}
	return id, nil
}
