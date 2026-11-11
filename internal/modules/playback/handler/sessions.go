package handler

import (
	"context"

	playbackv1 "github.com/lukerops/mserver/gen/playback/v1"
	"github.com/lukerops/mserver/internal/modules/playback/service"
)

func (h *Handler) StartSession(ctx context.Context, req *playbackv1.StartSessionRequest) (*playbackv1.StartSessionResponse, error) {
	accountID, err := parseID("account_id", req.GetAccountId())
	if err != nil {
		return nil, err
	}
	profileID, err := parseID("profile_id", req.GetProfileId())
	if err != nil {
		return nil, err
	}
	deviceID, err := parseID("device_id", req.GetDeviceId())
	if err != nil {
		return nil, err
	}
	itemID, err := parseID("item_id", req.GetItemId())
	if err != nil {
		return nil, err
	}
	fileID, err := parseID("file_id", req.GetFileId())
	if err != nil {
		return nil, err
	}
	sess, err := h.svc.Start(ctx, &service.StartRequest{
		AccountID:  accountID,
		ProfileID:  profileID,
		DeviceID:   deviceID,
		ItemID:     itemID,
		FileID:     fileID,
		PlayMethod: req.GetPlayMethod(),
		PositionMs: req.GetPositionMs(),
		Paused:     req.GetPaused(),
	})
	if err != nil {
		return nil, h.connectError(err)
	}
	return &playbackv1.StartSessionResponse{Session: toSession(sess)}, nil
}

func (h *Handler) GetSession(ctx context.Context, req *playbackv1.GetSessionRequest) (*playbackv1.GetSessionResponse, error) {
	id, err := parseID("id", req.GetId())
	if err != nil {
		return nil, err
	}
	sess, err := h.svc.Get(ctx, id)
	if err != nil {
		return nil, h.connectError(err)
	}
	return &playbackv1.GetSessionResponse{Session: toSession(sess)}, nil
}

func (h *Handler) ListSessions(ctx context.Context, req *playbackv1.ListSessionsRequest) (*playbackv1.ListSessionsResponse, error) {
	limit, offset, err := page(req.GetLimit(), req.GetOffset())
	if err != nil {
		return nil, err
	}
	sessions, err := h.svc.List(ctx, limit, offset)
	if err != nil {
		return nil, h.connectError(err)
	}
	resp := &playbackv1.ListSessionsResponse{Sessions: make([]*playbackv1.Session, len(sessions))}
	for i := range sessions {
		resp.Sessions[i] = toSession(&sessions[i])
	}
	return resp, nil
}

func (h *Handler) ReportProgress(ctx context.Context, req *playbackv1.ReportProgressRequest) (*playbackv1.ReportProgressResponse, error) {
	id, err := parseID("id", req.GetId())
	if err != nil {
		return nil, err
	}
	sess, err := h.svc.Progress(ctx, id, &service.ProgressRequest{
		PositionMs: req.GetPositionMs(),
		Paused:     req.GetPaused(),
	})
	if err != nil {
		return nil, h.connectError(err)
	}
	return &playbackv1.ReportProgressResponse{Session: toSession(sess)}, nil
}

func (h *Handler) HeartbeatSession(ctx context.Context, req *playbackv1.HeartbeatSessionRequest) (*playbackv1.HeartbeatSessionResponse, error) {
	id, err := parseID("id", req.GetId())
	if err != nil {
		return nil, err
	}
	if err := h.svc.Heartbeat(ctx, id); err != nil {
		return nil, h.connectError(err)
	}
	return &playbackv1.HeartbeatSessionResponse{}, nil
}

func (h *Handler) StopSession(ctx context.Context, req *playbackv1.StopSessionRequest) (*playbackv1.StopSessionResponse, error) {
	id, err := parseID("id", req.GetId())
	if err != nil {
		return nil, err
	}
	if err := h.svc.Stop(ctx, id, &service.StopRequest{PositionMs: req.PositionMs}); err != nil {
		return nil, h.connectError(err)
	}
	return &playbackv1.StopSessionResponse{}, nil
}
