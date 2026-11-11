package service

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/lukerops/mserver/internal/events"
)

// Setup creates the first account, as administrator, with its first
// profile. It returns ErrAlreadyExists if any account exists.
func (s *Service) Setup(ctx context.Context, req *SetupRequest) (*SetupResult, error) {
	username := strings.TrimSpace(req.Username)
	email := strings.TrimSpace(req.Email)
	if err := validateUsername(username); err != nil {
		return nil, err
	}
	if err := validateEmail(email); err != nil {
		return nil, err
	}
	if err := validatePassword(req.Password); err != nil {
		return nil, err
	}
	newProfile, err := normalizeProfile(req.Profile)
	if err != nil {
		return nil, err
	}
	hash, err := hashPassword(req.Password)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	res := &SetupResult{
		Account: Account{
			ID:        newID(),
			Username:  username,
			Email:     email,
			IsAdmin:   true,
			CreatedAt: now,
			UpdatedAt: now,
		},
		Profile: Profile{
			ID:          newID(),
			Handle:      newProfile.Handle,
			DisplayName: newProfile.DisplayName,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}
	res.Profile.AccountID = res.Account.ID

	if err := s.store.CreateFirstAccount(ctx, &res.Account, hash, &res.Profile); err != nil {
		return nil, err
	}
	s.log.InfoContext(ctx, "identity: setup done",
		slog.String("account_id", res.Account.ID.String()),
		slog.String("profile_id", res.Profile.ID.String()),
	)

	if err := s.publishAccount(ctx, events.AccountCreatedV1, &res.Account); err != nil {
		s.logPublishError(ctx, events.AccountCreatedV1, err)
	}
	if err := s.publishProfile(ctx, events.ProfileCreatedV1, &res.Profile); err != nil {
		s.logPublishError(ctx, events.ProfileCreatedV1, err)
	}
	return res, nil
}
