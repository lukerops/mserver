package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/library/store"
)

const movieColumns = `id, library_id, file_id, location_id, path, title, sort_title, year, media_info, created_at, updated_at`

func scanMovie(row rowScanner) (store.Movie, error) {
	var (
		m                    store.Movie
		mediaInfo            []byte
		createdAt, updatedAt int64
	)
	if err := row.Scan(&m.ID, &m.LibraryID, &m.FileID, &m.LocationID, &m.Path, &m.Title,
		&m.SortTitle, &m.Year, &mediaInfo, &createdAt, &updatedAt); err != nil {
		return store.Movie{}, err
	}
	if mediaInfo != nil {
		m.MediaInfo = new(store.MediaInfo)
		if err := json.Unmarshal(mediaInfo, m.MediaInfo); err != nil {
			return store.Movie{}, fmt.Errorf("media info of movie %s: %w", m.ID, err)
		}
	}
	m.CreatedAt = fromNanos(createdAt)
	m.UpdatedAt = fromNanos(updatedAt)
	return m, nil
}

func (s *Store) GetMovie(ctx context.Context, id uuid.UUID) (store.Movie, error) {
	row := s.conn(ctx).QueryRowContext(ctx, `SELECT `+movieColumns+` FROM movies WHERE id = ?`, id)
	m, err := scanMovie(row)
	if err != nil {
		return store.Movie{}, notFound(err, "movie "+id.String())
	}
	return m, nil
}

func (s *Store) SaveMovie(ctx context.Context, m store.Movie) error {
	var mediaInfo any // NULL until the file is analyzed
	if m.MediaInfo != nil {
		b, err := json.Marshal(m.MediaInfo)
		if err != nil {
			return fmt.Errorf("store: media info of movie %s: %w", m.ID, err)
		}
		mediaInfo = b
	}
	_, err := s.conn(ctx).ExecContext(ctx, `
		INSERT INTO movies (`+movieColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			library_id = excluded.library_id,
			file_id = excluded.file_id,
			location_id = excluded.location_id,
			path = excluded.path,
			title = excluded.title,
			sort_title = excluded.sort_title,
			year = excluded.year,
			media_info = excluded.media_info,
			updated_at = excluded.updated_at`,
		m.ID, m.LibraryID, m.FileID, m.LocationID, m.Path, m.Title, m.SortTitle, m.Year,
		mediaInfo, nanos(m.CreatedAt), nanos(m.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("store: save movie %s: %w", m.ID, err)
	}
	return nil
}

func (s *Store) DeleteMovie(ctx context.Context, id uuid.UUID) error {
	if _, err := s.conn(ctx).ExecContext(ctx, `DELETE FROM movies WHERE id = ?`, id); err != nil {
		return fmt.Errorf("store: delete movie %s: %w", id, err)
	}
	return nil
}

func (s *Store) ListMovies(ctx context.Context, q store.MovieQuery) ([]store.Movie, int, error) {
	var (
		where []string
		args  []any
	)
	if q.LibraryID != uuid.Nil {
		where = append(where, "library_id = ?")
		args = append(args, q.LibraryID)
	}
	if len(q.IDs) > 0 {
		where = append(where, "id IN "+placeholders(len(q.IDs)))
		for _, id := range q.IDs {
			args = append(args, id)
		}
	}
	if len(q.Years) > 0 {
		where = append(where, "year IN "+placeholders(len(q.Years)))
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
		where = append(where, `EXISTS (SELECT 1 FROM json_each(media_info, '$.streams') AS s
			WHERE s.value ->> '$.type' = ? AND lower(s.value ->> '$.language') IN `+placeholders(len(f.languages))+`)`)
		args = append(args, f.typ)
		for _, l := range f.languages {
			args = append(args, strings.ToLower(l))
		}
	}
	filter := ""
	if len(where) > 0 {
		filter = " WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := s.conn(ctx).QueryRowContext(ctx, `SELECT COUNT(*) FROM movies`+filter, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("store: count movies: %w", err)
	}

	dir := "ASC"
	if q.Descending {
		dir = "DESC"
	}
	var order string
	switch q.SortBy {
	case store.SortByAdded:
		order = "created_at " + dir + ", sort_title, id"
	case store.SortByYear:
		order = "year " + dir + ", sort_title, id"
	default:
		order = "sort_title " + dir + ", id " + dir
	}
	limit := -1 // no limit
	if q.Limit > 0 {
		limit = q.Limit
	}
	rows, err := s.conn(ctx).QueryContext(ctx,
		`SELECT `+movieColumns+` FROM movies`+filter+` ORDER BY `+order+` LIMIT ? OFFSET ?`,
		append(args, limit, max(q.Offset, 0))...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("store: list movies: %w", err)
	}
	defer rows.Close()

	movies := []store.Movie{}
	for rows.Next() {
		m, err := scanMovie(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("store: list movies: %w", err)
		}
		movies = append(movies, m)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("store: list movies: %w", err)
	}
	return movies, total, nil
}

// placeholders returns "(?, ?, ...)" with n parameters; n must be positive.
func placeholders(n int) string {
	return "(?" + strings.Repeat(", ?", n-1) + ")"
}
