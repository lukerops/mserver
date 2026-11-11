package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// GetSeries returns ErrNotFound if the series does not exist.
func (s *Store) GetSeries(ctx context.Context, id uuid.UUID) (*Series, error) {
	series, err := s.querySeries(ctx, selectSeriesByID, id)
	if err != nil {
		return nil, err
	}
	if len(series) == 0 {
		return nil, fmt.Errorf("%w: series %s", ErrNotFound, id)
	}
	return &series[0], nil
}

// ListSeries returns a page of the series selected by q and the total
// number of series selected (ignoring the offset and limit).
func (s *Store) ListSeries(ctx context.Context, q *SeriesQuery) ([]Series, int, error) {
	var (
		conds []string
		args  []any
	)
	if q.LibraryID != uuid.Nil {
		conds = append(conds, "sr.library_id = "+arg(&args, q.LibraryID))
	}
	if len(q.IDs) > 0 {
		conds = append(conds, "sr.id IN "+inClause(&args, q.IDs))
	}
	if len(q.Years) > 0 {
		conds = append(conds, "sr.year IN "+inClause(&args, q.Years))
	}
	if len(q.AudioLanguages) > 0 {
		conds = append(conds, episodeLanguageFilter(&args, "ep.audio_languages", q.AudioLanguages))
	}
	if len(q.SubtitleLanguages) > 0 {
		conds = append(conds, episodeLanguageFilter(&args, "ep.subtitle_languages", q.SubtitleLanguages))
	}
	filter := where(conds)

	var total int
	if err := s.db.QueryRowContext(ctx, countSeries+filter, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("store: count series: %w", err)
	}

	dir := "ASC"
	if q.Descending {
		dir = "DESC"
	}
	var order string
	switch q.SortBy {
	case SortByAdded:
		order = "sr.created_at " + dir + ", sr.sort_title, sr.id"
	case SortByYear:
		order = "sr.year " + dir + ", sr.sort_title, sr.id"
	default:
		order = "sr.sort_title " + dir + ", sr.id " + dir
	}
	query := paginate(selectSeriesBase+filter+" ORDER BY "+order, &args, q.Limit, q.Offset)
	series, err := s.querySeries(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	return series, total, nil
}

// episodeLanguageFilter selects the series with an episode whose language
// column has one of the languages.
func episodeLanguageFilter(args *[]any, column string, languages []string) string {
	return `EXISTS (SELECT 1 FROM episodes AS ep WHERE ep.series_id = sr.id AND ` +
		languageFilter(args, column, languages) + `)`
}

func (s *Store) querySeries(ctx context.Context, query string, args ...any) ([]Series, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: query series: %w", err)
	}
	defer rows.Close()

	var series []Series
	for rows.Next() {
		var (
			sr        Series
			createdAt int64
		)
		if err := rows.Scan(&sr.ID, &sr.LibraryID, &sr.Title, &sr.SortTitle, &sr.Key, &sr.Year,
			&createdAt, &sr.SeasonCount, &sr.EpisodeCount); err != nil {
			return nil, fmt.Errorf("store: query series: %w", err)
		}
		sr.CreatedAt = fromNanos(createdAt)
		series = append(series, sr)
	}
	return series, rows.Err()
}

// GetSeason returns ErrNotFound if the season does not exist.
func (s *Store) GetSeason(ctx context.Context, id uuid.UUID) (*Season, error) {
	seasons, err := s.querySeasons(ctx, selectSeasonByID, id)
	if err != nil {
		return nil, err
	}
	if len(seasons) == 0 {
		return nil, fmt.Errorf("%w: season %s", ErrNotFound, id)
	}
	return &seasons[0], nil
}

// ListSeasons returns the seasons selected by q, ordered by series and
// number.
func (s *Store) ListSeasons(ctx context.Context, q *SeasonQuery) ([]Season, error) {
	var (
		conds []string
		args  []any
	)
	if q.LibraryID != uuid.Nil {
		conds = append(conds, "sn.library_id = "+arg(&args, q.LibraryID))
	}
	if q.SeriesID != uuid.Nil {
		conds = append(conds, "sn.series_id = "+arg(&args, q.SeriesID))
	}
	if len(q.IDs) > 0 {
		conds = append(conds, "sn.id IN "+inClause(&args, q.IDs))
	}
	return s.querySeasons(ctx, selectSeasonBase+where(conds)+orderSeasons, args...)
}

func (s *Store) querySeasons(ctx context.Context, query string, args ...any) ([]Season, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: query seasons: %w", err)
	}
	defer rows.Close()

	var seasons []Season
	for rows.Next() {
		var (
			sn        Season
			createdAt int64
		)
		if err := rows.Scan(&sn.ID, &sn.SeriesID, &sn.LibraryID, &sn.Number, &createdAt,
			&sn.SeriesTitle, &sn.SeriesYear, &sn.EpisodeCount); err != nil {
			return nil, fmt.Errorf("store: query seasons: %w", err)
		}
		sn.CreatedAt = fromNanos(createdAt)
		seasons = append(seasons, sn)
	}
	return seasons, rows.Err()
}
