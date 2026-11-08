package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

func (s *service) GetSeries(ctx context.Context, id uuid.UUID) (Series, error) {
	return s.store.GetSeries(ctx, id)
}

func (s *service) ListSeries(ctx context.Context, q SeriesQuery) (SeriesPage, error) {
	switch q.SortBy {
	case "", SortByTitle, SortByAdded, SortByYear:
	default:
		return SeriesPage{}, fmt.Errorf("%w: unknown sort %q", ErrInvalidArgument, q.SortBy)
	}
	if q.Offset < 0 || q.Limit < 0 {
		return SeriesPage{}, fmt.Errorf("%w: negative offset or limit", ErrInvalidArgument)
	}
	series, total, err := s.store.ListSeries(ctx, q)
	if err != nil {
		return SeriesPage{}, err
	}
	return SeriesPage{Series: series, Total: total}, nil
}

func (s *service) GetSeason(ctx context.Context, id uuid.UUID) (Season, error) {
	return s.store.GetSeason(ctx, id)
}

func (s *service) ListSeasons(ctx context.Context, q SeasonQuery) ([]Season, error) {
	return s.store.ListSeasons(ctx, q)
}

func (s *service) GetEpisode(ctx context.Context, id uuid.UUID) (Episode, error) {
	return s.store.GetEpisode(ctx, id)
}

func (s *service) ListEpisodes(ctx context.Context, q EpisodeQuery) (EpisodePage, error) {
	switch q.SortBy {
	case "", SortByNumber, SortByAdded:
	default:
		return EpisodePage{}, fmt.Errorf("%w: unknown sort %q", ErrInvalidArgument, q.SortBy)
	}
	if q.Offset < 0 || q.Limit < 0 {
		return EpisodePage{}, fmt.Errorf("%w: negative offset or limit", ErrInvalidArgument)
	}
	episodes, total, err := s.store.ListEpisodes(ctx, q)
	if err != nil {
		return EpisodePage{}, err
	}
	return EpisodePage{Episodes: episodes, Total: total}, nil
}

// episodeNamespace derives the episode IDs from the file IDs. It differs
// from movieNamespace, so that a file never has a movie and an episode
// with the same ID.
var episodeNamespace = uuid.MustParse("9e2d4a61-5c3b-4f8a-b7d0-1a6e8c2f9b54")

// episodeID is the ID of the episode of a file: always the same for the
// same file, so that the catalog can be rebuilt from the storage events.
func episodeID(file uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(episodeNamespace, file[:])
}
