package service

import (
	"context"
	"net/url"

	"github.com/google/uuid"
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
	loc, err := s.store.GetLocation(ctx, f.LocationID)
	if err != nil {
		return "", err
	}
	d, err := newDriver(loc.Driver, loc.Options)
	if err != nil {
		return "", err
	}
	return (&url.URL{Scheme: "file", Path: d.LocalPath(f.Path)}).String(), nil
}
