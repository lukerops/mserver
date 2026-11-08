package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/library/store"
)

// The queries alias the tables: sr (series), sn (seasons) and ep
// (episodes).

const seriesColumns = `sr.id, sr.library_id, sr.title, sr.sort_title, sr.key, sr.year, sr.created_at,
	(SELECT COUNT(*) FROM seasons WHERE series_id = sr.id),
	(SELECT COUNT(*) FROM episodes WHERE series_id = sr.id)`

func scanSeries(row rowScanner) (store.Series, error) {
	var (
		s         store.Series
		createdAt int64
	)
	if err := row.Scan(&s.ID, &s.LibraryID, &s.Title, &s.SortTitle, &s.Key, &s.Year, &createdAt,
		&s.SeasonCount, &s.EpisodeCount); err != nil {
		return store.Series{}, err
	}
	s.CreatedAt = fromNanos(createdAt)
	return s, nil
}

func (s *Store) GetSeries(ctx context.Context, id uuid.UUID) (store.Series, error) {
	row := s.conn(ctx).QueryRowContext(ctx, `SELECT `+seriesColumns+` FROM series AS sr WHERE sr.id = ?`, id)
	sr, err := scanSeries(row)
	if err != nil {
		return store.Series{}, notFound(err, "series "+id.String())
	}
	return sr, nil
}

func (s *Store) FindSeries(ctx context.Context, library uuid.UUID, key string, year int) (store.Series, error) {
	row := s.conn(ctx).QueryRowContext(ctx,
		`SELECT `+seriesColumns+` FROM series AS sr WHERE sr.library_id = ? AND sr.key = ? AND sr.year = ?`,
		library, key, year)
	sr, err := scanSeries(row)
	if err != nil {
		return store.Series{}, notFound(err, fmt.Sprintf("series %q (%d)", key, year))
	}
	return sr, nil
}

