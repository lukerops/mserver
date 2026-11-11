package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/storage/driver"
)

// GetFile returns ErrNotFound if the file does not exist.
func (s *Service) GetFile(ctx context.Context, id uuid.UUID) (*File, error) {
	return s.store.GetFile(ctx, id)
}

// ListFiles lists the files of a location ordered by path. The zero Page
// lists all of them.
func (s *Service) ListFiles(ctx context.Context, location uuid.UUID, page Page) ([]File, error) {
	if _, err := s.store.GetLocation(ctx, location); err != nil {
		return nil, err
	}
	return s.store.ListFiles(ctx, location, page)
}

// ListSiblings lists the other indexed files in the same directory (e.g.
// external subtitles).
func (s *Service) ListSiblings(ctx context.Context, id uuid.UUID) ([]Sibling, error) {
	f, err := s.store.GetFile(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.store.Siblings(ctx, f)
}

// Resolve returns the path of the content of the file in the data plane of
// the storage ("/locations/{location}/files/{file}"), to be joined to the
// address of the storage.
func (s *Service) Resolve(ctx context.Context, id uuid.UUID) (string, error) {
	f, err := s.store.GetFile(ctx, id)
	if err != nil {
		return "", err
	}
	return FilePath(f.LocationID, f.ID), nil
}

// FilePath is the path of the content of a file in the storage data
// plane.
func FilePath(location, file uuid.UUID) string {
	return "/locations/" + location.String() + "/files/" + file.String()
}

// OpenFile opens the content of a file of a location. The caller closes it.
// It returns ErrNotFound if the file is not indexed in the location or is
// missing from it, and ErrUnavailable if the location is not accessible.
func (s *Service) OpenFile(ctx context.Context, location, id uuid.UUID) (*File, driver.File, error) {
	f, err := s.store.GetFile(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if f.LocationID != location {
		return nil, nil, fmt.Errorf("%w: file %s in location %s", ErrNotFound, id, location)
	}
	loc, err := s.store.GetLocation(ctx, location)
	if err != nil {
		return nil, nil, err
	}
	d, err := newDriver(loc.Driver, loc.Options)
	if err != nil {
		return nil, nil, err
	}
	content, err := d.Open(ctx, f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		// The file is gone, or the whole location is (e.g. an unmounted
		// disk): only the latter is temporary.
		if checkErr := d.Check(ctx); checkErr != nil {
			s.log.WarnContext(ctx, "storage: location unavailable",
				slog.String("location_id", location.String()),
				slog.String("file_id", id.String()),
				slog.String("error", checkErr.Error()),
			)
			return nil, nil, fmt.Errorf("%w: location %s: %w", ErrUnavailable, location, checkErr)
		}
		s.log.DebugContext(ctx, "storage: indexed file missing on disk",
			slog.String("location_id", location.String()),
			slog.String("file_id", id.String()),
			slog.String("path", f.Path),
		)
		return nil, nil, fmt.Errorf("%w: file %s: %w", ErrNotFound, id, err)
	}
	if err != nil {
		return nil, nil, err
	}
	return f, content, nil
}
