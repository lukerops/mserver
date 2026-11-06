package api

import "errors"

// Domain errors. They map 1:1 to gRPC status codes. Implementations wrap
// them with context (fmt.Errorf("%w: ...", ErrNotFound)); consumers must
// use errors.Is.
var (
	ErrNotFound           = errors.New("not found")
	ErrInvalidArgument    = errors.New("invalid argument")
	ErrAlreadyExists      = errors.New("already exists")
	ErrFailedPrecondition = errors.New("failed precondition")
	ErrUnavailable        = errors.New("unavailable")
)