func (s *Store) InsertSeries(ctx context.Context, sr store.Series) error {
	_, err := s.conn(ctx).ExecContext(ctx, `
		INSERT INTO series (id, library_id, title, sort_title, key, year, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sr.ID, sr.LibraryID, sr.Title, sr.SortTitle, sr.Key, sr.Year, nanos(sr.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("store: insert series %s: %w", sr.ID, err)
	}
	return nil
}

func (s *Store) DeleteSeries(ctx context.Context, id uuid.UUID) error {
	// The seasons and episodes of the series are deleted in cascade.
	if _, err := s.conn(ctx).ExecContext(ctx, `DELETE FROM series WHERE id = ?`, id); err != nil {
		return fmt.Errorf("store: delete series %s: %w", id, err)
	}
	return nil
}

func (s *Store) ListSeries(ctx context.Context, q store.SeriesQuery) ([]store.Series, int, error) {
	var (
		where []string
		args  []any
	)
	if q.LibraryID != uuid.Nil {
		where = append(where, "sr.library_id = ?")
		args = append(args, q.LibraryID)
	}
	if len(q.IDs) > 0 {
		cond, condArgs := idsFilter("sr.id", q.IDs)
		where = append(where, cond)
		args = append(args, condArgs...)
	}
	if len(q.Years) > 0 {
		where = append(where, "sr.year IN "+placeholders(len(q.Years)))
		for _, y := range q.Years {
			args = append(args, y)
		}
	}
	for _, f := range []struct {
		typ       string
		languages []string
	}{{"audio", q.AudioLanguages}, {"subtitle", q.SubtitleLanguages}} {
		if len(f.languages) == 0 {
			continue
		}
		cond, condArgs := streamLanguageFilter("ep.media_info", f.typ, f.languages)
		where = append(where, `EXISTS (SELECT 1 FROM episodes AS ep WHERE ep.series_id = sr.id AND `+cond+`)`)
		args = append(args, condArgs...)
	}
	filter := ""
	if len(where) > 0 {
		filter = " WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := s.conn(ctx).QueryRowContext(ctx, `SELECT COUNT(*) FROM series AS sr`+filter, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("store: count series: %w", err)
	}

	dir := "ASC"
	if q.Descending {
		dir = "DESC"
	}
	var order string
	switch q.SortBy {
	case store.SortByAdded:
		order = "sr.created_at " + dir + ", sr.sort_title, sr.id"
	case store.SortByYear:
		order = "sr.year " + dir + ", sr.sort_title, sr.id"
	default:
		order = "sr.sort_title " + dir + ", sr.id " + dir
	}
	rows, err := s.conn(ctx).QueryContext(ctx,
		`SELECT `+seriesColumns+` FROM series AS sr`+filter+` ORDER BY `+order+` LIMIT ? OFFSET ?`,
		append(args, limitOffset(q.Offset, q.Limit)...)...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("store: list series: %w", err)
	}
	defer rows.Close()

	series := []store.Series{}
	for rows.Next() {
		sr, err := scanSeries(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("store: list series: %w", err)
		}
		series = append(series, sr)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("store: list series: %w", err)
	}
	return series, total, nil
}

const seasonColumns = `sn.id, sn.series_id, sn.library_id, sn.number, sn.created_at, sr.title, sr.year,
	(SELECT COUNT(*) FROM episodes WHERE season_id = sn.id)`

const seasonTables = ` FROM seasons AS sn JOIN series AS sr ON sr.id = sn.series_id`

func scanSeason(row rowScanner) (store.Season, error) {
	var (
		s         store.Season
		createdAt int64
	)
	if err := row.Scan(&s.ID, &s.SeriesID, &s.LibraryID, &s.Number, &createdAt,
		&s.SeriesTitle, &s.SeriesYear, &s.EpisodeCount); err != nil {
		return store.Season{}, err
	}
	s.CreatedAt = fromNanos(createdAt)
	return s, nil
}

func (s *Store) GetSeason(ctx context.Context, id uuid.UUID) (store.Season, error) {
	row := s.conn(ctx).QueryRowContext(ctx, `SELECT `+seasonColumns+seasonTables+` WHERE sn.id = ?`, id)
	sn, err := scanSeason(row)
	if err != nil {
		return store.Season{}, notFound(err, "season "+id.String())
	}
	return sn, nil
}

func (s *Store) FindSeason(ctx context.Context, series uuid.UUID, number int) (store.Season, error) {
	row := s.conn(ctx).QueryRowContext(ctx,
		`SELECT `+seasonColumns+seasonTables+` WHERE sn.series_id = ? AND sn.number = ?`, series, number)
	sn, err := scanSeason(row)
	if err != nil {
		return store.Season{}, notFound(err, fmt.Sprintf("season %d of series %s", number, series))
	}
	return sn, nil
}

func (s *Store) InsertSeason(ctx context.Context, sn store.Season) error {
	_, err := s.conn(ctx).ExecContext(ctx, `
		INSERT INTO seasons (id, series_id, library_id, number, created_at) VALUES (?, ?, ?, ?, ?)`,
		sn.ID, sn.SeriesID, sn.LibraryID, sn.Number, nanos(sn.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("store: insert season %s: %w", sn.ID, err)
	}
	return nil
}

func (s *Store) DeleteSeason(ctx context.Context, id uuid.UUID) error {
	// The episodes of the season are deleted in cascade.
	if _, err := s.conn(ctx).ExecContext(ctx, `DELETE FROM seasons WHERE id = ?`, id); err != nil {
		return fmt.Errorf("store: delete season %s: %w", id, err)
	}
	return nil
}

func (s *Store) ListSeasons(ctx context.Context, q store.SeasonQuery) ([]store.Season, error) {
	var (
		where []string
		args  []any
	)
	if q.LibraryID != uuid.Nil {
		where = append(where, "sn.library_id = ?")
		args = append(args, q.LibraryID)
	}
	if q.SeriesID != uuid.Nil {
		where = append(where, "sn.series_id = ?")
		args = append(args, q.SeriesID)
	}
	if len(q.IDs) > 0 {
		cond, condArgs := idsFilter("sn.id", q.IDs)
		where = append(where, cond)
		args = append(args, condArgs...)
	}
	filter := ""
	if len(where) > 0 {
		filter = " WHERE " + strings.Join(where, " AND ")
	}
	rows, err := s.conn(ctx).QueryContext(ctx,
		`SELECT `+seasonColumns+seasonTables+filter+` ORDER BY sr.sort_title, sr.id, sn.number`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list seasons: %w", err)
	}
	defer rows.Close()

	seasons := []store.Season{}
	for rows.Next() {
		sn, err := scanSeason(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list seasons: %w", err)
		}
		seasons = append(seasons, sn)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list seasons: %w", err)
	}
	return seasons, nil
}

const episodeColumns = `ep.id, ep.library_id, ep.series_id, ep.season_id, ep.file_id, ep.location_id, ep.path,
	ep.title, ep.sort_key, ep.number, ep.end_number, ep.media_info, ep.created_at, ep.updated_at,
	sr.title, sr.year, sn.number`

const episodeTables = ` FROM episodes AS ep
	JOIN series AS sr ON sr.id = ep.series_id
	JOIN seasons AS sn ON sn.id = ep.season_id`

func scanEpisode(row rowScanner) (store.Episode, error) {
	var (
		e                    store.Episode
		number, endNumber    sql.NullInt64
		mediaInfo            []byte
		createdAt, updatedAt int64
	)
	if err := row.Scan(&e.ID, &e.LibraryID, &e.SeriesID, &e.SeasonID, &e.FileID, &e.LocationID, &e.Path,
		&e.Title, &e.SortKey, &number, &endNumber, &mediaInfo, &createdAt, &updatedAt,
		&e.SeriesTitle, &e.SeriesYear, &e.SeasonNumber); err != nil {
		return store.Episode{}, err
	}
	e.Number = fromNullInt(number)
	e.EndNumber = fromNullInt(endNumber)
	info, err := unmarshalMediaInfo(mediaInfo, "episode "+e.ID.String())
	if err != nil {
		return store.Episode{}, err
	}
	e.MediaInfo = info
	e.CreatedAt = fromNanos(createdAt)
	e.UpdatedAt = fromNanos(updatedAt)
	return e, nil
}

func (s *Store) GetEpisode(ctx context.Context, id uuid.UUID) (store.Episode, error) {
	row := s.conn(ctx).QueryRowContext(ctx, `SELECT `+episodeColumns+episodeTables+` WHERE ep.id = ?`, id)
	e, err := scanEpisode(row)
	if err != nil {
		return store.Episode{}, notFound(err, "episode "+id.String())
	}
	return e, nil
}

func (s *Store) SaveEpisode(ctx context.Context, e store.Episode) error {
	mediaInfo, err := marshalMediaInfo(e.MediaInfo, "episode "+e.ID.String())
	if err != nil {
		return err
	}
	_, err = s.conn(ctx).ExecContext(ctx, `
		INSERT INTO episodes (id, library_id, series_id, season_id, file_id, location_id, path,
			title, sort_key, number, end_number, media_info, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			library_id = excluded.library_id,
			series_id = excluded.series_id,
			season_id = excluded.season_id,
			file_id = excluded.file_id,
			location_id = excluded.location_id,
			path = excluded.path,
			title = excluded.title,
			sort_key = excluded.sort_key,
			number = excluded.number,
			end_number = excluded.end_number,
			media_info = excluded.media_info,
			updated_at = excluded.updated_at`,
		e.ID, e.LibraryID, e.SeriesID, e.SeasonID, e.FileID, e.LocationID, e.Path,
		e.Title, e.SortKey, toNullInt(e.Number), toNullInt(e.EndNumber), mediaInfo,
		nanos(e.CreatedAt), nanos(e.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("store: save episode %s: %w", e.ID, err)
	}
	return nil
}

func (s *Store) DeleteEpisode(ctx context.Context, id uuid.UUID) error {
	if _, err := s.conn(ctx).ExecContext(ctx, `DELETE FROM episodes WHERE id = ?`, id); err != nil {
		return fmt.Errorf("store: delete episode %s: %w", id, err)
	}
	return nil
}

func (s *Store) ListEpisodes(ctx context.Context, q store.EpisodeQuery) ([]store.Episode, int, error) {
	var (
		where []string
		args  []any
	)
	for _, f := range []struct {
		column string
		id     uuid.UUID
	}{{"ep.library_id", q.LibraryID}, {"ep.series_id", q.SeriesID}, {"ep.season_id", q.SeasonID}} {
		if f.id != uuid.Nil {
			where = append(where, f.column+" = ?")
			args = append(args, f.id)
		}
	}
	if len(q.IDs) > 0 {
		cond, condArgs := idsFilter("ep.id", q.IDs)
		where = append(where, cond)
		args = append(args, condArgs...)
	}
	for _, f := range []struct {
		typ       string
		languages []string
	}{{"audio", q.AudioLanguages}, {"subtitle", q.SubtitleLanguages}} {
		if len(f.languages) == 0 {
			continue
		}
		cond, condArgs := streamLanguageFilter("ep.media_info", f.typ, f.languages)
		where = append(where, cond)
		args = append(args, condArgs...)
	}
	filter := ""
	if len(where) > 0 {
		filter = " WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := s.conn(ctx).QueryRowContext(ctx, `SELECT COUNT(*) FROM episodes AS ep`+filter, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("store: count episodes: %w", err)
	}

	dir := "ASC"
	if q.Descending {
		dir = "DESC"
	}
	// Episodes without number go after the numbered ones of their season.
	byNumber := func(dir string) string {
		return "sr.sort_title " + dir + ", sr.id " + dir + ", sn.number " + dir +
			", ep.number IS NULL " + dir + ", ep.number " + dir + ", ep.sort_key " + dir + ", ep.id " + dir
	}
	order := byNumber(dir)
	if q.SortBy == store.SortByAdded {
		order = "ep.created_at " + dir + ", " + byNumber("ASC")
	}
	rows, err := s.conn(ctx).QueryContext(ctx,
		`SELECT `+episodeColumns+episodeTables+filter+` ORDER BY `+order+` LIMIT ? OFFSET ?`,
		append(args, limitOffset(q.Offset, q.Limit)...)...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("store: list episodes: %w", err)
	}
	defer rows.Close()

	episodes := []store.Episode{}
	for rows.Next() {
		e, err := scanEpisode(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("store: list episodes: %w", err)
		}
		episodes = append(episodes, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("store: list episodes: %w", err)
	}
	return episodes, total, nil
}

func toNullInt(n *int) sql.NullInt64 {
	if n == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*n), Valid: true}
}

func fromNullInt(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}
