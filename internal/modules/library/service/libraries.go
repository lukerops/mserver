package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/infra/queue"
	"github.com/lukerops/mserver/internal/modules/library/events"
	storageclient "github.com/lukerops/mserver/internal/modules/storage/client"
)

func (s *service) CreateLibrary(ctx context.Context, req CreateLibraryRequest) (Library, error) {
	lib := Library{
		ID:          newID(),
		Name:        strings.TrimSpace(req.Name),
		MediaType:   req.MediaType,
		LocationIDs: req.LocationIDs,
		CreatedAt:   time.Now().UTC(),
	}
	switch {
	case lib.Name == "":
		return Library{}, fmt.Errorf("%w: name is required", ErrInvalidArgument)
	case lib.MediaType != MediaTypeMovies:
		return Library{}, fmt.Errorf("%w: unsupported media type %q", ErrInvalidArgument, lib.MediaType)
	case len(lib.LocationIDs) == 0:
		return Library{}, fmt.Errorf("%w: at least one location is required", ErrInvalidArgument)
	}
	for i, loc := range lib.LocationIDs {
		if loc == uuid.Nil || slices.Contains(lib.LocationIDs[:i], loc) {
			return Library{}, fmt.Errorf("%w: invalid or repeated location %s", ErrInvalidArgument, loc)
		}
	}

	for _, loc := range lib.LocationIDs {
		_, err := s.storage.GetLocation(ctx, loc)
		if errors.Is(err, storageclient.ErrNotFound) {
			return Library{}, fmt.Errorf("%w: unknown location %s", ErrInvalidArgument, loc)
		}
		if err != nil {
			return Library{}, fmt.Errorf("%w: get location %s: %w", ErrUnavailable, loc, err)
		}
	}

	if err := s.store.InsertLibrary(ctx, lib); err != nil {
		return Library{}, err
	}

	// The files of the locations are cataloged from the events of the
	// resync. A failed resync is not fatal: the library exists, and the
	// files are cataloged as the storage finds changes in them.
	for _, loc := range lib.LocationIDs {
		if err := s.resyncLocation(ctx, loc); err != nil {
			s.log.WarnContext(ctx, "library: resync of location failed",
				"library", lib.ID, "location", loc, "error", err)
		}
	}
	return lib, nil
}

// resyncLocation asks the storage to resync a location, retrying after
// each wait of resyncBackoff.
func (s *service) resyncLocation(ctx context.Context, loc uuid.UUID) error {
	err := s.storage.ResyncLocation(ctx, loc)
	for _, wait := range s.resyncBackoff {
		if err == nil {
			return nil
		}
		s.log.DebugContext(ctx, "library: resync of location failed, retrying",
			"location", loc, "in", wait, "error", err)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(err, ctx.Err())
		case <-timer.C:
		}
		err = s.storage.ResyncLocation(ctx, loc)
	}
	return err
}

func (s *service) DeleteLibrary(ctx context.Context, id uuid.UUID) error {
	return s.store.WithTx(ctx, func(ctx context.Context) error {
		movies, _, err := s.store.ListMovies(ctx, MovieQuery{LibraryID: id})
		if err != nil {
			return err
		}
		if err := s.store.DeleteLibrary(ctx, id); err != nil {
			return err
		}
		evs := make([]queue.Event, len(movies))
		for i, m := range movies {
			evs[i] = movieRemovedEvent(m)
		}
		return s.outbox.Publish(ctx, evs...)
	})
}

func (s *service) GetLibrary(ctx context.Context, id uuid.UUID) (Library, error) {
	return s.store.GetLibrary(ctx, id)
}

func (s *service) ListLibraries(ctx context.Context) ([]Library, error) {
	return s.store.ListLibraries(ctx)
}

func (s *service) ResyncLibrary(ctx context.Context, id uuid.UUID) error {
	return s.store.WithTx(ctx, func(ctx context.Context) error {
		if _, err := s.store.GetLibrary(ctx, id); err != nil {
			return err
		}
		movies, _, err := s.store.ListMovies(ctx, MovieQuery{LibraryID: id})
		if err != nil {
			return err
		}
		evs := make([]queue.Event, len(movies))
		for i, m := range movies {
			evs[i] = movieEvent(events.MovieAddedV1, m)
		}
		return s.outbox.Publish(ctx, evs...)
	})
}
