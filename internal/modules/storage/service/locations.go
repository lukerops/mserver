package service

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"time"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/events"
	"github.com/lukerops/mserver/internal/modules/storage/store"
)

// CreateLocation registers a location. It fails if the directory is inside,
// or contains, another location.
func (s *Service) CreateLocation(ctx context.Context, req *CreateLocationRequest) (*Location, error) {
	d, err := newDriver(req.Driver, req.Options)
	if err != nil {
		return nil, err
	}
	if err := d.Check(ctx); err != nil {
		return nil, fmt.Errorf("%w: location is not accessible: %w", ErrFailedPrecondition, err)
	}

	// The request is read-only: the location keeps its own copy of the
	// options.
	loc := &Location{
		ID:        newID(),
		Driver:    req.Driver,
		Options:   maps.Clone(req.Options),
		CreatedAt: time.Now().UTC(),
	}
	if err := s.store.InsertLocation(ctx, loc, d.Root()); err != nil {
		return nil, err
	}
	s.log.InfoContext(ctx, "storage: location created",
		slog.String("location_id", loc.ID.String()),
		slog.String("driver", loc.Driver),
		slog.String("root", d.Root()),
	)
	return loc, nil
}

// GetLocation returns ErrNotFound if the location does not exist.
func (s *Service) GetLocation(ctx context.Context, id uuid.UUID) (*Location, error) {
	return s.store.GetLocation(ctx, id)
}

// ListLocations lists every location ordered by ID.
func (s *Service) ListLocations(ctx context.Context) ([]Location, error) {
	return s.store.ListLocations(ctx)
}

// DeleteLocation removes a location and its files from the index.
func (s *Service) DeleteLocation(ctx context.Context, id uuid.UUID) error {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()

	// No scan changes the index between listing the files and deleting
	// the location.
	files, err := s.store.ListFiles(ctx, id, store.Page{})
	if err != nil {
		return err
	}
	if err := s.store.DeleteLocation(ctx, id); err != nil {
		return err
	}

	changes := make([]fileChange, len(files))
	for i := range files {
		changes[i] = fileChange{Type: events.FileRemovedV1, ID: files[i].ID, Path: files[i].Path}
	}
	if err := s.publishFileChanges(ctx, id, changes); err != nil {
		s.logPublishError(ctx, err)
	}
	s.log.InfoContext(ctx, "storage: location deleted",
		slog.String("location_id", id.String()),
		slog.Int("files_removed", len(files)),
	)
	return nil
}
