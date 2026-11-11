package handler

import (
	"context"

	identityv1 "github.com/lukerops/mserver/gen/identity/v1"
	"github.com/lukerops/mserver/internal/modules/identity/service"
)

func (h *Handler) Authenticate(ctx context.Context, req *identityv1.AuthenticateRequest) (*identityv1.AuthenticateResponse, error) {
	res, err := h.svc.Authenticate(ctx, &service.AuthenticateRequest{
		Login:    req.GetLogin(),
		Password: req.GetPassword(),
		Device: service.DeviceInfo{
			Identifier: req.GetDevice().GetIdentifier(),
			Name:       req.GetDevice().GetName(),
			Client:     req.GetDevice().GetClient(),
			Version:    req.GetDevice().GetVersion(),
		},
	})
	if err != nil {
		return nil, h.connectError(err)
	}
	return &identityv1.AuthenticateResponse{Session: toSession(&res.Session), Token: res.Token}, nil
}

func (h *Handler) SelectProfile(ctx context.Context, req *identityv1.SelectProfileRequest) (*identityv1.SelectProfileResponse, error) {
	sessionID, err := parseID("session_id", req.GetSessionId())
	if err != nil {
		return nil, err
	}
	profileID, err := parseID("profile_id", req.GetProfileId())
	if err != nil {
		return nil, err
	}
	session, err := h.svc.SelectProfile(ctx, sessionID, profileID)
	if err != nil {
		return nil, h.connectError(err)
	}
	return &identityv1.SelectProfileResponse{Session: toSession(session)}, nil
}

func (h *Handler) ValidateToken(ctx context.Context, req *identityv1.ValidateTokenRequest) (*identityv1.ValidateTokenResponse, error) {
	session, err := h.svc.ValidateToken(ctx, req.GetToken())
	if err != nil {
		return nil, h.connectError(err)
	}
	return &identityv1.ValidateTokenResponse{Session: toSession(session)}, nil
}

func (h *Handler) RevokeSession(ctx context.Context, req *identityv1.RevokeSessionRequest) (*identityv1.RevokeSessionResponse, error) {
	id, err := parseID("session_id", req.GetSessionId())
	if err != nil {
		return nil, err
	}
	if err := h.svc.RevokeSession(ctx, id); err != nil {
		return nil, h.connectError(err)
	}
	return &identityv1.RevokeSessionResponse{}, nil
}
