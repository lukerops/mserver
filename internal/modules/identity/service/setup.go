package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lukerops/mserver/internal/modules/identity/events"
)

func (s *service) Setup(ctx context.Context, req SetupRequest) (SetupResult, error) {
	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(req.Email)
	if err := validateUsername(req.Username); err != nil {
		return SetupResult{}, err
	}
	if err := validateEmail(req.Email); err != nil {
		return SetupResult{}, err
	}
	if err := validatePassword(req.Password); err != nil {
		return SetupResult{}, err
	}
	newProfile, err := normalizeProfile(req.Profile)
	if err != nil {
		return SetupResult{}, err
	}
	hash, err := hashPassword(req.Password)
	if err != nil {
		return SetupResult{}, err
	}

	now := time.Now().UTC()
	account := Account{
		ID:        newID(),
		Username:  req.Username,
		Email:     req.Email,
		IsAdmin:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	profile := Profile{
		ID:          newID(),
		AccountID:   account.ID,
		Handle:      newProfile.Handle,
		DisplayName: newProfile.DisplayName,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	err = s.store.WithTx(ctx, func(ctx context.Context) error {
		n, err := s.store.CountAccounts(ctx)
		if err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("%w: setup was already done", ErrAlreadyExists)
		}
		if err := s.store.InsertAccount(ctx, account, hash); err != nil {
			return err
		}
		if err := s.store.InsertProfile(ctx, profile); err != nil {
			return err
		}
		return s.outbox.Publish(ctx,
			accountEvent(events.AccountCreatedV1, account),
			profileEvent(events.ProfileCreatedV1, profile),
		)
	})
	if err != nil {
		return SetupResult{}, err
	}
	s.log.InfoContext(ctx, "identity: setup done", "account_id", account.ID, "profile_id", profile.ID)
	return SetupResult{Account: account, Profile: profile}, nil
}
