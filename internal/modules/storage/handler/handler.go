// Package handler implements the storage ConnectRPC service
// (storagev1connect.StorageService) and the HTTP data plane, on top of the
// storage service. It has no business rules: it only converts protobuf to
// service calls and back, and service errors to Connect codes.
//
// The requests received are read-only: in-process calls do not copy them.
package handler

import (
	"log/slog"
	"strings"

	"github.com/lukerops/mserver/gen/storage/v1/storagev1connect"
	"github.com/lukerops/mserver/internal/modules/storage/service"
)

var _ storagev1connect.StorageServiceHandler = (*Handler)(nil)

// Handler serves the storage API.
type Handler struct {
	svc *service.Service
	// baseURL is the address other modules reach the data plane at,
	// without the trailing slash (e.g. "http://127.0.0.1:8096").
	baseURL string
	log     *slog.Logger
}

// New returns the handler of svc. baseURL is the address of the HTTP server
// that serves Mux, used by ResolveFile.
func New(svc *service.Service, baseURL string, log *slog.Logger) *Handler {
	return &Handler{svc: svc, baseURL: strings.TrimRight(baseURL, "/"), log: log}
}
