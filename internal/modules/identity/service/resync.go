package service

import (
	"context"

	"github.com/lukerops/mserver/internal/infra/queue"
)

// ResyncAccounts publishes the created event of every account.
func (s *service) ResyncAccounts(ctx context.Context) error {
	accounts, err := s.store.ListAccounts(ctx)
	if err != nil {
		return err
	}
	events := make([]queue.Event, 0, len(accounts))
	for _, a := range accounts {
		events = append(events, accountEvent(EventAccountCreated, a))
	}
	return s.outbox.Publish(ctx, events...)
}

// ResyncProfiles publishes the created event of every profile.
func (s *service) ResyncProfiles(ctx context.Context) error {
	profiles, err := s.store.ListAllProfiles(ctx)
	if err != nil {
		return err
	}
	events := make([]queue.Event, 0, len(profiles))
	for _, p := range profiles {
		events = append(events, profileEvent(EventProfileCreated, p))
	}
	return s.outbox.Publish(ctx, events...)
}
