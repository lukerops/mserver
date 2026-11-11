package handler

import (
	"context"

	identityv1 "github.com/lukerops/mserver/gen/identity/v1"
	"github.com/lukerops/mserver/internal/modules/identity/service"
)

func (h *Handler) Setup(ctx context.Context, req *identityv1.SetupRequest) (*identityv1.SetupResponse, error) {
	res, err := h.svc.Setup(ctx, &service.SetupRequest{
		Username: req.GetUsername(),
		Email:    req.GetEmail(),
		Password: req.GetPassword(),
		Profile: service.NewProfile{
			Handle:      req.GetProfile().GetHandle(),
			DisplayName: req.GetProfile().GetDisplayName(),
		},
	})
	if err != nil {
		return nil, h.connectError(err)
	}
	return &identityv1.SetupResponse{
		Account: toAccount(&res.Account),
		Profile: toProfile(&res.Profile),
	}, nil
}

func (h *Handler) GetAccount(ctx context.Context, req *identityv1.GetAccountRequest) (*identityv1.GetAccountResponse, error) {
	id, err := parseID("id", req.GetId())
	if err != nil {
		return nil, err
	}
	account, err := h.svc.GetAccount(ctx, id)
	if err != nil {
		return nil, h.connectError(err)
	}
	return &identityv1.GetAccountResponse{Account: toAccount(account)}, nil
}

func (h *Handler) ResyncAccounts(ctx context.Context, _ *identityv1.ResyncAccountsRequest) (*identityv1.ResyncAccountsResponse, error) {
	if err := h.svc.ResyncAccounts(ctx); err != nil {
		return nil, h.connectError(err)
	}
	return &identityv1.ResyncAccountsResponse{}, nil
}
