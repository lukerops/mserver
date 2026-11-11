// Package handler implements the identity ConnectRPC service
// (identityv1connect.IdentityService) on top of the identity service. It has
// no business rules: it only converts protobuf to service calls and back,
// and service errors to Connect codes.
//
// The requests received are read-only: in-process calls do not copy them.
package handler

import (
	"log/slog"

	"github.com/lukerops/mserver/gen/identity/v1/identityv1connect"
	"github.com/lukerops/mserver/internal/modules/identity/service"
)

var _ identityv1connect.IdentityServiceHandler = (*Handler)(nil)

// Handler serves the identity API.
type Handler struct {
	svc *service.Service
	log *slog.Logger
}

// New returns the handler of svc.
func New(svc *service.Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}
