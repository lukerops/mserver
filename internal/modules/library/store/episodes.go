package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// GetEpisode returns ErrNotFound if the episode does not exist.
func (s *Store) GetEpisode(ctx context.Context, id uuid.UUID) (*Episode, error) {
	episodes, err := s.queryEpisodes(ctx, s.db, selectEpisodeByID, id)
	if err != nil {
		return nil, err
	}
	if len(episodes) == 0 {
		return nil, fmt.Errorf("%w: episode %s", ErrNotFound, id)
	}
	return &episodes[0], nil
}

// SaveEpisode inserts or replaces an episode in its series and season,
// atomically. The series is the one of the library with the key and year of
// series, and the season is the one of the series with the number of season;
// the candidates series and season (with new IDs) are inserted when they
// do not exist yet. When the episode moves to another season, the season
// and the series it leaves are removed if they are left empty.
//
// It fills the series and season fields of e (IDs, series title and year,
// season number) with the ones saved, and the library and series of the
// season candidate. It returns what was created and removed along with the
// episode.
func (s *Store) SaveEpisode(ctx context.Context, e *Episode, series *Series, season *Season) (*ShowChanges, error) {
	info, audio, subtitle, err := encodeMediaInfo(e.MediaInfo)
	if err != nil {
		return nil, err
	}
	subs, err := encodeSubtitles(e.ExternalSubtitles)
	if err != nil {
		return nil, err
	}

	changes := new(ShowChanges)
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		*changes = ShowChanges{}

		var oldSeries, oldSeason uuid.UUID
		err := tx.QueryRowContext(ctx, selectEpisodeParents, e.ID).Scan(&oldSeries, &oldSeason)
		hadEpisode := err == nil
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("store: find episode %s: %w", e.ID, err)
		}

		var title string
		err = tx.QueryRowContext(ctx, findSeries, series.LibraryID, series.Key, series.Year).Scan(&e.SeriesID, &title)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if _, err := tx.ExecContext(ctx, insertSeries, series.ID, series.LibraryID, series.Title,
				series.SortTitle, series.Key, series.Year, nanos(series.CreatedAt)); err != nil {
				return fmt.Errorf("store: insert series %s: %w", series.ID, err)
			}
			e.SeriesID, title = series.ID, series.Title
			changes.AddedSeries = series
		case err != nil:
			return fmt.Errorf("store: find series: %w", err)
		}
		e.SeriesTitle, e.SeriesYear = title, series.Year

		season.SeriesID, season.LibraryID = e.SeriesID, series.LibraryID
		err = tx.QueryRowContext(ctx, findSeason, e.SeriesID, season.Number).Scan(&e.SeasonID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if _, err := tx.ExecContext(ctx, insertSeason, season.ID, season.SeriesID, season.LibraryID,
				season.Number, nanos(season.CreatedAt)); err != nil {
				return fmt.Errorf("store: insert season %s: %w", season.ID, err)
			}
			e.SeasonID = season.ID
			changes.AddedSeason = season
		case err != nil:
			return fmt.Errorf("store: find season: %w", err)
		}
		e.SeasonNumber = season.Number

		if _, err := tx.ExecContext(ctx, upsertEpisode,
			e.ID, e.LibraryID, e.SeriesID, e.SeasonID, e.FileID, e.LocationID, e.Path,
			e.Title, e.SortKey, nullInt(e.Number), nullInt(e.EndNumber), info, audio, subtitle, subs,
			nanos(e.CreatedAt), nanos(e.UpdatedAt)); err != nil {
			return fmt.Errorf("store: save episode %s: %w", e.ID, err)
		}

		if hadEpisode && oldSeason != e.SeasonID {
			return prune(ctx, tx, oldSeason, oldSeries, changes)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return changes, nil
}

// UpdateEpisodeMediaInfo replaces the media info of an episode. It returns
// ErrNotFound if the episode does not exist.
func (s *Store) UpdateEpisodeMediaInfo(ctx context.Context, id uuid.UUID, info *MediaInfo, updatedAt time.Time) error {
	data, audio, subtitle, err := encodeMediaInfo(info)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, updateEpisodeMediaInfo, data, audio, subtitle, nanos(updatedAt), id)
	if err != nil {
		return fmt.Errorf("store: update media info of episode %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: episode %s", ErrNotFound, id)
	}
	return nil
}

// UpdateEpisodeSubtitles replaces the external subtitles of an episode. It
// returns ErrNotFound if the episode does not exist.
func (s *Store) UpdateEpisodeSubtitles(ctx context.Context, id uuid.UUID, subs []ExternalSubtitle, updatedAt time.Time) error {
	data, err := encodeSubtitles(subs)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, updateEpisodeSubtitles, data, nanos(updatedAt), id)
	if err != nil {
		return fmt.Errorf("store: update subtitles of episode %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: episode %s", ErrNotFound, id)
	}
	return nil
}

// DeleteEpisode removes an episode, atomically with its season and series
// if they are left empty. Removing a missing episode is not an error: it
// changes nothing.
func (s *Store) DeleteEpisode(ctx context.Context, id uuid.UUID) (*ShowChanges, error) {
	changes := new(ShowChanges)
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		*changes = ShowChanges{}

		var series, season uuid.UUID
		err := tx.QueryRowContext(ctx, selectEpisodeParents, id).Scan(&series, &season)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("store: find episode %s: %w", id, err)
		}
		if _, err := tx.ExecContext(ctx, deleteEpisode, id); err != nil {
			return fmt.Errorf("store: delete episode %s: %w", id, err)
		}
		return prune(ctx, tx, season, series, changes)
	})
	if err != nil {
		return nil, err
	}
	return changes, nil
}

