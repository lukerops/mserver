package service

import (
	"context"

	"github.com/lukerops/mserver/internal/infra/queue"
	"github.com/lukerops/mserver/internal/modules/identity/events"
)

// ResyncAccounts publishes the created event of every account.
func (s *service) ResyncAccounts(ctx context.Context) error {
	accounts, err := s.store.ListAccounts(ctx)
	if err != nil {
		return err
	}
	evs := make([]queue.Event, 0, len(accounts))
	for _, a := range accounts {
		evs = append(evs, accountEvent(events.AccountCreatedV1, a))
	}
	return s.outbox.Publish(ctx, evs...)
}

// ResyncProfiles publishes the created event of every profile.
func (s *service) ResyncProfiles(ctx context.Context) error {
	profiles, err := s.store.ListAllProfiles(ctx)
	if err != nil {
		return err
	}
	evs := make([]queue.Event, 0, len(profiles))
	for _, p := range profiles {
		evs = append(evs, profileEvent(events.ProfileCreatedV1, p))
	}
	return s.outbox.Publish(ctx, evs...)
}
