package storage

import (
	"context"
	"fmt"
	"maps"
	"time"

	"github.com/lukerops/mserver/internal/infra/queue"
	"github.com/lukerops/mserver/internal/modules/storage/api"
)

func (s *Service) CreateLocation(ctx context.Context, req api.CreateLocationRequest) (api.Location, error) {
	d, err := newDriver(req.Driver, req.Options)
	if err != nil {
		return api.Location{}, err
	}
	if err := d.Check(ctx); err != nil {
		return api.Location{}, fmt.Errorf("%w: location is not accessible: %w", api.ErrFailedPrecondition, err)
	}

	loc := api.Location{
		ID:        api.NewLocationID(),
		Driver:    req.Driver,
		Options:   maps.Clone(req.Options),
		CreatedAt: time.Now().UTC(),
	}
	if err := s.store.InsertLocation(ctx, loc, d.Root()); err != nil {
		return api.Location{}, err
	}
	return loc, nil
}

func (s *Service) GetLocation(ctx context.Context, id api.LocationID) (api.Location, error) {
	return s.store.GetLocation(ctx, id)
}

func (s *Service) ListLocations(ctx context.Context) ([]api.Location, error) {
	return s.store.ListLocations(ctx)
}

func (s *Service) DeleteLocation(ctx context.Context, id api.LocationID) error {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()

	return s.store.WithTx(ctx, func(ctx context.Context) error {
		files, err := s.store.ListFiles(ctx, id)
		if err != nil {
			return err
		}
		if err := s.store.DeleteLocation(ctx, id); err != nil {
			return err
		}
		events := make([]queue.Event, len(files))
		for i, f := range files {
			events[i] = newFileEvent(api.EventFileRemoved, id, f.ID, f.Path)
		}
		return s.outbox.Publish(ctx, events...)
	})
}
