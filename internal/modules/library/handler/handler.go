// Package handler implements the library ConnectRPC service
// (libraryv1connect.LibraryService) on top of the library service. It has
// no business rules: it only converts protobuf to service calls and back,
// and service errors to Connect codes.
//
// The requests received are read-only: in-process calls do not copy them.
package handler

import (
	"log/slog"

	"github.com/lukerops/mserver/gen/library/v1/libraryv1connect"
	"github.com/lukerops/mserver/internal/modules/library/service"
)

var _ libraryv1connect.LibraryServiceHandler = (*Handler)(nil)

// maxListLimit is the largest page of the lists, and the one used when the
// request sets no limit.
const maxListLimit = 1000

// Handler serves the library API.
type Handler struct {
	svc *service.Service
	log *slog.Logger
}

// New returns the handler of svc.
func New(svc *service.Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}
