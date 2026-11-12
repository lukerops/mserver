package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// GetMovie returns ErrNotFound if the movie does not exist.
func (s *Store) GetMovie(ctx context.Context, id uuid.UUID) (*Movie, error) {
	movies, err := s.queryMovies(ctx, selectMovie, id)
	if err != nil {
		return nil, err
	}
	if len(movies) == 0 {
		return nil, fmt.Errorf("%w: movie %s", ErrNotFound, id)
	}
	return &movies[0], nil
}

// SaveMovie inserts or replaces a movie.
func (s *Store) SaveMovie(ctx context.Context, m *Movie) error {
	info, audio, subtitle, err := encodeMediaInfo(m.MediaInfo)
	if err != nil {
		return err
	}
	subs, err := encodeSubtitles(m.ExternalSubtitles)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, upsertMovie,
		m.ID, m.LibraryID, m.FileID, m.LocationID, m.Path, m.Title, m.SortTitle, m.Year,
		info, audio, subtitle, subs, nanos(m.CreatedAt), nanos(m.UpdatedAt))
	if err != nil {
		return fmt.Errorf("store: save movie %s: %w", m.ID, err)
	}
	return nil
}

// UpdateMovieMediaInfo replaces the media info of a movie. It returns
// ErrNotFound if the movie does not exist.
func (s *Store) UpdateMovieMediaInfo(ctx context.Context, id uuid.UUID, info *MediaInfo, updatedAt time.Time) error {
	data, audio, subtitle, err := encodeMediaInfo(info)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, updateMovieMediaInfo, data, audio, subtitle, nanos(updatedAt), id)
	if err != nil {
		return fmt.Errorf("store: update media info of movie %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: movie %s", ErrNotFound, id)
	}
	return nil
}

// UpdateMovieSubtitles replaces the external subtitles of a movie. It
// returns ErrNotFound if the movie does not exist.
func (s *Store) UpdateMovieSubtitles(ctx context.Context, id uuid.UUID, subs []ExternalSubtitle, updatedAt time.Time) error {
	data, err := encodeSubtitles(subs)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, updateMovieSubtitles, data, nanos(updatedAt), id)
	if err != nil {
		return fmt.Errorf("store: update subtitles of movie %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: movie %s", ErrNotFound, id)
	}
	return nil
}

// DeleteMovie removes a movie; removing a missing movie is not an error.
func (s *Store) DeleteMovie(ctx context.Context, id uuid.UUID) error {
	if _, err := s.db.ExecContext(ctx, deleteMovie, id); err != nil {
		return fmt.Errorf("store: delete movie %s: %w", id, err)
	}
	return nil
}

// ListMovies returns a page of the movies selected by q and the total
// number of movies selected (ignoring the offset and limit).
func (s *Store) ListMovies(ctx context.Context, q *MovieQuery) ([]Movie, int, error) {
	var (
		conds []string
		args  []any
	)
	if q.LibraryID != uuid.Nil {
		conds = append(conds, "library_id = "+arg(&args, q.LibraryID))
	}
	if len(q.IDs) > 0 {
		conds = append(conds, "id IN "+inClause(&args, q.IDs))
	}
	if len(q.Years) > 0 {
		conds = append(conds, "year IN "+inClause(&args, q.Years))
	}
	if len(q.AudioLanguages) > 0 {
		conds = append(conds, languageFilter(&args, "audio_languages", q.AudioLanguages))
	}
	if len(q.SubtitleLanguages) > 0 {
		conds = append(conds, languageFilter(&args, "subtitle_languages", q.SubtitleLanguages))
	}
	filter := where(conds)

	var total int
	if err := s.db.QueryRowContext(ctx, countMovies+filter, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("store: count movies: %w", err)
	}

	dir := "ASC"
	if q.Descending {
		dir = "DESC"
	}
	var order string
	switch q.SortBy {
	case SortByAdded:
		order = "created_at " + dir + ", sort_title, id"
	case SortByYear:
		order = "year " + dir + ", sort_title, id"
	default:
		order = "sort_title " + dir + ", id " + dir
	}
	query := paginate(selectMovies+filter+" ORDER BY "+order, &args, q.Limit, q.Offset)
	movies, err := s.queryMovies(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	return movies, total, nil
}

func (s *Store) queryMovies(ctx context.Context, query string, args ...any) ([]Movie, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: query movies: %w", err)
	}
	defer rows.Close()

	var movies []Movie
	for rows.Next() {
		var (
			m                    Movie
			info, subs           sql.NullString
			createdAt, updatedAt int64
		)
		if err := rows.Scan(&m.ID, &m.LibraryID, &m.FileID, &m.LocationID, &m.Path, &m.Title,
			&m.SortTitle, &m.Year, &info, &subs, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("store: query movies: %w", err)
		}
		if m.MediaInfo, err = decodeMediaInfo(info); err != nil {
			return nil, fmt.Errorf("movie %s: %w", m.ID, err)
		}
		if m.ExternalSubtitles, err = decodeSubtitles(subs); err != nil {
			return nil, fmt.Errorf("movie %s: %w", m.ID, err)
		}
		m.CreatedAt = fromNanos(createdAt)
		m.UpdatedAt = fromNanos(updatedAt)
		movies = append(movies, m)
	}
	return movies, rows.Err()
}