// prune removes a season and a series left without episodes, recording
// them in changes.
func prune(ctx context.Context, tx *sql.Tx, season, series uuid.UUID, changes *ShowChanges) error {
	var n int
	if err := tx.QueryRowContext(ctx, countSeasonEps, season).Scan(&n); err != nil {
		return fmt.Errorf("store: count episodes of season %s: %w", season, err)
	}
	if n == 0 {
		sn := Season{ID: season}
		err := tx.QueryRowContext(ctx, selectSeasonInfo, season).Scan(&sn.SeriesID, &sn.LibraryID, &sn.Number)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return fmt.Errorf("store: find season %s: %w", season, err)
		default:
			if _, err := tx.ExecContext(ctx, deleteSeason, season); err != nil {
				return fmt.Errorf("store: delete season %s: %w", season, err)
			}
			changes.RemovedSeason = &sn
		}
	}

	if err := tx.QueryRowContext(ctx, countSeriesEps, series).Scan(&n); err != nil {
		return fmt.Errorf("store: count episodes of series %s: %w", series, err)
	}
	if n == 0 {
		sr := Series{ID: series}
		err := tx.QueryRowContext(ctx, selectSeriesInfo, series).Scan(&sr.LibraryID, &sr.Title, &sr.Year)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return fmt.Errorf("store: find series %s: %w", series, err)
		default:
			// Its seasons are empty, and were removed with their last episode.
			if _, err := tx.ExecContext(ctx, deleteSeries, series); err != nil {
				return fmt.Errorf("store: delete series %s: %w", series, err)
			}
			changes.RemovedSeries = &sr
		}
	}
	return nil
}

// ListEpisodes returns a page of the episodes selected by q and the total
// number of episodes selected (ignoring the offset and limit).
func (s *Store) ListEpisodes(ctx context.Context, q *EpisodeQuery) ([]Episode, int, error) {
	var (
		conds []string
		args  []any
	)
	if q.LibraryID != uuid.Nil {
		conds = append(conds, "ep.library_id = "+arg(&args, q.LibraryID))
	}
	if q.SeriesID != uuid.Nil {
		conds = append(conds, "ep.series_id = "+arg(&args, q.SeriesID))
	}
	if q.SeasonID != uuid.Nil {
		conds = append(conds, "ep.season_id = "+arg(&args, q.SeasonID))
	}
	if len(q.IDs) > 0 {
		conds = append(conds, "ep.id IN "+inClause(&args, q.IDs))
	}
	if len(q.AudioLanguages) > 0 {
		conds = append(conds, languageFilter(&args, "ep.audio_languages", q.AudioLanguages))
	}
	if len(q.SubtitleLanguages) > 0 {
		conds = append(conds, languageFilter(&args, "ep.subtitle_languages", q.SubtitleLanguages))
	}
	filter := where(conds)

	dir := "ASC"
	if q.Descending {
		dir = "DESC"
	}
	// Episodes without number go after the numbered ones of their season.
	byNumber := func(dir string) string {
		return "sr.sort_title " + dir + ", sr.id " + dir + ", sn.number " + dir +
			", CASE WHEN ep.number IS NULL THEN 1 ELSE 0 END " + dir +
			", ep.number " + dir + ", ep.sort_key " + dir + ", ep.id " + dir
	}
	order := byNumber(dir)
	if q.SortBy == SortByAdded {
		order = "ep.created_at " + dir + ", " + byNumber("ASC")
	}

	// The count and the page run in the same transaction: an episode saved
	// between the two queries would make the total disagree with the page.
	var (
		episodes []Episode
		total    int
	)
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, countEpisodes+filter, args...).Scan(&total); err != nil {
			return fmt.Errorf("store: count episodes: %w", err)
		}
		query := paginate(selectEpisodeBase+filter+" ORDER BY "+order, &args, q.Limit, q.Offset)
		var err error
		episodes, err = s.queryEpisodes(ctx, tx, query, args...)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return episodes, total, nil
}

func (s *Store) queryEpisodes(ctx context.Context, q queryer, query string, args ...any) ([]Episode, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: query episodes: %w", err)
	}
	defer rows.Close()

	var episodes []Episode
	for rows.Next() {
		var (
			e                    Episode
			number, endNumber    sql.NullInt64
			info, subs           sql.NullString
			createdAt, updatedAt int64
		)
		if err := rows.Scan(&e.ID, &e.LibraryID, &e.SeriesID, &e.SeasonID, &e.FileID, &e.LocationID, &e.Path,
			&e.Title, &e.SortKey, &number, &endNumber, &info, &subs, &createdAt, &updatedAt,
			&e.SeriesTitle, &e.SeriesYear, &e.SeasonNumber); err != nil {
			return nil, fmt.Errorf("store: query episodes: %w", err)
		}
		e.Number, e.EndNumber = intPtr(number), intPtr(endNumber)
		if e.MediaInfo, err = decodeMediaInfo(info); err != nil {
			return nil, fmt.Errorf("episode %s: %w", e.ID, err)
		}
		if e.ExternalSubtitles, err = decodeSubtitles(subs); err != nil {
			return nil, fmt.Errorf("episode %s: %w", e.ID, err)
		}
		e.CreatedAt = fromNanos(createdAt)
		e.UpdatedAt = fromNanos(updatedAt)
		episodes = append(episodes, e)
	}
	return episodes, rows.Err()
}

func nullInt(n *int) sql.NullInt64 {
	if n == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*n), Valid: true}
}

func intPtr(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}
