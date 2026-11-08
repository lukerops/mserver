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

func (s *service) GetSeason(ctx context.Context, seriesID, id uuid.UUID) (Season, error) {
	season, err := s.store.GetSeason(ctx, id)
	if err != nil {
		return Season{}, err
	}
	if seriesID != uuid.Nil && season.SeriesID != seriesID {
		return Season{}, fmt.Errorf("%w: season %s of series %s", ErrNotFound, id, seriesID)
	}
	return season, nil
}

func (s *service) ListSeasons(ctx context.Context, q SeasonQuery) ([]Season, error) {
	if q.SeriesID != uuid.Nil {
		if _, err := s.store.GetSeries(ctx, q.SeriesID); err != nil {
			return nil, err
		}
	}
	return s.store.ListSeasons(ctx, q)
}

func (s *service) GetEpisode(ctx context.Context, seriesID, seasonID, id uuid.UUID) (Episode, error) {
	e, err := s.store.GetEpisode(ctx, id)
	if err != nil {
		return Episode{}, err
	}
	if (seriesID != uuid.Nil && e.SeriesID != seriesID) || (seasonID != uuid.Nil && e.SeasonID != seasonID) {
		return Episode{}, fmt.Errorf("%w: episode %s of series %s, season %s", ErrNotFound, id, seriesID, seasonID)
	}
	return e, nil
}

// checkParents fails with ErrNotFound when the given series or season do
// not exist, or when the season does not belong to the series. Nil IDs
// are not checked.
func (s *service) checkParents(ctx context.Context, seriesID, seasonID uuid.UUID) error {
	if seasonID != uuid.Nil {
		// The season has the series, so its existence is implied.
		_, err := s.GetSeason(ctx, seriesID, seasonID)
		return err
	}
	if seriesID != uuid.Nil {
		_, err := s.store.GetSeries(ctx, seriesID)
		return err
	}
	return nil
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
	if err := s.checkParents(ctx, q.SeriesID, q.SeasonID); err != nil {
		return EpisodePage{}, err
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
