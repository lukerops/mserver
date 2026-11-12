// Package handler implements the subtitles ConnectRPC service
// (subtitlesv1connect.SubtitlesService) on top of the subtitles service. It
// has no business rules: it only converts protobuf to service calls and
// back, and service errors to Connect codes.
package handler

import (
	"context"
	"errors"
	"log/slog"

	"connectrpc.com/connect/v2"

	subtitlesv1 "github.com/lukerops/mserver/gen/subtitles/v1"
	"github.com/lukerops/mserver/gen/subtitles/v1/subtitlesv1connect"
	"github.com/lukerops/mserver/internal/modules/subtitles/service"
)

var _ subtitlesv1connect.SubtitlesServiceHandler = (*Handler)(nil)

// Handler serves the subtitles API.
type Handler struct {
	svc *service.Service
	log *slog.Logger
}

// New returns the handler of svc.
func New(svc *service.Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

func (h *Handler) ResyncSubtitles(ctx context.Context, _ *subtitlesv1.ResyncSubtitlesRequest) (*subtitlesv1.ResyncSubtitlesResponse, error) {
	if err := h.svc.ResyncSubtitles(ctx); err != nil {
		return nil, h.connectError(err)
	}
	return &subtitlesv1.ResyncSubtitlesResponse{}, nil
}

// connectError hides the errors of the service from the caller: the module
// has no domain errors, so any failure is internal and is logged.
func (h *Handler) connectError(err error) error {
	var cerr *connect.Error
	if errors.As(err, &cerr) {
		return err
	}
	h.log.Error("subtitles: request failed", slog.String("error", err.Error()))
	return connect.NewError(connect.CodeInternal, "internal error").WithCause(err)
}
