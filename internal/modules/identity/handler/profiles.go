package handler

import (
	"context"

	identityv1 "github.com/lukerops/mserver/gen/identity/v1"
)

func (h *Handler) GetProfile(ctx context.Context, req *identityv1.GetProfileRequest) (*identityv1.GetProfileResponse, error) {
	id, err := parseID("id", req.GetId())
	if err != nil {
		return nil, err
	}
	profile, err := h.svc.GetProfile(ctx, id)
	if err != nil {
		return nil, h.connectError(err)
	}
	return &identityv1.GetProfileResponse{Profile: toProfile(profile)}, nil
}

func (h *Handler) ListProfiles(ctx context.Context, req *identityv1.ListProfilesRequest) (*identityv1.ListProfilesResponse, error) {
	id, err := parseID("account_id", req.GetAccountId())
	if err != nil {
		return nil, err
	}
	profiles, err := h.svc.ListProfiles(ctx, id)
	if err != nil {
		return nil, h.connectError(err)
	}
	resp := &identityv1.ListProfilesResponse{Profiles: make([]*identityv1.Profile, len(profiles))}
	for i := range profiles {
		resp.Profiles[i] = toProfile(&profiles[i])
	}
	return resp, nil
}

func (h *Handler) ResyncProfiles(ctx context.Context, _ *identityv1.ResyncProfilesRequest) (*identityv1.ResyncProfilesResponse, error) {
	if err := h.svc.ResyncProfiles(ctx); err != nil {
		return nil, h.connectError(err)
	}
	return &identityv1.ResyncProfilesResponse{}, nil
}
