package handler

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"connectrpc.com/connect/v2"
	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/storage/service"
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
		h.log.Error("storage: request failed", slog.String("error", err.Error()))
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

// httpStatus is the HTTP status of an error of the service, for the data
// plane.
func httpStatus(err error) int {
	switch {
	case errors.Is(err, service.ErrInvalidArgument):
		return http.StatusBadRequest
	case errors.Is(err, service.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, service.ErrUnavailable):
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
