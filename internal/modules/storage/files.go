package storage

import (
	"context"
	"net/url"

	"github.com/lukerops/mserver/internal/modules/storage/api"
)

func (s *Service) GetFile(ctx context.Context, id api.FileID) (api.File, error) {
	return s.store.GetFile(ctx, id)
}

func (s *Service) ListFiles(ctx context.Context, id api.LocationID) ([]api.File, error) {
	if _, err := s.store.GetLocation(ctx, id); err != nil {
		return nil, err
	}
	return s.store.ListFiles(ctx, id)
}

func (s *Service) ListSiblings(ctx context.Context, id api.FileID) ([]api.Sibling, error) {
	f, err := s.store.GetFile(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.store.Siblings(ctx, f)
}

func (s *Service) Resolve(ctx context.Context, id api.FileID) (string, error) {
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
