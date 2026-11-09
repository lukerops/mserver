package service

import (
	"context"
	"fmt"
	"maps"
	"time"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/infra/queue"
	"github.com/lukerops/mserver/internal/modules/storage/events"
)

func (s *service) CreateLocation(ctx context.Context, req CreateLocationRequest) (Location, error) {
	d, err := newDriver(req.Driver, req.Options)
	if err != nil {
		return Location{}, err
	}
	if err := d.Check(ctx); err != nil {
		return Location{}, fmt.Errorf("%w: location is not accessible: %w", ErrFailedPrecondition, err)
	}

	loc := Location{
		ID:        newID(),
		Driver:    req.Driver,
		Options:   maps.Clone(req.Options),
		CreatedAt: time.Now().UTC(),
	}
	if err := s.store.InsertLocation(ctx, loc, d.Root()); err != nil {
		return Location{}, err
	}
	s.log.InfoContext(ctx, "storage: location created", "location_id", loc.ID, "driver", loc.Driver, "root", d.Root())
	return loc, nil
}

func (s *service) GetLocation(ctx context.Context, id uuid.UUID) (Location, error) {
	return s.store.GetLocation(ctx, id)
}

func (s *service) ListLocations(ctx context.Context) ([]Location, error) {
	return s.store.ListLocations(ctx)
}

func (s *service) DeleteLocation(ctx context.Context, id uuid.UUID) error {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()

	var removed int
	err := s.store.WithTx(ctx, func(ctx context.Context) error {
		files, err := s.store.ListFiles(ctx, id)
		if err != nil {
			return err
		}
		if err := s.store.DeleteLocation(ctx, id); err != nil {
			return err
		}
		evs := make([]queue.Event, len(files))
		for i, f := range files {
			evs[i] = fileEvent(events.FileRemovedV1, id, f.ID, f.Path)
		}
		removed = len(files)
		return s.outbox.Publish(ctx, evs...)
	})
	if err != nil {
		return err
	}
	s.log.InfoContext(ctx, "storage: location deleted", "location_id", id, "files_removed", removed)
	return nil
}
