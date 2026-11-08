package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/storage/driver"
)

func (s *service) GetFile(ctx context.Context, id uuid.UUID) (File, error) {
	return s.store.GetFile(ctx, id)
}

func (s *service) ListFiles(ctx context.Context, location uuid.UUID) ([]File, error) {
	if _, err := s.store.GetLocation(ctx, location); err != nil {
		return nil, err
	}
	return s.store.ListFiles(ctx, location)
}

func (s *service) ListSiblings(ctx context.Context, id uuid.UUID) ([]Sibling, error) {
	f, err := s.store.GetFile(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.store.Siblings(ctx, f)
}

func (s *service) Resolve(ctx context.Context, id uuid.UUID) (string, error) {
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

func (s *service) OpenFile(ctx context.Context, location, id uuid.UUID) (File, driver.File, error) {
	f, err := s.store.GetFile(ctx, id)
	if err != nil {
		return File{}, nil, err
	}
	if f.LocationID != location {
		return File{}, nil, fmt.Errorf("%w: file %s in location %s", ErrNotFound, id, location)
	}
	loc, err := s.store.GetLocation(ctx, location)
	if err != nil {
		return File{}, nil, err
	}
	d, err := newDriver(loc.Driver, loc.Options)
	if err != nil {
		return File{}, nil, err
	}
	content, err := d.Open(ctx, f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		// The file is gone, or the whole location is (e.g. an unmounted
		// disk): only the latter is temporary.
		if checkErr := d.Check(ctx); checkErr != nil {
			return File{}, nil, fmt.Errorf("%w: location %s: %w", ErrUnavailable, location, checkErr)
		}
		return File{}, nil, fmt.Errorf("%w: file %s: %w", ErrNotFound, id, err)
	}
	if err != nil {
		return File{}, nil, err
	}
	return f, content, nil
}
