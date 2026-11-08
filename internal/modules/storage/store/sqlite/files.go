package sqlite

import (
	"context"
	"fmt"
	"path"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/storage/store"
)

const fileColumns = `id, location_id, path, size, mod_time, inode`

func scanFile(row rowScanner) (store.File, error) {
	var (
		f              store.File
		modTime, inode int64
	)
	if err := row.Scan(&f.ID, &f.LocationID, &f.Path, &f.Size, &modTime, &inode); err != nil {
		return store.File{}, err
	}
	f.ModTime = fromNanos(modTime)
	f.Inode = uint64(inode)
	return f, nil
}

func (s *Store) GetFile(ctx context.Context, id uuid.UUID) (store.File, error) {
	row := s.conn(ctx).QueryRowContext(ctx, `SELECT `+fileColumns+` FROM files WHERE id = ?`, id)
	f, err := scanFile(row)
	if err != nil {
		return store.File{}, notFound(err, "file "+id.String())
	}
	return f, nil
}

func (s *Store) ListFiles(ctx context.Context, location uuid.UUID) ([]store.File, error) {
	return s.queryFiles(ctx,
		`SELECT `+fileColumns+` FROM files WHERE location_id = ? ORDER BY path`, location)
}

func (s *Store) Siblings(ctx context.Context, f store.File) ([]store.Sibling, error) {
	files, err := s.queryFiles(ctx,
		`SELECT `+fileColumns+` FROM files WHERE location_id = ? AND dir = ? AND id <> ? ORDER BY path`,
		f.LocationID, path.Dir(f.Path), f.ID,
	)
	if err != nil {
		return nil, err
	}
	siblings := make([]store.Sibling, len(files))
	for i, sib := range files {
		siblings[i] = store.Sibling{FileID: sib.ID, Path: sib.Path}
	}
	return siblings, nil
}

func (s *Store) queryFiles(ctx context.Context, query string, args ...any) ([]store.File, error) {
	rows, err := s.conn(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: query files: %w", err)
	}
	defer rows.Close()

	var files []store.File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, fmt.Errorf("store: query files: %w", err)
		}
		files = append(files, f)
	}
	return files, rows.Err()
}
