package service

import (
	"context"
	"log/slog"

	"github.com/lukerops/mserver/internal/events"
)

// ResyncAccounts publishes the created event of every account. It stops at
// the first failure.
func (s *Service) ResyncAccounts(ctx context.Context) error {
	accounts, err := s.store.ListAccounts(ctx)
	if err != nil {
		return err
	}
	for i := range accounts {
		if err := s.publishAccount(ctx, events.AccountCreatedV1, &accounts[i]); err != nil {
			return err
		}
	}
	s.log.DebugContext(ctx, "identity: accounts resynced", slog.Int("count", len(accounts)))
	return nil
}

// ResyncProfiles publishes the created event of every profile. It stops at
// the first failure.
func (s *Service) ResyncProfiles(ctx context.Context) error {
	profiles, err := s.store.ListAllProfiles(ctx)
	if err != nil {
		return err
	}
	for i := range profiles {
		if err := s.publishProfile(ctx, events.ProfileCreatedV1, &profiles[i]); err != nil {
			return err
		}
	}
	s.log.DebugContext(ctx, "identity: profiles resynced", slog.Int("count", len(profiles)))
	return nil
}
