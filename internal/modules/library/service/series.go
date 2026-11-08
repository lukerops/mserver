package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

func (s *service) GetSeries(ctx context.Context, libraryID, id uuid.UUID) (Series, error) {
	series, err := s.store.GetSeries(ctx, id)
	if err != nil {
		return Series{}, err
	}
	if !belongs(libraryID, series.LibraryID) {
		return Series{}, fmt.Errorf("%w: series %s of library %s", ErrNotFound, id, libraryID)
	}
	return series, nil
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
	if err := s.checkParents(ctx, q.LibraryID, uuid.Nil, uuid.Nil); err != nil {
		return SeriesPage{}, err
	}
	series, total, err := s.store.ListSeries(ctx, q)
	if err != nil {
		return SeriesPage{}, err
	}
	return SeriesPage{Series: series, Total: total}, nil
}

func (s *service) GetSeason(ctx context.Context, libraryID, seriesID, id uuid.UUID) (Season, error) {
	season, err := s.store.GetSeason(ctx, id)
	if err != nil {
		return Season{}, err
	}
	if !belongs(libraryID, season.LibraryID) || !belongs(seriesID, season.SeriesID) {
		return Season{}, fmt.Errorf("%w: season %s of library %s, series %s", ErrNotFound, id, libraryID, seriesID)
	}
	return season, nil
}

func (s *service) ListSeasons(ctx context.Context, q SeasonQuery) ([]Season, error) {
	if err := s.checkParents(ctx, q.LibraryID, q.SeriesID, uuid.Nil); err != nil {
		return nil, err
	}
	return s.store.ListSeasons(ctx, q)
}

func (s *service) GetEpisode(ctx context.Context, libraryID, seriesID, seasonID, id uuid.UUID) (Episode, error) {
	e, err := s.store.GetEpisode(ctx, id)
	if err != nil {
		return Episode{}, err
	}
	if !belongs(libraryID, e.LibraryID) || !belongs(seriesID, e.SeriesID) || !belongs(seasonID, e.SeasonID) {
		return Episode{}, fmt.Errorf("%w: episode %s of library %s, series %s, season %s",
			ErrNotFound, id, libraryID, seriesID, seasonID)
	}
	return e, nil
}

// checkParents fails with ErrNotFound when one of the given library,
// series and season does not exist, or does not belong to the others.
// Nil IDs are not checked.
func (s *service) checkParents(ctx context.Context, libraryID, seriesID, seasonID uuid.UUID) error {
	var err error
	switch {
	case seasonID != uuid.Nil:
		// The season holds its series and library: their existence is
		// implied.
		_, err = s.GetSeason(ctx, libraryID, seriesID, seasonID)
	case seriesID != uuid.Nil:
		_, err = s.GetSeries(ctx, libraryID, seriesID)
	case libraryID != uuid.Nil:
		_, err = s.store.GetLibrary(ctx, libraryID)
	}
	return err
}

// belongs reports whether an item whose parent is got belongs to want,
// a nil want matching any parent.
func belongs(want, got uuid.UUID) bool {
	return want == uuid.Nil || want == got
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
	if err := s.checkParents(ctx, q.LibraryID, q.SeriesID, q.SeasonID); err != nil {
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
